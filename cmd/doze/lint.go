package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
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
				reportLint(shape, moduleTypes(shape), "--offline")
				return nil
			}
			if _, err := loadConfig(); err != nil {
				if missing := unreachableModules(shape); len(missing) > 0 {
					reportLint(shape, missing, "not cached, and the registry cannot be reached")
					return nil
				}
				return err
			}
			reportLint(shape, nil, "")
			return nil
		},
	}
	cmd.Flags().BoolVar(&offline, "offline", false, "check only what needs no module (no cache or network access)")
	return cmd
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

// unreachableModules returns the declared module types that are not in the
// cache, when the registry cannot be reached to fetch them — the one case in
// which a failed module pass says nothing about the config. With the registry
// reachable, or everything cached, it returns nil and the failure stands.
func unreachableModules(cfg *config.Config) []string {
	mm := activeHost.Modules()
	if mm == nil {
		return nil
	}
	var missing []string
	for _, t := range moduleTypes(cfg) {
		if _, _, ok := mm.Cached(t); !ok {
			missing = append(missing, t)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if _, err := mm.Catalog(); err == nil {
		return nil
	}
	return missing
}

// reportLint prints the verdict: what the config declares and, when the
// module pass did not run for some types, exactly which and why.
func reportLint(cfg *config.Config, unchecked []string, why string) {
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
	fmt.Printf("%s %s is valid: %s\n", ui.OK("✓"), cfg.Path(), summary)
	if len(unchecked) > 0 {
		fmt.Printf("  %s module blocks not checked (%s): %s\n", ui.Warn("!"), why, strings.Join(unchecked, ", "))
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
