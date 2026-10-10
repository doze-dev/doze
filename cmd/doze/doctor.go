package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	names "github.com/doze-dev/doze-names"
	"github.com/doze-dev/doze-sdk/binaries"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/ui"
)

// doctorCheck is one diagnostic result — collected first, rendered as the
// table or as --json after.
type doctorCheck struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
	OK     bool   `json:"ok"`
}

func doctorCmd() *cobra.Command {
	var jsonOut, fix, printFix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the doze environment and configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if fix || printFix {
				return namesFix(printFix)
			}
			var checks []doctorCheck
			add := func(name, detail string, ok bool) {
				checks = append(checks, doctorCheck{Name: name, Detail: detail, OK: ok})
			}

			// A broken config is doctor's most common patient: report it as a
			// failing check and keep diagnosing the environment around it.
			cfg, cfgErr := loadConfig()
			if cfgErr != nil {
				add("config", cfgErr.Error(), false)
			} else {
				add("config", cfg.Path()+" parses cleanly", true)
			}

			plat, perr := binaries.HostPlatform()
			add("platform", plat.Triple, perr == nil)

			if cfg == nil {
				add("home", dozeHome(), true)
			} else {
				add("home", cfg.Home, true)
				writable := os.MkdirAll(cfg.ProjectDir(), 0o700) == nil
				add("project", cfg.ProjectDir(), writable)

				lock, _ := binaries.LoadLock(filepath.Join(configDir(cfg.Path()), binaries.LockFileName))
				for _, decl := range cfg.Instances {
					label := decl.Type + "/" + decl.Name
					pin, pinned := lock.Get(decl.Type, decl.Version, plat)
					if !pinned {
						add(label, decl.Version.String()+" (not pinned; resolves on first use)", true)
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
					add(label, detail, true)
				}

				if daemonRunning(cfg) {
					add("daemon", "running", true)
				} else {
					add("daemon", "stopped (starts automatically on first use)", true)
				}

				if cfg.Defaults.Domains {
					// A LIVE probe through the system resolver — the same path
					// curl/psql take — is the only honest check. Requires the
					// daemon (it answers the queries).
					// Probe a real per-service domain; AWS built-ins don't publish
					// one (they share a per-type host), so skip them, falling back
					// to that shared host only if the stack is all AWS built-ins.
					probe := ""
					for _, decl := range cfg.Instances {
						if !decl.Enabled {
							continue
						}
						if config.IsAWSBuiltin(decl.Type) {
							if probe == "" {
								if host, ok := cfg.AWSHost(decl.Type); ok {
									probe = host
								}
							}
							continue
						}
						probe = cfg.DomainFor(decl.Name)
						break
					}
					switch {
					case probe == "":
						// nothing declared; nothing to probe
					case !daemonRunning(cfg):
						add("domains", "daemon stopped — names publish while it runs (doze up)", true)
					case resolvesToLoopback(probe):
						add("domains", probe+" resolves through the system resolver", true)
					default:
						add("domains", probe+" did not resolve through the system resolver — run doze doctor --fix (one sudo)", false)
					}
					// The machine setup, part by part: what a failed lookup above is
					// usually missing. It is shared with doze-aws and doze-kafka.
					for _, step := range names.Check().Steps {
						add("names: "+step.Name, step.Detail, step.Done)
					}
					// Which program is answering for the zone. It need not be this
					// stack's daemon: whichever doze program started first serves all.
					zone := names.Open(cfg.Home, "doze")
					if h, ok := zone.ResolverHolder(); ok {
						add("names: resolver", fmt.Sprintf("%s (pid %d) is answering .%s", h.Owner, h.PID, config.DomainSuffix), true)
					}
					if h, ok := zone.IngressHolder(); ok {
						add("names: front door", fmt.Sprintf("%s (pid %d) is serving port-less http:// names", h.Owner, h.PID), true)
					}
					// Per-service addressing (share canonical ports by name) — a
					// nice-to-have; flag it but never fail the check on its absence.
					if names.PoolUsable() {
						add("shared ports", "each service has an address of its own — several can share a port (every Postgres on 5432)", true)
					} else if hasDuplicatePorts(cfg) {
						add("shared ports", "two services declare one port, and this machine is not set up to give each its own address — run doze doctor --fix (one sudo)", false)
					}
				}
			}

			ok := true
			for _, c := range checks {
				ok = ok && c.OK
			}

			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(struct {
					OK     bool          `json:"ok"`
					Checks []doctorCheck `json:"checks"`
				}{OK: ok, Checks: checks}); err != nil {
					return err
				}
				if !ok {
					return exitCodeError(1)
				}
				return nil
			}

			for _, c := range checks {
				renderCheck(c)
			}
			fmt.Println()
			if ok {
				fmt.Println(ui.OK("✓") + " all checks passed")
				return nil
			}
			fmt.Println(ui.Fail("✗") + " some checks need attention (see ✗ above)")
			return exitCodeError(1)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON instead of the table")
	cmd.Flags().BoolVar(&fix, "fix", false, "run the one-time machine setup for .doze names (one sudo)")
	cmd.Flags().BoolVar(&printFix, "print-fix", false, "print the setup script instead of running it")
	return cmd
}

// renderCheck prints one check line, indenting a multi-line detail (a config
// error, typically) under the label column.
func renderCheck(c doctorCheck) {
	mark := ui.OK("✓")
	if !c.OK {
		mark = ui.Fail("✗")
	}
	lines := strings.Split(strings.TrimRight(c.Detail, "\n"), "\n")
	fmt.Printf("  %s  %-16s %s\n", mark, c.Name, lines[0])
	for _, l := range lines[1:] {
		fmt.Printf("       %s\n", l)
	}
}

// resolvesToLoopback asks the system resolver (the path every client takes)
// whether name resolves to 127.0.0.1, with a short budget.
func resolvesToLoopback(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, name)
	if err != nil {
		return false
	}
	for _, a := range addrs {
		// Any loopback address counts: per-service addressing resolves each
		// service to its own 127.0.0.x (not just 127.0.0.1) so they can share
		// a canonical port, and the AWS shared host resolves to the ingress.
		if ip := net.ParseIP(a); ip != nil && ip.IsLoopback() {
			return true
		}
	}
	return false
}

// hasDuplicatePorts reports whether any two enabled instances declare the same
// port — the case per-service addressing (doze dns-setup) exists to serve.
func hasDuplicatePorts(cfg *config.Config) bool {
	seen := map[int]bool{}
	for _, d := range cfg.Instances {
		if d.Enabled && d.Port != 0 {
			if seen[d.Port] {
				return true
			}
			seen[d.Port] = true
		}
	}
	return false
}
