package config

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"

	"github.com/nerdmenot/doze/internal/engine"
)

func init() { engine.Register(fakeProcDriver{}) }

// fakeProcDriver mimics the process engine just enough to exercise the
// PortBinder-aware InstanceAddr and the env-reference dependency edge.
type fakeProcDriver struct{}

type fakeProcConfig struct {
	Port int
	Env  map[string]string
}

func (fakeProcDriver) Type() string { return "fakeproc" }
func (fakeProcDriver) Versionless() {}
func (fakeProcDriver) Resolve(context.Context, engine.VersionSpec, engine.Platform, engine.Locker, engine.Fetcher) (engine.Toolchain, error) {
	return engine.Toolchain{}, nil
}
func (fakeProcDriver) Provision(context.Context, engine.Instance, engine.Toolchain) error { return nil }
func (fakeProcDriver) Provisioned(string) bool                                            { return false }
func (fakeProcDriver) Spawn(context.Context, engine.Instance, engine.Toolchain) (engine.Process, error) {
	return nil, nil
}
func (fakeProcDriver) WaitReady(context.Context, engine.Instance, engine.Toolchain, engine.Process) error {
	return nil
}
func (fakeProcDriver) Supervised(engine.Instance) bool  { return true }
func (fakeProcDriver) BackendSocket(string, int) string { return "" }
func (fakeProcDriver) ConnString(engine.Instance, engine.Endpoint) (string, string) {
	return "", ""
}
func (fakeProcDriver) AdvertisedAddr(inst engine.Instance) (string, bool) {
	if c, ok := inst.Spec.(*fakeProcConfig); ok && c.Port > 0 {
		return "127.0.0.1:9090", true
	}
	return "", false
}
func (fakeProcDriver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, _ string) (engine.EngineConfig, error) {
	var s struct {
		Port int               `hcl:"port,optional"`
		Env  map[string]string `hcl:"env,optional"`
	}
	if d := gohcl.DecodeBody(body, ctx, &s); d.HasErrors() {
		return nil, d
	}
	return &fakeProcConfig{Port: s.Port, Env: s.Env}, nil
}

func TestPortBinderAdvertisesOwnAddress(t *testing.T) {
	src := `
fake "db" { version = 1 }
fakeproc "api" {
  port = 9090
  env  = { DATABASE_URL = fake.db.url }
}
`
	cfg, err := Parse([]byte(src), "doze.hcl")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	api := cfg.Lookup("api")
	if api == nil {
		t.Fatal("api not declared")
	}
	addr, err := cfg.InstanceAddr(api)
	if err != nil {
		t.Fatalf("InstanceAddr: %v", err)
	}
	if addr != "127.0.0.1:9090" {
		t.Errorf("InstanceAddr(api) = %q, want the app's own port 127.0.0.1:9090", addr)
	}
	// The env reference to fake.db.url must create a dependency edge.
	var dep bool
	for _, d := range api.Deps {
		if d.Name == "db" {
			dep = true
		}
	}
	if !dep {
		t.Errorf("api should depend on db via the env reference; deps = %+v", api.Deps)
	}
}

func TestSingleLineBlockErrorHasFixHint(t *testing.T) {
	// A multi-argument single-line block is invalid HCL; doze should turn the
	// cryptic grammar error into an actionable "put each on its own line" hint.
	src := `
fakeproc "api" {
  health { http = "http://x" interval = "2s" }
}
`
	_, err := Parse([]byte(src), "doze.hcl")
	if err == nil {
		t.Fatal("a single-line multi-argument block should be a parse error")
	}
	if !strings.Contains(err.Error(), "own line") {
		t.Errorf("expected a fix hint pointing at multi-line blocks, got:\n%s", err)
	}
}

func TestPortlessProcessFallsBackToProxyAddr(t *testing.T) {
	src := `
listen = "127.0.0.1:7000"
fakeproc "worker" {}
`
	cfg, err := Parse([]byte(src), "doze.hcl")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	addr, err := cfg.InstanceAddr(cfg.Lookup("worker"))
	if err != nil {
		t.Fatalf("InstanceAddr: %v", err)
	}
	// No port → no advertised address → it keeps a (never-bound) proxy slot.
	if !strings.HasPrefix(addr, "127.0.0.1:70") {
		t.Errorf("portless worker addr = %q, want a proxy-range fallback", addr)
	}
}
