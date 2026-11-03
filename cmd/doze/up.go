package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/ui"
)

func upCmd() *cobra.Command {
	var follow, detach, last, all bool
	cmd := &cobra.Command{
		Use:   "up [service…]",
		Short: "Bring the stack up in the background (converge structure + wake every service)",
		Long: "up converges declared structure and wakes every enabled service in\n" +
			"dependency order, gating on each health probe — then returns. The daemon\n" +
			"keeps supervising everything in the background; nothing stays attached to\n" +
			"your terminal. Watch logs in the dash (`doze`), or pass `-f` to wake and\n" +
			"stream in one step (Ctrl-C then just detaches — it does not stop anything).\n" +
			"`doze down` stops the stack. Disabled services are skipped.\n\n" +
			"With no names, up starts everything; when last time started fewer, it\n" +
			"asks whether to start the same again (on a terminal; -y says yes).\n" +
			"--last and --all decide without asking.",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: instanceCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			_ = detach // deprecated: up is always detached now
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			var targets []string
			if len(args) > 0 {
				for _, n := range args {
					if cfg.Lookup(n) == nil {
						return instanceNotFound(cfg, n)
					}
				}
				if last || all {
					return usageError(fmt.Errorf("--last and --all choose for a bare doze up; drop them, or the names"))
				}
				targets = args
			} else {
				if last && all {
					return usageError(fmt.Errorf("--last and --all ask for different things; pick one"))
				}
				if len(enabledNames(cfg)) == 0 {
					return fmt.Errorf("nothing to bring up (no enabled services declared)")
				}
				if targets, err = chooseUpTargets(cfg, last, all); err != nil {
					return err
				}
			}
			rememberUp(cfg, targets)
			return runUp(cfg, targets, follow)
		},
	}
	cmd.Flags().BoolVar(&last, "last", false, "start what was started last time, without asking")
	cmd.Flags().BoolVar(&all, "all", false, "start everything, without asking")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream logs after waking (Ctrl-C detaches; the stack keeps running)")
	cmd.Flags().BoolVar(&detach, "detach", false, "deprecated: up is always detached")
	_ = cmd.Flags().MarkHidden("detach")
	return cmd
}

// runUp ensures the daemon is up and boots the dependency closure of the targets
// in order, with progress. It then returns — the daemon supervises everything in
// the background. With follow, it additionally streams the process logs until
// Ctrl-C, which only detaches the stream (the stack keeps running).
func runUp(cfg *config.Config, targets []string, follow bool) error {
	if follow && jsonMode {
		return usageError(fmt.Errorf("-f streams logs; it can't be combined with --json"))
	}
	if !daemonRunning(cfg) {
		if err := startDaemon(cfg); err != nil {
			return err
		}
	}
	client := control.NewClient(daemon.ControlSocketPath(cfg))

	// How long each one took comes from the daemon's journal, not from a
	// stopwatch here: it is the daemon's number, the same one the dash shows.
	took := watchReady(commandContext(), client.Daemon())
	defer took.stop()
	r := newVerbReport(os.Stdout, os.Stderr)
	for _, name := range bootClosure(cfg, targets) {
		d, err := bootOne(client, cfg, name, took)
		if err != nil {
			return r.fail(name, err)
		}
		if cfg.Task(name) != nil {
			r.ok(name, "done", "done", d)
		} else {
			r.ok(name, "awake", "awake", d)
		}
	}

	if !follow {
		fmt.Println(ui.Muted("›") + " up — supervised in the background. " +
			ui.Muted("doze for the dash · doze down to stop"))
		return nil
	}

	// Follow: stream the process logs. Ctrl-C only stops the stream; the daemon
	// keeps supervising everything.
	procs := processNames(cfg, targets)
	if len(procs) == 0 {
		fmt.Println(ui.Muted("up — no process services to follow; the dash (doze) has the backend logs"))
		return nil
	}
	ctx, stop := signal.NotifyContext(commandContext(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Println(ui.Muted("streaming logs — press Ctrl-C to detach (the stack keeps running)"))
	streamErr := make(chan error, 1)
	go func() {
		streamErr <- client.Stream(ctx, control.Request{Op: "logs", Follow: true, Names: procs}, printLogLine)
	}()
	select {
	case <-ctx.Done():
	case err := <-streamErr:
		if err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, ui.ErrFail("✗")+" log stream ended: "+err.Error())
		}
	}
	stop()
	fmt.Println("\n" + ui.Muted("detached — the stack keeps running. doze down to stop"))
	return nil
}

// bootOne wakes a single instance and keeps the terminal honest while it does:
// the daemon's own progress (engine downloads can dominate a first boot) goes
// to its log file, so we tail that file and relay download/verify lines here,
// plus an elapsed-time tick on the waking line when stderr is a terminal.
// Progress is stderr's; the "awake" line that ends it is the result, on stdout.
func bootOne(client *control.Client, cfg *config.Config, name string, took *readyWatch) (time.Duration, error) {
	statusLine := ui.ErrMuted("→") + " " + name + " waking…"
	tty := isTerminal(os.Stderr)
	if tty {
		fmt.Fprint(os.Stderr, statusLine)
	} else {
		fmt.Fprintln(os.Stderr, statusLine)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		relayBootProgress(daemon.LogFilePath(cfg), statusLine, tty, done)
	}()

	bootCtx, cancel := context.WithTimeout(commandContext(), bootBudget)
	start := time.Now()
	_, err := client.DoContext(bootCtx, control.Request{Op: "up", DB: name}) // boot + converge
	verbTook := time.Since(start)
	cancel()
	close(done)
	wg.Wait()
	if tty {
		fmt.Fprint(os.Stderr, "\r\033[K") // clear the ticking line; the result line replaces it
	}
	if err != nil {
		return 0, err
	}
	return took.of(name, verbTook), nil
}

// relayBootProgress tails the daemon log from its current end until done,
// relaying progress lines (downloads/verification) so a long first boot isn't
// silent. On a terminal it also re-renders statusLine with the elapsed time;
// piped output gets only the plain relayed lines.
func relayBootProgress(logPath, statusLine string, tty bool, done <-chan struct{}) {
	var offset int64
	if fi, err := os.Stat(logPath); err == nil {
		offset = fi.Size()
	}
	start := time.Now()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var pending string // partial trailing line between polls
	for {
		select {
		case <-done:
			return
		case <-tick.C:
		}
		for _, line := range readNewLines(logPath, &offset, &pending) {
			if !bootProgressLine(line) {
				continue
			}
			// The daemon writes through its own logger prefixes; strip them so
			// the relayed line reads as ours.
			for _, p := range []string{"doze: ", "› ", "→ ", "✓ ", "✗ "} {
				line = strings.TrimPrefix(line, p)
			}
			msg := "  " + ui.ErrMuted("› "+line)
			if tty {
				fmt.Fprint(os.Stderr, "\r\033[K"+msg+"\n")
			} else {
				fmt.Fprintln(os.Stderr, msg)
			}
		}
		if tty {
			if elapsed := time.Since(start); elapsed >= 3*time.Second {
				fmt.Fprint(os.Stderr, "\r\033[K"+statusLine+ui.ErrMuted(fmt.Sprintf(" %ds", int(elapsed.Seconds()))))
			}
		}
	}
}

// readNewLines returns the complete lines appended to path since *offset,
// carrying any trailing partial line in *pending until it completes.
func readNewLines(path string, offset *int64, pending *string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(*offset, io.SeekStart); err != nil {
		return nil
	}
	buf, err := io.ReadAll(f)
	if err != nil || len(buf) == 0 {
		return nil
	}
	*offset += int64(len(buf))
	chunk := *pending + string(buf)
	lines := strings.Split(chunk, "\n")
	*pending = lines[len(lines)-1] // "" when the chunk ended on a newline
	var out []string
	for _, l := range lines[:len(lines)-1] {
		if l = strings.TrimRight(l, "\r"); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// bootProgressLine picks the daemon log lines worth relaying during a boot —
// the slow parts (fetching modules, downloading and verifying engine builds),
// not the routine supervision chatter.
func bootProgressLine(line string) bool {
	return containsAny(line, "downloading", "verifying", "fetching", "extracting")
}

// printLogLine renders one streamed log frame with a faint instance prefix.
func printLogLine(f control.LogFrame) {
	fmt.Println(ui.Muted(f.Instance) + ui.Muted(" │ ") + f.Line)
}

// bootClosure returns the targets plus their transitive dependencies in dependency
// order (each instance after the ones it depends on), so progress reads top-down.
func bootClosure(cfg *config.Config, targets []string) []string {
	visited := map[string]bool{}
	var order []string
	var visit func(string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		decl := cfg.Lookup(name)
		if decl == nil {
			return
		}
		for _, dep := range decl.Deps {
			visit(dep.Name)
		}
		order = append(order, name)
	}
	for _, t := range targets {
		visit(t)
	}
	return order
}

// processNames filters the targets' closure to just the process instances, in
// dependency order (for log streaming).
func processNames(cfg *config.Config, targets []string) []string {
	var out []string
	for _, name := range bootClosure(cfg, targets) {
		if decl := cfg.Lookup(name); decl != nil && decl.Type == "process" {
			out = append(out, name)
		}
	}
	return out
}
