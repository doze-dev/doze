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

	"github.com/doze-dev/doze/internal/journal"
	"github.com/doze-dev/doze/internal/registry"
)

// Request is a command sent from the CLI to the daemon.
type Request struct {
	// Hello opens a connection; it is the whole of the first frame.
	Hello *Hello `json:"hello,omitempty"`
	// ID is chosen by the client and echoed on every frame that answers this
	// request, so one connection can carry several at once.
	ID uint64 `json:"id,omitempty"`

	Op string `json:"op,omitempty"` // "status", "up", "down", "subscribe", "cancel", …
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

	// The subscribe op: which topics, and how (see Subscription).
	Topics   []string `json:"topics,omitempty"`
	Since    uint64   `json:"since,omitempty"`
	Interval string   `json:"interval,omitempty"` // a duration, e.g. "1s"
	Backlog  int      `json:"backlog,omitempty"`
	// Panel and Selector scope the "console" topic: which stream panel to run
	// and which row of its table it follows.
	Panel    string `json:"panel,omitempty"`
	Selector string `json:"selector,omitempty"`

	// The logs op (see LogQuery): which run, how many lines, and from when.
	Run       int       `json:"run,omitempty"`
	Tail      int       `json:"tail,omitempty"`
	SinceTime time.Time `json:"since_time,omitzero"`
}

// LogFrame is one streamed, instance-tagged log line (logs follow op).
type LogFrame struct {
	Instance string `json:"instance"`
	Line     string `json:"line"`
	// Stream is "o" (stdout) or "e" (stderr), followed by ":<part>" for one
	// process of an instance that runs several, or "s:<name>" for a step.
	Stream string `json:"stream,omitempty"`
	// Run numbers the start of the instance this line belongs to.
	Run  int       `json:"run,omitempty"`
	Time time.Time `json:"time,omitempty"`
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

// HolderView is one reason an instance is awake: a running instance that
// depends on it ("dependant", with Name), a pin ("pin"), or open client
// connections ("connection", with Count).
type HolderView struct {
	Kind  string `json:"kind"`
	Name  string `json:"name,omitempty"`
	Count int    `json:"count,omitempty"`
}

// InstanceView is a serializable snapshot of one backend's state.
type InstanceView struct {
	Name string `json:"name"`
	// Kind is the block that declared it: "module", "app", "service" or "task".
	Kind string `json:"kind,omitempty"`
	// Task is how a task last ran; nil for anything else, and for a task that
	// has not run.
	Task    *TaskView `json:"task,omitempty"`
	Engine  string    `json:"engine"`
	State   string    `json:"state"`
	Version string    `json:"version"`
	PID     int       `json:"pid"`
	Conns   int       `json:"conns"`
	// Holders is why the instance is awake. Conns counts every hold, including
	// the ones dependants take; the "connection" holder counts clients only.
	Holders   []HolderView `json:"holders,omitempty"`
	StartedAt time.Time    `json:"started_at"`
	IdleSince time.Time    `json:"idle_since,omitempty"` // when Conns hit 0; drives the reap countdown
	// IdleAfter is this instance's own idle time, when it has one; zero means the
	// stack's (Response.IdleTimeout).
	IdleAfter time.Duration `json:"idle_after,omitempty"`
	RAM       int64         `json:"ram,omitempty"`      // resident bytes of the backend, 0 when reaped
	CPU       float64       `json:"cpu,omitempty"`      // CPU usage percent (one core = 100), 0 when reaped
	Endpoint  string        `json:"endpoint,omitempty"` // client-facing address
	// Bind is the address the backend/listener actually occupies (host:port or
	// unix:/path) — the dialable truth behind the client-facing Endpoint: the
	// per-instance loopback bind (127.0.0.11:5432) for proxied services, the
	// internal backend behind the shared host for AWS built-ins, the self-bound
	// address for processes.
	Bind   string `json:"bind,omitempty"`
	Domain string `json:"domain,omitempty"` // local DNS name, e.g. "orders-pg.<stack>.doze"
	// NameOnly marks an app that is reachable at its name but not also at
	// 127.0.0.1:<port>, because something else on the machine has that port.
	NameOnly bool `json:"name_only,omitempty"`
	// Console is the URL of the instance's web console, when it has one.
	Console string `json:"console,omitempty"`
	// HasConsole says the module describes a console of tables and streams, and
	// ConsoleFront is the tab it asks to open on ("console", "web" or "").
	HasConsole   bool   `json:"has_console,omitempty"`
	ConsoleFront string `json:"console_front,omitempty"`
	// NameHeld says that a name this instance wanted belongs to another program
	// on the machine, and which: "aws.shop.doze is held by doze-aws (pid 412)".
	NameHeld string `json:"name_held,omitempty"`
	URL      string `json:"url,omitempty"` // full connection string
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
	// RestartMax is the restart budget and NextRestart when the next attempt is
	// due; both are set only while State is "crashing".
	RestartMax  int       `json:"restart_max,omitempty"`
	NextRestart time.Time `json:"next_restart,omitempty"`
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
	Log         []LogFrame     `json:"log,omitempty"`       // the same lines with their time, stream and run
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
	// LogLines reads lines an instance printed, running or not.
	LogLines(q LogQuery) ([]LogFrame, error)
	// StreamLogs follows the named instances' logs (empty = all running backends),
	// calling emit for each new line until ctx is cancelled or emit returns an error
	// (the client disconnected).
	StreamLogs(ctx context.Context, names []string, emit func(LogFrame) error) error
	// StreamEvents follows instance-state transitions, calling emit for each until
	// ctx is cancelled or emit returns an error (the client disconnected).
	StreamEvents(ctx context.Context, emit func(EventFrame) error) error
	// Subscribe streams the topics sub names, calling emit for each frame until
	// ctx is cancelled or emit returns an error. The daemon does the work a
	// topic needs — sampling, following — only while a subscription is open.
	Subscribe(ctx context.Context, sub Subscription, emit func(Frame) error) error
	KeepAwake(db string) error // toggle the idle-reaper exemption for db
	Apply(ctx context.Context, db string) error
	Destroy(ctx context.Context, db string) error
	// Reset stops an instance and wipes its data + socket dirs so the next
	// connection re-provisions and re-converges — `doze reset` semantics, NOT
	// Destroy's drop-the-declared-objects sync lifecycle.
	Reset(ctx context.Context, db string) error
	// RunTask runs a task now, whatever its state, and returns once it is over:
	// nil for done, an error carrying its output for failed.
	RunTask(ctx context.Context, db string) error
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
		IdleAfter:    inst.IdleAfter,
		LastError:    inst.LastError,
		Tainted:      inst.Tainted,
		Declared:     declared,
		KeepAwake:    inst.KeepAwake,
		RestartCount: inst.RestartCount,
		RestartMax:   inst.RestartMax,
		NextRestart:  inst.NextRestart,
		Healthy:      inst.Healthy,
	}
}

// ProtoVersion is the control protocol this build speaks. A connection opens
// with a hello carrying it; a daemon and a client that disagree say so in
// words instead of failing to decode each other.
const ProtoVersion = 2

// Hello is the first frame in each direction.
type Hello struct {
	Proto int `json:"proto"`
	// Client names who is connecting ("cli", "dash", "lib"), for the daemon's log.
	Client string `json:"client,omitempty"`
}

// The topics a client can subscribe to.
const (
	TopicInstances = "instances" // a full snapshot, then each instance as it changes
	TopicStats     = "stats"     // CPU and memory per running instance, on an interval
	TopicJournal   = "journal"   // journal events, from a sequence number on
	TopicLogs      = "logs"      // log lines for chosen instances
	TopicConsole   = "console"   // a module's console: its spec, its tables, a stream
)

// StatView is one running instance's meters, sampled for the stats topic.
type StatView struct {
	Name  string  `json:"name"`
	PID   int     `json:"pid"`
	RAM   int64   `json:"ram"`
	CPU   float64 `json:"cpu"`
	Conns int     `json:"conns"`
	// Children are an app's own subprocesses, each with its meters.
	Children []ProcView `json:"children,omitempty"`
}

// Subscription is what a subscribe request asks for.
type Subscription struct {
	Topics []string
	// Names scopes the logs topic (empty = every declared instance).
	Names []string
	// Since is the journal sequence number to resume after. Zero sends only
	// events from now on; JournalFromStart sends everything kept.
	Since uint64
	// Interval is the stats sampling period (zero = one second).
	Interval time.Duration
	// Backlog is how many recent log lines of each instance to send before
	// following (zero = the default; negative = none).
	Backlog int
	// Panel and Selector scope the console topic: the stream panel to run and
	// the row of its table it follows. Without a Panel only tables are sent.
	Panel    string
	Selector string
}

// JournalFromStart, as Subscription.Since, asks for every journal event kept.
const JournalFromStart = ^uint64(0)

// Frame is one message from the daemon. ID ties it to the request it answers.
// A unary request gets one frame with Response. A stream gets a Response as
// its acknowledgement, then topic frames, then End.
type Frame struct {
	ID       uint64    `json:"id,omitempty"`
	Hello    *Hello    `json:"hello,omitempty"`
	Response *Response `json:"response,omitempty"`
	// Error is set on the frame that refuses a connection (a protocol mismatch).
	Error string `json:"error,omitempty"`

	Topic string `json:"topic,omitempty"`
	// Snapshot marks an instances frame that carries every instance: the first
	// one, and any sent because a subscriber fell behind. Other instances
	// frames carry only what changed.
	Snapshot  bool           `json:"snapshot,omitempty"`
	Instances []InstanceView `json:"instances,omitempty"`
	// Removed names instances that are gone since the last instances frame.
	Removed []string       `json:"removed,omitempty"`
	Stats   []StatView     `json:"stats,omitempty"`
	Event   *journal.Event `json:"event,omitempty"`
	Log     *LogFrame      `json:"log,omitempty"`
	Console *ConsoleFrame  `json:"console,omitempty"`

	// End closes a stream.
	End bool `json:"end,omitempty"`
}

// TaskView is how a task last ran.
type TaskView struct {
	Result   string    `json:"result"` // "done" | "failed"
	ExitCode int       `json:"exit_code,omitempty"`
	TookMS   int64     `json:"took_ms"`
	RanAt    time.Time `json:"ran_at"`
	Why      string    `json:"why,omitempty"`
}

// ConsoleSpec is a module's description of its console, as the dash draws it.
// It mirrors engine.ConsoleSpec, so a surface needs nothing from the SDK.
type ConsoleSpec struct {
	Front  string         `json:"front,omitempty"`
	WebURL string         `json:"web_url,omitempty"`
	Panels []ConsolePanel `json:"panels,omitempty"`
}

// ConsolePanel is one block of a console: a table, a row of counters, or a stream.
type ConsolePanel struct {
	ID      string          `json:"id"`
	Title   string          `json:"title"`
	Kind    string          `json:"kind"` // table | counters | stream
	Source  string          `json:"source,omitempty"`
	Columns []ConsoleColumn `json:"columns,omitempty"`
	Select  string          `json:"select,omitempty"`
}

// ConsoleColumn is one column of a table, one field of a stream row.
type ConsoleColumn struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Align string `json:"align,omitempty"`
	Role  string `json:"role,omitempty"`
}

// ConsoleRow is one row: its cells by column key, and a tone.
type ConsoleRow struct {
	Cells map[string]string `json:"cells"`
	Tone  string            `json:"tone,omitempty"`
}

// ConsoleFrame is one message of the console topic. The first carries the
// spec and the actions; after that a table or counters panel is sent whole
// each time it is read (Rows replaces what the panel showed), and a stream
// panel sends its rows one at a time (Row). State says whether the instance
// is awake: a console never wakes what it is looking at.
type ConsoleFrame struct {
	Instance string       `json:"instance"`
	State    string       `json:"state,omitempty"`
	Spec     *ConsoleSpec `json:"spec,omitempty"`
	Actions  []ActionView `json:"actions,omitempty"`
	Panel    string       `json:"panel,omitempty"`
	Rows     []ConsoleRow `json:"rows,omitempty"`
	Row      *ConsoleRow  `json:"row,omitempty"`
	Error    string       `json:"error,omitempty"`
}
