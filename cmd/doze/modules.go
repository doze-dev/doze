package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze-sdk/binaries"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/modules"
)

func modulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "modules",
		Aliases: []string{"mod"},
		Short:   "Inspect out-of-process engine plugin modules",
		Long: "modules inspects how each engine is provided: a compiled-in driver, a\n" +
			"local DOZE_<TYPE>_PLUGIN override, or a plugin module fetched from\n" +
			"doze-modules (DOZE_MODULES_MIRROR) and cached under ~/.doze/modules.",
	}
	cmd.AddCommand(modulesListCmd(), modulesWhichCmd(), modulesInfoCmd(), modulesSearchCmd(), modulesUpgradeCmd(), modulesDocsCmd())
	return cmd
}

// projectManager builds a module Manager configured like the main resolver:
// the project's modules{} block applied, lock wired, requirements fed from the
// declared instances.
func projectManager(cfg *config.Config) (*modules.Manager, error) {
	mm, err := shallowManager(cfg.Modules, cfg.Path())
	if err != nil {
		return nil, err
	}
	for _, decl := range cfg.Instances {
		mm.Require(decl.Type, string(decl.Version))
	}
	return mm, nil
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
	return mm, nil
}

func modulesUpgradeCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "upgrade [engine-type ...]",
		Short: "Move module pins in doze.lock to the newest compatible releases",
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

			w := tabwriter.NewWriter(os.Stdout, 0, 2, 3, ' ', 0)
			fmt.Fprintln(w, "ENGINE\tSOURCE\tPINNED\tSELECTED\tSTATUS")
			upgrades, failures := 0, 0
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
						fmt.Fprintf(w, "%s\t%s\t%s\t-\t%v\n", t, source, from, err)
					case insp.Version != pin.Version:
						upgrades++
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\tupgrade available\n", t, source, from, insp.Version)
					default:
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\tup to date\n", t, source, from, insp.Version)
					}
					continue
				}
				old, next, changed, err := mm.Upgrade(cmd.Context(), t)
				switch {
				case err != nil:
					failures++
					fmt.Fprintf(w, "%s\t%s\t%s\t-\t%v\n", t, source, from, err)
				case changed:
					upgrades++
					if old == "" {
						old = "-"
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\tupgraded\n", t, source, old, next)
				default:
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\tup to date\n", t, source, from, next)
				}
			}
			_ = w.Flush()
			if failures > 0 {
				return fmt.Errorf("%d module(s) failed", failures)
			}
			if check {
				if upgrades > 0 {
					return fmt.Errorf("%d module upgrade(s) available — run 'doze modules upgrade'", upgrades)
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

func modulesSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "search [query]",
		Aliases: []string{"available"},
		Short:   "Search the registry for published modules",
		Long: "search lists the modules published to the registry (the live index.json\n" +
			"catalog), optionally filtered by a query against the source/tagline. This is\n" +
			"how you discover what's available — no module list is built into doze.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			q := ""
			if len(args) == 1 {
				q = strings.ToLower(args[0])
			}
			mm, err := modules.NewManager(dozeHome())
			if err != nil {
				return err
			}
			mm.SetLogger(stderrLogger)
			entries, err := mm.CatalogModules()
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 3, ' ', 0)
			fmt.Fprintln(w, "SOURCE\tENGINE VERSIONS\t\tDESCRIPTION")
			shown := 0
			for _, e := range entries {
				if q != "" && !strings.Contains(strings.ToLower(e.Source+" "+e.Tagline+" "+e.Category), q) {
					continue
				}
				vers := "built-in"
				if len(e.EngineVersions) > 0 {
					vers = strings.Join(e.EngineVersions, " ")
				}
				badge := ""
				if e.Official {
					badge = "official"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Source, vers, badge, e.Tagline)
				shown++
			}
			_ = w.Flush()
			if shown == 0 {
				fmt.Printf("no modules match %q in %s\n", q, mm.Mirror())
				return nil
			}
			fmt.Printf("\nuse one: declare the engine type, or `modules { <type> { source = \"<ns>/<name>\" } }`\n")
			return nil
		},
	}
}

func modulesInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "info <source>",
		Aliases: []string{"verify"},
		Short:   "Fetch a registry source's index and verify its signatures",
		Long: "info fetches the module index for a registry source (e.g. doze/postgres),\n" +
			"pins the namespace's publisher key trust-on-first-use, and reports each\n" +
			"platform artifact and whether its ed25519 signature verifies — the same\n" +
			"check doze enforces before running a module. No archives are downloaded.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			source := args[0]
			// Pure inspection of a registry source — no project config is loaded, so
			// it has no side effects on the working directory's stack. The mirror comes
			// from DOZE_MODULES_MIRROR (else the public registry).
			mm, err := modules.NewManager(dozeHome())
			if err != nil {
				return err
			}
			mm.SetLogger(stderrLogger)

			insp, err := mm.Inspect(source, "")
			if err != nil {
				return err
			}
			engines := "any (versionless)"
			if len(insp.Engines) > 0 {
				engines = strings.Join(insp.Engines, ", ")
			}
			idxSig := "✓ signed"
			if !insp.IndexSigned {
				idxSig = "✗ UNSIGNED"
			}
			fmt.Printf("source:   %s\nmodule:   %s (stable)\nprotocol: %d\nengines:  %s\nreleases: %s\nindex:    %s\nmirror:   %s\n\n",
				insp.Source, insp.Version, insp.Protocol, engines,
				strings.Join(insp.Releases, ", "), idxSig, mm.Mirror())
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 3, ' ', 0)
			fmt.Fprintln(w, "PLATFORM\tSIGNATURE\tARCHIVE")
			allSigned := insp.IndexSigned
			for _, p := range insp.Platforms {
				sig := "✓ signed"
				if !p.Signed {
					sig = "✗ UNSIGNED"
					allSigned = false
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", p.Triple, sig, p.URL)
			}
			_ = w.Flush()
			if !allSigned {
				return fmt.Errorf("the index or one of its artifacts is not validly signed by %s's publisher key", insp.Namespace)
			}
			return nil
		},
	}
}

func modulesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List each declared engine type and how it's provided",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			var mm *modules.Manager
			if mc := cfg.Modules; mc.Enabled || modules.Enabled() {
				if mm, err = modules.NewManager(dozeHome()); err != nil {
					return err
				}
				mm.Configure(mc.Mirror, mc.Enabled, mc.Sources, mc.Versions)
				mm.UseLock(func() string { return filepath.Join(configDir(cfg.Path()), binaries.LockFileName) })
				fmt.Printf("module mirror: %s\n\n", mm.Mirror())
			} else {
				fmt.Printf("module fetching is off (set DOZE_MODULES_MIRROR or a modules{} block to enable)\n\n")
			}

			seen := map[string]bool{}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 3, ' ', 0)
			fmt.Fprintln(w, "ENGINE\tPROVIDED BY\tMODULE\tENGINES\tPATH")
			for _, decl := range cfg.Instances {
				if seen[decl.Type] {
					continue
				}
				seen[decl.Type] = true
				source, version, engines, path := resolveSource(decl.Type, mm)
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", decl.Type, source, version, engines, path)
			}
			_ = w.Flush()
			return nil
		},
	}
}

// resolveSource reports, without launching, how an engine type is provided. The
// MODULE column is the plugin release (doze.lock pin first, else the cached
// build) — a different axis from the engine version declared on the block.
func resolveSource(engineType string, mm *modules.Manager) (source, version, engines, path string) {
	if modules.InTree(engineType) {
		return "in-tree", "-", "-", "-"
	}
	if p := os.Getenv("DOZE_" + strings.ToUpper(engineType) + "_PLUGIN"); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return "override", "-", "-", p
		}
	}
	if mm == nil {
		return "module?", "-", "-", "(module fetching is off)"
	}
	engines = "-"
	if pin, src, ok := mm.Pinned(engineType); ok {
		if len(pin.Engines) > 0 {
			engines = strings.Join(pin.Engines, " ")
		} else {
			engines = "any"
		}
		path = "(not yet fetched)"
		if p, v, cached := mm.Cached(engineType); cached && v == pin.Version {
			path = p
		}
		return src, pin.Version, engines, path
	}
	if p, v, ok := mm.Cached(engineType); ok {
		return "module (unpinned)", v, engines, p
	}
	return "module?", "-", "-", "(not yet fetched)"
}

func modulesWhichCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "which <engine-type>",
		Short: "Fetch (if needed) and print the plugin binary for an engine type",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engineType := args[0]
			if p := os.Getenv("DOZE_" + strings.ToUpper(engineType) + "_PLUGIN"); p != "" {
				fmt.Println(p)
				return nil
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if mc := cfg.Modules; !mc.Enabled && !modules.Enabled() {
				return fmt.Errorf("module fetching is off and no DOZE_%s_PLUGIN override is set", strings.ToUpper(engineType))
			}
			mm, err := projectManager(cfg)
			if err != nil {
				return err
			}
			path, err := mm.Resolve(cmd.Context(), engineType)
			if err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
}
