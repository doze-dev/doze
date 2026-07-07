package main

import (
	"testing"

	"github.com/doze-dev/doze/internal/actions"
)

// TestActionRegistryParity enforces the single-source-of-truth contract in
// both directions: every CLI-scoped registry action has a cobra command, and
// every operational cobra command is declared in the registry. Adding a verb
// to one surface without the other fails the build — parity is structural,
// not a code-review discipline.
func TestActionRegistryParity(t *testing.T) {
	root := rootCmd()
	commands := map[string]bool{}
	for _, c := range root.Commands() {
		commands[c.Name()] = true
		for _, a := range c.Aliases {
			commands[a] = true
		}
	}

	for _, a := range actions.All() {
		if !a.CLI {
			continue
		}
		if !commands[a.Name] {
			t.Errorf("action %q is CLI-scoped in the registry but has no cobra command", a.Name)
		}
	}

	// Meta/plumbing commands that are legitimately not stack actions.
	meta := map[string]bool{
		"dash": true, "modules": true, "version": true, "dns-setup": true,
		"completion": true, "help": true, "__daemon": true,
	}
	registered := map[string]bool{}
	for _, a := range actions.All() {
		registered[a.Name] = true
		for _, al := range a.Aliases {
			registered[al] = true
		}
	}
	for _, c := range root.Commands() {
		if meta[c.Name()] || c.Hidden {
			continue
		}
		if !registered[c.Name()] {
			t.Errorf("cobra command %q is not declared in the action registry (internal/actions)", c.Name())
		}
	}
}
