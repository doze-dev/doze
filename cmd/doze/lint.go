package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/ui"
)

func lintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint",
		Short: "Validate the config without touching the daemon or any data",
		Long: "lint statically checks doze.hcl: syntax, per-engine schema, variable and\n" +
			"reference resolution, and the dependency graph (acyclic, and no enabled\n" +
			"service depending on a disabled one). It runs nothing and changes nothing —\n" +
			"safe for CI and pre-commit hooks.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			enabled := 0
			for _, d := range cfg.Instances {
				if d.Enabled {
					enabled++
				}
			}
			n := len(cfg.Instances)
			fmt.Printf("%s %s is valid: %d service(s), %d enabled, %d disabled\n",
				ui.OK("✓"), cfg.Path(), n, enabled, n-enabled)
			return nil
		},
	}
}
