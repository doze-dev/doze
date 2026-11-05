package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/ui"

	"github.com/doze-dev/doze/internal/desk/app"
	"github.com/doze-dev/doze/internal/desk/devserve"
	"github.com/doze-dev/doze/internal/desk/link"
)

// openCmd is `doze open [dir]`: the app, at this project. It is the only
// command that starts the GUI; everything else in the binary is the CLI or
// the daemon, and never links a window.
func openCmd() *cobra.Command {
	var measure, window bool
	var serveAddr, fixture, show string
	var closeAfter, quitAfter time.Duration
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
			if serveAddr != "" {
				// The page and its API over HTTP, no window: for a browser,
				// Playwright and the Vite dev server.
				open := ""
				if len(args) == 1 {
					open, _ = filepath.Abs(args[0])
				}
				var setup func(dir, action, arg string) error
				if fixture == "" {
					setup = setupFolder
				}
				return devserve.Run(cmd.Context(), devserve.Options{Addr: serveAddr, Fixture: fixture, Out: os.Stderr, Version: version, Open: open, Setup: setup})
			}
			dir, linkArg := "", ""
			if len(args) == 1 && link.Is(args[0]) {
				// A doze:// link: the app finds the stack itself.
				linkArg, args = args[0], nil
			}
			if len(args) == 1 {
				abs, err := filepath.Abs(args[0])
				if err != nil {
					return err
				}
				dir = abs
				configPath = filepath.Join(dir, "doze.hcl")
			}
			// A project with a config gets its daemon started, as the dash did,
			// so the first thing the app shows is live. Over a fixture there
			// is no project.
			if fixture != "" {
				dir = ""
			} else if cfg, err := loadConfig(); err == nil {
				if dir == "" {
					dir, _ = filepath.Abs(configDir(configPath))
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
			// From a terminal the app runs on its own, so the prompt comes back:
			// the same binary, detached, asked the same thing. A second copy hands
			// over to a running one (single instance) and exits.
			if !measure && fixture == "" && os.Getenv(envAppChild) == "" && isTerminal(os.Stdin) {
				if err := detachApp(dir, show, linkArg); err == nil {
					fmt.Println(ui.OK("✓") + " doze is open " + ui.Muted("· the bat stays in the menu bar; stacks keep running when it quits"))
					return nil
				}
			}
			return app.Run(app.Options{Open: dir, Show: show, Link: linkArg, Measure: measure, Fixture: fixture, Window: window, CloseAfter: closeAfter, QuitAfter: quitAfter, Version: version, Setup: setupFolder})
		},
	}
	c.Flags().StringVar(&show, "show", "", "open the window at this service of the project")
	c.Flags().BoolVar(&measure, "measure", false, "print timings to stderr for the desk bench")
	c.Flags().StringVar(&serveAddr, "serve", "", "serve the app's page over HTTP at this address instead of opening a window")
	c.Flags().StringVar(&fixture, "fixture", "", "a scripted stack (shop, crash, modules) instead of the real daemons")
	c.Flags().BoolVar(&window, "window", false, "with --fixture: open the window too")
	c.Flags().DurationVar(&closeAfter, "close-after", 0, "close the window this long after it opens (the desk bench)")
	c.Flags().DurationVar(&quitAfter, "quit-after", 0, "quit this long after starting (the desk bench)")
	for _, f := range []string{"measure", "serve", "fixture", "window", "close-after", "quit-after"} {
		_ = c.Flags().MarkHidden(f)
	}
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

// envAppChild marks the detached copy, so it does not detach again.
const envAppChild = "DOZE_APP_CHILD"

// detachApp starts `doze open dir` in its own session, with its output in
// $DOZE_HOME/desk/app.log, and returns without waiting.
func detachApp(dir, show, linkArg string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"open"}
	if linkArg != "" {
		args = append(args, linkArg)
	}
	if dir != "" {
		args = append(args, dir)
	}
	if show != "" {
		args = append(args, "--show", show)
	}
	logDir := filepath.Join(dozeHome(), "desk")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(logDir, "app.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = logf.Close() }() // the child has its own copy
	c := exec.Command(exe, args...)
	c.Env = append(os.Environ(), envAppChild+"=1")
	c.Stdout, c.Stderr = logf, logf
	c.SysProcAttr = detachAttr()
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}
