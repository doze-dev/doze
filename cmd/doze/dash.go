package main

import (
	"errors"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/daemon"
	dashnext "github.com/doze-dev/doze/internal/dash"
	"github.com/doze-dev/doze/internal/tui"
)

func dashCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dash",
		Short: "Launch the live TUI dashboard",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The dashboard is a full-screen TUI; without a terminal on both ends
			// Bubble Tea can only fail obscurely, so fail plainly up front.
			if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
				return errors.New("doze dash needs an interactive terminal — try `doze status`")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			// The dashboard reflects live daemon state; start it if needed.
			if !daemonRunning(cfg) {
				if err := startDaemon(cfg); err != nil {
					return err
				}
			}
			return tui.Run(daemon.ControlSocketPath(cfg))
		},
	}
}

// isTerminal reports whether f is attached to an interactive terminal. Not a
// ModeCharDevice check: /dev/null is a char device and must count as non-TTY.
func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd())
}

// dashNextCmd opens the dash that is replacing the one above (docs/design/next,
// phase 5). It is hidden until it has everything the old one does; unit 5.11
// makes it `doze dash` and removes the old.
func dashNextCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "dash-next",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
				return errors.New("doze dash-next needs an interactive terminal — try `doze status`")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if !daemonRunning(cfg) {
				if err := startDaemon(cfg); err != nil {
					return err
				}
			}
			return dashnext.Run(cmd.Context(), daemonFor(cfg), dashnext.Options{
				Dark:  dashnext.Dark(),
				Plain: os.Getenv("NO_COLOR") != "",
				Home:  cfg.Home,
			})
		},
	}
}
