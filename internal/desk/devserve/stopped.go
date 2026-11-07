package devserve

import (
	"os"
	"path/filepath"
	"time"

	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/view"
	"github.com/doze-dev/doze/internal/projects"
)

// stoppedConfig is the doze.hcl of "acme", the stopped stack every fixture
// home carries beside the running one: the Library shows it below, its
// page is the StackStopped board, and a Playwright spec can tick and Up
// it without a daemon.
const stoppedConfig = `stack = "acme"

defaults {
  names = true
}

app "api" {
  run  = "go run ./cmd/api"
  port = 8080
  env  = { DATABASE_URL = module.db.url, CACHE_URL = module.cache.url }
}

app "web" {
  run     = "pnpm dev"
  port    = 3000
  domains = ["dev.acme.com"]
  env     = { API_URL = app.api.url }
}

app "worker" {
  run = "go run ./cmd/worker"
}

module postgres "db" { version = 17 }

module valkey "cache" { version = 8 }

task "migrate" {
  run = "go run ./cmd/migrate"
  env = { DATABASE_URL = module.db.url }
}
`

// seedStopped lays the stopped stack out: its folder in work with the
// config, and in home the project record that says it was up three days
// ago with api, web and db.
func seedStopped(home, work string, now time.Time) error {
	dir := filepath.Join(work, "acme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "doze.hcl"), []byte(stoppedConfig), 0o644); err != nil {
		return err
	}
	then := now.Add(-3 * 24 * time.Hour)
	return projects.Save(filepath.Join(home, "projects", "acme"), projects.Project{
		Config:   filepath.Join(dir, "doze.hcl"),
		Name:     "acme",
		LastUp:   then,
		LastDown: then.Add(6 * time.Hour),
		LastSet:  []string{"api", "web", "db"},
	})
}

// brokenConfig is the badconfig fixture's doze.hcl: a port written as a
// string, the ConfigError board's mistake.
const brokenConfig = `stack = "shop"

app "api" {
  run  = "go run ./cmd/api"
  port = 8080
}

app "worker" {
  run   = "go run ./cmd/worker"
  port  = "eighty-eighty-one"
}

module postgres "db" { version = 17 }
`

// changedConfig is the changed fixture's doze.hcl: valid, and newer than
// what the running stack read.
const changedConfig = `stack = "shop"

app "api" {
  run  = "go run ./cmd/api"
  port = 8080
}

module postgres "db" { version = 17 }
`

// setUpMachine is the machine report of a Mac set up as the docs say: names
// on, each declared domain routed, the authority trusted, doze on PATH.
func setUpMachine(stacks []hub.StackView, version string) []view.Check {
	out := []view.Check{
		{Section: "names", Name: "network", OK: true, Text: "198.19.0.0/17 served by the doze network daemon"},
		{Section: "names", Name: "resolver route", OK: true, Text: "/etc/resolver/doze → 127.0.0.1:5323"},
	}
	var domains []string
	for _, st := range stacks {
		domains = append(domains, view.Domains(st)...)
	}
	for _, d := range domains {
		out = append(out, view.Check{Section: "domains", Name: "route " + d, OK: true, Text: "/etc/resolver/" + d + " → 127.0.0.1:5323"})
	}
	if len(domains) > 0 {
		out = append(out, view.Check{Section: "domains", Name: "certificate authority", OK: true, Text: "trusted in the System keychain · doze local CA · expires 2036-11-01"})
	}
	return append(out, view.Check{Section: "app", Name: "doze on PATH", OK: true, Text: "/opt/homebrew/bin/doze · " + version})
}
