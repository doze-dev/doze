package main

import (
	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/state"
)

func planCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plan [instance]",
		Short: "Show the structural changes apply would make",
		Long: "plan diffs the declared structure (roles, databases, schemas, extensions,\n" +
			"buckets, queues, topics) against the last applied state and prints what\n" +
			"apply would create, change, or destroy. It makes no changes.",
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
			return nil
		},
	}
}
