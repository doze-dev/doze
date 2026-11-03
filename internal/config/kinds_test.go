package config

import (
	"strings"
	"testing"
)

// A plugin finds its block as `<type> "<name>"`, so the keyword comes off the
// header — and nothing else moves, or the positions a plugin reports would
// stop matching the user's file.
func TestPluginSourceRewritesOnlyModuleHeaders(t *testing.T) {
	const src = `stack = "shop"

module postgres "db" {
  version = 16 # keep
  role "shop" {}
}

app "api" {
  run = "go run ./cmd/api"
  env = { DATABASE_URL = module.db.url }
}

module kafka "events" {
  version = 4
}
`
	got := string(pluginSource([]byte(src), "doze.hcl"))
	want := strings.NewReplacer(`module postgres "db"`, `postgres "db"`, `module kafka "events"`, `kafka "events"`).Replace(src)
	if got != want {
		t.Fatalf("plugin source:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(got, "module.db.url") {
		t.Fatal("references must be left alone")
	}
}

func TestPluginSourceLeavesOtherFilesUntouched(t *testing.T) {
	const src = "app \"api\" {\n  run = \"x\"\n}\n"
	if got := string(pluginSource([]byte(src), "doze.hcl")); got != src {
		t.Fatalf("a file with no module block must come back byte for byte, got:\n%s", got)
	}
}

// A module never sees the arguments doze reads itself: they are blanked, not
// removed, so the module's own error positions still match the file.
func TestPluginSourceBlanksCoreArguments(t *testing.T) {
	const src = `module postgres "db" {
  version = 16
  after   = [
    task.migrate,
  ]
  sleep   = "never"
  role "app" {}
}
`
	got := string(pluginSource([]byte(src), "doze.hcl"))
	for _, gone := range []string{"after", "sleep", "task.migrate", "never"} {
		if strings.Contains(got, gone) {
			t.Errorf("%q reached the module:\n%s", gone, got)
		}
	}
	for _, kept := range []string{`postgres "db"`, "version = 16", `role "app" {}`} {
		if !strings.Contains(got, kept) {
			t.Errorf("%q is missing:\n%s", kept, got)
		}
	}
	if strings.Count(got, "\n") != strings.Count(src, "\n") {
		t.Errorf("line count changed: %d, want %d", strings.Count(got, "\n"), strings.Count(src, "\n"))
	}
	line := func(s string) int { return strings.Count(s[:strings.Index(s, `role "app"`)], "\n") + 1 }
	if line(got) != line(src) {
		t.Errorf("role moved from line %d to %d", line(src), line(got))
	}
}
