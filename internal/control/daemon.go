package control

import (
	"context"
	"time"
)

// Daemon is what a screen needs from a doze daemon. The CLI and the dash
// depend on this, not on a socket: the same code runs against a live daemon
// and against a scripted one (fakedaemon), which is what makes their output
// testable byte for byte.
type Daemon interface {
	// Snapshot is the stack right now, with each running instance's meters.
	Snapshot(ctx context.Context) (Snapshot, error)
	// Subscribe streams topic frames to on until the daemon ends the stream,
	// on returns an error, or ctx is done.
	Subscribe(ctx context.Context, sub Subscription, on func(Frame) error) error
	// Do runs a verb and waits for it.
	Do(ctx context.Context, verb Verb) error
	// Logs reads lines an instance printed. The instance need not be running.
	Logs(ctx context.Context, q LogQuery) ([]LogFrame, error)
}

// Snapshot is the answer to "what is the stack doing".
type Snapshot struct {
	Listen      string
	IdleTimeout time.Duration
	Instances   []InstanceView
}

// The verbs. Each is also the name of its control op.
const (
	VerbUp      = "up"      // converge and wake; an empty target is the whole stack
	VerbWake    = "wake"    // start now, with dependencies
	VerbSleep   = "sleep"   // stop, keeping data; an empty target is everything awake
	VerbRestart = "restart" // stop and start
	VerbReset   = "reset"   // stop, wipe the data, provision fresh on the next wake
	VerbTask    = "task"    // run a task now, whatever its state
	VerbPin     = "pin"     // toggle: keep awake past the idle timeout
	VerbSync    = "apply"   // reconcile declared structure
)

// Verb is one thing to do to one instance.
type Verb struct {
	Name   string
	Target string
}

// LogQuery says which lines to read.
type LogQuery struct {
	Instance string
	// Run picks the run: 0 is the newest, -1 the one before, a positive number
	// a run by its id.
	Run int
	// Tail is how many lines from the end (zero = the default).
	Tail int
	// Since drops lines older than this (zero = keep all).
	Since time.Time
}

// socketDaemon is the Daemon behind a control socket.
type socketDaemon struct{ c *Client }

// Daemon returns the daemon behind this client's socket.
func (c *Client) Daemon() Daemon { return socketDaemon{c} }

func (d socketDaemon) Snapshot(ctx context.Context) (Snapshot, error) {
	resp, err := d.c.DoContext(ctx, Request{Op: "status"})
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Listen: resp.Listen, IdleTimeout: resp.IdleTimeout, Instances: resp.Instances}, nil
}

func (d socketDaemon) Subscribe(ctx context.Context, sub Subscription, on func(Frame) error) error {
	cn, err := d.c.Dial(ctx)
	if err != nil {
		return err
	}
	defer cn.Close()
	return cn.Subscribe(ctx, sub, on)
}

func (d socketDaemon) Do(ctx context.Context, verb Verb) error {
	_, err := d.c.DoContext(ctx, Request{Op: verb.Name, DB: verb.Target})
	return err
}

func (d socketDaemon) Logs(ctx context.Context, q LogQuery) ([]LogFrame, error) {
	resp, err := d.c.DoContext(ctx, Request{Op: "logs", DB: q.Instance, Run: q.Run, Tail: q.Tail, SinceTime: q.Since})
	if err != nil {
		return nil, err
	}
	return resp.Log, nil
}
