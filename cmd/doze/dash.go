package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/dash"
	"github.com/doze-dev/doze/internal/detect"
	"github.com/doze-dev/doze/internal/ui"
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
				if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
					return emptyScreen()
				}
				return err
			}
			// The dashboard reflects live daemon state; start it if needed.
			if !daemonRunning(cfg) {
				if err := startDaemon(cfg); err != nil {
					return err
				}
			}
			return dash.Run(cmd.Context(), daemonFor(cfg), dash.Options{Plain: dash.Plain()})
		},
	}
}

// isTerminal reports whether f is attached to an interactive terminal. Not a
// ModeCharDevice check: /dev/null is a char device and must count as non-TTY.
func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd())
}

// emptyScreen is bare doze in a folder with no doze.hcl: a menu of the three
// ways to start, which then runs the one chosen. Without a terminal, or with
// the colour off, it is the plain text it always was.
func emptyScreen() error {
	if dash.Plain() {
		firstRun()
		return nil
	}
	res, _ := detect.Scan(configDir(configPath))
	var found []string
	for _, c := range []struct {
		n    int
		what string
	}{{len(res.Apps), "app"}, {len(res.Modules), "module"}, {len(res.Services), "service"}, {len(res.Tasks), "task"}} {
		if c.n > 0 {
			found = append(found, plural(c.n, c.what))
		}
	}
	dir, _ := os.Getwd()
	pick, err := dash.Empty(dash.EmptyOptions{Dir: tildePath(dir), Found: found})
	if err != nil {
		return err
	}
	self, _ := os.Executable()
	run := func(args ...string) error {
		c := exec.Command(self, args...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return c.Run()
	}
	switch pick.Action {
	case "init":
		return run("init")
	case "add":
		return run("add", pick.Arg)
	case "empty":
		if err := os.WriteFile(configPath, []byte(starterFile), 0o644); err != nil {
			return err
		}
		fmt.Println(ui.OK("✓") + " wrote " + configPath + ui.Muted("  — fill it in, then: doze lint · doze up"))
	}
	return nil
}

func tildePath(p string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, h) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}

// starterFile is what "start with an empty file" writes: the four nouns, and
// what each is for, with nothing declared.
const starterFile = `# doze.hcl: what this project runs, and what it needs.
#
#   app     "name" { run = "..." }          your code
#   module  postgres "db" { }              from the doze registry
#   service "name" { run = "..." }         anything else
#   task    "name" { run = "..." }         runs once, then gets out of the way
#
# doze add postgres   adds one for you.   doze up   starts it all.

stack = "` + "my-project" + `"
`
