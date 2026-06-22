// Package daemon wires the runtime, per-instance proxy listeners, reaper, and
// control socket into the long-running `doze serve` process.
package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/control"
	"github.com/nerdmenot/doze/internal/endpoints"
	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/proxy"
	"github.com/nerdmenot/doze/internal/runtime"
)

// ControlSocketPath returns the admin socket path for a project.
func ControlSocketPath(cfg *config.Config) string {
	return filepath.Join(cfg.RunDir(), "doze.admin.sock")
}

// PidFilePath returns the daemon pidfile path for a project.
func PidFilePath(cfg *config.Config) string {
	return filepath.Join(cfg.RunDir(), "doze.pid")
}

// LogFilePath returns the daemon log file path for a project.
func LogFilePath(cfg *config.Config) string {
	return filepath.Join(cfg.RunDir(), "doze.log")
}

// Daemon bundles the running components.
type Daemon struct {
	cfg  *config.Config
	rt   *runtime.Runtime
	logf func(format string, args ...any)
}

// New builds a Daemon for cfg.
func New(cfg *config.Config, logf func(string, ...any)) (*Daemon, error) {
	rt, err := runtime.New(cfg)
	if err != nil {
		return nil, err
	}
	rt.SetLogger(logf)
	return &Daemon{cfg: cfg, rt: rt, logf: logf}, nil
}

// Runtime exposes the underlying runtime.
func (d *Daemon) Runtime() *runtime.Runtime { return d.rt }

// Run opens a listener per declared instance, plus the reaper and control
// socket, blocking until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	if err := d.rt.EnsureDataRoot(); err != nil {
		return err
	}
	// Reclaim any backends orphaned by a previous crash before we rebind their
	// sockets (macOS has no PDEATHSIG to do this for us).
	d.rt.Reconcile()

	px := proxy.New(d.rt)
	px.SetLogger(d.logf)
	if d.cfg.TLS.Enabled {
		tlsConf, err := proxy.BuildServerTLS(
			d.cfg.ResolvePath(d.cfg.TLS.Cert),
			d.cfg.ResolvePath(d.cfg.TLS.Key),
			d.cfg.TLSDir(),
		)
		if err != nil {
			return fmt.Errorf("configuring TLS: %w", err)
		}
		px.TLS = tlsConf
		px.RequireTLS = d.cfg.TLS.Required
		mode := "accepted"
		if d.cfg.TLS.Required {
			mode = "required"
		}
		d.logf("client TLS termination enabled (%s)", mode)
	}

	// One listener per declared instance.
	eps, err := endpoints.For(d.cfg)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, ep := range eps {
		drv, ok := engine.Lookup(ep.Engine)
		if !ok {
			return fmt.Errorf("no driver registered for engine %q (instance %q)", ep.Engine, ep.Name)
		}
		ln, err := proxy.Listen(ep.Address)
		if err != nil {
			return fmt.Errorf("listening for %q on %s: %w", ep.Name, ep.Address, err)
		}
		d.logf("%s/%s listening on %s", ep.Engine, ep.Name, ep.Address)
		name := ep.Name
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = px.ServeInstance(ctx, ln, name, drv)
		}()
	}
	// Publish the endpoint manifest for `doze run`/`doze env` and tooling.
	if err := endpoints.WriteManifest(endpoints.ManifestPath(d.cfg), eps); err != nil {
		d.logf("warning: could not write endpoints manifest: %v", err)
	}

	ctrl, err := control.NewServer(ControlSocketPath(d.cfg), &handler{d: d})
	if err != nil {
		return fmt.Errorf("control socket: %w", err)
	}

	// Own the pidfile so `doze stop`/`status` can find us however we started.
	pidPath := PidFilePath(d.cfg)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing pidfile: %w", err)
	}
	defer os.Remove(pidPath)

	go d.rt.RunReaper(ctx)
	go ctrl.Serve(ctx)

	<-ctx.Done()
	d.logf("shutting down; reaping backends…")
	// Bound the shutdown so a wedged backend can't deadlock exit; the supervisor
	// escalates to SIGKILL, and this caps the total wait.
	stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	d.rt.StopAll(stopCtx)
	wg.Wait()
	return nil
}

// handler adapts the runtime to the control.Handler interface.
type handler struct{ d *Daemon }

func (h *handler) Status() control.Response {
	resp := control.Response{Listen: h.d.cfg.Listen}
	addrs := h.endpointAddrs()
	seen := map[string]bool{}
	for _, inst := range h.d.rt.Registry().Snapshot() {
		engineType, version, declared := "", "", false
		if decl := h.d.cfg.Lookup(inst.Name); decl != nil {
			engineType, version, declared = decl.Type, decl.Version.String(), true
		}
		v := control.ViewFromRegistry(inst, engineType, version, declared)
		v.Endpoint = addrs[inst.Name]
		resp.Instances = append(resp.Instances, v)
		seen[inst.Name] = true
	}
	for _, decl := range h.d.cfg.Instances {
		if !seen[decl.Name] {
			resp.Instances = append(resp.Instances, control.InstanceView{
				Name: decl.Name, Engine: decl.Type, State: "reaped",
				Version: decl.Version.String(), Endpoint: addrs[decl.Name], Declared: true,
			})
		}
	}
	return resp
}

// endpointAddrs maps instance name -> client-facing address (best effort).
func (h *handler) endpointAddrs() map[string]string {
	out := map[string]string{}
	eps, err := endpoints.For(h.d.cfg)
	if err != nil {
		return out
	}
	for _, ep := range eps {
		out[ep.Name] = ep.Address
	}
	return out
}

func (h *handler) Boot(ctx context.Context, name string) error {
	_, err := h.d.rt.Boot(ctx, name)
	return err
}

func (h *handler) Restart(ctx context.Context, name string) error {
	if err := h.d.rt.Stop(ctx, name); err != nil {
		return err
	}
	_, err := h.d.rt.Boot(ctx, name)
	return err
}

func (h *handler) Up(ctx context.Context, name string) error {
	if name == "" {
		for _, decl := range h.d.cfg.Instances {
			if err := h.d.rt.Up(ctx, decl.Name); err != nil {
				return err
			}
		}
		return nil
	}
	return h.d.rt.Up(ctx, name)
}

func (h *handler) Down(ctx context.Context, name string) error {
	if name == "" {
		h.d.rt.StopAll(ctx)
		return nil
	}
	return h.d.rt.Stop(ctx, name)
}

func (h *handler) Logs(name string) ([]string, error) {
	p := h.d.rt.Backend(name)
	if p == nil {
		return nil, fmt.Errorf("instance %q is not running", name)
	}
	return p.Logs(), nil
}
