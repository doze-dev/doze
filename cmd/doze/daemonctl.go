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

	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/control"
	"github.com/nerdmenot/doze/internal/daemon"
	"github.com/nerdmenot/doze/internal/ui"
)

func startCmd() *cobra.Command {
	var foreground, all bool
	cmd := &cobra.Command{
		Use:   "start <instance | --all>",
		Short: "Boot an instance's backend now (or --all)",
		Long: "start boots a backend immediately — warming it up instead of waiting for a\n" +
			"connection. Name an instance, or pass --all to boot every declared one. The\n" +
			"background daemon starts automatically; you don't start it by hand. (Use\n" +
			"--foreground to run the daemon in this terminal for debugging.)",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if foreground {
				if all || len(args) == 1 {
					return fmt.Errorf("--foreground runs the daemon and takes no instance/--all")
				}
				return runDaemonForeground(cfg)
			}
			targets, err := lifecycleTargets(cfg, args, all, "start")
			if err != nil {
				return err
			}
			return bootInstances(cfg, targets)
		},
	}
	cmd.Flags().BoolVarP(&foreground, "foreground", "f", false, "run the daemon in the foreground (debugging)")
	cmd.Flags().BoolVar(&all, "all", false, "boot every declared instance")
	return cmd
}

func stopCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "stop <instance | --all>",
		Short: "Reap an instance's backend (or --all to stop everything)",
		Long: "stop reaps a backend. Name an instance to reap just that one (the daemon\n" +
			"keeps running and the next connection re-boots it). Pass --all to stop\n" +
			"everything — every backend and the background daemon itself.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if all {
				if len(args) == 1 {
					return fmt.Errorf("give an instance or --all, not both")
				}
				return stopDaemon(cfg) // full shutdown: the daemon exit reaps every backend
			}
			if len(args) == 1 {
				return stopInstance(cfg, args[0])
			}
			return fmt.Errorf("specify an instance (e.g. `doze stop app`) or --all to stop everything")
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "stop the daemon and reap every backend")
	return cmd
}

// lifecycleTargets resolves a start/stop invocation to the instance names to act
// on: the named instance, every instance with --all, or a helpful error when
// neither is given (so the verb never silently acts on "the daemon").
func lifecycleTargets(cfg *config.Config, args []string, all bool, verb string) ([]string, error) {
	switch {
	case all && len(args) == 1:
		return nil, fmt.Errorf("give an instance or --all, not both")
	case all:
		names := make([]string, 0, len(cfg.Instances))
		for _, d := range cfg.Instances {
			names = append(names, d.Name)
		}
		return names, nil
	case len(args) == 1:
		if cfg.Lookup(args[0]) == nil {
			return nil, instanceNotFound(cfg, args[0])
		}
		return []string{args[0]}, nil
	default:
		return nil, fmt.Errorf("specify an instance (e.g. `doze %s app`) or --all to %s everything", verb, verb)
	}
}

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

// stopInstance reaps one instance's backend (the old `down <instance>`): via the
// daemon when it's up, otherwise by signalling the backend's pidfile directly.
func stopInstance(cfg *config.Config, name string) error {
	if cfg.Lookup(name) == nil {
		return instanceNotFound(cfg, name)
	}
	client := control.NewClient(daemon.ControlSocketPath(cfg))
	if client.Available() {
		if _, err := client.Do(control.Request{Op: "down", DB: name}); err != nil {
			return err
		}
		fmt.Println("stopped", name)
		return nil
	}
	ok, err := stopByPidFile(cfg, name)
	if err != nil {
		return err
	}
	if ok {
		fmt.Println("stopped", name)
	} else {
		fmt.Println(name, "is not running")
	}
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

func restartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart [instance]",
		Short: "Restart the daemon, or a single instance",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			// Restart one instance (reap + re-boot) through the daemon.
			if len(args) == 1 {
				name := args[0]
				if cfg.Lookup(name) == nil {
					return instanceNotFound(cfg, name)
				}
				client := control.NewClient(daemon.ControlSocketPath(cfg))
				if !client.Available() {
					return fmt.Errorf("the daemon is not running; boot an instance first (e.g. `doze start <instance>` or `doze run`)")
				}
				if _, err := client.Do(control.Request{Op: "restart", DB: name}); err != nil {
					return err
				}
				fmt.Println(ui.OK("✓") + " restarted " + name)
				return nil
			}
			// Restart the daemon itself.
			if pid := daemonPid(cfg); pid != 0 {
				_ = syscall.Kill(pid, syscall.SIGTERM)
				for i := 0; i < 100 && processAlive(pid); i++ {
					time.Sleep(100 * time.Millisecond)
				}
			}
			if err := startDaemon(cfg); err != nil {
				return err
			}
			fmt.Printf("doze restarted, listening on %s\n", cfg.Listen)
			return nil
		},
	}
}

func logsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs [instance]",
		Short: "Show daemon logs, or an instance's backend logs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			// Backend logs for a specific database come from the daemon.
			if len(args) == 1 {
				client := control.NewClient(daemon.ControlSocketPath(cfg))
				if !client.Available() {
					return fmt.Errorf("the daemon is not running; boot an instance first (e.g. `doze start <instance>` or `doze run`)")
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
			// Otherwise tail the daemon's own log file.
			return tailFile(daemon.LogFilePath(cfg), follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow the log (like tail -f)")
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

	c := exec.Command(self, "start", "--foreground", "--config", absConfig)
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
