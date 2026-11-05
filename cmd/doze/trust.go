package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	names "github.com/doze-dev/doze-names"
	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/ui"
)

// trustCmd is `doze trust`: the one-time, one-prompt setup for an app's own
// domains. It routes each declared domain to doze's resolver and, when a
// domain is served from the doze certificate authority, trusts that
// authority on this machine. Routes nobody declares any more are removed
// in the same prompt. Opt-in: a machine that declared no domain never
// trusts the authority.
func trustCmd() *cobra.Command {
	var check, print, remove, rotate bool
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Set this machine up for the stack's own domains: routes and https (one sudo)",
		Long: "trust makes an app's `domains` reachable here, over https: it routes each\n" +
			"domain to doze's resolver and trusts the doze certificate authority for TLS\n" +
			"on this machine. doze mints only names declared here, but like any root the\n" +
			"authority could sign any name: its key stays in ~/.doze/ca, readable by you\n" +
			"alone. It needs sudo once. A domain that\n" +
			"no stack declares any more loses its route in the same step.\n\n" +
			"--check reports the state without sudo; --print shows the script instead of\n" +
			"running it; --remove undoes this stack's routes (and the authority's trust\n" +
			"when no stack is left declaring a domain); --rotate makes a new authority.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			home, dir := cfg.Home, projectDirOf(cfg)
			domains := cfg.AppDomains()
			if len(domains) == 0 && !remove {
				fmt.Println(ui.Muted("no app in this stack declares domains; add `domains = [\"dev.acme.com\"]` to one"))
				return nil
			}
			req := names.TrustRequest{Domains: domains, Home: home}
			if usesDozeCA(cfg) || remove {
				ca, err := names.LoadOrCreateCA(names.CADir(home))
				if err != nil {
					return err
				}
				if rotate {
					if err := ca.Rotate(); err != nil {
						return err
					}
					fmt.Println(ui.OK("✓") + " new certificate authority " + ui.Muted(ca.Subject()))
				}
				req.CA = ca
			}
			if err := names.RecordDomains(home, dir, domains); err != nil {
				return err
			}
			req.Prune = names.StaleRoutes(home)
			switch {
			case check:
				return trustCheck(req)
			case remove:
				if rotate {
					return usageError(errors.New("--rotate and --remove do not go together"))
				}
				// This stack's routes go unless another stack still declares the
				// domain; the authority's trust and files go with the last stack.
				// With --print nothing is changed, only shown.
				if !print {
					_ = names.ForgetProject(home, dir)
				}
				req.Domains, req.Prune = domainsOnlyOurs(home, dir, domains), nil
				last := !names.AnyDeclared(home) || (print && !declaredElsewhere(home, dir))
				if !last {
					req.CA = nil // another stack still serves a domain from it
				}
				if err := names.Untrust(names.Options{Out: os.Stdout, Print: print}, req); err != nil {
					return err
				}
				if last && !print && req.CA != nil {
					if err := names.RemoveCA(home); err != nil {
						return err
					}
					fmt.Println(ui.OK("✓") + " the certificate authority's files are gone")
				}
				return nil
			default:
				if err := names.Trust(names.Options{Out: os.Stdout, Print: print}, req); err != nil {
					return err
				}
				if !print {
					for _, d := range domains {
						if !names.Wildcard(d) {
							fmt.Println(ui.OK("✓") + " https://" + d + ui.Muted("  once the stack is up"))
						}
					}
				}
				return nil
			}
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report what is set up (no sudo)")
	cmd.Flags().BoolVar(&print, "print", false, "print the privileged script instead of running it")
	cmd.Flags().BoolVar(&remove, "remove", false, "remove this stack's routes, and the authority's trust with the last stack (needs sudo)")
	cmd.Flags().BoolVar(&rotate, "rotate", false, "make a new certificate authority and trust it")
	return cmd
}

// trustCheck prints each part of the setup and whether it is in place.
func trustCheck(req names.TrustRequest) error {
	steps := names.TrustStatus(req)
	ok := true
	for _, step := range steps {
		mark := ui.OK("✓")
		if !step.Done {
			mark = ui.Fail("✗")
			ok = false
		}
		fmt.Printf("  %s  %-28s %s\n", mark, step.Name, ui.Muted(step.Detail))
	}
	if ok {
		fmt.Println("\n" + ui.OK("ready") + " — the domains resolve here and https is trusted")
		return nil
	}
	fmt.Println("\nrun " + ui.Title("doze trust") + " to set it up (one sudo)")
	return shown(errors.New("the domains are not set up"))
}

// usesDozeCA says some app declares a domain without its own certificate,
// so the doze authority signs for it and must be trusted here.
func usesDozeCA(cfg *config.Config) bool {
	for _, decl := range cfg.Decls() {
		if decl.Enabled && len(decl.Domains) > 0 && decl.Cert == "" {
			return true
		}
	}
	return false
}

// projectDirOf is the folder a config lives in, absolute: the routes
// record keys projects by it, and a relative "." would mean nothing later.
func projectDirOf(cfg *config.Config) string {
	dir := filepath.Dir(cfg.Path())
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// domainsOnlyOurs are this project's domains no other project with a
// doze.hcl still declares: the ones whose routes --remove may take away.
func domainsOnlyOurs(home, dir string, domains []string) []string {
	var out []string
	for _, d := range domains {
		if !routeDeclaredByOther(home, dir, names.RouteDomain(d)) {
			out = append(out, d)
		}
	}
	return out
}

// declaredElsewhere says another project still declares a domain.
func declaredElsewhere(home, dir string) bool {
	for _, d := range names.RecordedRoutes(home) {
		if routeDeclaredByOther(home, dir, d) {
			return true
		}
	}
	return false
}

func routeDeclaredByOther(home, dir, route string) bool {
	for _, other := range names.Claimants(home, route) {
		if other != dir {
			return true
		}
	}
	return false
}
