package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/runtime"
	"github.com/doze-dev/doze/internal/state"
	"github.com/doze-dev/doze/internal/ui"
)

func syncCmd() *cobra.Command {
	var dryRun, autoApprove bool
	cmd := &cobra.Command{
		Use:     "sync [service]",
		Aliases: []string{"apply"},
		Short:   "Reconcile the stack with the config — create new, update changed, prune removed",
		Long: "sync brings the local environment in line with doze.hcl: it creates or\n" +
			"updates databases, roles, schemas, extensions, buckets, queues and topics,\n" +
			"and drops structure that was applied before but is no longer declared. A\n" +
			"disabled (enabled = false) service is left untouched — neither converged nor\n" +
			"pruned, so its data survives. --dry-run shows the changes without making them.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			var target string
			if len(args) == 1 {
				target = args[0]
				if cfg.Lookup(target) == nil {
					return instanceNotFound(cfg, target)
				}
			}
			prior, err := state.Load(state.Path(cfg.Path()))
			if err != nil {
				return err
			}
			plan := enabledPlan(cfg, state.BuildPlan(cfg, prior))
			if target != "" {
				plan = filterPlan(plan, target)
			}
			renderPlan(plan)
			if dryRun || plan.Empty() {
				return nil
			}
			if !autoApprove && !confirm("Apply these changes?") {
				fmt.Println("Sync cancelled.")
				return nil
			}

			ctx := context.Background()
			client := control.NewClient(daemon.ControlSocketPath(cfg))
			if client.Available() {
				if _, err := client.Do(control.Request{Op: "apply", DB: target}); err != nil {
					return err
				}
			} else {
				rt, err := runtime.New(cfg)
				if err != nil {
					return err
				}
				rt.SetLogger(stderrLogger)
				if err := rt.EnsureDataRoot(); err != nil {
					return err
				}
				if err := rt.Apply(ctx, target); err != nil {
					return err
				}
				rt.StopAll(ctx) // standalone: leave nothing running
			}
			fmt.Println(ui.OK("✓") + " sync complete")
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the changes without making them")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "skip the confirmation prompt")
	return cmd
}

// enabledPlan drops paused instances (enabled = false) from a plan: they are
// neither converged nor pruned. A block removed from the config entirely still
// appears (its prior structure is deleted), since that isn't a paused instance.
func enabledPlan(cfg *config.Config, plan state.Plan) state.Plan {
	var out state.Plan
	for _, ip := range plan.Instances {
		if d := cfg.Lookup(ip.Name); d != nil && !d.Enabled {
			continue
		}
		out.Instances = append(out.Instances, ip)
	}
	return out
}
