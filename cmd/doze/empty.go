package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/display"
)

// setupFolder is what the app does when a folder with no doze.hcl picks one
// of the three ways to start, the same three as bare doze in an empty
// folder (emptyScreen): init reads the project, add names one thing, empty
// writes the starter file. Then the stack's daemon is started, so the app
// shows it live. init and add are this binary's own commands, run in the
// folder with no terminal, so they never ask.
func setupFolder(dir, action, arg string) error {
	path := filepath.Join(dir, "doze.hcl")
	switch action {
	case "init", "add":
		self, err := os.Executable()
		if err != nil {
			return err
		}
		args := []string{action}
		if action == "add" {
			if arg == "" {
				return fmt.Errorf("add what? name a module or a service")
			}
			args = append(args, arg)
		}
		c := exec.Command(self, args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "NO_COLOR=1")
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("doze %s: %s", action, lastLine(string(out), err.Error()))
		}
	case "empty":
		if err := os.WriteFile(path, []byte(display.StarterFile), 0o644); err != nil {
			return err
		}
	default:
		return fmt.Errorf("no way to start called %q", action)
	}
	// The stack is shown live: its daemon is started as doze open does.
	cfg, err := config.Load(path, activeHost.ConfigHooks())
	if err != nil {
		return err
	}
	if !daemonRunning(cfg) {
		return startDaemon(cfg)
	}
	return nil
}

// lastLine is the last non-empty line of a command's output, or fallback.
func lastLine(out, fallback string) string {
	last := ""
	start := 0
	for i := 0; i <= len(out); i++ {
		if i == len(out) || out[i] == '\n' {
			if line := out[start:i]; len(line) > 0 {
				last = line
			}
			start = i + 1
		}
	}
	if last == "" {
		return fallback
	}
	return last
}
