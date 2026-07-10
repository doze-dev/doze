package doze

import (
	"context"
	"fmt"

	"github.com/doze-dev/doze/internal/control"
)

// backend is the transport the Session speaks to the daemon over. There are two:
// directBackend calls the in-process daemon's Handler with native Go types and
// errors (Serve mode — a library you call), and socketBackend marshals over the
// control socket to a separate daemon process (Attach mode). Same operations;
// the direct path avoids serialization, so typed errors and rich returns survive.
type backend interface {
	status(ctx context.Context) (control.Response, error)
	op(ctx context.Context, op, db string) error
	logs(ctx context.Context, db string) ([]string, error)
	followLogs(ctx context.Context, names []string, emit func(control.LogFrame)) error
	events(ctx context.Context, emit func(control.EventFrame)) error
	add(ctx context.Context, block string) (control.InstanceView, error)
	remove(ctx context.Context, name string, wipe bool) error
	resources(ctx context.Context, db string) ([]control.ResourceView, []control.ActionView, error)
	admin(ctx context.Context, db, action, resource, input string) (string, error)
}

// --- direct (in-process) backend: Serve mode ---

type directBackend struct{ h control.Handler }

func (b directBackend) status(ctx context.Context) (control.Response, error) {
	return b.h.Status(), nil
}

func (b directBackend) op(ctx context.Context, op, db string) error {
	switch op {
	case "up":
		return b.h.Up(ctx, db)
	case "down":
		return b.h.Down(ctx, db)
	case "boot":
		return b.h.Boot(ctx, db)
	case "restart":
		return b.h.Restart(ctx, db)
	case "apply":
		return b.h.Apply(ctx, db)
	case "destroy":
		return b.h.Destroy(ctx, db)
	case "reset":
		return b.h.Reset(ctx, db)
	case "keepawake":
		return b.h.KeepAwake(db)
	default:
		return fmt.Errorf("unknown op %q", op)
	}
}

func (b directBackend) logs(ctx context.Context, db string) ([]string, error) {
	return b.h.Logs(db)
}

func (b directBackend) followLogs(ctx context.Context, names []string, emit func(control.LogFrame)) error {
	return b.h.StreamLogs(ctx, names, func(f control.LogFrame) error {
		emit(f)
		return nil
	})
}

func (b directBackend) events(ctx context.Context, emit func(control.EventFrame)) error {
	return b.h.StreamEvents(ctx, func(f control.EventFrame) error {
		emit(f)
		return nil
	})
}

func (b directBackend) add(ctx context.Context, block string) (control.InstanceView, error) {
	return b.h.AddInstance(ctx, block)
}

func (b directBackend) remove(ctx context.Context, name string, wipe bool) error {
	return b.h.RemoveInstance(ctx, name, wipe)
}

func (b directBackend) resources(ctx context.Context, db string) ([]control.ResourceView, []control.ActionView, error) {
	return b.h.Resources(ctx, db)
}

func (b directBackend) admin(ctx context.Context, db, action, resource, input string) (string, error) {
	return b.h.Admin(ctx, db, action, resource, input)
}

// --- socket backend: Attach mode ---

type socketBackend struct{ c *control.Client }

func (b socketBackend) status(ctx context.Context) (control.Response, error) {
	return b.c.DoContext(ctx, control.Request{Op: "status"})
}

func (b socketBackend) op(ctx context.Context, op, db string) error {
	_, err := b.c.DoContext(ctx, control.Request{Op: op, DB: db})
	return err
}

func (b socketBackend) logs(ctx context.Context, db string) ([]string, error) {
	resp, err := b.c.DoContext(ctx, control.Request{Op: "logs", DB: db})
	if err != nil {
		return nil, err
	}
	return resp.Lines, nil
}

func (b socketBackend) followLogs(ctx context.Context, names []string, emit func(control.LogFrame)) error {
	return b.c.Stream(ctx, control.Request{Op: "logs", Follow: true, Names: names}, emit)
}

func (b socketBackend) events(ctx context.Context, emit func(control.EventFrame)) error {
	return b.c.StreamEvents(ctx, emit)
}

func (b socketBackend) add(ctx context.Context, block string) (control.InstanceView, error) {
	return b.c.Add(ctx, block)
}

func (b socketBackend) remove(ctx context.Context, name string, wipe bool) error {
	return b.c.Remove(ctx, name, wipe)
}

func (b socketBackend) resources(ctx context.Context, db string) ([]control.ResourceView, []control.ActionView, error) {
	resp, err := b.c.DoContext(ctx, control.Request{Op: "resources", DB: db})
	if err != nil {
		return nil, nil, err
	}
	return resp.Resources, resp.Actions, nil
}

func (b socketBackend) admin(ctx context.Context, db, action, resource, input string) (string, error) {
	resp, err := b.c.DoContext(ctx, control.Request{Op: "admin", DB: db, Action: action, Resource: resource, Input: input})
	if err != nil {
		return "", err
	}
	return resp.Result, nil
}
