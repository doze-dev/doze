package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/binaries"
	"github.com/nerdmenot/doze/internal/engine"
)

func versionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "versions [engine]",
		Short: "List database versions available from the mirror",
		Long: "versions lists the engine versions the doze-binaries mirror offers (like\n" +
			"`nvm ls-remote`), marking which are installed locally and pinned in\n" +
			"doze.lock. With an engine argument it also shows the platforms each\n" +
			"version is built for.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			plat, err := binaries.HostPlatform()
			if err != nil {
				return err
			}
			lock, _ := binaries.LoadLock(filepath.Join(configDir(cfg.Path()), binaries.LockFileName))

			filterEngine := ""
			if len(args) == 1 {
				filterEngine = args[0]
			}

			mgr := binaries.NewManager(cfg.Home)

			// Each engine has its own release/index.json, so fetch per engine and
			// pull out its slice. With no argument we list every registered engine.
			engines := engine.Types()
			if filterEngine != "" {
				if _, ok := engine.Lookup(filterEngine); !ok {
					return fmt.Errorf("unknown engine %q", filterEngine)
				}
				engines = []string{filterEngine}
			}
			detail := filterEngine != ""

			for i, eng := range engines {
				if i > 0 {
					fmt.Println()
				}
				fmt.Println(eng)
				man, err := mgr.Manifest(eng)
				if err != nil {
					fmt.Printf("  (unavailable: %v)\n", err)
					continue
				}
				em := man.Engines[eng]

				pinned := map[string]bool{}
				for _, full := range lock.Resolved(eng) {
					pinned[full] = true
				}

				fulls := make([]string, 0, len(em.Artifacts))
				for full := range em.Artifacts {
					fulls = append(fulls, full)
				}
				sort.Slice(fulls, func(a, b int) bool { return versionLess(fulls[b], fulls[a]) }) // newest first

				if len(fulls) == 0 {
					fmt.Println("  (no versions)")
					continue
				}
				for _, full := range fulls {
					var marks []string
					if em.Versions[majorOf(full)] == full {
						marks = append(marks, "latest "+majorOf(full)+".x")
					}
					if installedToolchain(cfg.Home, eng, full, plat.Triple) {
						marks = append(marks, "installed")
					}
					if pinned[full] {
						marks = append(marks, "pinned")
					}
					line := "  " + full
					if detail {
						triples := make([]string, 0, len(em.Artifacts[full]))
						for t := range em.Artifacts[full] {
							triples = append(triples, t)
						}
						sort.Strings(triples)
						line += "  [" + strings.Join(triples, ", ") + "]"
					}
					if len(marks) > 0 {
						line += "  (" + strings.Join(marks, ", ") + ")"
					}
					fmt.Println(line)
				}
			}
			return nil
		},
	}
}

func majorOf(full string) string {
	if i := strings.IndexByte(full, '.'); i >= 0 {
		return full[:i]
	}
	return full
}

func installedToolchain(home, engine, full, triple string) bool {
	st, err := os.Stat(filepath.Join(home, engine, full+"-"+triple, "bin"))
	return err == nil && st.IsDir()
}

// versionLess reports whether dotted-numeric a < b.
func versionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var ai, bi int
		if i < len(as) {
			ai, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bi, _ = strconv.Atoi(bs[i])
		}
		if ai != bi {
			return ai < bi
		}
	}
	return false
}
