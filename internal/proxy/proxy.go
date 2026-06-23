// Package proxy is the client-facing daemon. It opens one listener per declared
// instance; a connection on an instance's listener lazily boots that instance
// and splices the bytes to its backend. The generic path is protocol-agnostic
// (accept -> boot -> count -> splice); engines whose wire protocol needs a
// preamble, TLS termination, or out-of-band control routing implement
// engine.ProxyFilter, which the proxy dispatches via type assertion.
package proxy

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nerdmenot/doze/internal/engine"
)

// Router is the subset of the runtime the proxy needs.
type Router interface {
	Boot(ctx context.Context, name string) (engine.Endpoint, error)
	Acquire(name string)
	Release(name string)
}

// Proxy accepts client connections per instance and splices them to backends.
type Proxy struct {
	router  Router
	logf    func(format string, args ...any)
	cancels *cancelRegistry

	// TLS, when set, enables client-facing TLS termination for engines that
	// support it (via engine.ProxyFilter). The proxy↔backend hop stays plaintext.
	TLS *tls.Config
	// RequireTLS rejects plaintext TCP clients when true.
	RequireTLS bool
	// BootTimeout bounds how long a client waits for a cold boot.
	BootTimeout time.Duration
}

// New constructs a proxy over the given router.
func New(router Router) *Proxy {
	return &Proxy{
		router:      router,
		logf:        func(string, ...any) {},
		cancels:     newCancelRegistry(),
		BootTimeout: 35 * time.Second,
	}
}

// SetLogger installs a logging callback.
func (p *Proxy) SetLogger(f func(format string, args ...any)) { p.logf = f }

// Listen opens a listener. addr is "host:port" (TCP) or "unix:/path/to.sock".
func Listen(addr string) (net.Listener, error) {
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		_ = removeSocket(path)
		return net.Listen("unix", path)
	}
	return net.Listen("tcp", addr)
}

// ServeInstance accepts connections on ln, routing each to the named instance
// (booting it on demand) until ctx is cancelled or the listener closes.
func (p *Proxy) ServeInstance(ctx context.Context, ln net.Listener, name string, drv engine.Driver) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.handle(ctx, conn, name, drv)
		}()
	}
}

// handle processes one client connection end to end.
func (p *Proxy) handle(ctx context.Context, raw net.Conn, name string, drv engine.Driver) {
	defer raw.Close()

	client := raw
	var replay []byte
	localUnix := raw.LocalAddr().Network() == "unix"

	// Optional protocol-aware preamble (PG startup parse, TLS, cancel routing).
	if pf, ok := drv.(engine.ProxyFilter); ok {
		res, err := pf.Preamble(ctx, client, p.cancels, engine.ProxyOpts{
			TLS: p.TLS, RequireTLS: p.RequireTLS, LocalUnix: localUnix,
		})
		if err != nil {
			p.logf("preamble for %q failed: %v", name, err)
			return
		}
		if res.Handled {
			return // out-of-band (e.g. cancel) or rejected
		}
		client = res.Client
		replay = res.Replay
	}

	budget := p.BootTimeout
	if sb, ok := drv.(engine.SlowBooter); ok && sb.BootBudget() > budget {
		budget = sb.BootBudget() // e.g. documentdb's first boot builds a PG cluster + extension
	}
	bootCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	ep, err := p.router.Boot(bootCtx, name)
	if err != nil {
		p.logf("boot %q failed: %v", name, err)
		writeError(drv, client, "57P03", "doze: "+err.Error())
		return
	}

	upstream, err := net.Dial("unix", ep.Backend)
	if err != nil {
		p.logf("dial backend %q (%s) failed: %v", name, ep.Backend, err)
		writeError(drv, client, "08006", "doze: backend unreachable: "+err.Error())
		return
	}
	defer upstream.Close()

	if len(replay) > 0 {
		if _, err := upstream.Write(replay); err != nil {
			p.logf("replaying preamble to %q failed: %v", name, err)
			return
		}
	}

	upstreamR := bufio.NewReader(upstream)
	if pf, ok := drv.(engine.ProxyFilter); ok {
		ready, cleanup, herr := pf.Handshake(client, upstreamR, ep.Backend, p.cancels)
		if cleanup != nil {
			defer cleanup()
		}
		if herr != nil || !ready {
			return
		}
	}

	p.router.Acquire(name)
	defer p.router.Release(name)
	splice(client, upstream, upstreamR)
}

func writeError(drv engine.Driver, w io.Writer, code, message string) {
	if ew, ok := drv.(engine.ErrorWriter); ok {
		ew.WriteError(w, code, message)
	}
}

// splice copies bytes in both directions until either side closes, then tears
// down both connections. The backend→client direction reads from backendR so
// any handshake read-ahead is preserved.
func splice(client, backend net.Conn, backendR io.Reader) {
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = client.Close()
			_ = backend.Close()
		})
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(backend, client)
		closeBoth()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, backendR)
		closeBoth()
	}()
	wg.Wait()
}

func removeSocket(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// --- cancelRegistry: the generic engine.CancelRegistry implementation ---

type cancelRegistry struct {
	mu sync.Mutex
	m  map[string]engine.CancelTarget
}

func newCancelRegistry() *cancelRegistry {
	return &cancelRegistry{m: map[string]engine.CancelTarget{}}
}

// Register stores t under a freshly generated, unique 8-byte synthetic key with
// a nonzero first word (so it never looks like an unset key to clients).
func (r *cancelRegistry) Register(t engine.CancelTarget) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	for {
		key := make([]byte, 8)
		_, _ = rand.Read(key)
		if key[0] == 0 && key[1] == 0 && key[2] == 0 && key[3] == 0 {
			continue
		}
		if _, exists := r.m[string(key)]; !exists {
			r.m[string(key)] = t
			return key
		}
	}
}

func (r *cancelRegistry) Unregister(key []byte) {
	r.mu.Lock()
	delete(r.m, string(key))
	r.mu.Unlock()
}

func (r *cancelRegistry) Lookup(key []byte) (engine.CancelTarget, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.m[string(key)]
	return t, ok
}
