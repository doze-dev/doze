// Command doze is a weightless, no-Docker local Postgres: a lazy-splice daemon
// that boots a real per-database Postgres on first connect and reaps it when
// idle.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	_ "github.com/nerdmenot/doze/engine/documentdb" // register the documentdb driver
	_ "github.com/nerdmenot/doze/engine/kvrocks"    // register the kvrocks driver
	"github.com/nerdmenot/doze/engine/postgres"
	"github.com/nerdmenot/doze/engine/s3"
	"github.com/nerdmenot/doze/engine/sns"
	"github.com/nerdmenot/doze/engine/sqs"
	_ "github.com/nerdmenot/doze/engine/valkey" // register the valkey driver
	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/ui"
)

var (
	configPath string
	varFlags   []string // --var name=value (repeatable)
)

func main() {
	// Surface engine convergence warnings on stderr (the daemon redirects its
	// stderr to the log file). Importing engine/postgres also registers the driver.
	postgres.Logf = stderrLogger
	s3.Logf = stderrLogger
	sqs.Logf = stderrLogger
	sns.Logf = stderrLogger
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "doze",
		Short: "Weightless local databases & AWS services — real engines, lazy boot, idle reap",
		Long: "doze runs real local backing services without Docker — Postgres, Valkey,\n" +
			"Kvrocks, FerretDB, and built-in S3/SQS/SNS. A thin proxy routes each\n" +
			"connection to a per-instance backend, booting it on first connect and\n" +
			"reaping it when idle.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&configPath, "config", "c", "doze.hcl", "path to doze.hcl")
	root.PersistentFlags().StringArrayVar(&varFlags, "var", nil, "set a config variable: --var name=value (repeatable)")

	root.AddCommand(
		serveCmd(),
		startCmd(),
		stopCmd(),
		restartCmd(),
		logsCmd(),
		bootCmd(),
		planCmd(),
		applyCmd(),
		destroyCmd(),
		downCmd(),
		resetCmd(),
		statusCmd(),
		runCmd(),
		envCmd(),
		outputCmd(),
		ephemeralCmd(),
		versionsCmd(),
		psqlCmd(),
		dashCmd(),
		binariesCmd(),
		initCmd(),
		doctorCmd(),
		versionCmd(),
		serveInternalCmd(),
	)
	return root
}

// loadConfig loads and validates the configuration referenced by --config,
// applying any --var overrides.
func loadConfig() (*config.Config, error) {
	cliVars, err := parseVarFlags(varFlags)
	if err != nil {
		return nil, err
	}
	return config.LoadWithVars(configPath, cliVars)
}

// parseVarFlags turns repeated --var name=value flags into a map.
func parseVarFlags(flags []string) (map[string]string, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(flags))
	for _, f := range flags {
		name, val, ok := strings.Cut(f, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid --var %q: expected name=value", f)
		}
		out[name] = val
	}
	return out, nil
}

// stderrLogger is the daemon/engine logging sink for foreground commands. It
// styles runtime progress (boot/ready/reap/failure) with symbols and color; ui
// renders plain when stderr's peer isn't a terminal (piped output, log files).
func stderrLogger(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	var prefix string
	switch {
	case containsAny(msg, "ready", "converged", "cloned", "reclaimed"):
		prefix = ui.OK("✓")
	case containsAny(msg, "failed", "unexpectedly", "error"):
		prefix = ui.Fail("✗")
	case containsAny(msg, "booting", "reaping", "reaped", "shutting down"):
		prefix = ui.Muted("›")
	default:
		prefix = ui.Muted("doze:")
	}
	fmt.Fprintln(os.Stderr, prefix+" "+msg)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// instanceNotFound is the shared "unknown instance" error: it lists the declared
// instances so the user can correct the name without hunting through the config.
func instanceNotFound(cfg *config.Config, name string) error {
	var names []string
	for _, d := range cfg.Instances {
		names = append(names, d.Name)
	}
	if len(names) == 0 {
		return fmt.Errorf("instance %q is not declared (no instances in %s)", name, configLabel(cfg))
	}
	return fmt.Errorf("instance %q is not declared; declared: %s (see `doze status`)", name, strings.Join(names, ", "))
}

func configLabel(cfg *config.Config) string {
	if p := cfg.Path(); p != "" {
		return p
	}
	return "the config"
}
