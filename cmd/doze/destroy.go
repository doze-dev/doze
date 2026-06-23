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

func destroyCmd() *cobra.Command {
	var autoApprove bool
	cmd := &cobra.Command{
		Use:   "destroy [instance]",
		Short: "Destroy declared structure tracked in state",
		Long: "destroy drops the structural objects doze has applied (roles, databases,\n" +
			"schemas, extensions, buckets, queues, topics) and clears them from state.\n" +
			"It does NOT delete the engine's data directory — that is `doze reset`. It\n" +
			"shows a plan and asks for confirmation first (skip with --auto-approve).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			var target string
			if len(args) == 1 {
				target = args[0]
			}
			prior, err := state.Load(state.Path(cfg.Path()))
			if err != nil {
				return err
			}
			plan := state.DestroyPlan(prior)
			if target != "" {
				plan = filterPlan(plan, target)
			}
			renderPlan(plan)
			if plan.Empty() {
				return nil
			}
			if !autoApprove && !confirm(ui.Fail("Destroy these objects? This is irreversible.")) {
				fmt.Println("Destroy cancelled.")
				return nil
			}

			ctx := context.Background()
			client := control.NewClient(daemon.ControlSocketPath(cfg))
			if client.Available() {
				if _, err := client.Do(control.Request{Op: "destroy", DB: target}); err != nil {
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
				if err := rt.Destroy(ctx, target); err != nil {
					return err
				}
			}
			fmt.Println(ui.OK("✓") + " destroy complete")
			return nil
		},
	}
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "skip the confirmation prompt")
	return cmd
}
