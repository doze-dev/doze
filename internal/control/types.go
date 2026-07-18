// Package control is the thin admin IPC between the `doze` CLI and a running
// doze daemon. It speaks newline-delimited JSON over a unix socket so commands
// like `status`, `stop`, and `dash` can reflect and steer live state.
//
// This file holds the wire-protocol request/response types shared by the
// Server and Client.
package control

import (
	"context"
	"time"

	"github.com/doze-dev/doze/internal/registry"
)

// Request is a command sent from the CLI to the daemon.
type Request struct {
	Op string `json:"op"`           // "status", "up", "down"
	DB string `json:"db,omitempty"` // target database (empty = all, where meaningful)

	// Admin (builtin data ops): "resources" lists a builtin's sub-resources;
	// "admin" runs Action on Resource (with optional Input) — see engine.Admin.
	Action   string `json:"action,omitempty"`
	Resource string `json:"resource,omitempty"`
	Input    string `json:"input,omitempty"`

	// Follow turns the "logs" op into a held streaming connection that emits
	// LogFrame lines as they arrive (used by `doze up` and `doze logs -f`).
	Follow bool `json:"follow,omitempty"`
	// Names scopes a streaming "logs" follow to specific instances (empty = every
	// running backend). DB still works for a single target.
	Names []string `json:"names,omitempty"`

	// Block is the rendered HCL of a single instance to add live ("add" op).
	Block string `json:"block,omitempty"`
	// Wipe, on the "remove" op, also deletes the instance's data directory.
	Wipe bool `json:"wipe,omitempty"`
}

// LogFrame is one streamed, instance-tagged log line (logs follow op).
type LogFrame struct {
	Instance string `json:"instance"`
	Line     string `json:"line"`
}

// EventFrame is one streamed instance-state transition (events op) — emitted
// when an instance's lifecycle-visible state changes (booting, active, idle,
// reaped, health, error, taint). It carries the full enriched view so a
// consumer needs no follow-up Status call.
type EventFrame struct {
	Instance InstanceView `json:"instance"`
}

// ProcView is one descendant process of a supervised instance.
type ProcView struct {
	PID   int     `json:"pid"`
	RSS   int64   `json:"rss"`
	CPU   float64 `json:"cpu"`
	Cmd   string  `json:"cmd"`
	Depth int     `json:"depth"`
}

// ResourceView is a serializable engine.Resource (a builtin's sub-resource).
type ResourceView struct {
	Kind   string            `json:"kind"`
	Name   string            `json:"name"`
	Status string            `json:"status"`
	Info   map[string]string `json:"info,omitempty"`
}

// ActionView is a serializable engine.Action (a builtin data operation).
type ActionView struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Kind        string `json:"kind"`
	Destructive bool   `json:"destructive,omitempty"`
	InputHint   string `json:"input_hint,omitempty"`
}

// InstanceView is a serializable snapshot of one backend's state.
type InstanceView struct {
	Name      string    `json:"name"`
	Engine    string    `json:"engine"`
	State     string    `json:"state"`
	Version   string    `json:"version"`
	PID       int       `json:"pid"`
	Conns     int       `json:"conns"`
	StartedAt time.Time `json:"started_at"`
	IdleSince time.Time `json:"idle_since,omitempty"` // when Conns hit 0; drives the reap countdown
	RAM       int64     `json:"ram,omitempty"`        // resident bytes of the backend, 0 when reaped
	CPU       float64   `json:"cpu,omitempty"`        // CPU usage percent (one core = 100), 0 when reaped
	Endpoint  string    `json:"endpoint,omitempty"`   // client-facing address
	// Bind is the address the backend/listener actually occupies (host:port or
	// unix:/path) — the dialable truth behind the client-facing Endpoint: the
	// per-instance loopback bind (127.0.0.11:5432) for proxied services, the
	// internal backend behind the shared host for AWS built-ins, the self-bound
	// address for processes.
	Bind   string `json:"bind,omitempty"`
	Domain string `json:"domain,omitempty"` // local DNS name, e.g. "orders-pg.<stack>.doze"
	URL    string `json:"url,omitempty"`    // full connection string
	// Resource is the full, directly-addressable path when the client-facing
	// endpoint is a shared front door: an AWS built-in's resource URL/ARN
	// (http://s3.<stack>.doze/<bucket>, a queue URL, a topic ARN) or an
	// ingress process's :80 URL (http://<name>.<stack>.doze). Empty otherwise.
	Resource  string `json:"resource,omitempty"`
	EnvVar    string `json:"env_var,omitempty"`    // conventional env var (DATABASE_URL, …)
	DataDir   string `json:"data_dir,omitempty"`   // where this instance's data is written
	LastError string `json:"last_error,omitempty"` // most recent boot/crash failure
	Tainted   bool   `json:"tainted,omitempty"`    // last convergence failed/incomplete
	Declared  bool   `json:"declared"`
	Disabled  bool   `json:"disabled,omitempty"`   // declared with enabled = false (paused; no listener, not booted)
	KeepAwake bool   `json:"keep_awake,omitempty"` // pinned: exempt from the idle reaper
	// Group is the display heading for status/dash; empty means "infer from engine
	// category" (an explicit `group=` or a module address can set it later).
	Group string `json:"group,omitempty"`
	// RestartCount is how many times a supervised process has been re-booted after
	// an unexpected exit; 0 for DB/AWS backends.
	RestartCount int `json:"restart_count,omitempty"`
	// Children are the instance's descendant processes (a supervised app's own
	// subprocesses), each with its own meters — the APPS view's process tree.
	Children []ProcView `json:"children,omitempty"`
	// Healthy is the latest periodic liveness result for a supervised process:
	// nil = not probed (or not a process), else true/false.
	Healthy *bool `json:"healthy,omitempty"`
}

// Response is the daemon's reply.
type Response struct {
	OK          bool           `json:"ok"`
	Error       string         `json:"error,omitempty"`
	Listen      string         `json:"listen,omitempty"`
	IdleTimeout time.Duration  `json:"idle_timeout,omitempty"` // reap window, for the countdown
	Instances   []InstanceView `json:"instances,omitempty"`
	Lines       []string       `json:"lines,omitempty"`
	Resources   []ResourceView `json:"resources,omitempty"` // builtin sub-resources (resources op)
	Actions     []ActionView   `json:"actions,omitempty"`   // available data actions (resources op)
	Result      string         `json:"result,omitempty"`    // an admin action's result line (admin op)
}

// Handler implements the daemon-side operations.
type Handler interface {
	Status() Response
	Up(ctx context.Context, db string) error
	Down(ctx context.Context, db string) error
	Boot(ctx context.Context, db string) error
	Restart(ctx context.Context, db string) error
	Logs(db string) ([]string, error)
	// StreamLogs follows the named instances' logs (empty = all running backends),
	// calling emit for each new line until ctx is cancelled or emit returns an error
	// (the client disconnected).
	StreamLogs(ctx context.Context, names []string, emit func(LogFrame) error) error
	// StreamEvents follows instance-state transitions, calling emit for each until
	// ctx is cancelled or emit returns an error (the client disconnected).
	StreamEvents(ctx context.Context, emit func(EventFrame) error) error
	KeepAwake(db string) error // toggle the idle-reaper exemption for db
	Apply(ctx context.Context, db string) error
	Destroy(ctx context.Context, db string) error
	// Reset stops an instance and wipes its data + socket dirs so the next
	// connection re-provisions and re-converges — `doze reset` semantics, NOT
	// Destroy's drop-the-declared-objects sync lifecycle.
	Reset(ctx context.Context, db string) error
	// Resources lists a builtin instance's sub-resources and the data actions it
	// offers; empty (no error) when the engine has no admin capability.
	Resources(ctx context.Context, db string) ([]ResourceView, []ActionView, error)
	// Admin runs a data action on a resource, returning its result line.
	Admin(ctx context.Context, db, action, resource, input string) (string, error)
	// AddInstance decodes and wires a single rendered HCL block into the running
	// stack, returning the new instance's view.
	AddInstance(ctx context.Context, block string) (InstanceView, error)
	// RemoveInstance tears an instance down (optionally wiping its data).
	RemoveInstance(ctx context.Context, name string, wipe bool) error
}

// ViewFromRegistry converts a registry instance into a serializable view.
func ViewFromRegistry(inst registry.Instance, engineType, version string, declared bool) InstanceView {
	return InstanceView{
		Name:         inst.Name,
		Engine:       engineType,
		State:        string(inst.State),
		Version:      version,
		PID:          inst.PID,
		Conns:        inst.Conns,
		StartedAt:    inst.StartedAt,
		IdleSince:    inst.IdleSince,
		LastError:    inst.LastError,
		Tainted:      inst.Tainted,
		Declared:     declared,
		KeepAwake:    inst.KeepAwake,
		RestartCount: inst.RestartCount,
		Healthy:      inst.Healthy,
	}
}
