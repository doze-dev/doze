package main

import (
	"github.com/doze-dev/doze/internal/control"
	"github.com/spf13/cobra"
)

func downCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Bring the whole stack down: sleep every service and stop the daemon",
		Long: "down is the counterpart to `doze up`: it sleeps every service and stops the\n" +
			"background daemon, so nothing is left running or listening. To sleep\n" +
			"services while keeping the daemon up (so they can wake on the next\n" +
			"connection), use `doze sleep`, or the switches in the app.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			// What was awake is remembered first, so the next up can offer it.
			var snap *control.Snapshot
			if daemonRunning(cfg) {
				if s, err := daemonFor(cfg).Snapshot(commandContext()); err == nil {
					snap = &s
				}
			}
			rememberDown(cfg, snap)
			// stopDaemon handles the not-running case itself (by pid file, which
			// also catches a daemon whose control socket has gone quiet).
			return stopDaemon(cfg) // shutting the daemon down reaps every backend
		},
	}
}
