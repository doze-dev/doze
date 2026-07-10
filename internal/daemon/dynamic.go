package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/endpoints"
	"github.com/doze-dev/doze/internal/proxy"
)

// This file implements live Add/Remove of instances on a running daemon: it
// decodes a single HCL block through the same plugin pipeline a file uses, wires
// a proxy listener + DNS name for a proxied engine (or just the config entry for
// a supervised process), and tears all of that down again on Remove. It backs
// the control "add"/"remove" ops, which back the embeddable facade's
// Session.AddProcess/AddModule/Remove.

// looksLikeUnresolvedRef reports whether a decode error is a reference to
// another service that couldn't resolve because the block was decoded alone.
func looksLikeUnresolvedRef(err error) bool {
	msg := err.Error()
	for _, s := range []string{"Unknown variable", "Variables not allowed", "no such variable", "There is no variable named"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// startProxyInstance opens a proxy listener for a proxied instance and serves it
// on its own cancelable context so Remove can stop just this one. The caller
// holds d.mu (or runs during single-threaded Run).
func (d *Daemon) startProxyInstance(name, engineType, domain, bindAddr string, drv engine.Driver, ip net.IP) error {
	ln, err := proxy.Listen(bindAddr)
	if err != nil {
		return fmt.Errorf("listening for %q on %s: %w", name, bindAddr, err)
	}
	shown := bindAddr
	if domain != "" {
		shown = domain + " (" + bindAddr + ")"
	}
	d.logf("%s/%s listening on %s", engineType, name, shown)

	instCtx, cancel := context.WithCancel(d.rootCtx)
	d.running[name] = &instHandle{cancel: cancel, ln: ln, ip: ip}
	d.instWG.Add(1)
	go func() {
		defer d.instWG.Done()
		_ = d.px.ServeInstance(instCtx, ln, name, drv)
	}()
	return nil
}

// AddInstance decodes a single instance block (rendered by the facade's Stack
// builder) through the normal config pipeline, registers it, wires its listener
// + DNS name (proxied engines) or boots it (supervised processes), and returns
// the new instance's name. Live add is limited to self-contained blocks —
// references to sibling services aren't resolved here (they need the full
// document); AWS-builtin/ingress engines are not yet supported live.
func (d *Daemon) AddInstance(ctx context.Context, block string) (string, error) {
	// Decode the block in isolation via the same pipeline (incl. plugin decode).
	// Because it's decoded alone, references to sibling services can't resolve —
	// surface that clearly (with the workaround) rather than a raw HCL error.
	mini, err := config.Parse([]byte(block), d.cfg.Path())
	if err != nil {
		if looksLikeUnresolvedRef(err) {
			return "", fmt.Errorf("a live-added service can't reference other services yet — pass the literal value instead (look it up with the client's Instance/Status): %w", err)
		}
		return "", fmt.Errorf("decoding block: %w", err)
	}
	if len(mini.Instances) != 1 {
		return "", fmt.Errorf("expected exactly one instance block, got %d", len(mini.Instances))
	}
	decl := mini.Instances[0]

	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return "", fmt.Errorf("no engine %q (is its module enabled?)", decl.Type)
	}
	if config.IsAWSBuiltin(decl.Type) {
		return "", fmt.Errorf("live add of AWS built-in engine %q is not yet supported — declare it in the config and restart", decl.Type)
	}

	supervised := false
	if lc, isLC := drv.(engine.Lifecycle); isLC {
		supervised = lc.Supervised(engine.Instance{Name: decl.Name, Type: decl.Type, Spec: decl.Spec})
	}

	// Wire the instance under the lock (config + listener + DNS), but boot a
	// supervised process AFTER releasing it — Boot can be slow (a command that
	// must compile, a health-gate wait) and the DNS resolver also takes d.mu.
	d.mu.Lock()
	if d.cfg.Lookup(decl.Name) != nil {
		d.mu.Unlock()
		return "", fmt.Errorf("an instance named %q already exists", decl.Name)
	}
	decl.Index = len(d.cfg.Instances)
	d.cfg.Add(decl)

	// Compute this instance's endpoint (address + domain). A portless background
	// process legitimately has none — that's fine; only proxied engines need one.
	ep, hasEndpoint := d.endpointFor(decl.Name)

	if !supervised {
		if !hasEndpoint {
			d.cfg.Remove(decl.Name)
			d.mu.Unlock()
			return "", fmt.Errorf("engine %q needs a client-facing port — set one on the block", decl.Type)
		}
		bindAddr, ip, err := d.bindFor(decl.Name, ep.Address)
		if err != nil {
			d.cfg.Remove(decl.Name)
			d.mu.Unlock()
			return "", err
		}
		if err := d.startProxyInstance(decl.Name, decl.Type, ep.Domain, bindAddr, drv, ip); err != nil {
			d.cfg.Remove(decl.Name)
			d.mu.Unlock()
			return "", err
		}
		d.binds[decl.Name] = bindAddr
	}
	if hasEndpoint && ep.Domain != "" {
		regIP := net.IPv4(127, 0, 0, 1) // a supervised process resolves to loopback
		if !supervised {
			if h := d.running[decl.Name]; h != nil && h.ip != nil {
				regIP = h.ip
			}
		}
		d.resolveMap[ep.Domain] = regIP
		d.republishDomains()
	}
	d.writeManifest()
	d.mu.Unlock()

	// A proxied engine lazy-boots on first connection; a supervised process must
	// be booted to actually run after Add.
	if supervised && decl.Enabled {
		if _, err := d.rt.Boot(ctx, decl.Name); err != nil {
			d.mu.Lock()
			d.rollbackAdd(decl.Name, ep.Domain)
			d.mu.Unlock()
			return "", fmt.Errorf("booting %q: %w", decl.Name, err)
		}
	}
	return decl.Name, nil
}

// RemoveInstance stops an instance's backend, tears down its listener + DNS
// name, drops it from the config, and (when wipe) deletes its data.
func (d *Daemon) RemoveInstance(ctx context.Context, name string, wipe bool) error {
	if d.cfg.Lookup(name) == nil {
		return fmt.Errorf("no instance named %q", name)
	}
	// Stop the backend first (outside the lock — Stop can block on a slow exit).
	if err := d.rt.Stop(ctx, name); err != nil {
		d.logf("remove %q: stopping backend: %v", name, err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if h, ok := d.running[name]; ok {
		h.cancel()
		_ = h.ln.Close()
		delete(d.running, name)
		// The loopback IP is not returned to the allocator pool in v1 (released in
		// bulk at shutdown) — a bounded, per-session leak.
	}
	// Deregister the DNS name.
	if ep, ok := d.endpointFor(name); ok && ep.Domain != "" {
		delete(d.resolveMap, ep.Domain)
		d.republishDomains()
	}
	delete(d.binds, name)
	delete(d.resources, name)
	d.cfg.Remove(name)
	d.writeManifest()

	if wipe {
		_ = os.RemoveAll(d.cfg.ClusterDir(name))
		_ = os.RemoveAll(d.cfg.SocketDir(name))
	}
	return nil
}

// endpointFor computes one instance's endpoint from the live config, reporting
// whether one exists (a portless background process has none).
func (d *Daemon) endpointFor(name string) (endpoints.Endpoint, bool) {
	eps, err := endpoints.For(d.cfg)
	if err != nil {
		return endpoints.Endpoint{}, false
	}
	for _, ep := range eps {
		if ep.Name == name {
			return ep, true
		}
	}
	return endpoints.Endpoint{}, false
}

// bindFor picks the listener address for a newly-added proxied instance,
// mirroring buildBindPlan for a single instance: a per-service loopback IP when
// per-service addressing is on, else 127.0.0.1 with the instance's port.
func (d *Daemon) bindFor(name, address string) (string, net.IP, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", nil, fmt.Errorf("instance %q has no TCP port to bind", name)
	}
	if d.plan.loopback {
		ip, err := d.alloc.For(d.cfg.Stack(), name)
		if err != nil {
			return "", nil, err
		}
		return net.JoinHostPort(ip.String(), port), ip, nil
	}
	return net.JoinHostPort("127.0.0.1", port), nil, nil
}

// rollbackAdd undoes a partial Add after a boot failure.
func (d *Daemon) rollbackAdd(name, domain string) {
	if domain != "" {
		delete(d.resolveMap, domain)
		d.republishDomains()
	}
	d.cfg.Remove(name)
}

// republishDomains re-publishes the live name→IP map to the shared registry so
// the resolver (which reads resolveMap first, then the shared union) stays
// current. Caller holds d.mu.
func (d *Daemon) republishDomains() {
	if !d.cfg.Defaults.Domains {
		return
	}
	publishDomains(d.cfg.Home, d.resolveMap, os.Getpid())
}

// writeManifest refreshes the endpoints manifest after a topology change, and
// re-publishes the stack's ingress routes so a live-added (or removed) ingress
// process is fronted on the shared :80 handler (which reads routes per request).
// Caller holds d.mu.
func (d *Daemon) writeManifest() {
	eps, err := endpoints.For(d.cfg)
	if err != nil {
		return
	}
	if err := endpoints.WriteManifest(endpoints.ManifestPath(d.cfg), eps); err != nil {
		d.logf("warning: could not update endpoints manifest: %v", err)
	}
	d.refreshIngressRoutes(eps)
}

// refreshIngressRoutes rewrites this stack's ingress route file to match the
// current config. The :80 listener is bound once at Run; if the stack started
// with no ingress route, a live-added ingress process routes only after a
// restart (the listener isn't there to front it). Caller holds d.mu.
func (d *Daemon) refreshIngressRoutes(eps []endpoints.Endpoint) {
	if !d.cfg.Defaults.Domains || d.plan == nil {
		return
	}
	path := ingressPath(d.cfg.Home)
	pid := os.Getpid()
	stack := d.cfg.Stack()
	// Replace this stack+pid's routes with the freshly computed set.
	removeRoutes(path, stack, pid)
	routes := d.ingressRoutes(eps, d.plan)
	if len(routes) == 0 {
		return
	}
	if err := mergeRoutes(path, routes, stack, pid); err != nil {
		d.logf("ingress: refreshing routes: %v", err)
	}
}
