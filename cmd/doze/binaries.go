package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/binaries"
	"github.com/nerdmenot/doze/internal/runtime"
)

func binariesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "binaries",
		Aliases: []string{"bin"},
		Short:   "Inspect engine toolchains",
		Long: "binaries inspects the engine toolchains doze resolves from the mirror.\n" +
			"Resolved versions and checksums are pinned in doze.lock.",
	}
	cmd.AddCommand(binariesListCmd(), binariesWhichCmd(), binariesAvailableCmd())
	return cmd
}

func binariesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List declared instances and their pinned/cached toolchains",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			plat, _ := binaries.HostPlatform()
			lock, _ := binaries.LoadLock(filepath.Join(configDir(cfg.Path()), binaries.LockFileName))

			fmt.Printf("host platform: %s\n", plat.Triple)
			fmt.Printf("toolchain store: %s\n\n", cfg.Home)

			w := tabwriter.NewWriter(os.Stdout, 0, 2, 3, ' ', 0)
			fmt.Fprintln(w, "NAME\tENGINE\tSPEC\tRESOLVED\tSOURCE\tCACHED")
			for _, decl := range cfg.Instances {
				resolved, source, cached := "-", "-", "no"
				if pin, ok := lock.Get(decl.Type, decl.Version, plat); ok {
					resolved, source = pin.Resolved, pin.Source
					dir := filepath.Join(cfg.Home, decl.Type, pin.Resolved+"-"+plat.Triple, "bin")
					if st, err := os.Stat(dir); err == nil && st.IsDir() {
						cached = "yes"
					}
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", decl.Name, decl.Type, decl.Version, resolved, source, cached)
			}
			_ = w.Flush()
			return nil
		},
	}
}

func binariesWhichCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "which <instance>",
		Short: "Resolve and print the bin directory for an instance's toolchain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if cfg.Lookup(args[0]) == nil {
				return fmt.Errorf("instance %q is not declared in %s", args[0], cfg.Path())
			}
			rt, err := runtime.New(cfg)
			if err != nil {
				return err
			}
			rt.SetLogger(stderrLogger)
			tc, err := rt.ResolveToolchain(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Println(tc.BinDir)
			return nil
		},
	}
}

func configDir(path string) string {
	if path == "" {
		return "."
	}
	return filepath.Dir(path)
}
