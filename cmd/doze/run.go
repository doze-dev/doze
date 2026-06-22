package main

import (
	"errors"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/endpoints"
)

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run -- <command> [args...]",
		Short: "Run a command with instance connection strings injected",
		Long: "run ensures the doze daemon is up, injects each instance's connection\n" +
			"string into the environment (DATABASE_URL, … plus DOZE_<NAME>_URL), and\n" +
			"executes the command. Instances boot on first connect and reap when idle,\n" +
			"so the daemon keeps running after the command exits.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			// Ensure the daemon is up so connections boot instances on demand.
			if !daemonRunning(cfg) {
				if err := startDaemon(cfg); err != nil {
					return err
				}
			}
			eps, err := endpoints.For(cfg)
			if err != nil {
				return err
			}
			env := os.Environ()
			for k, v := range endpoints.EnvVars(eps) {
				env = append(env, k+"="+v)
			}

			c := exec.Command(args[0], args[1:]...)
			c.Env = env
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			err = c.Run()
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				os.Exit(ee.ExitCode())
			}
			return err
		},
	}
	cmd.Flags().SetInterspersed(false)
	return cmd
}
