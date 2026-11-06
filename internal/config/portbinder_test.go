package config

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"

	"github.com/doze-dev/doze-sdk/engine"
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
func (fakeProcDriver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, _ string, _ engine.VersionSpec) (engine.EngineConfig, error) {
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
module fake "db" {
  version = 1
  port    = 5432
}
module fakeproc "api" {
  port = 9090
  env  = { DATABASE_URL = module.db.url }
}
`
	cfg, err := Parse([]byte(src), "doze.hcl", nil)
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
	// The env reference to module.db.url must create a dependency edge.
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
module fakeproc "api" {
  health { http = "http://x" interval = "2s" }
}
`
	_, err := Parse([]byte(src), "doze.hcl", nil)
	if err == nil {
		t.Fatal("a single-line multi-argument block should be a parse error")
	}
	if !strings.Contains(err.Error(), "own line") {
		t.Errorf("expected a fix hint pointing at multi-line blocks, got:\n%s", err)
	}
}

func TestMissingPortIsAClearError(t *testing.T) {
	// doze does not auto-assign ports; a portless instance must fail with a clear,
	// actionable message at load time (not a silent fallback or an opaque bind error).
	dir := t.TempDir()
	writeFile(t, dir, "doze.hcl", `module fake "db" { version = 1 }`) // a proxied engine needs a port
	_, err := Load(dir+"/doze.hcl", nil)
	if err == nil {
		t.Fatal("a portless proxied engine should error on load")
	}
	if !strings.Contains(err.Error(), "no port") || !strings.Contains(err.Error(), "port = ") {
		t.Errorf("error should name the missing port + the fix, got:\n%s", err)
	}
}

func TestPortlessProcessIsAllowed(t *testing.T) {
	// A supervised process with no endpoint (a background worker) needs no port.
	dir := t.TempDir()
	writeFile(t, dir, "doze.hcl", `module fakeproc "worker" {}`)
	if _, err := Load(dir+"/doze.hcl", nil); err != nil {
		t.Fatalf("a portless worker process should load fine: %v", err)
	}
}

func TestPortConflictIsAClearError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "doze.hcl", `
module fakeproc "a" { port = 8080 }
module fakeproc "b" { port = 8080 }
`)
	_, err := Load(dir+"/doze.hcl", nil)
	if err == nil {
		t.Fatal("two instances on the same port should error")
	}
	if !strings.Contains(err.Error(), "port conflict") || !strings.Contains(err.Error(), "8080") {
		t.Errorf("error should name the conflict + both instances, got:\n%s", err)
	}
}

// valkeyDriver stands in for a registry module whose kind of server has a
// well-known port, so a block that says none still has an address.
type valkeyDriver struct{ fakeDriver }

func (valkeyDriver) Type() string { return "valkey" }

func init() { engine.Register(valkeyDriver{}) }

func TestAModuleWithoutAPortTakesItsWellKnownOne(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "doze.hcl", `
module valkey "cache" { version = 9 }
module fakeproc "worker" {}
`)
	cfg, err := Load(dir+"/doze.hcl", nil)
	if err != nil {
		t.Fatalf("a valkey without a port should load: %v", err)
	}
	var cache *InstanceDecl
	for _, d := range cfg.Instances {
		if d.Name == "cache" {
			cache = d
		}
	}
	if cache == nil || cache.Port != 6379 {
		t.Fatalf("cache port = %v, want 6379", cache)
	}
	if addr, _ := cfg.InstanceAddr(cache); addr != "127.0.0.1:6379" {
		t.Errorf("address = %q", addr)
	}
	// a reference to its address now resolves, which was the failure that said
	// "no attribute named address"
	writeFile(t, dir, "doze.hcl", `
module valkey "cache" { version = 9 }
module fake "api" {
  version = 1
  port    = 7000
  ref     = module.cache.address
}
`)
	if _, err := Load(dir+"/doze.hcl", nil); err != nil {
		t.Errorf("referencing module.cache.address: %v", err)
	}
}

func TestAModuleWithoutAPortTakesTheOneItsModuleGives(t *testing.T) {
	src := []byte("module valkey \"cache\" { version = 9 }\n")
	// the module's Describe says 6400: it wins over doze's table
	hooks := &Hooks{DefaultPort: func(engineType string) int {
		if engineType == "valkey" {
			return 6400
		}
		return 0
	}}
	cfg, err := Parse(src, "doze.hcl", hooks)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Instances[0].Port; got != 6400 {
		t.Errorf("port from the module's Describe = %d, want 6400", got)
	}
	// a module that gives none, and the offline shape pass that asks no
	// module, fall back to the table
	cfg, err = Parse(src, "doze.hcl", &Hooks{DefaultPort: func(string) int { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Instances[0].Port; got != 6379 {
		t.Errorf("fallback port = %d, want 6379", got)
	}
	cfg, err = ParseShape(src, "doze.hcl")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Instances[0].Port; got != 6379 {
		t.Errorf("shape pass port = %d, want 6379", got)
	}
	// a port the block names is never replaced
	cfg, err = Parse([]byte("module valkey \"cache\" {\n  version = 9\n  port    = 7001\n}\n"), "doze.hcl", hooks)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Instances[0].Port; got != 7001 {
		t.Errorf("declared port = %d, want 7001", got)
	}
}

func TestTwoModulesOfAKindStillSayWhichPortToChange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "doze.hcl", `
module valkey "a" { version = 9 }
module valkey "b" { version = 9 }
`)
	_, err := Load(dir+"/doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "port conflict") {
		t.Fatalf("two valkeys on the one default port should be a port conflict, got %v", err)
	}
}

// A module doze does not front can't sleep: nothing would wake it. "never" is
// still fine, since that is what it does anyway.
func TestUnfrontedModuleCannotSleep(t *testing.T) {
	_, err := Parse([]byte("module fakeproc \"worker\" {\n  port  = 9090\n  sleep = \"5m\"\n}\n"), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "can't sleep") || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("sleep on an unfronted module: %v", err)
	}
	if _, err := Parse([]byte("module fakeproc \"worker\" {\n  port  = 9090\n  sleep = \"never\"\n}\n"), "doze.hcl", nil); err != nil {
		t.Fatalf("sleep = never: %v", err)
	}
}
