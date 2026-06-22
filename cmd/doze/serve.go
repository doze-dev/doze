package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/daemon"
)

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the listener daemon in the foreground",
		Long: "serve starts the proxy listener, the idle reaper, and the admin control\n" +
			"socket. Databases boot lazily on first connect and reap when idle.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			d, err := daemon.New(cfg, stderrLogger)
			if err != nil {
				return err
			}
			return d.Run(ctx)
		},
	}
}
