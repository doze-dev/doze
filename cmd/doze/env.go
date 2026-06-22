package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/endpoints"
)

func envCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "env",
		Short: "Print connection-string environment variables for declared instances",
		Long: "env prints shell `export` lines for each instance's connection string,\n" +
			"for use as `eval \"$(doze env)\"`. Each instance gets a unique DOZE_<NAME>_URL,\n" +
			"plus the conventional variable (DATABASE_URL, …) when it is unambiguous.\n" +
			"Start the daemon (or use `doze run`) so connections boot instances on demand.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			eps, err := endpoints.For(cfg)
			if err != nil {
				return err
			}
			vars := endpoints.EnvVars(eps)
			keys := make([]string, 0, len(vars))
			for k := range vars {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("export %s=%q\n", k, vars[k])
			}
			return nil
		},
	}
}
