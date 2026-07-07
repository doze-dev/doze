package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
)

// stopDaemon shuts the background daemon down (which reaps every backend).
func stopDaemon(cfg *config.Config) error {
	pid := daemonPid(cfg)
	if pid == 0 {
		fmt.Println("doze is not running")
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signalling daemon: %w", err)
	}
	// The daemon's shutdown reap is budgeted at 15s (worst case: a backend that
	// ignores everything until SIGKILL); wait past that so a slow-but-clean
	// shutdown isn't misreported as a failure.
	for i := 0; i < 200; i++ {
		if !processAlive(pid) {
			fmt.Println("doze stopped")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon (pid %d) did not stop", pid)
}

// daemonCmd is the hidden entry point startDaemon re-execs to run the background
// daemon (proxy listeners, idle reaper, control socket). Users never call it; the
// lifecycle commands spawn it automatically.
func daemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__daemon",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runDaemonForeground(cfg)
		},
	}
}

// runDaemonForeground runs the listener daemon in this terminal (the old
// `serve`): proxy listeners, idle reaper, and control socket, until SIGINT/TERM.
func runDaemonForeground(cfg *config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	d, err := daemon.New(cfg, stderrLogger)
	if err != nil {
		return err
	}
	return d.Run(ctx)
}

// --- helpers ---

// startDaemon launches the daemon (the hidden `doze __daemon` self-exec) as a detached
// background process whose output is redirected to the daemon log file, then
// waits for it to come up.
func startDaemon(cfg *config.Config) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.RunDir(), 0o700); err != nil {
		return err
	}
	logPath := daemon.LogFilePath(cfg)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	c := exec.Command(self, "__daemon", "--config", absConfig)
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
		if !processAlive(c.Process.Pid) {
			return fmt.Errorf("daemon exited during startup:\n%s", tailLines(logPath, 15))
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not come up within 10s; recent log:\n%s\n(full log: %s)", tailLines(logPath, 15), logPath)
}

// tailLines returns the last n lines of a file, for surfacing startup failures.
func tailLines(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "(no log available)"
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func daemonRunning(cfg *config.Config) bool {
	return control.NewClient(daemon.ControlSocketPath(cfg)).Available()
}

func daemonPid(cfg *config.Config) int {
	raw, err := os.ReadFile(daemon.PidFilePath(cfg))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0]))
	if err != nil || pid <= 0 || !processAlive(pid) {
		return 0
	}
	return pid
}

func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); err == nil {
		return true
	} else {
		return errors.Is(err, syscall.EPERM)
	}
}
