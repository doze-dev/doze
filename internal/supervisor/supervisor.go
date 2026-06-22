// Package supervisor runs and supervises a single backend process generically:
// it spawns a prepared command in its own process group, captures recent log
// output, and reaps it cleanly with escalating signals. Engine-specific
// concerns — which binary to run, the readiness probe, and stale-lock files —
// live in the engine drivers. A *Process satisfies engine.Process.
package supervisor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Process is a handle to one running backend process.
type Process struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	logs *ring
	done chan struct{}
	exit error
}

// Start spawns cmd in its own process group with stdout/stderr captured into a
// bounded log ring, and returns a running handle. It does NOT block on
// readiness — drivers probe that themselves via WaitReady. cmd must not have
// been started yet; its Stdout/Stderr/SysProcAttr are set here.
func Start(cmd *exec.Cmd) (*Process, error) {
	p := &Process{logs: newRing(200)}
	cmd.Stdout = p.logs
	cmd.Stderr = p.logs
	// Own process group so a terminal Ctrl+C on the daemon doesn't hit the
	// backend directly; on Linux also ask the kernel to signal it if the daemon
	// dies, so a crash never orphans a backend.
	cmd.SysProcAttr = sysProcAttr()

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

// PID returns the running process id, or 0.
func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Pid
	}
	return 0
}

// Logs returns the most recent backend log lines.
func (p *Process) Logs() []string { return p.logs.lines() }

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
	cmd, done := p.cmd, p.done
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
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
		_ = cmd.Process.Signal(step.sig)
		select {
		case <-done:
			return nil
		case <-time.After(step.grace):
			// escalate
		case <-ctx.Done():
			return ctx.Err()
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
