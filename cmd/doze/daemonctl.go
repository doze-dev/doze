package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	"github.com/doze-dev/doze/internal/ui"
)

// bootInstances ensures the daemon is up once, then boots each target. With more
// than one (i.e. --all) it is best-effort: a failing instance is reported but
// doesn't stop the rest.
func bootInstances(cfg *config.Config, names []string) error {
	if !daemonRunning(cfg) {
		if err := startDaemon(cfg); err != nil {
			return err
		}
	}
	var failed []string
	for _, name := range names {
		if err := bootInstance(cfg, name); err != nil {
			if len(names) == 1 {
				return err
			}
			failed = append(failed, name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed to start: %s", strings.Join(failed, ", "))
	}
	return nil
}

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
	for i := 0; i < 100; i++ {
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

// bootInstance starts one instance's backend now (the old `boot`): ensure the
// daemon is up so the backend is held, then boot it via the control socket.
func bootInstance(cfg *config.Config, name string) error {
	if cfg.Lookup(name) == nil {
		return instanceNotFound(cfg, name)
	}
	if !daemonRunning(cfg) {
		if err := startDaemon(cfg); err != nil {
			return err
		}
	}
	client := control.NewClient(daemon.ControlSocketPath(cfg))
	fmt.Print(ui.Muted("› booting "+name+"…") + "\r")
	if _, err := client.Do(control.Request{Op: "boot", DB: name}); err != nil {
		fmt.Println(ui.Fail("✗") + " " + name + ": " + err.Error())
		return err
	}
	fmt.Println(ui.OK("✓") + " booted " + name + "    ")
	return nil
}

// stopByPidFile sends SIGINT (fast shutdown) to a backend identified by its data
// dir's postmaster.pid, when no daemon is available to do it for us.
func stopByPidFile(cfg *config.Config, name string) (bool, error) {
	pidPath := filepath.Join(cfg.ClusterDir(name), "postmaster.pid")
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	line := strings.SplitN(string(raw), "\n", 2)[0]
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 0 {
		return false, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, nil
	}
	if err := proc.Signal(syscall.SIGINT); err != nil {
		return false, nil
	}
	return true, nil
}

func logsCmd() *cobra.Command {
	var follow, daemonLog bool
	cmd := &cobra.Command{
		Use:   "logs [service]",
		Short: "Show the logs of your services (databases, caches, queues, processes)",
		Long: "logs shows the output of your running services — the engine backends and\n" +
			"your processes, never doze's own supervisor chatter. With no service named\n" +
			"it aggregates them all, each line prefixed with its instance; name one to\n" +
			"see just that service's raw output. -f follows. --daemon shows doze's own\n" +
			"operational log instead (booting/reaping/listeners) — for debugging doze.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			// doze's own supervisor log is opt-in — it's not a service log.
			if daemonLog {
				if len(args) == 1 {
					return fmt.Errorf("--daemon shows doze's own log; drop the service name")
				}
				return tailFile(daemon.LogFilePath(cfg), follow)
			}

			client := control.NewClient(daemon.ControlSocketPath(cfg))
			if !client.Available() {
				return fmt.Errorf("no services are running — `doze up` first (or `doze logs --daemon` for doze's own log)")
			}

			// A single named service: its raw output, optionally followed.
			if len(args) == 1 {
				if cfg.Lookup(args[0]) == nil {
					return instanceNotFound(cfg, args[0])
				}
				if follow {
					ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
					defer stop()
					return client.Stream(ctx, control.Request{Op: "logs", Follow: true, DB: args[0]},
						func(f control.LogFrame) { fmt.Println(f.Line) })
				}
				resp, err := client.Do(control.Request{Op: "logs", DB: args[0]})
				if err != nil {
					return err
				}
				for _, line := range resp.Lines {
					fmt.Println(line)
				}
				return nil
			}

			// Every service, each line prefixed with its instance.
			if follow {
				ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				return client.Stream(ctx, control.Request{Op: "logs", Follow: true}, printLogLine)
			}
			printed := 0
			for _, d := range cfg.Instances {
				resp, err := client.Do(control.Request{Op: "logs", DB: d.Name})
				if err != nil {
					continue // not running — skip silently
				}
				for _, line := range resp.Lines {
					printLogLine(control.LogFrame{Instance: d.Name, Line: line})
					printed++
				}
			}
			if printed == 0 {
				fmt.Println(ui.Muted("no service logs yet — `doze up` to start them, or `doze logs -f` to follow"))
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow the logs (like tail -f)")
	cmd.Flags().BoolVar(&daemonLog, "daemon", false, "show doze's own supervisor log instead (for debugging doze)")
	return cmd
}

// --- helpers ---

// startDaemon launches the daemon (`doze start --foreground`) as a detached
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

func tailFile(path string, follow bool) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no daemon log yet at %s", path)
		}
		return err
	}
	defer f.Close()
	// io.Copy reads to EOF and stops; the *os.File keeps its offset, so on the
	// next pass it resumes from where new bytes were appended.
	if _, err := io.Copy(os.Stdout, f); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	for {
		time.Sleep(500 * time.Millisecond)
		if _, err := io.Copy(os.Stdout, f); err != nil {
			return err
		}
	}
}
