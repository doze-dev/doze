package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/control"
	"github.com/nerdmenot/doze/internal/daemon"
	"github.com/nerdmenot/doze/internal/runtime"
	"github.com/nerdmenot/doze/internal/ui"
)

func upCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up [db]",
		Short: "Converge config: provision declared instances",
		Long: "up brings the local environment in line with doze.hcl — creating or\n" +
			"updating databases, schemas, roles, privileges, extensions, and the\n" +
			"declared S3 buckets / SQS queues / SNS topics. It is idempotent. With no\n" +
			"argument it converges every instance.",
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

			// Prefer a running daemon so we don't fight over data-dir locks.
			client := control.NewClient(daemon.ControlSocketPath(cfg))
			if client.Available() {
				return convergeViaDaemon(client, cfg, target)
			}

			// Standalone: provision directly, leaving nothing running.
			rt, err := runtime.New(cfg)
			if err != nil {
				return err
			}
			rt.SetLogger(stderrLogger)
			if err := rt.EnsureDataRoot(); err != nil {
				return err
			}
			ctx := context.Background()
			for _, name := range targetNames(cfg, target) {
				if err := rt.ProvisionOnly(ctx, name); err != nil {
					return err
				}
				fmt.Println(ui.OK("✓") + " converged " + name)
			}
			return nil
		},
	}
}

// convergeViaDaemon converges each target through the running daemon, printing
// per-instance progress so the user sees what happened (the control op is
// synchronous, so a line per target is enough).
func convergeViaDaemon(client *control.Client, cfg *config.Config, target string) error {
	for _, name := range targetNames(cfg, target) {
		fmt.Print(ui.Muted("› converging "+name+"…") + "\r")
		if _, err := client.Do(control.Request{Op: "up", DB: name}); err != nil {
			fmt.Println(ui.Fail("✗") + " " + name + ": " + err.Error())
			return err
		}
		fmt.Println(ui.OK("✓") + " converged " + name + "    ")
	}
	return nil
}

// targetNames resolves a target (or all declared instances when empty).
func targetNames(cfg *config.Config, target string) []string {
	if target != "" {
		return []string{target}
	}
	names := make([]string, 0, len(cfg.Instances))
	for _, decl := range cfg.Instances {
		names = append(names, decl.Name)
	}
	return names
}
