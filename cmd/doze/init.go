package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/ui"
)

// initEngine is one offer in the wizard: a key, a one-line pitch, and the HCL
// block (with a conventional port) it scaffolds.
type initEngine struct {
	key, desc, block string
}

var initEngines = []initEngine{
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

			picks, app, doLint := []string{"postgres"}, "", false
			if isInteractive() {
				picks, app, doLint = runWizard()
			}
			if len(picks) == 0 && app == "" {
				fmt.Println("Nothing selected — run `doze init` again to pick services.")
				return nil
			}

			if err := os.WriteFile(configPath, []byte(scaffoldFor(picks, app)), 0o644); err != nil {
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

// runWizard asks three short questions (one shared reader, so no line is
// buffer-swallowed): which engines, an optional app command, and whether to lint.
func runWizard() (picks []string, app string, lint bool) {
	fmt.Println(ui.Title("Let's scaffold a doze stack.") + " " + ui.Muted("(Ctrl-C to bail)"))
	fmt.Println()
	for i, e := range initEngines {
		fmt.Printf("  %s  %-11s %s\n", ui.OK(fmt.Sprintf("%d", i+1)), e.key, ui.Muted(e.desc))
	}
	fmt.Println()
	r := bufio.NewReader(os.Stdin)

	fmt.Print(ui.Title("Services") + ui.Muted(" — numbers or names, space-separated [1]: "))
	line, _ := r.ReadString('\n')
	picks = parsePicks(strings.TrimSpace(line))

	fmt.Print(ui.Title("App command") + ui.Muted(" — e.g. 'go run . serve' (Enter to skip): "))
	cmd, _ := r.ReadString('\n')
	app = strings.TrimSpace(cmd)

	fmt.Print(ui.Title("Validate") + ui.Muted(" it now with `doze lint`? [y/N]: "))
	ans, _ := r.ReadString('\n')
	lint = strings.EqualFold(strings.TrimSpace(ans), "y") || strings.EqualFold(strings.TrimSpace(ans), "yes")
	return picks, app, lint
}

// parsePicks turns "1 3 valkey" into a deduped, order-preserving engine list.
// Empty input defaults to postgres.
func parsePicks(in string) []string {
	if in == "" {
		return []string{"postgres"}
	}
	seen := map[string]bool{}
	var out []string
	for _, tok := range strings.Fields(in) {
		key := ""
		for i, e := range initEngines {
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

func scaffoldFor(picks []string, app string) string {
	var b strings.Builder
	b.WriteString("# doze.hcl — declarative local services, no Docker.\n")
	b.WriteString("# Each instance pins its own port; doze boots it on first connect, reaps when idle.\n")
	b.WriteString("# Edit freely, then: doze lint · doze up · doze tree\n\n")
	b.WriteString("defaults {\n  idle_timeout = \"5m\"\n}\n")
	hasPostgres := false
	for _, p := range picks {
		for _, e := range initEngines {
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

// isInteractive reports whether stdin is a terminal (so the wizard prompts) vs a
// pipe/CI (so init just writes a sensible starter without blocking).
func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
