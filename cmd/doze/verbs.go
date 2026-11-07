package main

import (
	"context"
	"errors"
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
	"github.com/doze-dev/doze/internal/fault"
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
			return runWake(commandContext(), daemonFor(cfg), os.Stdout, os.Stderr, bootClosure(cfg, targets))
		},
	}
}

// verbResult is what a verb did to one instance: the --json result is a list
// of them, in the order they ran.
type verbResult struct {
	Name   string `json:"name"`
	State  string `json:"state,omitempty"`   // what it is now: awake, asleep, restarted, reset, pinned, unpinned, done
	TookMS int64  `json:"took_ms,omitempty"` // how long it took to become ready, by the daemon's journal
	// Changed says whether this run changed anything, for a verb that can
	// know (pin); absent when it can't.
	Changed *bool  `json:"changed,omitempty"`
	Error   string `json:"error,omitempty"`
}

// verbReport prints each outcome as it happens and keeps it for --json.
type verbReport struct {
	out, errw io.Writer
	Results   []verbResult `json:"results"`
}

func newVerbReport(out, errw io.Writer) *verbReport {
	r := &verbReport{out: out, errw: errw, Results: []verbResult{}}
	setResult(r)
	return r
}

// ok records name in state, and prints "✓ <name> <line>" with how long it took.
func (r *verbReport) ok(name, state, line string, took time.Duration) {
	r.Results = append(r.Results, verbResult{Name: name, State: state, TookMS: took.Milliseconds()})
	suffix := ""
	if took > 0 {
		suffix = ui.Muted(" in " + roundDuration(took))
	}
	fmt.Fprintln(r.out, ui.OK("✓")+" "+name+" "+line+suffix)
}

// fail records why name failed, prints it on the error stream, and returns
// the error as already shown.
func (r *verbReport) fail(name string, err error) error {
	msg := err.Error()
	if errors.Is(err, context.Canceled) {
		msg = "interrupted"
	}
	r.Results = append(r.Results, verbResult{Name: name, Error: msg})
	fmt.Fprintln(r.errw, ui.ErrFail("✗")+" "+name+": "+msg)
	return shown(err)
}

// runWake wakes each name in order, one line each, with how long the daemon
// says it took.
func runWake(ctx context.Context, d control.Daemon, out, errw io.Writer, names []string) error {
	r := newVerbReport(out, errw)
	took := watchReady(ctx, d)
	defer took.stop()
	for _, name := range names {
		wctx, cancel := context.WithTimeout(ctx, bootBudget)
		start := time.Now()
		err := d.Do(wctx, control.Verb{Name: control.VerbWake, Target: name})
		verbTook := time.Since(start)
		cancel()
		if err != nil {
			return r.fail(name, err)
		}
		r.ok(name, "awake", "awake", took.of(name, verbTook))
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
				newVerbReport(os.Stdout, os.Stderr) // nothing was awake: no results
				fmt.Println(ui.Muted("nothing is awake (the daemon is not running)"))
				return nil
			}
			return runSleep(commandContext(), daemonFor(cfg), os.Stdout, os.Stderr, args)
		},
	}
}

func offCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "off <name…>",
		Short: "Take a service out of this run: stop it, and nothing wakes it until doze on",
		Long: "off stops the named services and keeps them stopped: a connection does not\n" +
			"wake one, and neither does a dependant. In the app it is Stop trying on a\n" +
			"crashing service; a row's switch puts it to sleep instead. Data stays.\n" +
			"`doze on <name>` puts it back; `doze up` starts it as usual.",
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
				return fault.With(fault.DaemonDown, fmt.Errorf("the daemon is not running (doze up starts it)"))
			}
			return runOnOff(commandContext(), daemonFor(cfg), os.Stdout, os.Stderr, args, true)
		},
	}
}

func onCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "on <name…>",
		Short: "Put a service back into this run; it wakes on its next connection",
		Long: "on undoes `doze off`: the named services are part of the run again, and\n" +
			"wake on their next connection or the next `doze wake`. Nothing starts now.",
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
				return fault.With(fault.DaemonDown, fmt.Errorf("the daemon is not running (doze up starts it)"))
			}
			return runOnOff(commandContext(), daemonFor(cfg), os.Stdout, os.Stderr, args, false)
		},
	}
}

func runOnOff(ctx context.Context, d control.Daemon, out, errw io.Writer, names []string, off bool) error {
	r := newVerbReport(out, errw)
	verb, state := control.VerbOn, "on"
	if off {
		verb, state = control.VerbOff, "off"
	}
	for _, name := range names {
		if err := d.Do(ctx, control.Verb{Name: verb, Target: name}); err != nil {
			return r.fail(name, err)
		}
		r.ok(name, state, state, 0)
	}
	return nil
}

func runSleep(ctx context.Context, d control.Daemon, out, errw io.Writer, names []string) error {
	r := newVerbReport(out, errw)
	if len(names) == 0 {
		// Name what was awake, so the result says what was put to sleep.
		var awake []string
		if snap, err := d.Snapshot(ctx); err == nil {
			for _, v := range snap.Instances {
				if v.State == "active" || v.State == "idle" || v.State == "booting" {
					awake = append(awake, v.Name)
				}
			}
		}
		if err := d.Do(ctx, control.Verb{Name: control.VerbSleep}); err != nil {
			return err
		}
		for _, n := range awake {
			r.Results = append(r.Results, verbResult{Name: n, State: "asleep"})
		}
		fmt.Fprintln(out, ui.OK("✓")+" everything is asleep")
		return nil
	}
	for _, name := range names {
		if err := d.Do(ctx, control.Verb{Name: control.VerbSleep, Target: name}); err != nil {
			return r.fail(name, err)
		}
		r.ok(name, "asleep", "asleep", 0)
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
			return runRestart(commandContext(), daemonFor(cfg), os.Stdout, os.Stderr, args)
		},
	}
}

func runRestart(ctx context.Context, d control.Daemon, out, errw io.Writer, names []string) error {
	r := newVerbReport(out, errw)
	took := watchReady(ctx, d)
	defer took.stop()
	for _, name := range names {
		rctx, cancel := context.WithTimeout(ctx, bootBudget)
		start := time.Now()
		err := d.Do(rctx, control.Verb{Name: control.VerbRestart, Target: name})
		verbTook := time.Since(start)
		cancel()
		if err != nil {
			return r.fail(name, err)
		}
		r.ok(name, "restarted", "restarted", took.of(name, verbTook))
	}
	return nil
}

func resetCmd() *cobra.Command {
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
			if !assumeYes {
				if !canPrompt() {
					return fault.With(fault.NeedConfirm, fmt.Errorf("reset deletes data and will not guess: pass --yes to confirm"))
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
			return runReset(commandContext(), daemonFor(cfg), os.Stdout, os.Stderr, args)
		},
	}
	return cmd
}

func runReset(ctx context.Context, d control.Daemon, out, errw io.Writer, names []string) error {
	r := newVerbReport(out, errw)
	for _, name := range names {
		if err := d.Do(ctx, control.Verb{Name: control.VerbReset, Target: name}); err != nil {
			return r.fail(name, err)
		}
		r.ok(name, "reset", "reset — its data is gone; the next wake provisions it fresh", 0)
	}
	return nil
}

func pinCmd() *cobra.Command {
	var off, toggle bool
	cmd := &cobra.Command{
		Use:     "pin <name…>",
		Aliases: []string{"keepawake"},
		Short:   "Keep a service awake past its idle time (--off to unpin)",
		Long: "pin exempts the named services from the idle timer, so they stay awake with\n" +
			"nobody connected; --off lets them sleep again. Running it twice changes\n" +
			"nothing the second time, so a script can say what it wants. --toggle flips\n" +
			"each one, as the dash's key does. A pin lasts until the daemon stops.",
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
			if off && toggle {
				return usageError(fmt.Errorf("--off and --toggle ask for different things; pick one"))
			}
			want := pinOn
			switch {
			case off:
				want = pinOff
			case toggle:
				want = pinToggle
			}
			if !daemonRunning(cfg) {
				if want == pinOff {
					// Nothing runs, so nothing is pinned.
					r := newVerbReport(os.Stdout, os.Stderr)
					for _, name := range args {
						r.pin(name, false, false)
					}
					return nil
				}
				if err := startDaemon(cfg); err != nil {
					return err
				}
			}
			return runPin(commandContext(), daemonFor(cfg), os.Stdout, os.Stderr, args, want)
		},
	}
	cmd.Flags().BoolVar(&off, "off", false, "unpin: let them sleep again when idle")
	cmd.Flags().BoolVar(&toggle, "toggle", false, "flip each one, as the dash's p key does")
	return cmd
}

type pinWant int

const (
	pinOn pinWant = iota
	pinOff
	pinToggle
)

// runPin brings each name to the pin state asked for. The daemon's verb is a
// toggle, so it is sent only to the ones that differ, and the result says
// whether each changed.
func runPin(ctx context.Context, d control.Daemon, out, errw io.Writer, names []string, want pinWant) error {
	r := newVerbReport(out, errw)
	pinned, err := pinStates(ctx, d)
	if err != nil {
		return err
	}
	var flip []string
	for _, name := range names {
		if want == pinToggle || pinned[name] != (want == pinOn) {
			flip = append(flip, name)
		}
	}
	for i, name := range flip {
		if err := d.Do(ctx, control.Verb{Name: control.VerbPin, Target: name}); err != nil {
			// The ones before it did change: say so before the failure.
			for _, done := range flip[:i] {
				r.pin(done, !pinned[done], true)
			}
			return r.fail(name, err)
		}
	}
	// Report what the daemon says now, not what was asked for.
	now, err := pinStates(ctx, d)
	if err != nil {
		return err
	}
	for _, name := range names {
		r.pin(name, now[name], now[name] != pinned[name])
	}
	return nil
}

func pinStates(ctx context.Context, d control.Daemon) (map[string]bool, error) {
	snap, err := d.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	pinned := map[string]bool{}
	for _, v := range snap.Instances {
		pinned[v.Name] = v.KeepAwake
	}
	return pinned, nil
}

// pin records one instance's pin state and whether this run changed it.
func (r *verbReport) pin(name string, pinned, changed bool) {
	state, line := "unpinned", "unpinned — it sleeps again when idle"
	if pinned {
		state, line = "pinned", "pinned — it stays awake until unpinned or the daemon stops"
	}
	if !changed {
		line = "already " + state
	}
	r.Results = append(r.Results, verbResult{Name: name, State: state, Changed: &changed})
	fmt.Fprintln(r.out, ui.OK("✓")+" "+name+" "+line)
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

// of is how long name took to become ready, or 0 when the journal didn't say.
// The verb's reply and the journal frame travel on different connections, so
// it waits briefly for the frame — unless the verb itself returned at once
// (verbTook), which means nothing was started and there is no frame to wait
// for.
func (w *readyWatch) of(name string, verbTook time.Duration) time.Duration {
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
			return d
		}
		select {
		case <-w.seen:
		case <-w.done:
			w.mu.Lock()
			d := w.took[name]
			w.mu.Unlock()
			return d
		case <-deadline:
			return 0
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
				line := ep.HTTPS // the app's own domain over https, when it has one
				if line == "" {
					line = ep.URL
				}
				if line == "" {
					line = ep.Address
				}
				if line == "" {
					return fmt.Errorf("%s has no address: it declares no port", args[0])
				}
				setResult(struct {
					Name string `json:"name"`
					URL  string `json:"url"`
				}{args[0], line})
				fmt.Println(line)
				return nil
			}
			return fmt.Errorf("%s has no address: it declares no port", args[0])
		},
	}
}
