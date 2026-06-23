package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/control"
	"github.com/nerdmenot/doze/internal/daemon"
	"github.com/nerdmenot/doze/internal/runtime"
	"github.com/nerdmenot/doze/internal/state"
	"github.com/nerdmenot/doze/internal/ui"
)

func applyCmd() *cobra.Command {
	var autoApprove bool
	cmd := &cobra.Command{
		Use:     "apply [instance]",
		Aliases: []string{"up"},
		Short:   "Apply config: converge declared structure, pruning what's removed",
		Long: "apply brings the local environment in line with doze.hcl — creating or\n" +
			"updating databases, schemas, roles, extensions, and the declared S3\n" +
			"buckets / SQS queues / SNS topics, and dropping structure that was applied\n" +
			"before but is no longer declared. It shows a plan and asks for confirmation\n" +
			"first (skip with --auto-approve). With no argument it applies everything.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			plan := state.BuildPlan(cfg, prior)
			if target != "" {
				plan = filterPlan(plan, target)
			}
			renderPlan(plan)
			if plan.Empty() {
				return nil
			}
			if !autoApprove && !confirm("Apply these changes?") {
				fmt.Println("Apply cancelled.")
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
			fmt.Println(ui.OK("✓") + " apply complete")
			return nil
		},
	}
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "skip the confirmation prompt")
	return cmd
}
