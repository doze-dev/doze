package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	names "github.com/doze-dev/doze-names"
	"github.com/doze-dev/doze-sdk/binaries"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/portholder"
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

				// Who holds a port the stack wants, and what to do about it.
				daemonPID := 0
				if daemonRunning(cfg) {
					daemonPID = readPID(filepath.Join(cfg.RunDir(), "doze.pid"))
				}
				for _, t := range takenPorts(cfg, names.PoolUsable(), daemonPID, portholder.Who) {
					add(fmt.Sprintf("port %d", t.port), portAdvice(t, names.PoolUsable()), false)
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
		// Any address of this machine counts: each service resolves to an
		// address of its own (not just 127.0.0.1) so they can share a canonical
		// port, and the AWS shared host resolves to the ingress.
		if names.OnThisMachine(net.ParseIP(a)) {
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

// portTaken is one declared port something else is listening on.
type portTaken struct {
	port   int
	name   string
	holder portholder.Holder
}

// takenPorts lists the stack's declared ports that another program holds.
// Ports the daemon itself holds are not a conflict, and neither is any port
// when each service has an address of its own — then 5432 on 198.19.x.y does
// not meet 5432 on 127.0.0.1.
func takenPorts(cfg *config.Config, perServiceAddresses bool, daemonPID int, who func(int) (portholder.Holder, bool)) []portTaken {
	if perServiceAddresses && cfg.Defaults.Domains {
		return nil
	}
	var out []portTaken
	seen := map[int]bool{}
	for _, d := range cfg.Instances {
		if !d.Enabled || d.Port == 0 || seen[d.Port] || d.Kind == config.KindTask {
			continue
		}
		seen[d.Port] = true
		h, ok := who(d.Port)
		if !ok || h.PID == daemonPID {
			continue
		}
		out = append(out, portTaken{port: d.Port, name: d.Name, holder: h})
	}
	return out
}

// portAdvice is what to do about a port someone else holds, in the order of
// how little it asks of the person.
func portAdvice(t portTaken, perServiceAddresses bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "held by %s (pid %d) on %s\n", t.holder.Command, t.holder.PID, t.holder.Addr)
	fmt.Fprintf(&b, "stop it:               kill %d\n", t.holder.PID)
	if !perServiceAddresses {
		fmt.Fprintf(&b, "or give %s its own address: doze doctor --fix   (every service gets one, so ports never clash)\n", t.name)
	}
	fmt.Fprintf(&b, "or use another port:   port = … in the %q block of doze.hcl", t.name)
	return b.String()
}

// readPID reads the pid on the first line of a pid file, or 0.
func readPID(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0]))
	return n
}
