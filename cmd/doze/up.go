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

// bootBudget caps how long a single instance may take to come up (a process whose
// command must compile first can be slow), well beyond the control client's
// default 60s reply deadline.
const bootBudget = 5 * time.Minute

func upCmd() *cobra.Command {
	var follow, detach bool
	cmd := &cobra.Command{
		Use:   "up [service…]",
		Short: "Bring the stack up in the background (converge structure + wake every service)",
		Long: "up converges declared structure and wakes every enabled service in\n" +
			"dependency order, gating on each health probe — then returns. The daemon\n" +
			"keeps supervising everything in the background; nothing stays attached to\n" +
			"your terminal. Watch logs in the dash (`doze`), or pass `-f` to wake and\n" +
			"stream in one step (Ctrl-C then just detaches — it does not stop anything).\n" +
			"`doze down` stops the stack. Disabled services are skipped.",
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
				targets = args
			} else {
				for _, d := range cfg.Instances {
					if d.Enabled {
						targets = append(targets, d.Name)
					}
				}
				if len(targets) == 0 {
					return fmt.Errorf("nothing to bring up (no enabled services declared)")
				}
			}
			return runUp(cfg, targets, follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream logs after waking (Ctrl-C detaches; services keep running)")
	cmd.Flags().BoolVar(&detach, "detach", false, "deprecated: up is always detached")
	_ = cmd.Flags().MarkHidden("detach")
	return cmd
}

// runUp ensures the daemon is up and boots the dependency closure of the targets
// in order, with progress. It then returns — the daemon supervises everything in
// the background. With follow, it additionally streams the process logs until
// Ctrl-C, which only detaches the stream (the services keep running).
func runUp(cfg *config.Config, targets []string, follow bool) error {
	if !daemonRunning(cfg) {
		if err := startDaemon(cfg); err != nil {
			return err
		}
	}
	client := control.NewClient(daemon.ControlSocketPath(cfg))

	for _, name := range bootClosure(cfg, targets) {
		if err := bootOne(client, cfg, name); err != nil {
			// The styled line is the report; exitCodeError keeps main from
			// printing the same failure a second time.
			fmt.Println(ui.Fail("✗") + " " + name + ": " + err.Error())
			return exitCodeError(1)
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Println(ui.Muted("streaming logs — press Ctrl-C to detach (services keep running)"))
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
	fmt.Println("\n" + ui.Muted("detached — services keep running. doze down to stop"))
	return nil
}

// bootOne wakes a single instance and keeps the terminal honest while it does:
// the daemon's own progress (engine downloads can dominate a first boot) goes
// to its log file, so we tail that file and relay download/verify lines here,
// plus an elapsed-time tick on the waking line when stdout is a terminal.
func bootOne(client *control.Client, cfg *config.Config, name string) error {
	statusLine := ui.Muted("→") + " " + name + " waking…"
	tty := stdoutIsTerminal()
	if tty {
		fmt.Print(statusLine)
	} else {
		fmt.Println(statusLine)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		relayBootProgress(daemon.LogFilePath(cfg), statusLine, tty, done)
	}()

	bootCtx, cancel := context.WithTimeout(context.Background(), bootBudget)
	_, err := client.DoContext(bootCtx, control.Request{Op: "up", DB: name}) // boot + converge
	cancel()
	close(done)
	wg.Wait()
	if tty {
		fmt.Print("\r\033[K") // clear the ticking line; the result line replaces it
	}
	if err != nil {
		return err
	}
	fmt.Println(ui.OK("✓") + " " + name + " awake")
	return nil
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
			msg := "  " + ui.Muted("› "+line)
			if tty {
				fmt.Print("\r\033[K" + msg + "\n")
			} else {
				fmt.Println(msg)
			}
		}
		if tty {
			if elapsed := time.Since(start); elapsed >= 3*time.Second {
				fmt.Print("\r\033[K" + statusLine + ui.Muted(fmt.Sprintf(" %ds", int(elapsed.Seconds()))))
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
	defer f.Close()
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
