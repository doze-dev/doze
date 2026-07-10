// Package daemon wires the runtime, per-instance proxy listeners, reaper, and
// control socket into the long-running daemon process (the hidden `doze __daemon`
// self-exec, started automatically on first use).
package daemon

import (
	"context"
	"fmt"
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
	"github.com/doze-dev/doze/internal/registry"
	"github.com/doze-dev/doze/internal/runtime"
	"github.com/doze-dev/doze/internal/ui"
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
	var wg sync.WaitGroup
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
		ln, err := proxy.Listen(bindAddr)
		if err != nil {
			return fmt.Errorf("listening for %q on %s: %w", ep.Name, bindAddr, err)
		}
		shown := bindAddr
		if ep.Domain != "" {
			shown = ep.Domain + " (" + bindAddr + ")"
		}
		d.logf("%s/%s listening on %s", ep.Engine, ep.Name, shown)
		name := ep.Name
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = px.ServeInstance(ctx, ln, name, drv)
		}()
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

// handler adapts the runtime to the control.Handler interface.
type handler struct{ d *Daemon }

func (h *handler) Status() control.Response {
	resp := control.Response{Listen: h.d.cfg.Listen, IdleTimeout: h.d.cfg.Defaults.IdleTimeout}
	eps := h.endpointsByName()
	snapshot := h.d.rt.Registry().Snapshot()
	pids := make([]int, 0, len(snapshot))
	for _, inst := range snapshot {
		if inst.PID != 0 {
			pids = append(pids, inst.PID)
		}
	}
	stats := ui.ProcStats(pids) // one ps for every running backend (+ its subtree)
	seen := map[string]bool{}
	for _, inst := range snapshot {
		engineType, version, declared := "", "", false
		if decl := h.d.cfg.Lookup(inst.Name); decl != nil {
			engineType, version, declared = decl.Type, decl.Version.String(), true
		}
		v := control.ViewFromRegistry(inst, engineType, version, declared)
		if decl := h.d.cfg.Lookup(inst.Name); decl != nil && !decl.Enabled {
			v.Disabled = true
		}
		h.hydrateEndpoint(&v, eps[inst.Name])
		v.DataDir = h.dataDir(inst.Name)
		if st, ok := stats[inst.PID]; ok {
			v.RAM, v.CPU = st.RSS, st.CPU
		}
		resp.Instances = append(resp.Instances, v)
		seen[inst.Name] = true
	}
	for _, decl := range h.d.cfg.Instances {
		if !seen[decl.Name] {
			state := "reaped"
			if !decl.Enabled {
				state = "disabled"
			}
			v := control.InstanceView{
				Name: decl.Name, Engine: decl.Type, State: state,
				Version: decl.Version.String(), Declared: true, Disabled: !decl.Enabled,
			}
			h.hydrateEndpoint(&v, eps[decl.Name])
			v.DataDir = h.dataDir(decl.Name)
			resp.Instances = append(resp.Instances, v)
		}
	}
	return resp
}

// dataDir is where an instance's backend writes its data.
func (h *handler) dataDir(name string) string {
	return filepath.Join(h.d.cfg.ClustersDir(), name)
}

func (h *handler) hydrateEndpoint(v *control.InstanceView, ep endpoints.Endpoint) {
	cfg := h.d.cfg
	// Show the address the instance actually listens on. In per-service mode this
	// is its own 127.0.0.x, so two Postgres on 5432 read as 127.0.0.11:5432 and
	// 127.0.0.12:5432 rather than both as the canonical 127.0.0.1:5432 (which is
	// only the declared port, not where anything binds). Falls back to ep.Address
	// for supervised processes and disabled instances (no proxy listener).
	v.Endpoint = ep.Address
	if bind := h.d.binds[v.Name]; bind != "" {
		v.Endpoint = bind
	}
	// Bind keeps the dialable truth even where Endpoint gets prettified below
	// (AWS built-ins swap in the shared host); surfaces show it as the raw
	// address behind the connect line.
	v.Bind = v.Endpoint
	v.Domain = ep.Domain
	v.URL = ep.URL
	v.EnvVar = ep.EnvVar
	// AWS built-ins are reached at their shared per-type endpoint; the backend
	// port in ep.Address is internal-only, so show the shared host instead.
	if host, ok := cfg.AWSHost(v.Engine); ok {
		v.Endpoint = host
	}
	// The full, directly-addressable path (AWS resource URL/ARN, ingress :80 URL).
	v.Resource = h.d.resources[v.Name]
}

// endpointsByName maps instance name -> its resolved endpoint (best effort).
func (h *handler) endpointsByName() map[string]endpoints.Endpoint {
	out := map[string]endpoints.Endpoint{}
	eps, err := endpoints.For(h.d.cfg)
	if err != nil {
		return out
	}
	for _, ep := range eps {
		out[ep.Name] = ep
	}
	return out
}

func (h *handler) Boot(ctx context.Context, name string) error {
	if decl := h.d.cfg.Lookup(name); decl != nil && !decl.Enabled {
		return fmt.Errorf("instance %q is disabled (enabled = false); enable it in the config to wake it", name)
	}
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
			if !decl.Enabled {
				continue // paused: skip disabled instances on a whole-stack up
			}
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

func (h *handler) Apply(ctx context.Context, name string) error {
	return h.d.rt.Apply(ctx, name)
}

func (h *handler) Destroy(ctx context.Context, name string) error {
	return h.d.rt.Destroy(ctx, name)
}

func (h *handler) Reset(ctx context.Context, name string) error {
	return h.d.rt.ResetData(ctx, name)
}

func (h *handler) KeepAwake(name string) error {
	if name == "" {
		return fmt.Errorf("keepawake needs an instance name")
	}
	h.d.rt.ToggleKeepAwake(name)
	return nil
}

func (h *handler) Logs(name string) ([]string, error) {
	p := h.d.rt.Backend(name)
	if p == nil {
		return nil, fmt.Errorf("instance %q is not running", name)
	}
	return p.Logs(), nil
}

// StreamLogs polls the named backends' log rings (every 250ms) and emits each new
// line, tagged with its instance, until ctx is cancelled or emit fails. Empty names
// follows every declared instance. A process restart resets its ring; the cursor
// regression is detected and the new process's output is streamed from the start.
func (h *handler) StreamLogs(ctx context.Context, names []string, emit func(control.LogFrame) error) error {
	if len(names) == 0 {
		for _, d := range h.d.cfg.Instances {
			names = append(names, d.Name)
		}
	}
	sent := map[string]int{}
	last := map[string]engine.Process{} // last backend seen per name, to detect a restart by identity
	flush := func() error {
		for _, n := range names {
			p := h.d.rt.Backend(n)
			if p == nil {
				continue
			}
			if last[n] != p { // first sighting, or a restart replaced the ring — stream from its start
				sent[n] = 0
				last[n] = p
			}
			ls, ok := p.(interface {
				LogsSince(int) ([]string, int)
			})
			if !ok {
				continue
			}
			lines, cursor := ls.LogsSince(sent[n])
			for _, line := range lines {
				if err := emit(control.LogFrame{Instance: n, Line: line}); err != nil {
					return err
				}
			}
			sent[n] = cursor
		}
		return nil
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	if err := flush(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := flush(); err != nil {
				return err
			}
		}
	}
}

// StreamEvents forwards instance-state transitions to emit until ctx ends or the
// client disconnects. It subscribes to the registry's lossy feed, enriches each
// transition with config/endpoint metadata (the same shape Status returns, minus
// the per-instance ps stats), and emits it.
func (h *handler) StreamEvents(ctx context.Context, emit func(control.EventFrame) error) error {
	feed, cancel := h.d.rt.Registry().Subscribe(64)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case inst, ok := <-feed:
			if !ok {
				return nil
			}
			if err := emit(control.EventFrame{Instance: h.eventView(inst)}); err != nil {
				return err
			}
		}
	}
}

// eventView enriches a registry snapshot into a full InstanceView for the events
// stream, mirroring Status's per-instance enrichment without the ps call.
func (h *handler) eventView(inst registry.Instance) control.InstanceView {
	engineType, version, declared := "", "", false
	if decl := h.d.cfg.Lookup(inst.Name); decl != nil {
		engineType, version, declared = decl.Type, decl.Version.String(), true
	}
	v := control.ViewFromRegistry(inst, engineType, version, declared)
	if decl := h.d.cfg.Lookup(inst.Name); decl != nil && !decl.Enabled {
		v.Disabled = true
	}
	h.hydrateEndpoint(&v, h.endpointsByName()[inst.Name])
	v.DataDir = h.dataDir(inst.Name)
	return v
}

// Resources lists a builtin instance's sub-resources (queues/buckets/topics) with
// a live status line, plus the data actions its engine offers. Empty (no error)
// when the engine has no admin capability; an error when it isn't running.
func (h *handler) Resources(ctx context.Context, name string) ([]control.ResourceView, []control.ActionView, error) {
	adm, inst, err := h.d.rt.AdminFor(name)
	if err != nil {
		return nil, nil, err
	}
	if adm == nil {
		return nil, nil, nil
	}
	if h.d.rt.Backend(name) == nil {
		return nil, nil, fmt.Errorf("instance %q is not running — boot it first", name)
	}
	res, err := adm.Resources(ctx, inst, inst.Endpoint)
	if err != nil {
		return nil, nil, err
	}
	rv := make([]control.ResourceView, 0, len(res))
	for _, r := range res {
		rv = append(rv, control.ResourceView{Kind: r.Kind, Name: r.Name, Status: r.Status, Info: r.Info})
	}
	acts := adm.Actions()
	av := make([]control.ActionView, 0, len(acts))
	for _, a := range acts {
		av = append(av, control.ActionView{
			ID: a.ID, Label: a.Label, Kind: a.Kind, Destructive: a.Destructive, InputHint: a.InputHint,
		})
	}
	return rv, av, nil
}

// Admin runs a builtin data action (purge/empty/publish/…) on a named resource.
func (h *handler) Admin(ctx context.Context, name, action, resource, input string) (string, error) {
	adm, inst, err := h.d.rt.AdminFor(name)
	if err != nil {
		return "", err
	}
	if adm == nil {
		return "", fmt.Errorf("instance %q (%s) has no data actions", name, inst.Type)
	}
	if h.d.rt.Backend(name) == nil {
		return "", fmt.Errorf("instance %q is not running — boot it first", name)
	}
	return adm.Run(ctx, inst, inst.Endpoint, action, resource, input)
}
