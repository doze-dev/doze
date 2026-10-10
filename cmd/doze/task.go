package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/state"
	"github.com/doze-dev/doze/internal/ui"
)

func taskCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "task [name]",
		Short: "Run a task now, or list the tasks and how they last ran",
		Long: "task runs the named task whatever its state, after waking what it\n" +
			"references, and returns when it is over: exit 0 if it succeeded, 1 with its\n" +
			"output if it failed. A task also runs by itself when something that lists it\n" +
			"in `after` starts, if it never succeeded or its `inputs` changed; this runs\n" +
			"it regardless, which is the only way to run a `manual = true` task.\n\n" +
			"With no name it lists every task and how it last ran.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: taskCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return listTasks(cfg, os.Stdout)
			}
			name := args[0]
			if cfg.Task(name) == nil {
				return taskNotFound(cfg, name)
			}
			if !daemonRunning(cfg) {
				if err := startDaemon(cfg); err != nil {
					return err
				}
			}
			return runTask(context.Background(), cfg, daemonFor(cfg), os.Stdout, name)
		},
	}
}

func taskNotFound(cfg *config.Config, name string) error {
	if d := cfg.Lookup(name); d != nil {
		return fmt.Errorf("%s is not a task: `doze wake %s` starts it", d.Title(), name)
	}
	var names []string
	for _, t := range cfg.Tasks {
		names = append(names, t.Name)
	}
	if len(names) == 0 {
		return fmt.Errorf("no task called %q: this stack declares no tasks", name)
	}
	return fmt.Errorf("no task called %q: the tasks are %s", name, strings.Join(names, ", "))
}

func taskCompletion(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	cfg, err := loadConfig()
	if err != nil || len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, t := range cfg.Tasks {
		out = append(out, t.Name)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// runTask asks the daemon to run a task and says how it went.
func runTask(ctx context.Context, cfg *config.Config, d control.Daemon, out *os.File, name string) error {
	fmt.Fprintln(out, ui.Muted("→")+" "+name+" running…")
	start := time.Now()
	if err := d.Do(ctx, control.Verb{Name: control.VerbTask, Target: name}); err != nil {
		fmt.Fprintln(out, ui.Fail("✗")+" "+name+": "+err.Error())
		return exitCodeError(1)
	}
	fmt.Fprintf(out, "%s %s done in %s\n", ui.OK("✓"), name, time.Since(start).Round(time.Millisecond))
	return nil
}

func listTasks(cfg *config.Config, out *os.File) error {
	if len(cfg.Tasks) == 0 {
		fmt.Fprintln(out, ui.Muted("no tasks declared"))
		return nil
	}
	st, err := state.Load(state.Path(cfg.Path()))
	if err != nil {
		return err
	}
	names := make([]string, 0, len(cfg.Tasks))
	for _, t := range cfg.Tasks {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	for _, name := range names {
		rec, ran := st.Tasks[name]
		task := cfg.Task(name)
		switch {
		case !ran && task.Manual:
			fmt.Fprintf(out, "%-14s %s\n", name, ui.Muted("manual — never run"))
		case !ran:
			fmt.Fprintf(out, "%-14s %s\n", name, ui.Muted("never ran"))
		case rec.Result == "done":
			fmt.Fprintf(out, "%-14s %s %s\n", name, ui.OK("done"), ui.Muted(fmt.Sprintf("%s ago in %s — %s",
				since(rec.RanAt), time.Duration(rec.TookMS)*time.Millisecond, rec.Why)))
		default:
			fmt.Fprintf(out, "%-14s %s %s\n", name, ui.Fail("failed"), ui.Muted(fmt.Sprintf("%s ago, exit %d — %s",
				since(rec.RanAt), rec.ExitCode, rec.Why)))
		}
	}
	return nil
}

func since(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	switch {
	case d < time.Minute:
		return d.String()
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
