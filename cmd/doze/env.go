package main

import (
	"fmt"
	"os"
	"strings"

	names "github.com/doze-dev/doze-names"
	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/endpoints"
	"github.com/doze-dev/doze/internal/ui"
)

// envAssignments derives the env-var → URL pairs; the logic lives in
// internal/endpoints so the dash's :env palette command renders the same table.
func envAssignments(cfg *config.Config, only []string) ([]endpoints.EnvPair, error) {
	pairs, err := endpoints.EnvAssignments(cfg, only)
	if err != nil && len(only) > 0 {
		// The shared helper doesn't know the CLI's richer unknown-name message;
		// re-derive it here for parity with every other command.
		for _, n := range only {
			if cfg.Lookup(n) == nil {
				return nil, instanceNotFound(cfg, n)
			}
		}
	}
	return pairs, err
}

func envCmd() *cobra.Command {
	var dotenv bool
	cmd := &cobra.Command{
		Use:               "env [service…]",
		ValidArgsFunction: instanceCompletion,
		Short:             "Print the services' connection env vars as eval-able exports",
		Long: "env prints one export line per declared service — DATABASE_URL,\n" +
			"REDIS_URL, AWS_ENDPOINT_URL_S3, … — pointing at doze's endpoints, so an\n" +
			"unmodified app or the aws CLI talks to your local stack:\n\n" +
			"    eval \"$(doze env)\"\n\n" +
			"When any AWS-style endpoint is present it also exports dummy credentials\n" +
			"and a region (local AWS SDKs require them; doze ignores them). Name\n" +
			"services to limit the output. --dotenv writes KEY=value lines for a .env\n" +
			"file; --json is for tooling. Endpoints come from the config's declared\n" +
			"ports — the daemon doesn't need to be running, and connecting is what\n" +
			"wakes a service.",
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			pairs, err := envAssignments(cfg, args)
			if err != nil {
				return err
			}
			pairs = append(pairs, caPairs(cfg)...)
			switch {
			case jsonMode:
				if pairs == nil {
					pairs = []endpoints.EnvPair{}
				}
				setResult(pairs)
			case dotenv:
				for _, p := range pairs {
					fmt.Printf("%s=%s\n", p.Var, p.Value)
				}
			default:
				if len(pairs) == 0 {
					fmt.Fprintln(os.Stderr, ui.Muted("no services with connection URLs declared"))
					return nil
				}
				for _, p := range pairs {
					comment := ""
					if p.Service != "" {
						comment = "  # " + p.Engine + " " + p.Service
					}
					fmt.Printf("export %s=%s%s\n", p.Var, shellQuote(p.Value), comment)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dotenv, "dotenv", false, "emit KEY=value lines for a .env file")
	return cmd
}

// shellQuote single-quotes a value for POSIX shells.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// caPairs are the variables that point clients at the doze authority, for
// a stack that serves a domain from it, once the daemon has made it.
func caPairs(cfg *config.Config) []endpoints.EnvPair {
	if !usesDozeCA(cfg) {
		return nil
	}
	ca, ok := names.OpenCA(names.CADir(cfg.Home))
	if !ok {
		return nil
	}
	pairs := []endpoints.EnvPair{
		{Var: "DOZE_CA_FILE", Value: ca.RootPath(), Service: "the doze certificate authority"},
		{Var: "NODE_EXTRA_CA_CERTS", Value: ca.RootPath(), Service: "Node adds it"},
	}
	// The two that replace a client's store get the bundle of the machine's
	// roots and this one, never the root alone, which would cut the shell off
	// from every public https host. It is written here when no daemon has.
	bundle, err := ca.EnsureBundle()
	if err != nil {
		fmt.Fprintln(os.Stderr, ui.Muted("SSL_CERT_FILE and REQUESTS_CA_BUNDLE left unset: "+err.Error()))
		return pairs
	}
	return append(pairs,
		endpoints.EnvPair{Var: "SSL_CERT_FILE", Value: bundle, Service: "OpenSSL, Go on Linux, curl"},
		endpoints.EnvPair{Var: "REQUESTS_CA_BUNDLE", Value: bundle, Service: "Python requests"},
	)
}
