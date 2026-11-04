package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/desk/app"
)

// openCmd is `doze open [dir]`: the app, at this project. It is the only
// command that starts the GUI; everything else in the binary is the CLI or
// the daemon, and never links a window.
func openCmd() *cobra.Command {
	var measure bool
	c := &cobra.Command{
		Use:   "open [dir]",
		Short: "Open the doze app at this project",
		Long: `Open the doze app, with a tab for the project in dir (the current
folder by default). The app is one window for every stack on this machine
and a menu bar icon that stays after the window closes; quitting it leaves
stacks running.

With no display (SSH, CI) it prints the status table instead.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ""
			if len(args) == 1 {
				abs, err := filepath.Abs(args[0])
				if err != nil {
					return err
				}
				dir = abs
				configPath = filepath.Join(dir, "doze.hcl")
			}
			// A project with a config gets its daemon started, as the dash did,
			// so the first thing the app shows is live.
			if cfg, err := loadConfig(); err == nil {
				if dir == "" {
					dir = configDir(configPath)
				}
				if !daemonRunning(cfg) {
					if err := startDaemon(cfg); err != nil {
						return err
					}
				}
			} else if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
				return err
			}
			if face(faceInput{Args: []string{"open"}, Env: environ(), Exe: executable(), StdinTTY: isTerminal(os.Stdin), StdoutTTY: isTerminal(os.Stdout)}) != faceApp {
				fmt.Fprintln(cmd.ErrOrStderr(), "no display here; showing the status table instead")
				return treeCmd().RunE(cmd, nil)
			}
			return app.Run(app.Options{Open: dir, Measure: measure})
		},
	}
	c.Flags().BoolVar(&measure, "measure", false, "print timings to stderr for the desk bench")
	_ = c.Flags().MarkHidden("measure")
	return c
}

func environ() map[string]string {
	m := map[string]string{}
	for _, kv := range os.Environ() {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				m[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return m
}

func executable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}
