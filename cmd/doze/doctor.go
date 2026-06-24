package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/binaries"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the doze environment and configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			ok := check("config", cfg.Path()+" parses cleanly", true)

			plat, perr := binaries.HostPlatform()
			ok = check("platform", plat.Triple, perr == nil) && ok

			check("home", cfg.Home, true)
			writable := os.MkdirAll(cfg.ProjectDir(), 0o700) == nil
			ok = check("project", cfg.ProjectDir(), writable) && ok

			lock, _ := binaries.LoadLock(filepath.Join(configDir(cfg.Path()), binaries.LockFileName))
			for _, decl := range cfg.Instances {
				label := decl.Type + "/" + decl.Name
				pin, pinned := lock.Get(decl.Type, decl.Version, plat)
				if !pinned {
					check(label, decl.Version.String()+" (not pinned; resolves on first use)", true)
					continue
				}
				detail := decl.Version.String() + " → " + pin.Resolved + " (" + pin.Source + ")"
				cached := false
				if pin.Resolved != "" {
					if st, err := os.Stat(filepath.Join(cfg.Home, decl.Type, pin.Resolved+"-"+plat.Triple, "bin")); err == nil {
						cached = st.IsDir()
					}
				}
				if cached {
					detail += " — cached"
				} else {
					detail += " — not cached (downloads on first use)"
				}
				check(label, detail, true)
			}

			if daemonRunning(cfg) {
				check("daemon", "running", true)
			} else {
				check("daemon", "stopped (starts automatically on first use)", true)
			}

			fmt.Println()
			if ok {
				fmt.Println("all checks passed")
			} else {
				fmt.Println("some checks need attention (see ✗ above)")
			}
			return nil
		},
	}
}

func check(label, detail string, ok bool) bool {
	mark := "✓"
	if !ok {
		mark = "✗"
	}
	fmt.Printf("  %s  %-16s %s\n", mark, label, detail)
	return ok
}
