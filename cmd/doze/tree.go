package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/ui"
)

func treeCmd() *cobra.Command {
	var graph, jsonOut, watch bool
	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"tree", "ls", "ps"},
		Short:   "Show every service: state, endpoint, and resource use",
		Long: "status lists the stack as a grouped table — services by category, each with\n" +
			"its live state (active / idle / waking / asleep / disabled, plus error and\n" +
			"tainted when something needs attention), endpoint, open connections, memory\n" +
			"and CPU, and a note: what is holding it awake, when it will sleep, or what\n" +
			"went wrong. -w keeps the table on screen and redraws it as things change. With the daemon down it shows the declared\n" +
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
			daemonUp := daemonRunning(cfg)
			if watch {
				if jsonOut || graph {
					return fmt.Errorf("-w redraws the table; it cannot be combined with --json or --graph")
				}
				if !daemonUp {
					return fmt.Errorf("-w watches a running stack, and the daemon is not running (doze up starts it)")
				}
				ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				return watchStatus(ctx, daemonFor(cfg), os.Stdout, cfg, stdoutIsTerminal())
			}
			views := map[string]control.InstanceView{}
			var idle time.Duration
			if daemonUp {
				if snap, err := daemonFor(cfg).Snapshot(context.Background()); err == nil {
					idle = snap.IdleTimeout
					for _, v := range snap.Instances {
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
				renderTable(os.Stdout, cfg, views, daemonUp, idle)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&graph, "graph", false, "draw the dependency tree instead of the table")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON instead of the table")
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "keep the table on screen and redraw it as things change (Ctrl-C to stop)")
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

// watchStatus keeps the status table current: it draws the stack, then draws
// it again whenever an instance changes or new meters arrive. On a terminal
// each draw replaces the last; piped, each is printed after the one before.
func watchStatus(ctx context.Context, d control.Daemon, out io.Writer, cfg *config.Config, tty bool) error {
	snap, err := d.Snapshot(ctx)
	if err != nil {
		return err
	}
	views := map[string]control.InstanceView{}
	for _, v := range snap.Instances {
		views[v.Name] = v
	}
	draw := func() {
		if tty {
			fmt.Fprint(out, "\033[H\033[2J")
		}
		renderTable(out, cfg, views, true, snap.IdleTimeout)
		if !tty {
			fmt.Fprintln(out)
		}
	}
	draw()
	sub := control.Subscription{Topics: []string{control.TopicInstances, control.TopicStats}}
	return d.Subscribe(ctx, sub, func(f control.Frame) error {
		switch f.Topic {
		case control.TopicInstances:
			if f.Snapshot {
				// The opening snapshot repeats what Snapshot just returned,
				// without the meters: keep the meters already in hand.
				for _, v := range f.Instances {
					if old, ok := views[v.Name]; ok {
						v.RAM, v.CPU, v.Children = old.RAM, old.CPU, old.Children
					}
					views[v.Name] = v
				}
				return nil
			}
			for _, v := range f.Instances {
				if old, ok := views[v.Name]; ok && v.PID == old.PID {
					v.RAM, v.CPU, v.Children = old.RAM, old.CPU, old.Children
				}
				views[v.Name] = v
			}
			for _, name := range f.Removed {
				delete(views, name)
			}
		case control.TopicStats:
			for _, s := range f.Stats {
				if v, ok := views[s.Name]; ok {
					v.RAM, v.CPU = s.RAM, s.CPU
					views[s.Name] = v
				}
			}
		default:
			return nil
		}
		draw()
		return nil
	})
}
