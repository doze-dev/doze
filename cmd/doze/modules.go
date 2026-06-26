package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze-sdk/binaries"
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
	cmd.AddCommand(modulesListCmd(), modulesWhichCmd(), modulesInfoCmd())
	return cmd
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
			fmt.Printf("source:  %s\nversion: %s\nmirror:  %s\n\n", insp.Source, insp.Version, mm.Mirror())
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 3, ' ', 0)
			fmt.Fprintln(w, "PLATFORM\tSIGNATURE\tARCHIVE")
			allSigned := true
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
				return fmt.Errorf("one or more artifacts are not validly signed by %s's publisher key", insp.Namespace)
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
				mm.Configure(mc.Mirror, mc.Enabled, mc.Sources)
				fmt.Printf("module mirror: %s\n\n", mm.Mirror())
			} else {
				fmt.Printf("module fetching is off (set DOZE_MODULES_MIRROR or a modules{} block to enable)\n\n")
			}

			seen := map[string]bool{}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 3, ' ', 0)
			fmt.Fprintln(w, "ENGINE\tSOURCE\tVERSION\tPATH")
			for _, decl := range cfg.Instances {
				if seen[decl.Type] {
					continue
				}
				seen[decl.Type] = true
				source, version, path := resolveSource(decl.Type, mm)
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", decl.Type, source, version, path)
			}
			_ = w.Flush()
			return nil
		},
	}
}

// resolveSource reports, without launching, how an engine type is provided.
func resolveSource(engineType string, mm *modules.Manager) (source, version, path string) {
	if p := os.Getenv("DOZE_" + strings.ToUpper(engineType) + "_PLUGIN"); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return "override", "-", p
		}
	}
	if mm != nil {
		if p, v, ok := mm.Cached(engineType); ok {
			return "module", v, p
		}
		return "module?", "-", "(not yet fetched)"
	}
	return "in-tree", "-", "-"
}

func modulesWhichCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "which <engine-type>",
		Short: "Fetch (if needed) and print the plugin binary for an engine type",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			engineType := args[0]
			if p := os.Getenv("DOZE_" + strings.ToUpper(engineType) + "_PLUGIN"); p != "" {
				fmt.Println(p)
				return nil
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			mc := cfg.Modules
			if !mc.Enabled && !modules.Enabled() {
				return fmt.Errorf("module fetching is off and no DOZE_%s_PLUGIN override is set", strings.ToUpper(engineType))
			}
			mm, err := modules.NewManager(dozeHome())
			if err != nil {
				return err
			}
			mm.Configure(mc.Mirror, mc.Enabled, mc.Sources)
			mm.SetLogger(stderrLogger)
			mm.UseLock(func() string { return filepath.Join(configDir(cfg.Path()), binaries.LockFileName) })
			path, err := mm.Resolve(context.Background(), engineType, "")
			if err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
}
