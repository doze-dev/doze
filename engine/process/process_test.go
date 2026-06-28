package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2/hclparse"

	"github.com/doze-dev/doze-sdk/engine"
)

func decode(t *testing.T, src string) (*Config, error) {
	t.Helper()
	f, diags := hclparse.NewParser().ParseHCL([]byte(src), "test.hcl")
	if diags.HasErrors() {
		t.Fatalf("parsing test HCL: %s", diags.Error())
	}
	c, err := Driver{}.DecodeConfig(f.Body, nil, ".")
	if err != nil {
		return nil, err
	}
	return c.(*Config), nil
}

func TestDecodeMinimal(t *testing.T) {
	c, err := decode(t, `command = "go run ./..."`)
	if err != nil {
		t.Fatalf("minimal process should decode: %v", err)
	}
	if c.Command != "go run ./..." {
		t.Fatalf("command = %q", c.Command)
	}
	if c.Restart.Policy != engine.RestartOnFailure {
		t.Fatalf("default restart policy = %q, want on_failure", c.Restart.Policy)
	}
	if c.Restart.MaxRetries <= 0 {
		t.Fatalf("default restart should have a positive retry budget, got %d", c.Restart.MaxRetries)
	}
	if c.Health != nil {
		t.Fatalf("no health block should leave Health nil")
	}
}

func TestDecodeRequiresCommand(t *testing.T) {
	if _, err := decode(t, `port = 8080`); err == nil {
		t.Fatal("a process without a command should be rejected")
	}
}

func TestDecodeHealthSingleKind(t *testing.T) {
	if _, err := decode(t, `
		command = "x"
		health {
			http = "http://localhost/h"
			tcp  = "localhost:1"
		}
	`); err == nil {
		t.Fatal("two probe kinds in one health block should be rejected")
	}
	c, err := decode(t, `
		command = "x"
		health {
			http    = "http://localhost/h"
			retries = 5
		}
	`)
	if err != nil {
		t.Fatalf("single-kind health should decode: %v", err)
	}
	if c.Health.Kind != "http" || c.Health.Target != "http://localhost/h" {
		t.Fatalf("health = %+v", c.Health)
	}
	if c.Health.Retries != 5 {
		t.Fatalf("retries = %d, want 5", c.Health.Retries)
	}
	if c.Health.Interval != defaultHealthInterval {
		t.Fatalf("interval default = %s", c.Health.Interval)
	}
}

func TestDecodeRestartDefaults(t *testing.T) {
	c, err := decode(t, `
		command = "x"
		restart { policy = "always" }
	`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if c.Restart.Policy != engine.RestartAlways {
		t.Fatalf("policy = %q", c.Restart.Policy)
	}
	if c.Restart.MaxRetries != defaultMaxRetries {
		t.Fatalf("max_retries default = %d, want %d", c.Restart.MaxRetries, defaultMaxRetries)
	}

	// `policy = "no"` opts out: zero retry budget so the runtime never restarts.
	off, err := decode(t, `
		command = "x"
		restart { policy = "no" }
	`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if off.Restart.Policy != engine.RestartNo || off.Restart.MaxRetries != 0 {
		t.Fatalf("policy=no should zero retries, got policy=%q retries=%d", off.Restart.Policy, off.Restart.MaxRetries)
	}
	if _, err := decode(t, `command = "x"
		restart { policy = "sometimes" }`); err == nil {
		t.Fatal("an invalid restart policy should be rejected")
	}
}

func TestAdvertisedAddr(t *testing.T) {
	inst := engine.Instance{Spec: &Config{Port: 8080}}
	addr, ok := Driver{}.AdvertisedAddr(inst)
	if !ok || addr != "127.0.0.1:8080" {
		t.Fatalf("AdvertisedAddr = %q, %v", addr, ok)
	}
	if _, ok := (Driver{}).AdvertisedAddr(engine.Instance{Spec: &Config{}}); ok {
		t.Fatal("a portless process should not advertise an address")
	}
}

func TestMergedEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("FROM_FILE=file\nSHARED=file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROC_TEST_BASE", "base")
	c := &Config{
		EnvFile: envFile,
		Env:     map[string]string{"SHARED": "explicit", "FROM_ENV": "env"},
	}
	got := map[string]string{}
	for _, kv := range c.mergedEnv() {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	// explicit env{} beats env_file; os.Environ is the floor. doze injects nothing.
	checks := map[string]string{
		"PROC_TEST_BASE": "base",
		"FROM_FILE":      "file",
		"FROM_ENV":       "env",
		"SHARED":         "explicit",
	}
	for k, want := range checks {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
}
