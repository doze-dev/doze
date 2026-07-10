// Command doze is a weightless, no-Docker local Postgres: a lazy-splice daemon
// that boots a real per-database Postgres on first connect and reaps it when
// idle.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze-sdk/binaries"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/hostboot"
	"github.com/doze-dev/doze/internal/ui"
)

var (
	configPath string
	varFlags   []string // --var name=value (repeatable)

	// lockWritesAllowed is set per-invocation by rootCmd's PersistentPreRun:
	// true only for commands that materialize state (up, sync, wake, shell, …).
	// Read commands (status, lint, doctor, …) resolve modules in memory and
	// leave doze.lock byte-identical.
	lockWritesAllowed bool
)

// lockWritesKey marks a command as allowed to persist module pins and TOFU
// keys to doze.lock (see mutating()).
const lockWritesKey = "doze.lockWrites"

// mutating marks a command as one that may write doze.lock.
func mutating(cmd *cobra.Command) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[lockWritesKey] = "true"
	return cmd
}

func main() {
	// All exits funnel through here so realMain's defers (plugin reaping) always
	// run — an os.Exit anywhere deeper would orphan the engine-plugin processes.
	os.Exit(realMain())
}

// exitCodeError carries a child process's exit code up to main without
// bypassing deferred cleanup the way a raw os.Exit would.
type exitCodeError int

func (e exitCodeError) Error() string { return fmt.Sprintf("exit code %d", int(e)) }

func realMain() int {
	// Wire the process-global engine host (module manager, plugin manager, engine
	// resolver, config decode hooks). The exact same wiring backs the embeddable
	// facade — it lives in internal/hostboot so the two can't drift. The CLI-
	// specific policy (where doze.lock lives, when it may be written, how warnings
	// print) is passed in so behavior is byte-identical to the inline version.
	host, err := hostboot.Init(hostboot.Options{
		Home: dozeHome(),
		Logf: stderrLogger,
		LockPath: func() string {
			return filepath.Join(configDir(configPath), binaries.LockFileName)
		},
		PersistLock: func() bool { return lockWritesAllowed },
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, ui.ErrFail("error:")+" "+err.Error())
		return 1
	}
	defer host.Close()

	if err := rootCmd().Execute(); err != nil {
		// exitCodeError is also the silent-error sentinel: commands that have
		// already printed a styled report return it so the failure isn't
		// echoed twice, while the exit code still lands.
		var code exitCodeError
		if errors.As(err, &code) {
			return int(code)
		}
		fmt.Fprintln(os.Stderr, ui.ErrFail("error:")+" "+err.Error())
		return 1
	}
	return 0
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "doze",
		Short: "Weightless local databases & AWS services — real engines, lazy boot, idle reap",
		Long: "doze runs real local backing services without Docker — Postgres, MariaDB,\n" +
			"Valkey, Kvrocks, FerretDB (Mongo wire), Temporal, and built-in S3/SQS/SNS.\n" +
			"A thin proxy boots each service on its first connection and reaps it when\n" +
			"idle.\n\n" +
			"Running `doze` opens the dash — the primary surface: the live fleet, logs,\n" +
			"charts, a command palette (:wake · :sleep · :restart · :reset · :logs …),\n" +
			"and a manager for each built-in AWS service (press enter on it). The\n" +
			"commands below are the headless automation core (CI, scripts, Makefiles)\n" +
			"plus the tools for before the dash can run.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			lockWritesAllowed = cmd.Annotations[lockWritesKey] == "true"
			// A dry run must leave doze.lock byte-identical, whatever the
			// command's usual annotation says.
			if f := cmd.Flags().Lookup("dry-run"); f != nil && f.Value.String() == "true" {
				lockWritesAllowed = false
			}
			// init is excluded from the upward config search: it scaffolds in
			// the current directory and must never adopt (or with --force,
			// overwrite) a parent project's config.
			if cmd.Name() != "init" {
				resolveConfigPath()
			}
		},
	}
	root.PersistentFlags().StringVarP(&configPath, "config", "c", "doze.hcl", "path to doze.hcl")
	root.PersistentFlags().StringArrayVar(&varFlags, "var", nil, "set a config variable: --var name=value (repeatable)")

	// The CLI is deliberately small: the dash (`doze`) is the human surface —
	// wake/sleep/restart/reset/logs/resource-management all live inside it —
	// and what remains here is the automation projection (CI, Makefiles,
	// scripts) plus the tools for before the dash can run (init/lint/doctor).
	// The cobra groups mirror that structure so --help isn't flat-alphabetical.
	root.AddGroup(
		&cobra.Group{ID: groupCore, Title: "Automation core (scriptable stack lifecycle):"},
		&cobra.Group{ID: groupTools, Title: "Before-the-dash tools:"},
		&cobra.Group{ID: groupDash, Title: "The dash:"},
		&cobra.Group{ID: groupLock, Title: "Lockfile maintenance (CI):"},
	)
	root.AddCommand(
		// Automation core: scriptable stack lifecycle
		grouped(groupCore, mutating(upCmd())),
		grouped(groupCore, downCmd()),
		grouped(groupCore, mutating(syncCmd())),
		grouped(groupCore, treeCmd()),
		grouped(groupCore, envCmd()),
		grouped(groupCore, mutating(runCmd())),
		// Before-the-dash tools
		grouped(groupTools, lintCmd()),
		grouped(groupTools, initCmd()),
		grouped(groupTools, doctorCmd()),
		grouped(groupTools, dnsSetupCmd()),
		// The dash, explicitly (also the default command)
		grouped(groupDash, dashCmd()),
		// Lockfile maintenance (CI)
		grouped(groupLock, modulesCmd()),
		versionCmd(),
		// Hidden: the daemon self-exec entry point
		mutating(daemonCmd()),
	)
	return root
}

// Help group IDs, mirroring the deliberate command grouping above.
const (
	groupCore  = "core"
	groupTools = "tools"
	groupDash  = "dash"
	groupLock  = "lock"
)

// grouped assigns a command to a root help group.
func grouped(id string, cmd *cobra.Command) *cobra.Command {
	cmd.GroupID = id
	return cmd
}

// resolveConfigPath applies the upward config search: like git, cd anywhere
// inside the project and doze still finds it. Only the untouched default
// searches upward — an explicit --config path means exactly that path.
// Idempotent, so completion helpers may call it too.
func resolveConfigPath() {
	if configPath != "doze.hcl" {
		return
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if found := searchUpward("doze.hcl"); found != "" {
			configPath = found
		}
	}
}

// instanceCompletion completes instance-name arguments from a shallow config
// read — no driver lookups or plugin launches, completion must be instant.
// Each candidate carries its engine type as the completion description.
func instanceCompletion(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	resolveConfigPath()
	sc, err := config.LoadShallow(configPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	taken := map[string]bool{}
	for _, a := range args {
		taken[a] = true
	}
	var out []string
	for _, d := range sc.Decls {
		if !taken[d.Name] {
			out = append(out, d.Name+"\t"+d.Type)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// engineTypeCompletion completes engine-type arguments (modules which/docs/
// upgrade) from the declared blocks — shallow, instant, no network.
func engineTypeCompletion(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	resolveConfigPath()
	sc, err := config.LoadShallow(configPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	taken := map[string]bool{}
	for _, a := range args {
		taken[a] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range sc.Decls {
		if !seen[d.Type] && !taken[d.Type] {
			seen[d.Type] = true
			out = append(out, d.Type)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// searchUpward walks from the current directory's parent toward the filesystem
// root looking for name, returning the first hit ("" if none). The caller has
// already ruled out the current directory.
func searchUpward(name string) string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
}

// configDir returns the directory holding the config file — where doze.lock lives.
func configDir(path string) string {
	if path == "" {
		return "."
	}
	return filepath.Dir(path)
}

// dozeHome is the shared cache root (binaries + fetched modules), DOZE_HOME or
// ~/.doze — the same location the daemon and plugins use.
func dozeHome() string {
	if h := os.Getenv("DOZE_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".doze")
}

// loadConfig loads and validates the configuration referenced by --config,
// applying any --var overrides.
func loadConfig() (*config.Config, error) {
	cliVars, err := parseVarFlags(varFlags)
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadWithVars(configPath, cliVars)
	if err != nil && os.IsNotExist(err) {
		// The very first command in a new project shouldn't greet anyone with a
		// stat error — point at the way in.
		return nil, fmt.Errorf("no %s in this directory or any parent — run doze init to scaffold one, or point --config at your config", configPath)
	}
	return cfg, err
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
	case containsAny(msg, "ready", "awake", "converged", "cloned", "reclaimed"):
		prefix = ui.ErrOK("✓")
	case containsAny(msg, "failed", "unexpectedly", "error", "gave up", "cannot", "won't resolve"):
		prefix = ui.ErrFail("✗")
	case containsAny(msg, "booting", "waking", "reaping", "reaped", "shutting down",
		"downloading", "verifying", "fetching", "extracting"):
		prefix = ui.ErrMuted("›")
	default:
		prefix = ui.ErrMuted("doze:")
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
	return fmt.Errorf("instance %q is not declared; declared: %s (see doze status)", name, strings.Join(names, ", "))
}

func configLabel(cfg *config.Config) string {
	if p := cfg.Path(); p != "" {
		return p
	}
	return "the config"
}
