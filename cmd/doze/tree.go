package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/ui"
)

func treeCmd() *cobra.Command {
	var graph, jsonOut bool
	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"tree", "ls", "ps"},
		Short:   "Show every service: state, endpoint, and resource use",
		Long: "status lists the stack as a grouped table — services by category, each with\n" +
			"its live state (active / idle / waking / asleep / disabled, plus error and\n" +
			"tainted when something needs attention), endpoint, open connections, memory\n" +
			"and CPU, and what it depends on. With the daemon down it shows the declared\n" +
			"structure. --graph draws the dependency tree instead; --json emits the same\n" +
			"facts machine-readably for scripts and CI.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, cfgErr := loadConfig()
			if cfgErr != nil {
				// A broken block must not brick the whole status view: fall back
				// to the driver-free shallow load so the declared stack (and any
				// live daemon state) still renders, with the error alongside.
				return degradedStatus(cfgErr)
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
			switch {
			case jsonOut:
				return renderStatusJSON(cfg, views, daemonUp)
			case graph:
				renderGraph(cfg, views, daemonUp)
			default:
				renderTable(cfg, views, daemonUp)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&graph, "graph", false, "draw the dependency tree instead of the table")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON instead of the table")
	return cmd
}

// degradedStatus renders what a failing config still allows: the engine block
// headers from a shallow load, joined with live state from a running daemon
// (found via the default project dir). The load error prints after the table
// so the user sees both their stack and what's wrong with it.
func degradedStatus(cfgErr error) error {
	sc, serr := config.LoadShallow(configPath)
	if serr != nil {
		return cfgErr // not even parseable — the full error is the answer
	}
	views := map[string]control.InstanceView{}
	client := control.NewClient(daemon.ControlSocketPathIn(config.DefaultProjectDir(sc.Path())))
	daemonUp := client.Available()
	if daemonUp {
		if resp, err := client.Do(control.Request{Op: "status"}); err == nil {
			for _, v := range resp.Instances {
				views[v.Name] = v
			}
		}
	}

	fmt.Println(ui.Fail("config has errors") + ui.Muted(" — showing declared blocks; details below") + "\n")
	t := ui.NewTable("NAME", "ENGINE", "STATE", "ENDPOINT")
	for _, d := range sc.Decls {
		v, running := views[d.Name]
		eng := d.Type
		if d.Version != "" {
			eng += " " + d.Version
		}
		state := ui.State("asleep")
		if running {
			state = stateText(v, true, nil)
		}
		t.Row(ui.Title(d.Name), ui.Muted(eng), state, endpointCell(nil, nil, v, running))
	}
	fmt.Println(t.String())
	fmt.Println()
	fmt.Println(cfgErr.Error())
	return exitCodeError(1)
}
