package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const scaffold = `# doze.hcl — declarative local databases, no Docker.
# doze creates the databases/instances declared here and boots each lazily on
# first connect, reaping it when idle. It does not seed data or run migrations.

defaults {
  idle_timeout = "5m"        # reap an instance after this long with no connections
}

postgres "app" {
  version    = 16            # major (newest minor) or an exact "16.14"
  owner      = "app"
  extensions = ["uuid-ossp"]

  role "app" {
    password = "app"
  }

  grant {
    role       = "app"
    database   = "app"
    privileges = ["ALL"]
  }
}
`

func initCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a doze.hcl in the current directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := os.Stat(configPath); err == nil && !force {
				return fmt.Errorf("%s already exists (use --force to overwrite)", configPath)
			}
			if err := os.WriteFile(configPath, []byte(scaffold), 0o644); err != nil {
				return err
			}
			fmt.Printf("wrote %s\n", configPath)
			fmt.Println("next: `doze up` to provision, then `doze start` and connect")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config")
	return cmd
}
