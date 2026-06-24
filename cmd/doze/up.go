package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/control"
	"github.com/nerdmenot/doze/internal/daemon"
	"github.com/nerdmenot/doze/internal/ui"
)

// bootBudget caps how long a single instance may take to come up (a process whose
// command must compile first can be slow), well beyond the control client's
// default 60s reply deadline.
const bootBudget = 5 * time.Minute

func upCmd() *cobra.Command {
	var detach bool
	cmd := &cobra.Command{
		Use:   "up [process…]",
		Short: "Bring processes (and their dependencies) up, then stream their logs",
		Long: "up boots application processes declared with `process` blocks — and the\n" +
			"databases/queues they depend on — in dependency order, gating on each\n" +
			"health probe, then streams their interleaved logs. Ctrl-C stops the\n" +
			"processes in reverse order. Name one or more processes, or omit to bring up\n" +
			"every declared process. Use --detach to return once everything is up.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			targets, err := processTargets(cfg, args)
			if err != nil {
				return err
			}
			return runUp(cfg, targets, detach)
		},
	}
	cmd.Flags().BoolVar(&detach, "detach", false, "boot everything and return (the daemon keeps supervising)")
	return cmd
}

// runUp ensures the daemon is up, boots the dependency closure of the targets in
// order with compose-style progress, then (unless detached) streams logs until
// Ctrl-C and shuts the processes down in reverse order.
func runUp(cfg *config.Config, targets []string, detach bool) error {
	if !daemonRunning(cfg) {
		if err := startDaemon(cfg); err != nil {
			return err
		}
	}
	client := control.NewClient(daemon.ControlSocketPath(cfg))

	order := bootClosure(cfg, targets)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	for _, name := range order {
		fmt.Println(ui.Muted("→") + " " + name + " starting…")
		bootCtx, cancel := context.WithTimeout(ctx, bootBudget)
		_, err := client.DoContext(bootCtx, control.Request{Op: "boot", DB: name})
		cancel()
		if err != nil {
			if ctx.Err() != nil { // interrupted mid-boot
				return shutdown(cfg, client, targets)
			}
			fmt.Println(ui.Fail("✗") + " " + name + ": " + err.Error())
			return fmt.Errorf("bringing up %q: %w", name, err)
		}
		fmt.Println(ui.OK("✓") + " " + name + " ready")
	}

	if detach {
		fmt.Println(ui.Muted("processes running in the background; `doze down` to stop, `doze logs -f` to follow"))
		return nil
	}

	fmt.Println(ui.Muted("streaming logs — press Ctrl-C to stop"))
	streamErr := make(chan error, 1)
	go func() {
		streamErr <- client.Stream(ctx, control.Request{Op: "logs", Follow: true, Names: processNames(cfg, targets)}, printLogLine)
	}()

	select {
	case <-ctx.Done():
	case err := <-streamErr:
		if err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, ui.Fail("✗")+" log stream ended: "+err.Error())
		}
	}
	stop() // restore default signal handling so a second Ctrl-C is forceful
	fmt.Println()
	return shutdown(cfg, client, targets)
}

// shutdown stops the process targets in reverse order (PreStop → SIGINT via the
// daemon). Databases are left to reap on idle.
func shutdown(cfg *config.Config, client *control.Client, targets []string) error {
	fmt.Println(ui.Muted("›") + " stopping processes…")
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	procs := processNames(cfg, targets)
	for i := len(procs) - 1; i >= 0; i-- {
		name := procs[i]
		if _, err := client.DoContext(stopCtx, control.Request{Op: "down", DB: name}); err != nil {
			fmt.Println(ui.Fail("✗") + " " + name + ": " + err.Error())
		} else {
			fmt.Println(ui.OK("✓") + " " + name + " stopped")
		}
	}
	return nil
}

// printLogLine renders one streamed log frame with a faint instance prefix.
func printLogLine(f control.LogFrame) {
	fmt.Println(ui.Muted(f.Instance) + ui.Muted(" │ ") + f.Line)
}

// processTargets resolves the up/down targets: the named instances (validated), or
// every declared process when none are named.
func processTargets(cfg *config.Config, names []string) ([]string, error) {
	if len(names) > 0 {
		for _, n := range names {
			if cfg.Lookup(n) == nil {
				return nil, instanceNotFound(cfg, n)
			}
		}
		return names, nil
	}
	var procs []string
	for _, d := range cfg.Instances {
		if d.Type == "process" {
			procs = append(procs, d.Name)
		}
	}
	if len(procs) == 0 {
		return nil, fmt.Errorf("no process instances declared; add a `process` block or name an instance")
	}
	return procs, nil
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
// dependency order (for log streaming and reverse-order shutdown).
func processNames(cfg *config.Config, targets []string) []string {
	var out []string
	for _, name := range bootClosure(cfg, targets) {
		if decl := cfg.Lookup(name); decl != nil && decl.Type == "process" {
			out = append(out, name)
		}
	}
	return out
}
