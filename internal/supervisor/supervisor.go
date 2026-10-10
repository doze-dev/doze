// Package supervisor runs and supervises a single backend process generically:
// it spawns a prepared command in its own process group, hands its output to
// a log sink, and reaps it cleanly with escalating signals. Engine-specific
// concerns — which binary to run, the readiness probe, and stale-lock files —
// live in the engine drivers. A *Process satisfies engine.Process.
package supervisor

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// waitDelay bounds how long cmd.Wait lingers for stdout/stderr to drain after the
// process itself has exited, before closing the pipes and returning. It exists so
// orphaned grandchildren holding the log pipe can't wedge exit detection.
const waitDelay = 3 * time.Second

// Process is a handle to one running backend process.
type Process struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	tail      func() []string
	done      chan struct{}
	exit      error
	groupKill bool // signal the whole process group on Stop (see StartTree)
}

// Sink is where a process's output goes. The supervisor keeps none of it: the
// writers are the log store's, and Tail reads recent lines back from there.
// A zero Sink discards the output.
type Sink struct {
	Stdout io.Writer
	Stderr io.Writer
	// Tail returns the most recent lines, for a readiness probe that watches
	// the log and for the message when a start fails.
	Tail func() []string
}

// Start spawns cmd in its own process group with stdout/stderr connected to
// sink, and returns a running handle. It does NOT block on readiness — drivers
// probe that themselves via WaitReady. cmd must not have been started yet; its
// Stdout/Stderr/SysProcAttr are set here.
func Start(cmd *exec.Cmd, sink Sink) (*Process, error) {
	p := &Process{tail: sink.Tail}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if sink.Stdout != nil {
		cmd.Stdout = sink.Stdout
	}
	if sink.Stderr != nil {
		cmd.Stderr = sink.Stderr
	}
	// Own process group so a terminal Ctrl+C on the daemon doesn't hit the
	// backend directly; on Linux also ask the kernel to signal it if the daemon
	// dies, so a crash never orphans a backend.
	cmd.SysProcAttr = sysProcAttr()
	// Bound the post-exit I/O wait. cmd.Wait blocks until the stdout/stderr pipes
	// are fully closed, which means until every process that inherited them exits —
	// not just our direct child. A backend that forks children (Postgres spawns
	// background workers like the pg_cron launcher) can be killed harshly, leaving
	// orphans that hold the pipe open; without WaitDelay, Wait would hang forever
	// and the supervisor would never notice the process died (so a Composite would
	// never tear down its siblings). WaitDelay makes Wait return shortly after the
	// process itself exits, closing the pipes out from under any lingering orphans.
	cmd.WaitDelay = waitDelay

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p.cmd = cmd
	p.done = make(chan struct{})
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.exit = err
		close(p.done)
		p.mu.Unlock()
	}()
	return p, nil
}

// StartTree is like Start but Stop signals the whole process group, so a command
// that forks children — a build tool, a shell pipeline, `go run` (which runs the
// compiled binary as a child) — is reaped as a tree instead of leaving orphans
// that hold the log pipe open and wedge shutdown. Use it for supervised app
// processes; backends that choreograph their own children (e.g. Postgres) use
// Start so their shutdown signalling is not disturbed.
func StartTree(cmd *exec.Cmd, sink Sink) (*Process, error) {
	p, err := Start(cmd, sink)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.groupKill = true
	p.mu.Unlock()
	return p, nil
}

// PID returns the running process id, or 0.
func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Pid
	}
	return 0
}

// Logs returns the most recent log lines, from the sink.
func (p *Process) Logs() []string {
	if p.tail == nil {
		return nil
	}
	return p.tail()
}

// Alive reports whether the process is currently running.
func (p *Process) Alive() bool {
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()
	if done == nil {
		return false
	}
	select {
	case <-done:
		return false
	default:
		return true
	}
}

// Stop performs a fast, clean shutdown: SIGINT, escalating to SIGQUIT and
// finally SIGKILL if the process refuses to leave.
func (p *Process) Stop(ctx context.Context) error {
	p.mu.Lock()
	cmd, done, groupKill := p.cmd, p.done, p.groupKill
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// signal delivers sig to just the process, or to its whole group (negative pid,
	// which the Setpgid in sysProcAttr makes equal to the leader's pid) so children
	// are reaped too.
	signal := func(sig syscall.Signal) {
		if groupKill {
			if err := syscall.Kill(-cmd.Process.Pid, sig); err == nil {
				return
			}
		}
		_ = cmd.Process.Signal(sig)
	}
	signals := []struct {
		sig   syscall.Signal
		grace time.Duration
	}{
		{syscall.SIGINT, 5 * time.Second},
		{syscall.SIGQUIT, 3 * time.Second},
		{syscall.SIGKILL, 2 * time.Second},
	}
	for _, step := range signals {
		signal(step.sig)
		select {
		case <-done:
			return nil
		case <-time.After(step.grace):
			// escalate
		case <-ctx.Done():
			// The budget is gone, but returning with the process alive orphans
			// it (the caller is often a daemon about to exit). SIGKILL is not
			// ignorable — deliver it before giving up.
			signal(syscall.SIGKILL)
			select {
			case <-done:
				return nil
			case <-time.After(2 * time.Second):
				return ctx.Err()
			}
		}
	}
	return errors.New("backend did not exit after SIGKILL")
}

// Wait blocks until the process exits and returns its exit error.
func (p *Process) Wait() error {
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()
	if done == nil {
		return nil
	}
	<-done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exit
}

// ProcessAlive reports whether pid refers to a live process, using signal 0.
// Drivers use it for stale-lock detection.
func ProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		return true
	} else if errors.Is(err, syscall.EPERM) {
		return true // exists but we can't signal it
	}
	return false
}
