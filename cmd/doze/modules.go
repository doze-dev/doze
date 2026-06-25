package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/binaries"
	"github.com/nerdmenot/doze/internal/modules"
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
	cmd.AddCommand(modulesListCmd(), modulesWhichCmd())
	return cmd
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
				mm.Configure(mc.Mirror, mc.Enabled, mc.Versions)
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
			mm.Configure(mc.Mirror, mc.Enabled, mc.Versions)
			mm.SetLogger(stderrLogger)
			mm.UseLock(func() string { return filepath.Join(configDir(cfg.Path()), binaries.LockFileName) })
			path, err := mm.Resolve(context.Background(), engineType, mc.Versions[engineType])
			if err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
}
