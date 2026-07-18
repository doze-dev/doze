package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze-sdk/binaries"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/modules"
	"github.com/doze-dev/doze/internal/ui"
)

func modulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "modules",
		Aliases: []string{"mod"},
		Short:   "Maintain the module pins in doze.lock",
		Long: "modules maintains the engine-plugin pins in doze.lock — the lockfile CI\n" +
			"and teammates resolve against. Browsing what each engine is provided by\n" +
			"(and module docs) lives in the dash.",
	}
	cmd.AddCommand(mutating(modulesUpgradeCmd()))
	return cmd
}

// shallowProjectManager is projectManager over a SHALLOW config load — no
// driver lookups, so it works when the full load fails on the very gates the
// caller (upgrade) exists to fix. It returns the manager and the declared
// engine types in declaration order.
func shallowProjectManager() (*modules.Manager, []string, error) {
	sc, err := config.LoadShallow(configPath)
	if err != nil {
		return nil, nil, err
	}
	mm, err := shallowManager(sc.Modules, sc.Path())
	if err != nil {
		return nil, nil, err
	}
	var types []string
	seen := map[string]bool{}
	for _, d := range sc.Decls {
		mm.Require(d.Type, d.Version)
		if !seen[d.Type] && !modules.InTree(d.Type) {
			seen[d.Type] = true
			types = append(types, d.Type)
		}
	}
	return mm, types, nil
}

func shallowManager(mc config.ModulesConfig, cfgPath string) (*modules.Manager, error) {
	mm, err := modules.NewManager(dozeHome())
	if err != nil {
		return nil, err
	}
	mm.Configure(mc.Mirror, mc.Enabled, mc.Sources, mc.Versions)
	mm.SetLogger(stderrLogger)
	mm.UseLock(func() string { return filepath.Join(configDir(cfgPath), binaries.LockFileName) })
	mm.PersistWhen(func() bool { return lockWritesAllowed })
	return mm, nil
}

func modulesUpgradeCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:               "upgrade [engine-type ...]",
		ValidArgsFunction: engineTypeCompletion,
		Short:             "Move module pins in doze.lock to the newest compatible releases",
		Long: "upgrade re-resolves each engine type's module against the registry —\n" +
			"selecting the newest release compatible with this doze and the engine\n" +
			"versions the config declares — downloads and verifies it, and rewrites the\n" +
			"pin in doze.lock. Without arguments it upgrades every plugin-backed engine\n" +
			"type the config declares. Commit the updated doze.lock.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// A SHALLOW config read: upgrade must run when the full load fails on
			// the exact protocol/engine-support gate it exists to fix.
			mm, declared, err := shallowProjectManager()
			if err != nil {
				return err
			}
			types := args
			if len(types) == 0 {
				types = declared
			}
			if len(types) == 0 {
				fmt.Println("no plugin-backed engine types declared")
				return nil
			}

			table := ui.NewTable("ENGINE", "SOURCE", "PINNED", "SELECTED", "STATUS")
			upgrades, failures := 0, 0
			var failDetails []string // "engine: error", printed under the table
			for _, t := range types {
				pin, source, _ := mm.Pinned(t)
				from := pin.Version
				if from == "" {
					from = "-"
				}
				if check {
					insp, err := mm.Inspect(source, "")
					switch {
					case err != nil:
						failures++
						table.Row(t, source, from, "-", ui.Fail("failed"))
						failDetails = append(failDetails, fmt.Sprintf("%s: %v", t, err))
					case insp.Version != pin.Version:
						upgrades++
						table.Row(t, source, from, insp.Version, ui.Warn("upgrade available"))
					default:
						table.Row(t, source, from, insp.Version, ui.Muted("up to date"))
					}
					continue
				}
				old, next, changed, err := mm.Upgrade(cmd.Context(), t)
				switch {
				case err != nil:
					failures++
					table.Row(t, source, from, "-", ui.Fail("failed"))
					failDetails = append(failDetails, fmt.Sprintf("%s: %v", t, err))
				case changed:
					upgrades++
					if old == "" {
						old = "-"
					}
					table.Row(t, source, old, next, ui.OK("upgraded"))
				default:
					table.Row(t, source, from, next, ui.Muted("up to date"))
				}
			}
			fmt.Println(table.String())
			for _, d := range failDetails {
				fmt.Println(ui.Fail("✗") + " " + d)
			}
			if failures > 0 {
				return fmt.Errorf("%d module(s) failed", failures)
			}
			if check {
				if upgrades > 0 {
					return fmt.Errorf("%d module upgrade(s) available — run doze modules upgrade", upgrades)
				}
				fmt.Println("\nall module pins are at their newest compatible releases")
				return nil
			}
			if upgrades > 0 {
				fmt.Println("\ndoze.lock updated — commit it so the team and CI pick up the same builds")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report available upgrades without changing doze.lock (exit 1 if any)")
	return cmd
}
