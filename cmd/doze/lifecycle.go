package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/ui"
)

func wakeCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "wake [service]",
		Aliases: []string{"start"},
		Short:   "Wake a sleeping service (and its dependencies); no arg wakes all",
		Long: "wake boots a service now instead of waiting for the first connection,\n" +
			"bringing up its dependencies first. With no argument it wakes every enabled\n" +
			"service. Disabled (enabled = false) services are skipped.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				if cfg.Lookup(args[0]) == nil {
					return instanceNotFound(cfg, args[0])
				}
				// Wake its dependency closure, dependencies first.
				return bootInstances(cfg, bootClosure(cfg, []string{args[0]}))
			}
			var names []string
			for _, d := range cfg.Instances {
				if d.Enabled {
					names = append(names, d.Name)
				}
			}
			if len(names) == 0 {
				return fmt.Errorf("no enabled services to wake")
			}
			return bootInstances(cfg, names)
		},
	}
}

func sleepCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "sleep [service]",
		Aliases: []string{"stop"},
		Short:   "Sleep a service and everything that depends on it; no arg sleeps all awake",
		Long: "sleep reaps a running service. Named, it first sleeps every service that\n" +
			"depends on it (so dependents drain before their dependency), then the\n" +
			"service itself. With no argument it sleeps all awake services. The daemon\n" +
			"keeps running so a later connection can wake a service again — use\n" +
			"`doze down` to stop the daemon too.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			client := control.NewClient(daemon.ControlSocketPath(cfg))
			if !client.Available() {
				fmt.Println("doze is not running")
				return nil
			}
			if len(args) == 0 {
				if _, err := client.Do(control.Request{Op: "down"}); err != nil { // StopAll; daemon stays
					return err
				}
				fmt.Println(ui.OK("✓") + " all services asleep")
				return nil
			}
			name := args[0]
			if cfg.Lookup(name) == nil {
				return instanceNotFound(cfg, name)
			}
			for _, n := range dependentsClosure(cfg, name) {
				if _, err := client.Do(control.Request{Op: "down", DB: n}); err != nil {
					fmt.Println(ui.Fail("✗") + " " + n + ": " + err.Error())
				} else {
					fmt.Println(ui.OK("✓") + " " + n + " asleep")
				}
			}
			return nil
		},
	}
}

// dependentsClosure returns name preceded by every service that (transitively)
// depends on it, dependents-first — the order to reap so nothing is pulled out
// from under a live dependent.
func dependentsClosure(cfg *config.Config, name string) []string {
	dependents := map[string][]string{} // dependency -> services that depend on it
	for _, d := range cfg.Instances {
		for _, dep := range d.Deps {
			dependents[dep.Name] = append(dependents[dep.Name], d.Name)
		}
	}
	var order []string
	seen := map[string]bool{}
	var visit func(string)
	visit = func(n string) {
		if seen[n] {
			return
		}
		seen[n] = true
		for _, dep := range dependents[n] {
			visit(dep) // dependents appended before n (post-order)
		}
		order = append(order, n)
	}
	visit(name)
	return order
}
