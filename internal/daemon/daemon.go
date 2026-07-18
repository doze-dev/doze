// Package daemon wires the runtime, per-instance proxy listeners, reaper, and
// control socket into the long-running daemon process (the hidden `doze __daemon`
// self-exec, started automatically on first use).
package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/endpoints"
	"github.com/doze-dev/doze/internal/loopback"
	"github.com/doze-dev/doze/internal/proxy"
	"github.com/doze-dev/doze/internal/runtime"
)

// ControlSocketPath returns the admin socket path for a project.
func ControlSocketPath(cfg *config.Config) string {
	return ControlSocketPathIn(cfg.ProjectDir())
}

// ControlSocketPathIn returns the admin socket path under a project state dir —
// for callers that must reach a running daemon when the full config won't load
// (degraded status) and only have config.DefaultProjectDir to go on.
func ControlSocketPathIn(projectDir string) string {
	return filepath.Join(projectDir, "run", "doze.admin.sock")
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
	// hooks is the module integration for config decode, used when a live-added
	// instance block is parsed (see dynamic.go). Nil means pure parse.
	hooks *config.Hooks
	// resources maps an instance name to its full, directly-addressable path when
	// the endpoint is a shared front door — an AWS built-in's resource URL/ARN or
	// an ingress process's :80 URL. Built once at Run(); read by Status.
	resources map[string]string
	// binds maps a proxied instance name to the address it ACTUALLY listens on —
	// its per-service 127.0.0.x:port in loopback mode, or 127.0.0.1:port in the
	// fallback. This is the truth Status shows (not the canonical ep.Address, which
	// is 127.0.0.1:<port> for every same-port service and so can't disambiguate two
	// Postgres on 5432). Built once at Run() from the bind plan; empty for supervised
	// processes (they bind their own port) and disabled instances.
	binds map[string]string

	// Dynamic instance management (live Add/Remove). Populated during Run and
	// guarded by mu. See dynamic.go.
	mu         sync.Mutex
	px         *proxy.Proxy
	alloc      *loopback.Allocator
	plan       *bindPlan
	rootCtx    context.Context
	instWG     *sync.WaitGroup
	running    map[string]*instHandle // proxied instances with a live listener
	resolveMap map[string]net.IP      // live view of domain -> IP (mutated on add/remove)
}

// instHandle tracks a live proxy listener so it can be torn down on Remove.
type instHandle struct {
	cancel context.CancelFunc
	ln     net.Listener
	ip     net.IP // allocated loopback IP (nil in the 127.0.0.1 fallback)
}

// New builds a Daemon for cfg. hooks is the module integration passed to
// config.Parse when a live-added instance block is decoded; nil means pure parse.
func New(cfg *config.Config, logf func(string, ...any), hooks *config.Hooks) (*Daemon, error) {
	rt, err := runtime.New(cfg)
	if err != nil {
		return nil, err
	}
	rt.SetLogger(logf)
	return &Daemon{cfg: cfg, rt: rt, logf: logf, hooks: hooks}, nil
}

// Runtime exposes the underlying runtime.
func (d *Daemon) Runtime() *runtime.Runtime { return d.rt }

// Handler returns the daemon's operation surface as a control.Handler, for an
// in-process embedder (the doze library's Serve mode) that calls it directly
// instead of dialing the control socket — same operations, native Go types and
// errors, no serialization.
func (d *Daemon) Handler() control.Handler { return &handler{d: d} }

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
	// Per-service addressing: allocate a loopback IP per proxied instance (when
	// domains are on and the range is aliased) so many services share a canonical
	// port, resolved by name. Falls back to 127.0.0.1 + distinct ports otherwise.
	alloc := loopback.NewAllocator(d.cfg.Home, os.Getpid())
	plan, err := d.buildBindPlan(eps, alloc)
	if err != nil {
		return err
	}
	if plan.loopback {
		defer alloc.Release()
		d.logf("domains: per-service addressing on — each service has its own 127.0.0.x, sharing canonical ports")
	}
	// Remember where each instance actually listens so Status shows the real
	// per-service address, not the canonical 127.0.0.1:<port> placeholder.
	d.binds = plan.bind
	// Full, directly-addressable paths for services behind a shared front door —
	// AWS resource URLs/ARNs (filled by buildAWSRoutes) and ingress processes'
	// :80 URLs — surfaced in the dash's detail card.
	d.resources = map[string]string{}
	// AWS single endpoints (s3./sqs./sns.<stack>.doze) route by resource on
	// the shared :80 ingress; register their hosts in the resolver.
	awsRoutes := d.buildAWSRoutes(plan)
	if d.cfg.Defaults.Domains {
		for _, decl := range d.cfg.Instances {
			if !decl.Enabled || decl.Type != "process" {
				continue
			}
			if fwd, ok := decl.Spec.(interface{ ForwardPort() int }); ok && fwd.ForwardPort() > 0 {
				url := "http://" + d.cfg.DomainFor(decl.Name)
				if p := fwd.ForwardPort(); p != 80 {
					url += fmt.Sprintf(":%d", p)
				}
				d.resources[decl.Name] = url
			}
		}
	}
	disabled := map[string]bool{}
	for _, decl := range d.cfg.Instances {
		if !decl.Enabled {
			disabled[decl.Name] = true
		}
	}
	// Publish the state the dynamic Add/Remove path needs (see dynamic.go).
	var wg sync.WaitGroup
	d.px = px
	d.alloc = alloc
	d.plan = plan
	d.rootCtx = ctx
	d.instWG = &wg
	d.running = map[string]*instHandle{}
	d.resolveMap = plan.resolve
	for _, ep := range eps {
		drv, ok := engine.Lookup(ep.Engine)
		if !ok {
			return fmt.Errorf("no driver registered for engine %q (instance %q)", ep.Engine, ep.Name)
		}
		// A disabled (paused) instance gets no listener: no endpoint, no lazy-boot.
		if disabled[ep.Name] {
			d.logf("%s/%s is disabled; no proxy listener", ep.Engine, ep.Name)
			continue
		}
		// Supervised processes bind their own port — doze does not front them with a
		// proxy. They boot eagerly via `doze up`/`doze wake`, not on a connection.
		if lc, ok := drv.(engine.Lifecycle); ok && lc.Supervised(engine.Instance{Name: ep.Name, Type: ep.Engine}) {
			d.logf("%s/%s is a supervised process; no proxy listener", ep.Engine, ep.Name)
			continue
		}
		bindAddr := plan.bind[ep.Name]
		if bindAddr == "" {
			bindAddr = ep.Address // safety net; shouldn't happen for a proxied instance
		}
		if err := d.startProxyInstance(ep.Name, ep.Engine, ep.Domain, bindAddr, drv, nil); err != nil {
			return err
		}
	}
	// Publish the endpoint manifest for supervised processes and external tooling.
	if err := endpoints.WriteManifest(endpoints.ManifestPath(d.cfg), eps); err != nil {
		d.logf("warning: could not write endpoints manifest: %v", err)
	}
	// Local DNS names (defaults{domains=true}): <service>.<stack>.doze via
	// the built-in resolver, each answering with the service's per-service IP.
	releaseStack, err := d.setupDomains(ctx, plan.resolve)
	if err != nil {
		return err
	}
	defer releaseStack()
	// HTTP ingress: processes with `ingress = true` share :80, routed by Host.
	releaseIngress, err := d.setupIngress(ctx, eps, plan, awsRoutes)
	if err != nil {
		return err
	}
	defer releaseIngress()

	ctrl, err := control.NewServer(ControlSocketPath(d.cfg), &handler{d: d})
	if err != nil {
		return fmt.Errorf("control socket: %w", err)
	}

	// Own the pidfile so `doze down`/`status` can find us however we started.
	pidPath := PidFilePath(d.cfg)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing pidfile: %w", err)
	}
	defer os.Remove(pidPath)

	go d.rt.RunReaper(ctx)
	go d.rt.RunHealthProber(ctx)
	go ctrl.Serve(ctx)

	<-ctx.Done()
	d.logf("shutting down; reaping backends…")
	// No boots or crash restarts may land behind the reap: a dependent process
	// crashes when its dependency is stopped, and its restart policy would
	// otherwise respawn it (and re-boot the dependency) mid-shutdown.
	d.rt.BeginShutdown()
	// Bound the shutdown so a wedged backend can't deadlock exit; the supervisor
	// escalates to SIGKILL, and this caps the total wait. Stops run in parallel,
	// so the budget bounds the slowest backend, not the sum.
	stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	d.rt.StopAll(stopCtx)
	d.logf("all backends reaped")
	wg.Wait()
	return nil
}
