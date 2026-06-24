package process

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2/hclparse"

	"github.com/nerdmenot/doze/internal/engine"
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
	if c.Restart.Policy != engine.RestartNo {
		t.Fatalf("default restart policy = %q, want no", c.Restart.Policy)
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
	for _, kv := range c.mergedEnv(map[string]string{"INJECTED": "inj", "SHARED": "injected"}) {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	// explicit env{} beats env_file beats injected; os.Environ is the floor.
	checks := map[string]string{
		"PROC_TEST_BASE": "base",
		"INJECTED":       "inj",
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

func TestProbeHTTP(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	h := &Health{Kind: "http", Target: ok.URL, Timeout: time.Second}
	if err := h.probe(context.Background(), nil); err != nil {
		t.Fatalf("2xx should pass: %v", err)
	}
	h.Target = bad.URL
	if err := h.probe(context.Background(), nil); err == nil {
		t.Fatal("5xx should fail the http probe")
	}
}

func TestProbeTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	h := &Health{Kind: "tcp", Target: ln.Addr().String(), Timeout: time.Second}
	if err := h.probe(context.Background(), nil); err != nil {
		t.Fatalf("listening port should accept: %v", err)
	}
	_ = ln.Close()
	h2 := &Health{Kind: "tcp", Target: "127.0.0.1:1", Timeout: 200 * time.Millisecond}
	if err := h2.probe(context.Background(), nil); err == nil {
		t.Fatal("a closed port should fail the tcp probe")
	}
}

func TestProbeExec(t *testing.T) {
	pass := &Health{Kind: "exec", Target: "true", Timeout: time.Second}
	if err := pass.probe(context.Background(), nil); err != nil {
		t.Fatalf("`true` should pass: %v", err)
	}
	fail := &Health{Kind: "exec", Target: "false", Timeout: time.Second}
	if err := fail.probe(context.Background(), nil); err == nil {
		t.Fatal("`false` should fail the exec probe")
	}
}

func TestProbeLogLine(t *testing.T) {
	h := &Health{Kind: "log_line", Target: "listening on", Timeout: time.Second}
	if err := h.probe(context.Background(), []string{"starting up", "listening on :8080"}); err != nil {
		t.Fatalf("matching line should pass: %v", err)
	}
	if err := h.probe(context.Background(), []string{"starting up"}); err == nil {
		t.Fatal("no matching line should fail the log_line probe")
	}
}
