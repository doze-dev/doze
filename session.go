package doze

import (
	"context"
	"fmt"

	"github.com/doze-dev/doze/internal/control"
)

// StackName returns the config's stack name.
func (s *Session) StackName() string { return s.cfg.Stack() }

// Services returns the names of every declared instance, in declaration order.
func (s *Session) Services() []string {
	out := make([]string, 0, len(s.cfg.Instances))
	for _, d := range s.cfg.Instances {
		out = append(out, d.Name)
	}
	return out
}

// Up converges declared structure and boots the given services (or every
// enabled service when none are named) in dependency order, gating on each
// health probe. It returns once everything is up; the daemon keeps supervising.
func (s *Session) Up(ctx context.Context, names ...string) error {
	targets := names
	if len(targets) == 0 {
		for _, d := range s.cfg.Instances {
			if d.Enabled {
				targets = append(targets, d.Name)
			}
		}
	} else {
		for _, n := range targets {
			if s.cfg.Lookup(n) == nil {
				return fmt.Errorf("no service named %q", n)
			}
		}
	}
	for _, name := range s.bootClosure(targets) {
		if _, err := s.client.DoContext(ctx, control.Request{Op: "up", DB: name}); err != nil {
			return fmt.Errorf("bringing up %q: %w", name, err)
		}
	}
	return nil
}

// Boot wakes a single service (and its dependencies) without the full converge
// semantics of Up. An empty name boots the whole stack.
func (s *Session) Boot(ctx context.Context, name string) error {
	return s.op(ctx, "boot", name)
}

// Down stops the named service, or the whole stack when name is empty. Data is
// preserved.
func (s *Session) Down(ctx context.Context, name string) error {
	return s.op(ctx, "down", name)
}

// Restart stops and re-boots the named service (or the whole stack).
func (s *Session) Restart(ctx context.Context, name string) error {
	return s.op(ctx, "restart", name)
}

// Apply converges declared structure for the named service (or all) without
// changing its running state.
func (s *Session) Apply(ctx context.Context, name string) error {
	return s.op(ctx, "apply", name)
}

// Destroy drops the declared objects (databases, buckets, …) for the named
// service — the `doze destroy` lifecycle. Irreversible.
func (s *Session) Destroy(ctx context.Context, name string) error {
	return s.op(ctx, "destroy", name)
}

// Reset stops the named service and wipes its data + sockets so the next
// connection re-provisions and re-converges. Irreversible.
func (s *Session) Reset(ctx context.Context, name string) error {
	return s.op(ctx, "reset", name)
}

func (s *Session) op(ctx context.Context, name, db string) error {
	_, err := s.client.DoContext(ctx, control.Request{Op: name, DB: db})
	return err
}

// Status returns a snapshot of every service's live state.
func (s *Session) Status(ctx context.Context) ([]Instance, error) {
	resp, err := s.client.DoContext(ctx, control.Request{Op: "status"})
	if err != nil {
		return nil, err
	}
	out := make([]Instance, 0, len(resp.Instances))
	for _, v := range resp.Instances {
		out = append(out, instanceFromView(v))
	}
	return out, nil
}

// Instance returns the state of one service, and whether it was found.
func (s *Session) Instance(ctx context.Context, name string) (Instance, bool, error) {
	all, err := s.Status(ctx)
	if err != nil {
		return Instance{}, false, err
	}
	for _, in := range all {
		if in.Name == name {
			return in, true, nil
		}
	}
	return Instance{}, false, nil
}

// Endpoints maps each running service's name to its client-facing address.
func (s *Session) Endpoints(ctx context.Context) (map[string]string, error) {
	all, err := s.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, in := range all {
		if in.Endpoint != "" {
			out[in.Name] = in.Endpoint
		}
	}
	return out, nil
}

// Env maps each service's conventional environment variable to its connection
// string — the same pairs `doze env` prints.
func (s *Session) Env(ctx context.Context) (map[string]string, error) {
	all, err := s.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, in := range all {
		if in.EnvVar != "" && in.URL != "" {
			out[in.EnvVar] = in.URL
		}
	}
	return out, nil
}

// Logs returns the buffered log lines for a service.
func (s *Session) Logs(ctx context.Context, name string) ([]string, error) {
	resp, err := s.client.DoContext(ctx, control.Request{Op: "logs", DB: name})
	if err != nil {
		return nil, err
	}
	return resp.Lines, nil
}

// FollowLogs streams log lines for the named services (empty = all running
// backends), calling emit for each until ctx is cancelled.
func (s *Session) FollowLogs(ctx context.Context, names []string, emit func(instance, line string)) error {
	return s.client.Stream(ctx, control.Request{Op: "logs", Follow: true, Names: names}, func(f control.LogFrame) {
		emit(f.Instance, f.Line)
	})
}

// Events streams instance-state transitions, calling emit for each until ctx is
// cancelled.
func (s *Session) Events(ctx context.Context, emit func(Instance)) error {
	return s.client.StreamEvents(ctx, func(f control.EventFrame) {
		emit(instanceFromView(f.Instance))
	})
}

// Close releases the session's engine host (reaping plugin processes). In Serve
// mode it also stops the in-process daemon; in Attach mode the background daemon
// keeps running (use Shutdown to stop it).
func (s *Session) Close() error {
	if s.served != nil {
		s.serveCancel()
		<-s.serveDone
		s.served = nil
	}
	return s.host.Close()
}

// Shutdown stops the daemon and then closes the session. In Serve mode it stops
// the in-process daemon; in Attach mode it stops the background daemon.
func (s *Session) Shutdown(ctx context.Context) error {
	if s.served != nil {
		return s.Close()
	}
	_, err := s.client.DoContext(ctx, control.Request{Op: "down"})
	// A down op reaps backends but leaves the daemon; stopping the daemon itself
	// is the CLI's `doze down`/stop path. Close releases our host regardless.
	cerr := s.host.Close()
	if err != nil {
		return err
	}
	return cerr
}

// bootClosure returns targets plus their transitive dependencies in
// boot-before-dependents order (mirrors the CLI's up walk).
func (s *Session) bootClosure(targets []string) []string {
	visited := map[string]bool{}
	var order []string
	var visit func(string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		decl := s.cfg.Lookup(name)
		if decl == nil {
			return
		}
		for _, dep := range decl.Deps {
			visit(dep.Name)
		}
		order = append(order, name)
	}
	for _, t := range targets {
		visit(t)
	}
	return order
}

func instanceFromView(v control.InstanceView) Instance {
	return Instance{
		Name:      v.Name,
		Engine:    v.Engine,
		Version:   v.Version,
		State:     v.State,
		PID:       v.PID,
		Conns:     v.Conns,
		StartedAt: v.StartedAt,
		IdleSince: v.IdleSince,
		Endpoint:  v.Endpoint,
		Domain:    v.Domain,
		URL:       v.URL,
		EnvVar:    v.EnvVar,
		Resource:  v.Resource,
		DataDir:   v.DataDir,
		Declared:  v.Declared,
		Disabled:  v.Disabled,
		KeepAwake: v.KeepAwake,
		Tainted:   v.Tainted,
		LastError: v.LastError,
		Healthy:   v.Healthy,
	}
}
