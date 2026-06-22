package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	return &cobra.Command{
		Use:   "start",
		Short: "Start the daemon in the background",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if daemonRunning(cfg) {
				fmt.Println("doze is already running")
				return nil
			}
			if err := startDaemon(cfg); err != nil {
				return err
			}
			fmt.Printf("doze started, listening on %s\n", cfg.Listen)
			return nil
		},
	}
}

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the background daemon (reaping all backends)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			pid := daemonPid(cfg)
			if pid == 0 {
				fmt.Println("doze is not running")
				return nil
			}
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("signalling daemon: %w", err)
			}
			// Wait for it to exit.
			for i := 0; i < 100; i++ {
				if !processAlive(pid) {
					fmt.Println("doze stopped")
					return nil
				}
				time.Sleep(100 * time.Millisecond)
			}
			return fmt.Errorf("daemon (pid %d) did not stop", pid)
		},
	}
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
					return fmt.Errorf("doze is not running; start it with `doze start`")
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
		Use:   "logs [db]",
		Short: "Show daemon logs, or a database's backend logs",
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
					return fmt.Errorf("doze is not running; start it with `doze start`")
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

// startDaemon launches `doze serve` as a detached background process whose
// output is redirected to the daemon log file, then waits for it to come up.
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

	c := exec.Command(self, "serve", "--config", absConfig)
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
