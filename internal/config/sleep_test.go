package config_test

import (
	"strings"
	"testing"

	"github.com/doze-dev/doze/internal/config"
)

// A module's sleep is read by doze: a duration, "never", or a clear error.
func TestModuleSleep(t *testing.T) {
	cfg, err := config.ParseShape([]byte(`
module postgres "db" {
  version = 17
  sleep   = "never"
}
module valkey "cache" {
  version = 9
  sleep   = "90s"
}
`), "/tmp/x/doze.hcl")
	if err != nil {
		t.Fatal(err)
	}
	if s := cfg.Lookup("db").Sleep; !s.Set || !s.Never {
		t.Errorf("db sleep = %+v, want never", s)
	}
	if s := cfg.Lookup("cache").Sleep; !s.Set || s.Never || s.After.String() != "1m30s" {
		t.Errorf("cache sleep = %+v, want 1m30s", s)
	}
	_, err = config.ParseShape([]byte("module postgres \"db\" {\n  version = 17\n  sleep = \"soon\"\n}\n"), "/tmp/x/doze.hcl")
	if err == nil || !strings.Contains(err.Error(), `sleep = "soon"`) || !strings.Contains(err.Error(), "never") {
		t.Fatalf("bad sleep: %v", err)
	}
}

// wakes_on is a later release's: refused, never ignored, so the blocks it
// names do not become dependencies of a worker that cannot use them yet.
func TestWakesOnIsRefused(t *testing.T) {
	_, err := config.ParseShape([]byte(`
module valkey "events" {
  version = 9
}
module valkey "cache" {
  version  = 9
  wakes_on = [module.events]
}
`), "/tmp/x/doze.hcl")
	if err == nil || !strings.Contains(err.Error(), "wakes_on") || !strings.Contains(err.Error(), "isn't in this release") {
		t.Fatalf("wakes_on: %v", err)
	}
}

// A service that still sets front or logs is told to delete it, at the
// argument, whatever its value (an empty one too).
func TestServiceRemovedArguments(t *testing.T) {
	for _, arg := range []string{"front", "logs"} {
		src := "service \"s\" {\n  run = \"x\"\n  " + arg + " = \"\"\n}\n"
		_, err := config.ParseShape([]byte(src), "/tmp/x/doze.hcl")
		if err == nil || !strings.Contains(err.Error(), "`"+arg+"` is no longer an argument: it was never acted on; delete it") || !strings.Contains(err.Error(), "line 3") {
			t.Errorf("%s: %v", arg, err)
		}
	}
}
