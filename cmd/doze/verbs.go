package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/endpoints"
	"github.com/doze-dev/doze/internal/journal"
	"github.com/doze-dev/doze/internal/ui"
)

// The lifecycle verbs. Everything the dash can do to an instance has a command
// here that does the same thing, for a script or for a terminal with no room
// for a dash.

// daemonFor is the daemon behind a config's control socket.
func daemonFor(cfg *config.Config) control.Daemon {
	return control.NewClient(daemon.ControlSocketPath(cfg)).Daemon()
}

// targetsOrAll checks each named instance is declared, and with no names
// returns every enabled one.
func targetsOrAll(cfg *config.Config, args []string) ([]string, error) {
	if len(args) > 0 {
		for _, n := range args {
			if cfg.Lookup(n) == nil {
				return nil, instanceNotFound(cfg, n)
			}
		}
		return args, nil
	}
	var all []string
	for _, d := range cfg.Instances {
		if d.Enabled {
			all = append(all, d.Name)
		}
	}
	return all, nil
}

func wakeCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "wake [name…]",
		Aliases: []string{"boot"},
		Short:   "Wake a service now, with what it depends on (no name wakes everything)",
		Long: "wake starts the named services and whatever they depend on, in order, and\n" +
			"waits until each is ready. With no name it wakes every enabled service.\n" +
			"Unlike up it does not reconcile declared structure first.",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: instanceCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			targets, err := targetsOrAll(cfg, args)
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				return fmt.Errorf("nothing to wake (no enabled services declared)")
			}
			if !daemonRunning(cfg) {
				if err := startDaemon(cfg); err != nil {
					return err
				}
			}
			return runWake(context.Background(), daemonFor(cfg), os.Stdout, bootClosure(cfg, targets))
		},
	}
}

// runWake wakes each name in order, one line each, with how long the daemon
// says it took.
func runWake(ctx context.Context, d control.Daemon, out io.Writer, names []string) error {
	took := watchReady(ctx, d)
	defer took.stop()
	for _, name := range names {
		wctx, cancel := context.WithTimeout(ctx, bootBudget)
		start := time.Now()
		err := d.Do(wctx, control.Verb{Name: control.VerbWake, Target: name})
		verbTook := time.Since(start)
		cancel()
		if err != nil {
			fmt.Fprintln(out, ui.Fail("✗")+" "+name+": "+err.Error())
			return exitCodeError(1)
		}
		fmt.Fprintln(out, ui.OK("✓")+" "+name+" awake"+took.suffix(name, verbTook))
	}
	return nil
}

func sleepCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "sleep [name…]",
		Aliases: []string{"reap"},
		Short:   "Put a service to sleep, keeping its data (no name sleeps everything awake)",
		Long: "sleep stops the named services. Their data stays, and each wakes again on\n" +
			"its next connection or the next `doze wake`. With no name it puts everything\n" +
			"that is awake to sleep; the daemon keeps running. `doze down` stops that too.",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: instanceCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if _, err := targetsOrAll(cfg, args); err != nil {
				return err
			}
			if !daemonRunning(cfg) {
				fmt.Println(ui.Muted("nothing is awake (the daemon is not running)"))
				return nil
			}
			return runSleep(context.Background(), daemonFor(cfg), os.Stdout, args)
		},
	}
}

func runSleep(ctx context.Context, d control.Daemon, out io.Writer, names []string) error {
	if len(names) == 0 {
		if err := d.Do(ctx, control.Verb{Name: control.VerbSleep}); err != nil {
			return err
		}
		fmt.Fprintln(out, ui.OK("✓")+" everything is asleep")
		return nil
	}
	for _, name := range names {
		if err := d.Do(ctx, control.Verb{Name: control.VerbSleep, Target: name}); err != nil {
			fmt.Fprintln(out, ui.Fail("✗")+" "+name+": "+err.Error())
			return exitCodeError(1)
		}
		fmt.Fprintln(out, ui.OK("✓")+" "+name+" asleep")
	}
	return nil
}

func restartCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "restart <name…>",
		Short:             "Stop a service and start it again",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: instanceCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if _, err := targetsOrAll(cfg, args); err != nil {
				return err
			}
			if !daemonRunning(cfg) {
				if err := startDaemon(cfg); err != nil {
					return err
				}
			}
			return runRestart(context.Background(), daemonFor(cfg), os.Stdout, args)
		},
	}
}

func runRestart(ctx context.Context, d control.Daemon, out io.Writer, names []string) error {
	took := watchReady(ctx, d)
	defer took.stop()
	for _, name := range names {
		rctx, cancel := context.WithTimeout(ctx, bootBudget)
		start := time.Now()
		err := d.Do(rctx, control.Verb{Name: control.VerbRestart, Target: name})
		verbTook := time.Since(start)
		cancel()
		if err != nil {
			fmt.Fprintln(out, ui.Fail("✗")+" "+name+": "+err.Error())
			return exitCodeError(1)
		}
		fmt.Fprintln(out, ui.OK("✓")+" "+name+" restarted"+took.suffix(name, verbTook))
	}
	return nil
}

func resetCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset <name…>",
		Short: "Delete a service's data; it is provisioned fresh on its next wake",
		Long: "reset stops the named services and deletes their data. The next wake\n" +
			"provisions each one fresh from the config. This cannot be undone, so it asks\n" +
			"first; pass --yes to skip the question (required when not on a terminal).",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: instanceCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if _, err := targetsOrAll(cfg, args); err != nil {
				return err
			}
			if !yes {
				if !isInteractive() {
					return fmt.Errorf("reset deletes data and will not guess: pass --yes to confirm")
				}
				if !confirm(fmt.Sprintf("Reset %s? The data is deleted and provisioned fresh on the next wake.", strings.Join(args, ", "))) {
					fmt.Println(ui.Muted("nothing was reset"))
					return nil
				}
			}
			if !daemonRunning(cfg) {
				if err := startDaemon(cfg); err != nil {
					return err
				}
			}
			return runReset(context.Background(), daemonFor(cfg), os.Stdout, args)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask")
	return cmd
}

func runReset(ctx context.Context, d control.Daemon, out io.Writer, names []string) error {
	for _, name := range names {
		if err := d.Do(ctx, control.Verb{Name: control.VerbReset, Target: name}); err != nil {
			fmt.Fprintln(out, ui.Fail("✗")+" "+name+": "+err.Error())
			return exitCodeError(1)
		}
		fmt.Fprintln(out, ui.OK("✓")+" "+name+" reset — its data is gone; the next wake provisions it fresh")
	}
	return nil
}

func pinCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "pin <name…>",
		Aliases: []string{"keepawake"},
		Short:   "Keep a service awake past its idle time (run again to unpin)",
		Long: "pin exempts the named services from the idle timer, so they stay awake with\n" +
			"nobody connected. It is a toggle: pin a pinned service to unpin it. A pin\n" +
			"lasts until the daemon stops.",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: instanceCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if _, err := targetsOrAll(cfg, args); err != nil {
				return err
			}
			if !daemonRunning(cfg) {
				if err := startDaemon(cfg); err != nil {
					return err
				}
			}
			return runPin(context.Background(), daemonFor(cfg), os.Stdout, args)
		},
	}
}

func runPin(ctx context.Context, d control.Daemon, out io.Writer, names []string) error {
	for _, name := range names {
		if err := d.Do(ctx, control.Verb{Name: control.VerbPin, Target: name}); err != nil {
			fmt.Fprintln(out, ui.Fail("✗")+" "+name+": "+err.Error())
			return exitCodeError(1)
		}
	}
	// A pin is a toggle, so say which way each one went.
	snap, err := d.Snapshot(ctx)
	if err != nil {
		return err
	}
	pinned := map[string]bool{}
	for _, v := range snap.Instances {
		pinned[v.Name] = v.KeepAwake
	}
	for _, name := range names {
		if pinned[name] {
			fmt.Fprintln(out, ui.OK("✓")+" "+name+" pinned — it stays awake until unpinned or the daemon stops")
		} else {
			fmt.Fprintln(out, ui.OK("✓")+" "+name+" unpinned — it sleeps again when idle")
		}
	}
	return nil
}

// readyWatch collects, from the journal, how long each instance took to
// become ready while a command runs.
type readyWatch struct {
	mu     sync.Mutex
	took   map[string]time.Duration
	seen   chan struct{} // signalled on each ready event
	cancel context.CancelFunc
	done   chan struct{}
}

// watchReady follows the journal from now on. It is best-effort: a daemon
// that cannot be subscribed to just yields no durations.
func watchReady(ctx context.Context, d control.Daemon) *readyWatch {
	ctx, cancel := context.WithCancel(ctx)
	w := &readyWatch{took: map[string]time.Duration{}, seen: make(chan struct{}, 1), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(w.done)
		_ = d.Subscribe(ctx, control.Subscription{Topics: []string{control.TopicJournal}}, func(f control.Frame) error {
			if f.Event != nil && f.Event.Kind == journal.Ready {
				w.mu.Lock()
				w.took[f.Event.Instance] = f.Event.Took
				w.mu.Unlock()
				select {
				case w.seen <- struct{}{}:
				default:
				}
			}
			return nil
		})
	}()
	return w
}

// suffix renders " in 38ms" for name. The verb's reply and the journal frame
// travel on different connections, so it waits briefly for the frame — unless
// the verb itself returned at once (verbTook), which means nothing was started
// and there is no frame to wait for.
func (w *readyWatch) suffix(name string, verbTook time.Duration) string {
	wait := 150 * time.Millisecond
	if verbTook < 5*time.Millisecond {
		wait = 0
	}
	deadline := time.After(wait)
	for {
		w.mu.Lock()
		d, ok := w.took[name]
		delete(w.took, name)
		w.mu.Unlock()
		if ok {
			return ui.Muted(" in " + roundDuration(d))
		}
		select {
		case <-w.seen:
		case <-w.done:
			w.mu.Lock()
			d, ok := w.took[name]
			w.mu.Unlock()
			if ok {
				return ui.Muted(" in " + roundDuration(d))
			}
			return ""
		case <-deadline:
			return ""
		}
	}
}

func (w *readyWatch) stop() {
	w.cancel()
	<-w.done
}

// roundDuration prints a duration the way a person says it: 38ms, 1.2s, 2m5s.
func roundDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return d.Round(time.Millisecond).String()
	case d < time.Minute:
		return d.Round(100 * time.Millisecond).String()
	default:
		return d.Round(time.Second).String()
	}
}

func urlCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "url <name>",
		Aliases: []string{"copy"},
		Short:   "Print a service's connect line (pipe it to your clipboard)",
		Long: "url prints the one line a client needs to reach a service: its connection\n" +
			"string, or its address when it has none. It reads the config, so the\n" +
			"service need not be running.\n\n" +
			"  doze url db | pbcopy\n" +
			"  psql \"$(doze url db)\"",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: instanceCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if cfg.Lookup(args[0]) == nil {
				return instanceNotFound(cfg, args[0])
			}
			eps, err := endpoints.For(cfg)
			if err != nil {
				return err
			}
			for _, ep := range eps {
				if ep.Name != args[0] {
					continue
				}
				line := ep.URL
				if line == "" {
					line = ep.Address
				}
				if line == "" {
					return fmt.Errorf("%s has no address: it declares no port", args[0])
				}
				fmt.Println(line)
				return nil
			}
			return fmt.Errorf("%s has no address: it declares no port", args[0])
		},
	}
}
