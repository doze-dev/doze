package main

import (
	"context"
	"fmt"
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

// bootBudget caps how long a single instance may take to come up (a process whose
// command must compile first can be slow), well beyond the control client's
// default 60s reply deadline.
const bootBudget = 5 * time.Minute

func upCmd() *cobra.Command {
	var follow, detach bool
	cmd := &cobra.Command{
		Use:   "up [service…]",
		Short: "Bring the stack up in the background (converge structure + boot every service)",
		Long: "up converges declared structure and boots every enabled service in\n" +
			"dependency order, gating on each health probe — then returns. The daemon\n" +
			"keeps supervising everything in the background; nothing stays attached to\n" +
			"your terminal. Watch logs with `doze logs -f`, or pass `-f` to boot and\n" +
			"stream in one step (Ctrl-C then just detaches — it does not stop anything).\n" +
			"`doze down` stops the stack. Disabled services are skipped.",
		Args: cobra.ArbitraryArgs,
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
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream logs after booting (Ctrl-C detaches; services keep running)")
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
		fmt.Println(ui.Muted("→") + " " + name + " starting…")
		bootCtx, cancel := context.WithTimeout(context.Background(), bootBudget)
		_, err := client.DoContext(bootCtx, control.Request{Op: "up", DB: name}) // boot + converge
		cancel()
		if err != nil {
			fmt.Println(ui.Fail("✗") + " " + name + ": " + err.Error())
			return fmt.Errorf("bringing up %q: %w", name, err)
		}
		fmt.Println(ui.OK("✓") + " " + name + " ready")
	}

	if !follow {
		fmt.Println(ui.Muted("›") + " up — supervised in the background. " +
			ui.Muted("`doze tree` to view · `doze logs -f` to follow · `doze down` to stop"))
		return nil
	}

	// Follow: stream the process logs. Ctrl-C only stops the stream; the daemon
	// keeps supervising everything.
	procs := processNames(cfg, targets)
	if len(procs) == 0 {
		fmt.Println(ui.Muted("up — no process services to follow; `doze logs -f` for backend logs"))
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
			fmt.Fprintln(os.Stderr, ui.Fail("✗")+" log stream ended: "+err.Error())
		}
	}
	stop()
	fmt.Println("\n" + ui.Muted("detached — services keep running. `doze down` to stop"))
	return nil
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
