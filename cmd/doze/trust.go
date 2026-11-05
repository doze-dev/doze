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
			"domain to doze's resolver and trusts the doze certificate authority, which\n" +
			"signs only names declared on this machine. It needs sudo once. A domain that\n" +
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
			home, dir := cfg.Home, filepath.Dir(cfg.Path())
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
				// This stack's routes go unless another stack still declares the
				// domain; the authority's trust goes with the last stack.
				_ = names.ForgetProject(home, dir)
				req.Domains = nil
				for _, d := range domains {
					for _, stale := range names.StaleRoutes(home) {
						if stale == names.RouteDomain(d) {
							req.Domains = append(req.Domains, d)
						}
					}
				}
				if len(names.RecordedRoutes(home)) > len(names.StaleRoutes(home)) {
					req.CA = nil // someone else still needs it
				}
				req.Prune = nil
				return names.Untrust(names.Options{Out: os.Stdout, Print: print}, req)
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
