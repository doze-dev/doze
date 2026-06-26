package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func downCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Bring the whole stack down: sleep every service and stop the daemon",
		Long: "down is the counterpart to `doze up`: it sleeps every service and stops the\n" +
			"background daemon, so nothing is left running or listening. To sleep\n" +
			"services while keeping the daemon up (so they can wake on the next\n" +
			"connection), use `doze sleep` instead.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if !daemonRunning(cfg) {
				fmt.Println("doze is not running")
				return nil
			}
			return stopDaemon(cfg) // shutting the daemon down reaps every backend
		},
	}
}
