package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/modules"
	"github.com/doze-dev/doze/internal/ui"
)

// initEngine is one offer in the wizard: a key (engine type), a one-line pitch,
// and the HCL block it scaffolds. Offers come from the live registry catalog when
// reachable, falling back to this small seed offline.
type initEngine struct {
	key, desc, block string
}

// initSeed is the offline fallback when the registry catalog can't be fetched.
var initSeed = []initEngine{
	{"postgres", "SQL database (Postgres)", "postgres \"app\" {\n  version = 18\n  port    = 5432\n\n  role \"app\" { password = \"app\" }\n}"},
	{"valkey", "in-memory cache (Redis API)", "valkey \"cache\" {\n  version          = 9\n  port             = 6379\n  maxmemory        = \"256mb\"\n  maxmemory_policy = \"allkeys-lru\"\n}"},
	{"kvrocks", "durable Redis API on disk", "kvrocks \"store\" {\n  version = 2\n  port    = 6380\n}"},
	{"documentdb", "MongoDB-compatible store", "documentdb \"docs\" {\n  port = 27017\n}"},
	{"s3", "S3 object storage", "s3 \"media\" {\n  port = 9000\n  bucket \"uploads\" {}\n}"},
	{"sqs", "SQS message queue", "sqs \"jobs\" {\n  port = 9324\n  queue \"tasks\" {}\n}"},
	{"sns", "SNS pub/sub topics", "sns \"events\" {\n  port = 9911\n  topic \"updates\" {}\n}"},
}

func initCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a doze.hcl — an interactive wizard, or a starter when non-interactive",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if _, err := os.Stat(configPath); err == nil && !force {
				return fmt.Errorf("%s already exists (use --force to overwrite)", configPath)
			}

			offers := gatherOffers()
			picks, app, doLint := []string{offers[0].key}, "", false
			if isInteractive() {
				picks, app, doLint = runWizard(offers)
			}
			if len(picks) == 0 && app == "" {
				fmt.Println("Nothing selected — run `doze init` again to pick services.")
				return nil
			}

			if err := os.WriteFile(configPath, []byte(scaffoldFor(picks, app, offers)), 0o644); err != nil {
				return err
			}
			fmt.Println(ui.OK("✓") + " wrote " + configPath + " — " + strings.Join(append(append([]string{}, picks...), appLabel(app)...), ", "))
			fmt.Println(ui.Muted("  next:") + " doze lint   ·   doze up   ·   doze tree")

			if doLint {
				if cfg, err := loadConfig(); err != nil {
					fmt.Println(ui.Fail("✗") + " " + err.Error())
				} else {
					fmt.Printf("%s looks good — %d service(s)\n", ui.OK("✓"), len(cfg.Instances))
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config")
	return cmd
}

// gatherOffers builds the wizard's engine list from the live registry catalog
// (official modules), so new official engines appear with no code change. If the
// registry is unreachable it falls back to the built-in seed.
func gatherOffers() []initEngine {
	mm, err := modules.NewManager(dozeHome())
	if err != nil {
		return initSeed
	}
	entries, err := mm.CatalogModules()
	if err != nil || len(entries) == 0 {
		return initSeed
	}
	var offers []initEngine
	for _, e := range entries {
		if !e.Official {
			continue // wizard offers official starters; third-party via `search` + modules{}
		}
		offers = append(offers, initEngine{key: e.Name, desc: e.Tagline, block: blockFromCatalog(e)})
	}
	if len(offers) == 0 {
		return initSeed
	}
	return postgresFirst(offers)
}

// postgresFirst moves postgres to the front of an offer list. Offers double as
// the default pick (option 1, and what a non-interactive init scaffolds) — and
// the sensible first database is postgres, not whatever sorts first
// alphabetically in the catalog.
func postgresFirst(offers []initEngine) []initEngine {
	for i, e := range offers {
		if e.key == "postgres" && i > 0 {
			reordered := append([]initEngine{e}, append(append([]initEngine{}, offers[:i]...), offers[i+1:]...)...)
			return reordered
		}
	}
	return offers
}

// blockFromCatalog renders a minimal valid block for a catalog module: the latest
// engine version (if any) and its conventional port.
func blockFromCatalog(e modules.CatalogEntry) string {
	label := e.Label
	if label == "" {
		label = e.Name
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %q {\n", e.Name, label)
	if len(e.EngineVersions) > 0 {
		fmt.Fprintf(&b, "  version = %s\n", e.EngineVersions[len(e.EngineVersions)-1])
	}
	if e.Port > 0 {
		fmt.Fprintf(&b, "  port    = %d\n", e.Port)
	}
	b.WriteString("}")
	return b.String()
}

// runWizard asks three short questions (one shared reader, so no line is
// buffer-swallowed): which engines, an optional app command, and whether to lint.
func runWizard(offers []initEngine) (picks []string, app string, lint bool) {
	fmt.Println(ui.Title("Let's scaffold a doze stack.") + " " + ui.Muted("(Ctrl-C to bail)"))
	fmt.Println()
	for i, e := range offers {
		fmt.Printf("  %s  %-12s %s\n", ui.OK(fmt.Sprintf("%d", i+1)), e.key, ui.Muted(e.desc))
	}
	fmt.Println()
	r := bufio.NewReader(os.Stdin)

	fmt.Print(ui.Title("Services") + ui.Muted(" — numbers or names, space-separated [1]: "))
	line, err := r.ReadString('\n')
	picks = parsePicks(strings.TrimSpace(line), offers)
	if err != nil {
		// stdin died mid-wizard (EOF): take the defaults and stop prompting a
		// reader that can't answer.
		fmt.Println()
		return picks, "", false
	}

	fmt.Print(ui.Title("App command") + ui.Muted(" — e.g. 'go run . serve' (Enter to skip): "))
	cmd, err := r.ReadString('\n')
	app = strings.TrimSpace(cmd)
	if err != nil {
		fmt.Println()
		return picks, app, false
	}

	fmt.Print(ui.Title("Validate") + ui.Muted(" it now with `doze lint`? [y/N]: "))
	ans, _ := r.ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(ans))
	lint = a == "y" || a == "yes"
	return picks, app, lint
}

// parsePicks turns "1 3 valkey" into a deduped, order-preserving engine list.
// Empty input defaults to the first offer.
func parsePicks(in string, offers []initEngine) []string {
	if in == "" {
		return []string{offers[0].key}
	}
	seen := map[string]bool{}
	var out []string
	for _, tok := range strings.Fields(in) {
		key := ""
		for i, e := range offers {
			if tok == e.key || tok == fmt.Sprintf("%d", i+1) {
				key = e.key
				break
			}
		}
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func scaffoldFor(picks []string, app string, offers []initEngine) string {
	var b strings.Builder
	b.WriteString("# doze.hcl — declarative local services, no Docker.\n")
	b.WriteString("# Each instance pins its own port; doze boots it on first connect, reaps when idle.\n")
	b.WriteString("# Edit freely, then: doze lint · doze up · doze tree\n\n")
	b.WriteString("defaults {\n  idle_timeout = \"5m\"\n  domains      = true # <name>.local via mDNS, e.g. app.local:5432\n}\n")
	hasPostgres := false
	for _, p := range picks {
		for _, e := range offers {
			if e.key == p {
				b.WriteString("\n" + e.block + "\n")
				if p == "postgres" {
					hasPostgres = true
				}
			}
		}
	}
	if app != "" {
		b.WriteString("\nprocess \"api\" {\n  command = " + hclString(app) + "\n  port    = 8080\n\n  env = {\n")
		if hasPostgres {
			b.WriteString("    DATABASE_URL = postgres.app.url\n")
		}
		b.WriteString("    PORT         = \"8080\"\n  }\n}\n")
	}
	return b.String()
}

func appLabel(app string) []string {
	if app == "" {
		return nil
	}
	return []string{"api"}
}

func hclString(s string) string { return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\"" }

// isInteractive reports whether stdin AND stdout are real terminals (so the
// wizard prompts) vs a pipe, /dev/null, or CI (so init just writes the starter
// without blocking). Note os.ModeCharDevice is NOT the test: /dev/null is a
// char device, and `doze init </dev/null` must not run the wizard against EOF.
func isInteractive() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && stdoutIsTerminal()
}

// stdoutIsTerminal reports whether stdout is a real terminal — the gate for
// carriage-return spinners and other cursor tricks that turn into artifacts in
// pipes and log files.
func stdoutIsTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
}
