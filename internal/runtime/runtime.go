// Package runtime is the engine-agnostic orchestration core. It binds the
// config, binary fetcher, lockfile, and registry behind a small API: lazily
// Boot an instance (coalescing concurrent cold starts), track its live
// connections, and reap it when idle. It drives engine.Driver implementations
// and contains no engine-specific code.
package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/nerdmenot/doze/internal/binaries"
	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/endpoints"
	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/registry"
	"github.com/nerdmenot/doze/internal/state"
	"github.com/nerdmenot/doze/internal/supervisor"
)

// nominalPort is the default socket-naming port for unix-only backends; drivers
// that bind real ports override it via a NominalPort() int method.
const nominalPort = 5432

// Runtime manages every instance backend for a single doze configuration.
type Runtime struct {
	cfg   *config.Config
	mgr   *binaries.Manager
	lock  *binaries.Lock
	plat  engine.Platform
	reg   *registry.Registry
	group singleflight.Group

	mu    sync.Mutex
	procs map[string]engine.Process
	deps  map[string][]string // instance -> dependency names it holds running

	statePath string // .doze/state.json for apply/destroy object tracking

	logf func(format string, args ...any)
}

// New constructs a Runtime for cfg.
func New(cfg *config.Config) (*Runtime, error) {
	plat, err := binaries.HostPlatform()
	if err != nil {
		return nil, err
	}
	lock, err := binaries.LoadLock(lockPath(cfg))
	if err != nil {
		return nil, err
	}
	r := &Runtime{
		cfg:       cfg,
		mgr:       binaries.NewManager(cfg.Home),
		lock:      lock,
		plat:      plat,
		reg:       registry.New(),
		procs:     map[string]engine.Process{},
		deps:      map[string][]string{},
		statePath: state.Path(cfg.Path()),
		logf:      func(string, ...any) {},
	}
	return r, nil
}

// SetLogger installs a logging callback.
func (r *Runtime) SetLogger(f func(format string, args ...any)) {
	r.logf = f
	r.mgr.Logf = f
}

// Config returns the configuration this runtime serves.
func (r *Runtime) Config() *config.Config { return r.cfg }

// Registry exposes the underlying state for status/TUI consumers.
func (r *Runtime) Registry() *registry.Registry { return r.reg }

func lockPath(cfg *config.Config) string {
	dir := "."
	if cfg.Path() != "" {
		dir = filepath.Dir(cfg.Path())
	}
	return filepath.Join(dir, binaries.LockFileName)
}

// instanceFor builds the engine.Instance the driver operates on.
func (r *Runtime) instanceFor(decl *config.InstanceDecl, drv engine.Driver) engine.Instance {
	port := nominalPort
	if np, ok := drv.(interface{ NominalPort() int }); ok {
		port = np.NominalPort()
	}
	socketDir := r.cfg.SocketDir(decl.Name)
	return engine.Instance{
		Name:      decl.Name,
		Type:      decl.Type,
		Version:   decl.Version,
		DataDir:   r.cfg.ClusterDir(decl.Name),
		SocketDir: socketDir,
		Port:      port,
		Endpoint:  engine.Endpoint{Backend: drv.BackendSocket(socketDir, port)},
		Spec:      decl.Spec,
	}
}

// Boot ensures the named instance is provisioned and running (to the Healthy
// condition — backend accepts/health probe passes), returning its endpoint.
// Concurrent cold boots for the same name coalesce onto one start.
func (r *Runtime) Boot(ctx context.Context, name string) (engine.Endpoint, error) {
	return r.bootCond(ctx, name, engine.Healthy)
}

// bootCond boots name to a readiness condition: Healthy runs the full readiness
// gate (today's behavior), Started returns as soon as the process has spawned and
// survived a brief liveness window — used by dependents that don't need a peer's
// health probe to pass first. Coalescing is keyed by name, so a concurrent Healthy
// boot wins over a Started one (Healthy is the stronger guarantee).
func (r *Runtime) bootCond(ctx context.Context, name string, cond engine.Condition) (engine.Endpoint, error) {
	if inst, ok := r.reg.Get(name); ok && (inst.State == registry.Active || inst.State == registry.Idle) {
		r.mu.Lock()
		p := r.procs[name]
		r.mu.Unlock()
		if p != nil && p.Alive() {
			return r.endpointFor(name)
		}
	}
	res, err, _ := r.group.Do(name, func() (any, error) { return r.bootLocked(ctx, name, cond) })
	if err != nil {
		return engine.Endpoint{}, err
	}
	return res.(engine.Endpoint), nil
}

// AdminFor returns the builtin data-admin capability for an instance plus the
// engine.Instance to operate on. The returned Admin is nil (with a nil error)
// when the engine has no admin capability — callers treat that as "no actions".
func (r *Runtime) AdminFor(name string) (engine.Admin, engine.Instance, error) {
	decl := r.cfg.Lookup(name)
	if decl == nil {
		return nil, engine.Instance{}, fmt.Errorf("instance %q is not declared", name)
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return nil, engine.Instance{}, fmt.Errorf("no driver for engine %q", decl.Type)
	}
	adm, _ := drv.(engine.Admin) // nil when the engine has no admin capability
	return adm, r.instanceFor(decl, drv), nil
}

func (r *Runtime) endpointFor(name string) (engine.Endpoint, error) {
	decl := r.cfg.Lookup(name)
	if decl == nil {
		return engine.Endpoint{}, fmt.Errorf("instance %q is not declared", name)
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return engine.Endpoint{}, fmt.Errorf("no driver for engine %q", decl.Type)
	}
	inst := r.instanceFor(decl, drv)
	return inst.Endpoint, nil
}

func (r *Runtime) bootLocked(ctx context.Context, name string, cond engine.Condition) (engine.Endpoint, error) {
	if inst, ok := r.reg.Get(name); ok && (inst.State == registry.Active || inst.State == registry.Idle) {
		r.mu.Lock()
		p := r.procs[name]
		r.mu.Unlock()
		if p != nil && p.Alive() {
			return r.endpointFor(name)
		}
	}

	decl := r.cfg.Lookup(name)
	if decl == nil {
		return engine.Endpoint{}, fmt.Errorf("instance %q is not declared in %s", name, configName(r.cfg))
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return engine.Endpoint{}, fmt.Errorf("no driver registered for engine %q", decl.Type)
	}
	inst := r.instanceFor(decl, drv)

	r.reg.MarkBooting(name)
	r.logf("booting %q (%s %s)…", name, decl.Type, decl.Version)

	// Dependencies: boot and hold any instances this one references (derived from
	// the config reference graph, e.g. an sns instance referencing sqs.jobs)
	// before provisioning this one.
	var held []string
	if len(decl.Deps) > 0 {
		deps, h, err := r.bootDeps(ctx, name, decl.Deps)
		if err != nil {
			r.reg.MarkReaped(name)
			r.reg.SetError(name, err.Error())
			return engine.Endpoint{}, err
		}
		inst.Deps, held = deps, h
	}
	// fail releases any held dependencies, marks this instance reaped, and records
	// the error so `doze status`/`doctor`/the TUI can surface it.
	fail := func(err error) (engine.Endpoint, error) {
		for _, dn := range held {
			r.Release(dn)
		}
		r.reg.MarkReaped(name)
		r.reg.SetError(name, err.Error())
		return engine.Endpoint{}, err
	}

	tc, err := drv.Resolve(ctx, decl.Version, r.plat, r.lock, r.mgr)
	if err != nil {
		return fail(err)
	}
	_ = r.lock.Save()

	fresh := !drv.Provisioned(inst.DataDir)
	if fresh {
		r.tryCloneTemplate(ctx, drv, inst, tc)
	}
	if err := drv.Provision(ctx, inst, tc); err != nil {
		return fail(err)
	}

	// Supervised processes run with the same environment `doze run` injects
	// (connection strings, AWS creds/region, DOZE_<NAME>_URL); their config also
	// references peers directly, so this is the floor, not the only source.
	if r.isSupervised(drv, inst) {
		inst.InjectedEnv = r.injectedEnv()
	}

	// Lifecycle hooks run around the start: pre_start (e.g. migrations) after deps
	// are up but before the process spawns. A failure aborts and taints the boot.
	if h, ok := drv.(engine.Hooked); ok {
		if err := h.PreStart(ctx, inst); err != nil {
			r.reg.SetTainted(name)
			return fail(fmt.Errorf("pre_start for %q: %w", name, err))
		}
	}

	proc, err := drv.Spawn(ctx, inst, tc)
	if err != nil {
		return fail(err)
	}
	// Healthy waits the readiness gate; Started returns once spawned (the driver's
	// WaitReady still ran nothing, so a brief liveness check is the process driver's
	// job when it has no health block).
	if cond != engine.Started {
		if err := drv.WaitReady(ctx, inst, tc, proc); err != nil {
			_ = proc.Stop(context.Background())
			return fail(err)
		}
	}

	r.mu.Lock()
	r.procs[name] = proc
	r.deps[name] = held
	r.mu.Unlock()
	r.reg.MarkRunning(name, inst.SocketDir, inst.Port, proc.PID())
	r.writePidfile(name, proc.PID()) // for orphan reconciliation after a daemon crash
	go r.watch(name, proc)           // detect an unexpected exit and mark it reaped

	// Converge to the declared structure when the instance has not yet fully
	// converged — on a fresh provision, or on any later boot whose prior converge
	// never completed (marker absent). A failure tears the backend down and taints
	// the instance, so incomplete structure never silently serves.
	if c, ok := drv.(engine.Converger); ok && (fresh || !r.isConverged(name)) {
		if err := c.Converge(ctx, inst, tc, inst.Endpoint); err != nil {
			r.reg.SetTainted(name)
			r.logf("convergence for %q failed: %v", name, err)
			r.mu.Lock() // delete before Stop so the crash watcher no-ops
			delete(r.procs, name)
			delete(r.deps, name)
			r.mu.Unlock()
			_ = proc.Stop(context.Background())
			r.removePidfile(name)
			return fail(fmt.Errorf("provisioning %q: %w", name, err))
		}
		r.markConverged(name)
		r.reg.ClearTainted(name)
	}

	// post_start runs once the instance is up and (for Healthy) ready. A failure
	// taints and tears it down so a half-started service never looks healthy.
	if cond != engine.Started {
		if h, ok := drv.(engine.Hooked); ok {
			if err := h.PostStart(ctx, inst); err != nil {
				r.reg.SetTainted(name)
				r.mu.Lock()
				delete(r.procs, name)
				delete(r.deps, name)
				r.mu.Unlock()
				_ = proc.Stop(context.Background())
				r.removePidfile(name)
				return fail(fmt.Errorf("post_start for %q: %w", name, err))
			}
		}
	}

	r.logf("%q ready (pid %d)", name, proc.PID())
	return inst.Endpoint, nil
}

// ToggleKeepAwake flips an instance's idle-reaper exemption and returns the new
// value, so a slow-booting engine can be pinned awake from the dashboard.
func (r *Runtime) ToggleKeepAwake(name string) bool { return r.reg.ToggleKeepAwake(name) }

// bootDeps boots and holds (via Acquire) every instance the named instance
// depends on, returning the resolved deps and the list of held names. On any
// failure it releases the deps it already held. Each dependency is booted to its
// declared Condition: Healthy waits the full readiness gate, Started returns once
// the dependency has spawned (used to start a service before a peer process's
// health probe passes).
func (r *Runtime) bootDeps(ctx context.Context, name string, depList []engine.Dependency) (map[string]engine.Dep, []string, error) {
	deps := map[string]engine.Dep{}
	var held []string
	release := func() {
		for _, dn := range held {
			r.Release(dn)
		}
	}
	for _, dep := range depList {
		dn := dep.Name
		if dn == "" || dn == name {
			continue
		}
		depDecl := r.cfg.Lookup(dn)
		if depDecl == nil {
			release()
			return nil, nil, fmt.Errorf("instance %q depends on undeclared instance %q", name, dn)
		}
		cond := dep.Condition
		if cond == "" {
			cond = engine.Healthy
		}
		if _, err := r.bootCond(ctx, dn, cond); err != nil {
			release()
			return nil, nil, fmt.Errorf("booting dependency %q: %w", dn, err)
		}
		r.Acquire(dn) // hold so the reaper keeps the dependency alive while we run
		held = append(held, dn)

		depDrv, _ := engine.Lookup(depDecl.Type)
		depInst := r.instanceFor(depDecl, depDrv)
		dep := engine.Dep{Name: dn, Engine: depDecl.Type, SocketDir: depInst.SocketDir, Backend: depInst.Endpoint.Backend}
		if bp, ok := depDrv.(engine.BackendProvider); ok {
			dep.URL = bp.BackendURL(depInst)
		}
		deps[dn] = dep
	}
	return deps, held, nil
}

// tryCloneTemplate provisions a cold instance by cloning a shared, version-keyed
// template (copy-on-write where supported) instead of running the engine's
// init step each time. It is best-effort: any failure falls through to a normal
// Provision. Requires a resolved version (skipped for bindir overrides).
func (r *Runtime) tryCloneTemplate(ctx context.Context, drv engine.Driver, inst engine.Instance, tc engine.Toolchain) {
	t, ok := drv.(engine.Templater)
	if !ok || tc.Full == "" {
		return
	}
	templateDir := filepath.Join(r.cfg.Home, inst.Type, "_templates", tc.Full)
	if err := t.EnsureTemplate(ctx, tc, templateDir); err != nil {
		r.logf("template for %s %s unavailable (%v); provisioning %q directly", inst.Type, tc.Full, err, inst.Name)
		return
	}
	if err := t.CloneTemplate(ctx, templateDir, inst.DataDir); err != nil {
		r.logf("cloning %q from template failed (%v); provisioning directly", inst.Name, err)
		_ = os.RemoveAll(inst.DataDir)
		return
	}
	r.logf("cloned %q from the %s %s template", inst.Name, inst.Type, tc.Full)
}

// Acquire registers a new live connection to name.
func (r *Runtime) Acquire(name string) { r.reg.Acquire(name) }

// Release removes a live connection from name.
func (r *Runtime) Release(name string) { r.reg.Release(name) }

// Stop reaps a single backend, if running, and releases any dependencies it was
// holding (so they can reap once nothing else needs them).
func (r *Runtime) Stop(ctx context.Context, name string) error {
	r.mu.Lock()
	p := r.procs[name]
	delete(r.procs, name)
	held := r.deps[name]
	delete(r.deps, name)
	r.mu.Unlock()
	if p == nil && len(held) == 0 {
		return nil
	}
	// An intentional stop clears the restart budget, so a later `up` starts fresh.
	r.reg.ResetRestart(name)
	var err error
	if p != nil {
		r.logf("reaping %q…", name)
		// pre_stop hooks (e.g. drain) run before the process is signalled.
		if decl := r.cfg.Lookup(name); decl != nil {
			if drv, ok := engine.Lookup(decl.Type); ok {
				if h, ok := drv.(engine.Hooked); ok {
					inst := r.instanceFor(decl, drv)
					if r.isSupervised(drv, inst) {
						inst.InjectedEnv = r.injectedEnv()
					}
					if e := h.PreStop(ctx, inst); e != nil {
						r.logf("pre_stop for %q failed: %v", name, e)
					}
				}
			}
		}
		err = p.Stop(ctx)
	}
	for _, dn := range held {
		r.Release(dn)
	}
	r.removePidfile(name)
	r.reg.MarkReaped(name)
	return err
}

// watch waits for a backend to exit and, if that exit was NOT an intentional
// Stop (which removes it from r.procs first), marks the instance reaped and
// records the failure — so the next connect cleanly re-boots instead of dialing
// a dead socket.
func (r *Runtime) watch(name string, proc engine.Process) {
	exitErr := proc.Wait()
	r.mu.Lock()
	if r.procs[name] != proc {
		r.mu.Unlock() // intentionally stopped/replaced; nothing to do
		return
	}
	delete(r.procs, name)
	held := r.deps[name]
	delete(r.deps, name)
	r.mu.Unlock()
	for _, dn := range held {
		r.Release(dn)
	}
	r.removePidfile(name)
	r.reg.MarkReaped(name)

	// A supervised process may restart per its policy instead of staying down.
	if spec, ok := r.restartSpec(name); ok && shouldRestart(spec.Policy, exitErr) {
		count := r.reg.IncRestart(name)
		if count <= spec.MaxRetries {
			delay := backoffFor(spec.Backoff, count)
			r.reg.SetError(name, fmt.Sprintf("exited; restarting (%d/%d) in %s", count, spec.MaxRetries, delay))
			r.logf("process %q exited; restarting (%d/%d) in %s", name, count, spec.MaxRetries, delay)
			go func() {
				time.Sleep(delay)
				if _, err := r.Boot(context.Background(), name); err != nil {
					r.logf("restart of %q failed: %v", name, err)
				}
			}()
			return
		}
		r.reg.SetError(name, fmt.Sprintf("exited; gave up after %d restarts", spec.MaxRetries))
		r.logf("process %q exited; gave up after %d restarts", name, spec.MaxRetries)
		return
	}

	r.reg.SetError(name, "backend exited unexpectedly")
	r.logf("backend %q exited unexpectedly; it will re-boot on the next connection", name)
}

// restartSpec returns the restart policy for an instance whose driver is
// Restartable, and ok=false when it isn't (or the policy is "no") — in which case
// an unexpected exit leaves the instance reaped, today's behavior.
func (r *Runtime) restartSpec(name string) (engine.RestartSpec, bool) {
	decl := r.cfg.Lookup(name)
	if decl == nil {
		return engine.RestartSpec{}, false
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return engine.RestartSpec{}, false
	}
	rs, ok := drv.(engine.Restartable)
	if !ok {
		return engine.RestartSpec{}, false
	}
	spec := rs.RestartPolicy(r.instanceFor(decl, drv))
	if spec.Policy == engine.RestartNo || spec.MaxRetries <= 0 {
		return engine.RestartSpec{}, false
	}
	return spec, true
}

// shouldRestart decides whether an exit warrants a restart under a policy: always
// on any exit; on_failure only on a non-nil exit error (non-zero status).
func shouldRestart(policy engine.RestartPolicy, exitErr error) bool {
	switch policy {
	case engine.RestartAlways:
		return true
	case engine.RestartOnFailure:
		return exitErr != nil
	default:
		return false
	}
}

// backoffFor grows the base delay exponentially with the attempt number, capped at
// 30s so a flapping process backs off without waiting absurdly long.
func backoffFor(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= 30*time.Second {
			return 30 * time.Second
		}
	}
	return d
}

// convergedMarkerPath is a sentinel written inside an instance's data dir after
// a successful convergence. Its absence means the declared structure has not been
// fully applied — so a provisioned-but-unconverged instance (e.g. one whose first
// converge failed and was torn down) re-converges on its next boot instead of
// silently serving incomplete structure. `doze reset` deletes the data dir and
// with it the marker.
func (r *Runtime) convergedMarkerPath(name string) string {
	return filepath.Join(r.cfg.ClusterDir(name), ".doze-converged")
}

func (r *Runtime) isConverged(name string) bool {
	_, err := os.Stat(r.convergedMarkerPath(name))
	return err == nil
}

func (r *Runtime) markConverged(name string) {
	_ = os.WriteFile(r.convergedMarkerPath(name), nil, 0o644)
}

func (r *Runtime) clearConvergedMarker(name string) { _ = os.Remove(r.convergedMarkerPath(name)) }

// pidfilePath is where a running backend's pid is recorded for reconciliation.
func (r *Runtime) pidfilePath(name string) string {
	return filepath.Join(r.cfg.RunDir(), "backend-"+name+".pid")
}

func (r *Runtime) writePidfile(name string, pid int) {
	_ = os.WriteFile(r.pidfilePath(name), []byte(strconv.Itoa(pid)), 0o644)
}

func (r *Runtime) removePidfile(name string) { _ = os.Remove(r.pidfilePath(name)) }

// Reconcile reclaims backends orphaned by a previous daemon crash. On Linux the
// kernel kills children via PDEATHSIG, but macOS has no equivalent, so on every
// daemon start we kill any still-alive backend pid recorded on disk before we
// rebind. Best-effort.
func (r *Runtime) Reconcile() {
	entries, err := os.ReadDir(r.cfg.RunDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "backend-") || !strings.HasSuffix(e.Name(), ".pid") {
			continue
		}
		path := filepath.Join(r.cfg.RunDir(), e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err == nil && pid > 0 && supervisor.ProcessAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			r.logf("reclaimed orphaned backend (pid %d) from a prior run", pid)
		}
		_ = os.Remove(path)
	}
}

// StopAll reaps every running backend.
func (r *Runtime) StopAll(ctx context.Context) {
	r.mu.Lock()
	names := make([]string, 0, len(r.procs))
	for n := range r.procs {
		names = append(names, n)
	}
	r.mu.Unlock()
	for _, n := range names {
		_ = r.Stop(ctx, n)
	}
}

// Backend returns the process handle for name (for log access), if running.
func (r *Runtime) Backend(name string) engine.Process {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.procs[name]
}

// RunReaper runs the idle-reaper loop until ctx is cancelled. It reaps a backend
// only when it has had zero connections for the whole idle_timeout — never on
// query inactivity, so pools holding idle connections keep their backend alive.
func (r *Runtime) RunReaper(ctx context.Context) {
	timeout := r.cfg.Defaults.IdleTimeout
	if timeout <= 0 {
		return
	}
	// Check at least once a second so an instance reaps within ~1s of crossing
	// its deadline — keeping the dashboard's "sleeps in" countdown honest rather
	// than reaping a coarse interval late. The scan is a cheap in-memory pass
	// over a handful of instances, so a 1s cadence is negligible.
	interval := timeout / 10
	if interval > time.Second {
		interval = time.Second
	}
	if interval < 250*time.Millisecond {
		interval = 250 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, name := range r.reg.Reapable(timeout) {
				if r.supervised(name) {
					continue // a supervised (always-on) instance is exempt from the reaper
				}
				if err := r.Stop(context.Background(), name); err != nil {
					r.logf("reaping %q failed: %v", name, err)
				} else {
					r.logf("reaped %q after %s idle", name, timeout)
				}
			}
		}
	}
}

// supervised reports whether an instance's engine marks it as a long-lived,
// always-on process (engine.Lifecycle) — exempt from the idle reaper. The process
// engine implements it; lazy DB/AWS backends do not.
func (r *Runtime) supervised(name string) bool {
	decl := r.cfg.Lookup(name)
	if decl == nil {
		return false
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return false
	}
	lc, ok := drv.(engine.Lifecycle)
	if !ok {
		return false
	}
	return lc.Supervised(r.instanceFor(decl, drv))
}

// isSupervised reports whether a resolved (driver, instance) pair is a long-lived
// supervised process.
func (r *Runtime) isSupervised(drv engine.Driver, inst engine.Instance) bool {
	lc, ok := drv.(engine.Lifecycle)
	return ok && lc.Supervised(inst)
}

// injectedEnv is the doze-managed environment handed to a supervised process: the
// same connection strings + AWS creds/region + DOZE_<NAME>_URL that `doze run`
// injects, derived from every declared instance's endpoint. Best-effort.
func (r *Runtime) injectedEnv() map[string]string {
	eps, err := endpoints.For(r.cfg)
	if err != nil {
		return nil
	}
	return endpoints.EnvVars(eps)
}

// RunHealthProber periodically probes running supervised instances that expose an
// engine.HealthChecker and records the result in the registry (for the dashboard
// badge). It never restarts on a failed probe in v1 — that is the crash watcher's
// job — so a transient blip can't trigger a flap.
func (r *Runtime) RunHealthProber(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.probeHealthOnce(ctx)
		}
	}
}

func (r *Runtime) probeHealthOnce(ctx context.Context) {
	for _, inst := range r.reg.Snapshot() {
		if inst.State != registry.Active && inst.State != registry.Idle {
			continue
		}
		decl := r.cfg.Lookup(inst.Name)
		if decl == nil {
			continue
		}
		drv, ok := engine.Lookup(decl.Type)
		if !ok {
			continue
		}
		hc, ok := drv.(engine.HealthChecker)
		if !ok {
			continue
		}
		ei := r.instanceFor(decl, drv)
		if !r.isSupervised(drv, ei) {
			continue
		}
		r.reg.SetHealthy(inst.Name, hc.CheckHealth(ctx, ei) == nil)
	}
}

// Up boots an instance and converges it to its declared state, leaving it
// running. Convergence is idempotent.
func (r *Runtime) Up(ctx context.Context, name string) error {
	if _, err := r.Boot(ctx, name); err != nil {
		return err
	}
	return r.ensureConverged(ctx, name)
}

// ProvisionOnly converges an instance without leaving the backend running.
func (r *Runtime) ProvisionOnly(ctx context.Context, name string) error {
	if err := r.Up(ctx, name); err != nil {
		_ = r.Stop(ctx, name)
		return err
	}
	return r.Stop(ctx, name)
}

// ensureConverged runs convergence against an already-running backend.
func (r *Runtime) ensureConverged(ctx context.Context, name string) error {
	decl := r.cfg.Lookup(name)
	if decl == nil {
		return fmt.Errorf("instance %q is not declared", name)
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return fmt.Errorf("no driver for engine %q", decl.Type)
	}
	c, ok := drv.(engine.Converger)
	if !ok {
		return nil // engine has no structure to converge
	}
	inst := r.instanceFor(decl, drv)
	tc, err := drv.Resolve(ctx, decl.Version, r.plat, r.lock, r.mgr)
	if err != nil {
		return err
	}
	if err := c.Converge(ctx, inst, tc, inst.Endpoint); err != nil {
		// An explicit `up`/`apply` is left running so the user can inspect it, but
		// the instance is tainted and surfaced loudly until a converge succeeds.
		r.clearConvergedMarker(name)
		r.reg.SetTainted(name)
		r.reg.SetError(name, err.Error())
		return err
	}
	r.markConverged(name)
	r.reg.ClearTainted(name)
	return nil
}

// ResolveToolchain resolves (and caches) the toolchain for a declared instance.
func (r *Runtime) ResolveToolchain(ctx context.Context, name string) (engine.Toolchain, error) {
	decl := r.cfg.Lookup(name)
	if decl == nil {
		return engine.Toolchain{}, fmt.Errorf("instance %q is not declared", name)
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return engine.Toolchain{}, fmt.Errorf("no driver for engine %q", decl.Type)
	}
	return drv.Resolve(ctx, decl.Version, r.plat, r.lock, r.mgr)
}

// EnsureDataRoot creates the doze home (shared) and per-project directories.
func (r *Runtime) EnsureDataRoot() error {
	for _, d := range []string{r.cfg.Home, r.cfg.CacheDir(), r.cfg.ClustersDir(), r.cfg.RunDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func configName(cfg *config.Config) string {
	if p := cfg.Path(); p != "" {
		return filepath.Base(p)
	}
	return "config"
}
