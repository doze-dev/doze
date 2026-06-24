package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/control"
	"github.com/nerdmenot/doze/internal/daemon"
)

func downCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down [process…]",
		Short: "Stop processes in reverse dependency order",
		Long: "down stops application processes (the counterpart to `doze up`), in reverse\n" +
			"dependency order so dependents drain before their dependencies. Name one or\n" +
			"more processes, or omit to stop every declared process. The databases they\n" +
			"used are left to reap on idle, and the daemon keeps running.",
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
			client := control.NewClient(daemon.ControlSocketPath(cfg))
			if !client.Available() {
				fmt.Println("doze is not running")
				return nil
			}
			return shutdown(cfg, client, targets)
		},
	}
}
