package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/daemonctl"
	"github.com/doze-dev/doze/internal/ui"
)

// stopDaemon shuts the background daemon down (which reaps every backend). The
// lifecycle lives in internal/daemonctl; this wrapper renders the result.
func stopDaemon(cfg *config.Config) error {
	stopped, err := daemonctl.Stop(cfg)
	if err != nil {
		return err
	}
	if !stopped {
		fmt.Println(ui.Muted("doze is not running"))
		return nil
	}
	fmt.Println(ui.OK("✓") + " doze stopped")
	return nil
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
	d, err := daemon.New(cfg, stderrLogger, activeHost.ConfigHooks())
	if err != nil {
		return err
	}
	return d.Run(ctx)
}

// --- helpers ---

// startDaemon launches the background daemon and waits for it to come up. The
// spawn logic lives in internal/daemonctl (shared with the embeddable facade);
// this wrapper supplies the CLI's executable + resolved config path.
func startDaemon(cfg *config.Config) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	return daemonctl.Start(cfg, self, absConfig)
}

func daemonRunning(cfg *config.Config) bool { return daemonctl.Running(cfg) }
