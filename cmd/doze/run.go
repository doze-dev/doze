package main

import (
	"errors"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func runCmd() *cobra.Command {
	var injectEnv bool
	cmd := &cobra.Command{
		Use:   "run -- <command> [args...]",
		Short: "Ensure the daemon is up, then run a command",
		Long: "run ensures the doze daemon is up (so instances boot on first connect and\n" +
			"reap when idle) and then executes the command — useful as a wrapper around a\n" +
			"test or dev-server command so the backends are awake before it connects. By\n" +
			"default doze injects nothing into the environment: because every instance has\n" +
			"an explicit port, your connection strings are stable — put them in your app\n" +
			"config, or declare the app as a `process` block to have its dependencies'\n" +
			"URLs injected. --env opts in to injecting the same variables `doze env`\n" +
			"prints (DATABASE_URL, AWS_ENDPOINT_URL_*, dummy AWS creds, …).",
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
			c := exec.Command(args[0], args[1:]...)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			if injectEnv {
				pairs, err := envAssignments(cfg, nil)
				if err != nil {
					return err
				}
				c.Env = os.Environ()
				for _, p := range pairs {
					c.Env = append(c.Env, p.Var+"="+p.Value)
				}
			}
			err = c.Run()
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				// Propagate the child's exit code without os.Exit: main unwraps
				// this after deferred cleanup (plugin reaping) has run.
				return exitCodeError(ee.ExitCode())
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&injectEnv, "env", false, "inject the `doze env` variables into the command's environment")
	cmd.Flags().SetInterspersed(false)
	return cmd
}
