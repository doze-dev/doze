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
