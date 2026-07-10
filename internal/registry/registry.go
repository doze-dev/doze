// Package registry holds the in-memory state of every known database
// instance: its backend socket, pid, live connection count, and the moment
// it last fell idle. It is the single source of truth the proxy, reaper, and
// status commands read from.
package registry

import (
	"sort"
	"sync"
	"time"
)

// State is the lifecycle phase of a database instance.
type State string

const (
	// Reaped means no backend process is running.
	Reaped State = "reaped"
	// Booting means a backend is being started (singleflight in flight).
	Booting State = "booting"
	// Active means the backend is running and has at least one connection.
	Active State = "active"
	// Idle means the backend is running but has zero connections; the idle
	// timer is counting down toward a reap.
	Idle State = "idle"
)

// Instance is the tracked state of one database's backend.
type Instance struct {
	Name string
	// SocketDir is the directory containing the backend's unix socket
	// (.s.PGSQL.<port>). Empty when reaped.
	SocketDir string
	// Port is the nominal port number used to name the unix socket file.
	Port int
	// PID of the postgres postmaster, 0 when reaped.
	PID int
	// State is the current lifecycle phase.
	State State
	// Conns is the number of live client connections spliced to this backend.
	Conns int
	// StartedAt is when the backend was booted.
	StartedAt time.Time
	// IdleSince is set when Conns drops to zero, zeroed when it rises again.
	// The reaper uses it to decide when to reap.
	IdleSince time.Time
	// LastError holds the most recent boot/convergence/crash failure, surfaced in
	// status and the TUI. Cleared on a new boot attempt and on success.
	LastError string
	// Tainted means the instance's last convergence failed or never completed, so
	// its declared structure (roles, databases, …) is known-incomplete even though
	// the backend may be serving. Unlike LastError it is NOT cleared by a fresh
	// boot — only a successful Converge clears it — so a half-provisioned engine
	// never silently looks healthy.
	Tainted bool
	// KeepAwake exempts the instance from the idle reaper — a per-instance "pin"
	// toggled live from the dashboard. It persists across state changes (e.g. for a
	// slow-booting engine you want to keep warm).
	KeepAwake bool
	// RestartCount is how many times a supervised process has been re-booted after
	// an unexpected exit (per its restart policy). Surfaced in status and the dash.
	RestartCount int
	// Healthy is the result of the most recent periodic liveness probe for a
	// supervised process: nil = not yet probed (or no probe), true/false otherwise.
	// Distinct from State (which tracks running/idle/reaped).
	Healthy *bool
}

// Registry is a concurrency-safe map of database name -> Instance.
type Registry struct {
	mu  sync.Mutex
	m   map[string]*Instance
	now func() time.Time

	// subs and lastSig back the lossy state-transition feed (see subscribe.go).
	subs    map[*chan Instance]struct{}
	lastSig map[string]instSig
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{m: map[string]*Instance{}, now: time.Now}
}

// Get returns a snapshot copy of the instance, and whether it exists.
func (r *Registry) Get(name string) (Instance, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst, ok := r.m[name]
	if !ok {
		return Instance{}, false
	}
	return *inst, true
}

// MarkBooting records that a boot is in progress for name.
func (r *Registry) MarkBooting(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst := r.ensure(name)
	inst.State = Booting
	inst.LastError = "" // a fresh attempt clears the previous failure
	r.emit(inst)
}

// SetError records the most recent failure for name (boot, convergence, crash).
func (r *Registry) SetError(name, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst := r.ensure(name)
	inst.LastError = msg
	r.emit(inst)
}

// SetTainted marks the instance's declared structure as known-incomplete (a
// convergence failed). It persists across boots until ClearTainted.
func (r *Registry) SetTainted(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst := r.ensure(name)
	inst.Tainted = true
	r.emit(inst)
}

// ClearTainted records that the instance has fully converged to its declared
// state. Called only on a successful Converge.
func (r *Registry) ClearTainted(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst := r.ensure(name)
	inst.Tainted = false
	r.emit(inst)
}

// IncRestart bumps and returns the restart counter for a supervised instance. The
// runtime calls it on each re-boot after an unexpected exit; the counter is not
// cleared by MarkReaped/MarkRunning so it reflects total restarts since the last
// intentional stop.
func (r *Registry) IncRestart(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst := r.ensure(name)
	inst.RestartCount++
	return inst.RestartCount
}

// ResetRestart zeroes the restart counter (on an intentional stop/restart).
func (r *Registry) ResetRestart(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensure(name).RestartCount = 0
}

// SetHealthy records the result of the most recent periodic liveness probe.
func (r *Registry) SetHealthy(name string, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst := r.ensure(name)
	inst.Healthy = &healthy
	r.emit(inst)
}

// MarkRunning records a successfully booted backend.
func (r *Registry) MarkRunning(name, socketDir string, port, pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst := r.ensure(name)
	inst.SocketDir = socketDir
	inst.Port = port
	inst.PID = pid
	inst.StartedAt = r.now()
	inst.IdleSince = r.now()
	inst.LastError = "" // booted successfully
	if inst.Conns > 0 {
		inst.State = Active
		inst.IdleSince = time.Time{}
	} else {
		inst.State = Idle
	}
	r.emit(inst)
}

// MarkReaped clears a backend's runtime state after it has stopped.
func (r *Registry) MarkReaped(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst := r.ensure(name)
	inst.State = Reaped
	inst.SocketDir = ""
	inst.PID = 0
	inst.Port = 0
	inst.Conns = 0
	inst.StartedAt = time.Time{}
	inst.IdleSince = time.Time{}
	inst.Healthy = nil // a stopped process has no current health
	r.emit(inst)
}

// Acquire increments the live connection count, returning the new count. It
// moves an Idle instance to Active and clears the idle timer.
func (r *Registry) Acquire(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst := r.ensure(name)
	inst.Conns++
	inst.IdleSince = time.Time{}
	if inst.State != Booting && inst.State != Reaped {
		inst.State = Active
	}
	r.emit(inst)
	return inst.Conns
}

// Release decrements the live connection count, returning the new count. When
// it reaches zero the instance moves to Idle and the idle timer starts.
func (r *Registry) Release(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst := r.ensure(name)
	if inst.Conns > 0 {
		inst.Conns--
	}
	if inst.Conns == 0 {
		inst.IdleSince = r.now()
		if inst.State == Active {
			inst.State = Idle
		}
	}
	r.emit(inst)
	return inst.Conns
}

// Reapable returns the names of instances that are Idle (zero connections)
// and have been so for at least idleTimeout. The reaper calls this on a tick.
func (r *Registry) Reapable(idleTimeout time.Duration) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for name, inst := range r.m {
		if inst.KeepAwake {
			continue // pinned: exempt from the idle reaper
		}
		if inst.State == Idle && inst.Conns == 0 && !inst.IdleSince.IsZero() {
			if r.now().Sub(inst.IdleSince) >= idleTimeout {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ToggleKeepAwake flips the idle-reaper exemption for an instance and returns the
// new value, creating a Reaped placeholder if it isn't tracked yet.
func (r *Registry) ToggleKeepAwake(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst := r.ensure(name)
	inst.KeepAwake = !inst.KeepAwake
	return inst.KeepAwake
}

// Snapshot returns copies of all tracked instances, sorted by name.
func (r *Registry) Snapshot() []Instance {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Instance, 0, len(r.m))
	for _, inst := range r.m {
		out = append(out, *inst)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ensure returns the instance for name, creating a Reaped placeholder if it
// does not yet exist. Caller must hold r.mu.
func (r *Registry) ensure(name string) *Instance {
	inst, ok := r.m[name]
	if !ok {
		inst = &Instance{Name: name, State: Reaped}
		r.m[name] = inst
	}
	return inst
}
