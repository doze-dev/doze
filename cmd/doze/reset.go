package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/binaries"
	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/control"
	"github.com/nerdmenot/doze/internal/daemon"
	"github.com/nerdmenot/doze/internal/supervisor"
)

func resetCmd() *cobra.Command {
	var force, hard, dropBinaries bool
	cmd := &cobra.Command{
		Use:   "reset [instance]",
		Short: "Wipe an instance's data and start fresh (all instances if none named)",
		Long: "reset stops the backend(s) and deletes their data directories. The next\n" +
			"connection re-provisions a fresh store and re-converges the declared\n" +
			"structure — roles, databases, schemas, extensions — so you get your schema\n" +
			"back with no rows. It's the clean-slate counterpart to `stop` (which only\n" +
			"reaps the process).\n\n" +
			"By default the downloaded engine toolchains are kept (immutable and\n" +
			"checksum-verified, so re-downloading yields identical bytes). --binaries\n" +
			"additionally drops the cached toolchain so the next boot re-downloads and\n" +
			"re-verifies it against doze.lock; --hard also drops the shared data-dir\n" +
			"template so even the initdb base is rebuilt.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			// The instances to reset: the named one, or every declared instance.
			var names []string
			if len(args) == 1 {
				if cfg.Lookup(args[0]) == nil {
					return instanceNotFound(cfg, args[0])
				}
				names = []string{args[0]}
			} else {
				for _, decl := range cfg.Instances {
					names = append(names, decl.Name)
				}
			}
			if len(names) == 0 {
				fmt.Println("no instances declared in", cfg.Path())
				return nil
			}

			// Engines involved, for the optional --hard template wipe.
			engineSet := map[string]bool{}
			for _, n := range names {
				if decl := cfg.Lookup(n); decl != nil {
					engineSet[decl.Type] = true
				}
			}

			if !force && !confirmReset(names, engineSet, hard, dropBinaries) {
				fmt.Println("aborted")
				return nil
			}

			client := control.NewClient(daemon.ControlSocketPath(cfg))
			daemonUp := client.Available()

			for _, name := range names {
				// Stop the backend first — deleting a live cluster's data dir would
				// corrupt a still-running process. Via the daemon this is synchronous
				// (it reaps and removes the pidfile before replying); without one we
				// signal the backend directly and wait for it to exit.
				if daemonUp {
					if _, err := client.Do(control.Request{Op: "down", DB: name}); err != nil {
						return fmt.Errorf("stopping %q: %w", name, err)
					}
				} else if _, err := stopByPidFile(cfg, name); err != nil {
					return err
				}
				if err := waitBackendStopped(cfg, name); err != nil {
					return err
				}
				if err := os.RemoveAll(cfg.ClusterDir(name)); err != nil {
					return fmt.Errorf("removing data for %q: %w", name, err)
				}
				if err := os.RemoveAll(cfg.SocketDir(name)); err != nil {
					return fmt.Errorf("removing sockets for %q: %w", name, err)
				}
				fmt.Println("reset", name)
			}

			if hard {
				for _, eng := range sortedKeys(engineSet) {
					// Templates are shared across projects in this home; removing them
					// makes the next boot rebuild the initdb base for every project on
					// this engine version. They regenerate automatically.
					tdir := filepath.Join(cfg.Home, eng, "_templates")
					if err := os.RemoveAll(tdir); err != nil {
						return fmt.Errorf("removing %s template: %w", eng, err)
					}
				}
				fmt.Println("dropped shared template(s) for:", strings.Join(sortedKeys(engineSet), ", "))
			}

			if dropBinaries {
				plat, err := binaries.HostPlatform()
				if err != nil {
					return err
				}
				var dropped []string
				for _, eng := range sortedKeys(engineSet) {
					r, err := clearToolchainCache(cfg.Home, eng, plat.Triple)
					if err != nil {
						return fmt.Errorf("clearing %s toolchain cache: %w", eng, err)
					}
					dropped = append(dropped, r...)
				}
				if len(dropped) == 0 {
					fmt.Println("no cached toolchains to drop")
				} else {
					fmt.Println("dropped cached toolchain(s):", strings.Join(dropped, ", "))
					fmt.Println("  the next boot re-downloads and verifies them against doze.lock")
				}
			}

			fmt.Println("done — the next connection re-provisions a fresh store")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "y", false, "skip the confirmation prompt")
	cmd.Flags().BoolVar(&dropBinaries, "binaries", false, "also drop the cached engine toolchain so it re-downloads on next boot")
	cmd.Flags().BoolVar(&hard, "hard", false, "also drop the shared data-dir template (rebuilds the initdb base)")
	return cmd
}

// clearToolchainCache removes the cached toolchain directories for an engine on
// the host platform (Home/<engine>/<full>-<triple>), leaving the shared
// _templates sibling alone. The next boot re-downloads and re-verifies them.
func clearToolchainCache(home, eng, triple string) ([]string, error) {
	dir := filepath.Join(home, eng)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var removed []string
	for _, e := range entries {
		if e.Name() == "_templates" || !strings.HasSuffix(e.Name(), "-"+triple) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return removed, err
		}
		removed = append(removed, eng+"/"+e.Name())
	}
	return removed, nil
}

// confirmReset prints exactly what will be deleted and reads a y/N answer.
func confirmReset(names []string, engineSet map[string]bool, hard, dropBinaries bool) bool {
	fmt.Println("This permanently deletes the data for:")
	for _, n := range names {
		fmt.Println("  •", n)
	}
	if dropBinaries {
		fmt.Printf("…and the cached %s toolchain(s) (re-downloaded + verified on next boot).\n",
			strings.Join(sortedKeys(engineSet), "/"))
	}
	if hard {
		fmt.Printf("…and the shared %s template(s) (rebuilt on next boot, affects other projects).\n",
			strings.Join(sortedKeys(engineSet), "/"))
	}
	fmt.Print("Proceed? [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// waitBackendStopped blocks (briefly) until no live process remains for the
// instance, so the data dir can be safely removed. It checks both doze's backend
// pidfile (every engine) and a Postgres postmaster.pid (postgres/documentdb).
func waitBackendStopped(cfg *config.Config, name string) error {
	pidFiles := []string{
		filepath.Join(cfg.RunDir(), "backend-"+name+".pid"),
		filepath.Join(cfg.ClusterDir(name), "postmaster.pid"),
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		alive := false
		for _, pf := range pidFiles {
			if pid := readPidFile(pf); pid > 0 && supervisor.ProcessAlive(pid) {
				alive = true
			}
		}
		if !alive {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("backend %q is still running; stop it (`doze stop %s`) before resetting", name, name)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// readPidFile returns the pid on the first line of a pidfile, or 0.
func readPidFile(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	line := strings.SplitN(string(raw), "\n", 2)[0]
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		return 0
	}
	return pid
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
