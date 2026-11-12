package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/modules"
	"github.com/doze-dev/doze/internal/ui"
)

func lintCmd() *cobra.Command {
	var offline bool
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Validate the config without touching the daemon or any data",
		Long: "lint statically checks doze.hcl in two passes.\n\n" +
			"The first needs nothing but the files: block kinds and names, the\n" +
			"arguments of app, service and task blocks, variables, references that\n" +
			"point at something declared, and the dependency graph (acyclic, and no\n" +
			"enabled block depending on a disabled one).\n\n" +
			"The second hands each module its own block to check, which needs the\n" +
			"module: from the cache, or fetched. If a module is not cached and the\n" +
			"registry cannot be reached, lint says which blocks went unchecked and\n" +
			"passes on the first pass alone. --offline skips the second pass outright.\n\n" +
			"It runs nothing and changes nothing — safe for CI and pre-commit hooks.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cliVars, err := parseVarFlags(varFlags)
			if err != nil {
				return err
			}
			shape, err := config.LoadShape(configPath, cliVars)
			if err != nil {
				if os.IsNotExist(err) {
					return noConfigError()
				}
				return err
			}
			if offline {
				reportLint(shape, moduleTypes(shape), "--offline", nil)
				return nil
			}
			_, err = loadConfig()
			return lintVerdict(shape, err)
		},
	}
	cmd.Flags().BoolVar(&offline, "offline", false, "check only what needs no module (no cache or network access)")
	return cmd
}

// lintVerdict reports the module pass's outcome (err is what the full load
// returned) and is the error lint exits with.
func lintVerdict(shape *config.Config, err error) error {
	if err == nil {
		reportLint(shape, nil, "", nil)
		return nil
	}
	var un *modules.UnreachableError
	if !errors.As(err, &un) {
		return err
	}
	// The module pass could not run: a mistake in those blocks would pass
	// unseen, so this is not a pass. It exits 5, the code for "a module it
	// names won't fetch", as status, env and url do for the same cause.
	reportLint(shape, unreachableModules(shape, un.Module), "", un)
	return shown(err)
}

// moduleTypes returns the distinct module types a config declares, sorted.
func moduleTypes(cfg *config.Config) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range cfg.Instances {
		if d.Kind == config.KindModule && !seen[d.Type] {
			seen[d.Type] = true
			out = append(out, d.Type)
		}
	}
	sort.Strings(out)
	return out
}

// unreachableModules returns the declared module types the module pass could
// not check because the registry did not answer: every one not in the cache
// (the load stops at the first, failed; the others would fail the same way),
// and always failed itself.
func unreachableModules(cfg *config.Config, failed string) []string {
	var mm *modules.Manager
	if activeHost != nil {
		mm = activeHost.Modules()
	}
	var missing []string
	for _, t := range moduleTypes(cfg) {
		if t == failed {
			missing = append(missing, t)
			continue
		}
		if mm == nil {
			continue
		}
		if _, _, ok := mm.Cached(t); !ok {
			missing = append(missing, t)
		}
	}
	if len(missing) == 0 {
		missing = []string{failed}
	}
	return missing
}

// reportLint prints the verdict: what the config declares and, when the
// module pass did not run for some types, exactly which and why. A tick is
// given only for what was checked: with un set (the registry did not answer)
// there is none.
func reportLint(cfg *config.Config, unchecked []string, why string, un *modules.UnreachableError) {
	count := map[string]int{}
	disabled := 0
	for _, d := range cfg.Instances {
		count[d.Kind]++
		if !d.Enabled {
			disabled++
		}
	}
	count[config.KindService] = len(cfg.Services)
	count[config.KindTask] = len(cfg.Tasks)
	var parts []string
	for _, kind := range []string{config.KindModule, config.KindApp, config.KindService, config.KindTask} {
		if n := count[kind]; n > 0 {
			parts = append(parts, plural(n, kind))
		}
	}
	if len(parts) == 0 {
		parts = []string{"nothing declared"}
	}
	summary := strings.Join(parts, ", ")
	if disabled > 0 {
		summary += fmt.Sprintf(" (%d disabled)", disabled)
	}
	if unchecked == nil {
		unchecked = []string{}
	}
	if un != nil {
		why = fmt.Sprintf("module %s could not be fetched: the registry can't be reached (%s: %s)", un.Module, un.Host, un.Cause)
	}
	setResult(struct {
		Config    string         `json:"config"`
		Declared  map[string]int `json:"declared"` // by kind
		Disabled  int            `json:"disabled"`
		Unchecked []string       `json:"unchecked"` // module types the module pass did not check
		Why       string         `json:"why,omitempty"`
		Warnings  []string       `json:"warnings,omitempty"`
	}{cfg.Path(), count, disabled, unchecked, why, cfg.Warnings})
	switch {
	case un != nil:
		fmt.Printf("%s %s: not checked: %s\n", ui.Fail("✗"), cfg.Path(), strings.Join(unchecked, ", "))
		fmt.Printf("  %s\n", why)
		fmt.Printf("  only what needs no module was checked (%s); a mistake in a %s block would pass unseen\n", summary, strings.Join(unchecked, " or "))
		fmt.Printf("  next: run doze lint again once the registry answers · doze doctor checks it\n")
	case len(unchecked) > 0:
		fmt.Printf("%s %s passes the checks that need no module: %s\n", ui.OK("✓"), cfg.Path(), summary)
		fmt.Printf("  %s module blocks not checked (%s): %s\n", ui.Warn("!"), why, strings.Join(unchecked, ", "))
	default:
		fmt.Printf("%s %s is valid: %s\n", ui.OK("✓"), cfg.Path(), summary)
	}
	for _, w := range cfg.Warnings {
		fmt.Printf("  %s %s\n", ui.Warn("!"), w)
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
