// Package daemonctl is the client-side lifecycle of the background doze daemon:
// spawn it (the detached `doze __daemon` self-exec), stop it, and probe whether
// it's running. The logic lived in cmd/doze; it moved here so the embeddable
// facade's Attach (auto-spawn) and Shutdown paths drive the daemon exactly the
// way the CLI does. Printing stays with the callers — these functions return
// values and errors, they don't render.
package daemonctl

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
)

// Running reports whether a daemon is accepting control connections for cfg.
func Running(cfg *config.Config) bool {
	return control.NewClient(daemon.ControlSocketPath(cfg)).Available()
}

// Pid returns the running daemon's pid, or 0 if none is alive.
func Pid(cfg *config.Config) int {
	raw, err := os.ReadFile(daemon.PidFilePath(cfg))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0]))
	if err != nil || pid <= 0 || !Alive(pid) {
		return 0
	}
	return pid
}

// Alive reports whether a process with the given pid exists.
func Alive(pid int) bool {
	if err := syscall.Kill(pid, 0); err == nil {
		return true
	} else {
		return errors.Is(err, syscall.EPERM)
	}
}

// Start launches the daemon (the hidden `doze __daemon` self-exec) as a detached
// background process whose output is redirected to the daemon log file, then
// waits for its control socket to accept. self is the doze executable path and
// absConfigPath the resolved --config path. On a startup failure it returns an
// error whose text includes the tail of the daemon log.
func Start(cfg *config.Config, self, absConfigPath string) error {
	if err := os.MkdirAll(cfg.RunDir(), 0o700); err != nil {
		return err
	}
	logPath := daemon.LogFilePath(cfg)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	c := exec.Command(self, "__daemon", "--config", absConfigPath)
	c.Stdout = logFile
	c.Stderr = logFile
	c.Stdin = nil
	// New session so it survives this process exiting.
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}
	_ = c.Process.Release()

	// Wait for the control socket to accept connections. If it never does, the
	// daemon likely failed to bind a port — surface the real reason from the log
	// instead of a bare "did not come up".
	client := control.NewClient(daemon.ControlSocketPath(cfg))
	for i := 0; i < 100; i++ {
		if client.Available() {
			return nil
		}
		if !Alive(c.Process.Pid) {
			return fmt.Errorf("daemon exited during startup:\n%s", TailLog(cfg, 15))
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not come up within 10s; recent log:\n%s\n(full log: %s)", TailLog(cfg, 15), logPath)
}

// Stop signals the daemon to shut down (which reaps every backend) and waits up
// to ~20s for it to exit. It returns (false, nil) when no daemon was running,
// (true, nil) on a clean stop, and an error if signalling failed or the daemon
// did not exit in time.
func Stop(cfg *config.Config) (bool, error) {
	pid := Pid(cfg)
	if pid == 0 {
		return false, nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return false, fmt.Errorf("signalling daemon: %w", err)
	}
	// The daemon's shutdown reap is budgeted at 15s (worst case: a backend that
	// ignores everything until SIGKILL); wait past that so a slow-but-clean
	// shutdown isn't misreported as a failure.
	for i := 0; i < 200; i++ {
		if !Alive(pid) {
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false, fmt.Errorf("daemon (pid %d) did not stop", pid)
}

// TailLog returns the last n lines of the daemon log, for surfacing failures.
func TailLog(cfg *config.Config, n int) string {
	data, err := os.ReadFile(daemon.LogFilePath(cfg))
	if err != nil {
		return "(no log available)"
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
