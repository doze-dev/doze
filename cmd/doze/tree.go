package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/ui"
)

func treeCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "tree",
		Aliases: []string{"status", "ls", "ps"},
		Short:   "Show the dependency tree of services and their state",
		Long: "tree prints the stack as a dependency tree: each service nested under the\n" +
			"one that needs it, with live state (awake / asleep / disabled), endpoint and\n" +
			"connection count. With the daemon down it shows the declared structure.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			views := map[string]control.InstanceView{}
			client := control.NewClient(daemon.ControlSocketPath(cfg))
			daemonUp := client.Available()
			if daemonUp {
				if resp, err := client.Do(control.Request{Op: "status"}); err == nil {
					for _, v := range resp.Instances {
						views[v.Name] = v
					}
				}
			}
			renderTree(cfg, views, daemonUp)
			return nil
		},
	}
}

func renderTree(cfg *config.Config, views map[string]control.InstanceView, daemonUp bool) {
	depsOf := map[string][]string{}
	hasDependents := map[string]bool{}
	for _, d := range cfg.Instances {
		for _, dep := range d.Deps {
			depsOf[d.Name] = append(depsOf[d.Name], dep.Name)
			hasDependents[dep.Name] = true
		}
	}
	// Roots are services nothing else depends on (the top of each chain).
	var roots []string
	for _, d := range cfg.Instances {
		if !hasDependents[d.Name] {
			roots = append(roots, d.Name)
		}
	}
	sort.Strings(roots)

	if !daemonUp {
		fmt.Println(ui.Muted("doze is not running — showing declared services") + "\n")
	}
	for i, r := range roots {
		printTreeNode(cfg, views, depsOf, r, "", i == len(roots)-1, true)
	}
}

func printTreeNode(cfg *config.Config, views map[string]control.InstanceView, depsOf map[string][]string, name, prefix string, last, root bool) {
	connector := "├─ "
	if last {
		connector = "└─ "
	}
	if root {
		connector = ""
	}
	fmt.Println(prefix + connector + nodeLabel(cfg, views, name))

	childPrefix := prefix
	if !root {
		if last {
			childPrefix += "   "
		} else {
			childPrefix += "│  "
		}
	}
	deps := append([]string(nil), depsOf[name]...)
	sort.Strings(deps)
	for i, dep := range deps {
		printTreeNode(cfg, views, depsOf, dep, childPrefix, i == len(deps)-1, false)
	}
}

// nodeLabel renders one service line: a state dot, its name + engine, the state
// word, and (when awake) its endpoint and connection count.
func nodeLabel(cfg *config.Config, views map[string]control.InstanceView, name string) string {
	decl := cfg.Lookup(name)
	engineType := ""
	if decl != nil {
		engineType = decl.Type
	}
	v, running := views[name]

	var dot, word string
	switch {
	case decl != nil && !decl.Enabled:
		dot, word = ui.Muted("⊘"), ui.Muted("disabled")
	case running && (v.Tainted || v.State == "tainted" || v.State == "error"):
		dot, word = ui.Fail("●"), ui.Fail(stateWord(v.State))
	case running && awakeState(v.State):
		dot, word = ui.OK("●"), ui.OK(v.State) // active / idle / booting
	default:
		dot, word = ui.Muted("○"), ui.Muted("asleep")
	}

	line := dot + " " + ui.Title(name)
	if engineType != "" {
		line += " " + ui.Muted(engineType)
	}
	line += "  " + word
	if running && awakeState(v.State) && v.Endpoint != "" {
		line += ui.Muted("  " + v.Endpoint)
		if v.Conns > 0 {
			line += ui.Muted(fmt.Sprintf("  (%d conn)", v.Conns))
		}
	}
	return line
}

// awakeState reports whether a backend is alive (booting, serving, or idling
// before the reaper takes it) — as opposed to reaped/absent.
func awakeState(s string) bool {
	return s == "active" || s == "idle" || s == "booting"
}

func stateWord(s string) string {
	if s == "" {
		return "asleep"
	}
	return s
}
