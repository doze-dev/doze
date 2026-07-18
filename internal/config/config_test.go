package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"

	"github.com/doze-dev/doze-sdk/engine"
)

func init() { engine.Register(fakeDriver{}) }

// fakeDriver is a minimal engine used to exercise the generic config parser
// without depending on a real engine package.
type fakeDriver struct{}

type fakeConfig struct {
	Color string
	Ref   string // populated from a cross-instance reference, e.g. fake.a.name
}

func (fakeDriver) Type() string { return "fake" }
func (fakeDriver) Resolve(context.Context, engine.VersionSpec, engine.Platform, engine.Locker, engine.Fetcher) (engine.Toolchain, error) {
	return engine.Toolchain{}, nil
}
func (fakeDriver) Provision(context.Context, engine.Instance, engine.Toolchain) error { return nil }
func (fakeDriver) Provisioned(string) bool                                            { return false }
func (fakeDriver) Spawn(context.Context, engine.Instance, engine.Toolchain) (engine.Process, error) {
	return nil, nil
}
func (fakeDriver) WaitReady(context.Context, engine.Instance, engine.Toolchain, engine.Process) error {
	return nil
}
func (fakeDriver) BackendSocket(string, int) string                             { return "" }
func (fakeDriver) ConnString(engine.Instance, engine.Endpoint) (string, string) { return "", "" }
func (fakeDriver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, _ string, _ engine.VersionSpec) (engine.EngineConfig, error) {
	var s struct {
		Color string `hcl:"color,optional"`
		Ref   string `hcl:"ref,optional"`
	}
	if d := gohcl.DecodeBody(body, ctx, &s); d.HasErrors() {
		return nil, d
	}
	return &fakeConfig{Color: s.Color, Ref: s.Ref}, nil
}

func TestParseRootAndInstances(t *testing.T) {
	src := `
listen = "127.0.0.1:7000"
defaults { idle_timeout = "90s" }

fake "a" {
  version = 16
  color   = "red"
}
fake "b" {
  version = "17.2"
}
`
	cfg, err := Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen != "127.0.0.1:7000" {
		t.Errorf("listen = %q", cfg.Listen)
	}
	if cfg.Defaults.IdleTimeout != 90*time.Second {
		t.Errorf("idle_timeout = %s", cfg.Defaults.IdleTimeout)
	}
	if len(cfg.Instances) != 2 {
		t.Fatalf("got %d instances, want 2", len(cfg.Instances))
	}
	a := cfg.Lookup("a")
	if a == nil || a.Type != "fake" || a.Version != "16" {
		t.Fatalf("a = %+v", a)
	}
	fc, ok := a.Spec.(*fakeConfig)
	if !ok || fc.Color != "red" {
		t.Errorf("a.Spec = %+v", a.Spec)
	}
	if b := cfg.Lookup("b"); b == nil || b.Version != "17.2" {
		t.Errorf("b = %+v", b)
	}
}

func TestVersionRequired(t *testing.T) {
	_, err := Parse([]byte(`fake "x" {}`), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("expected missing-version error, got %v", err)
	}
}

func TestDuplicateInstance(t *testing.T) {
	src := `
fake "dup" { version = 1 }
fake "dup" { version = 1 }
`
	_, err := Parse([]byte(src), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "already declared") {
		t.Errorf("expected duplicate error, got %v", err)
	}
}

func TestUnknownEngine(t *testing.T) {
	_, err := Parse([]byte(`mysql "x" { version = 8 }`), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "mysql") {
		t.Errorf("expected unknown-engine error mentioning mysql, got %v", err)
	}
}

func TestDefaultsApplied(t *testing.T) {
	cfg, err := Parse([]byte(`fake "x" { version = 1 }`), "doze.hcl", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != DefaultListen {
		t.Errorf("default listen = %q", cfg.Listen)
	}
	if cfg.Defaults.IdleTimeout != DefaultIdleTimeout {
		t.Errorf("default idle_timeout = %s", cfg.Defaults.IdleTimeout)
	}
	if cfg.Home == "" || cfg.DataDir == "" {
		t.Error("home and data_dir should default")
	}
}

func TestBadDuration(t *testing.T) {
	_, err := Parse([]byte(`defaults { idle_timeout = "not-a-duration" }`), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "idle_timeout") {
		t.Errorf("expected duration error, got %v", err)
	}
}

func TestUnknownKey(t *testing.T) {
	src := `
defaults {
  bogus_key = "oops"
}
`
	_, err := Parse([]byte(src), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "bogus_key") {
		t.Errorf("expected unknown-key error, got %v", err)
	}
}

func TestReferenceResolvesAndBuildsEdge(t *testing.T) {
	src := `
fake "a" { version = 1 }
fake "b" {
  version = 1
  ref     = fake.a.name
}
`
	cfg, err := Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	b := cfg.Lookup("b")
	if b == nil {
		t.Fatal("instance b missing")
	}
	// The reference resolves to a value...
	if got := b.Spec.(*fakeConfig).Ref; got != "a" {
		t.Errorf("ref = %q, want %q", got, "a")
	}
	// ...and the dependency edge is derived from it.
	if len(b.Deps) != 1 || b.Deps[0].Name != "a" {
		t.Errorf("b.Deps = %v, want [a]", b.Deps)
	}
	// The referenced instance has no deps.
	if a := cfg.Lookup("a"); len(a.Deps) != 0 {
		t.Errorf("a.Deps = %v, want none", a.Deps)
	}
}

func TestReferenceToUndeclaredInstance(t *testing.T) {
	src := `
fake "b" {
  version = 1
  ref     = fake.ghost.name
}
`
	_, err := Parse([]byte(src), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "undeclared instance") || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("expected undeclared-instance error, got %v", err)
	}
}

func TestReferenceCycleDetected(t *testing.T) {
	src := `
fake "x" {
  version = 1
  ref     = fake.y.name
}
fake "y" {
  version = 1
  ref     = fake.x.name
}
`
	_, err := Parse([]byte(src), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected cycle error, got %v", err)
	}
}

func TestFunctionsAvailableInExpressions(t *testing.T) {
	src := `
fake "a" {
  version = 1
  color   = upper("red")
}
`
	cfg, err := Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := cfg.Lookup("a").Spec.(*fakeConfig).Color; got != "RED" {
		t.Errorf("color = %q, want RED (upper applied)", got)
	}
}

func TestVariableDefaultAndOutput(t *testing.T) {
	src := `
variable "color" { default = "teal" }
fake "a" {
  version = 1
  color   = var.color
}
output "the_color" { value = fake.a.name }
`
	cfg, err := Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := cfg.Lookup("a").Spec.(*fakeConfig).Color; got != "teal" {
		t.Errorf("color = %q, want teal (from var default)", got)
	}
	if got := cfg.Outputs["the_color"].Value; got != "a" {
		t.Errorf("output = %q, want a", got)
	}
}

func TestVariableEnvOverride(t *testing.T) {
	t.Setenv("DOZE_VAR_color", "crimson")
	src := `
variable "color" { default = "teal" }
fake "a" {
  version = 1
  color   = var.color
}
`
	cfg, err := Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := cfg.Lookup("a").Spec.(*fakeConfig).Color; got != "crimson" {
		t.Errorf("color = %q, want crimson (DOZE_VAR_ override)", got)
	}
}

func TestLocalsResolveInDependencyOrder(t *testing.T) {
	src := `
variable "base" { default = "red" }
locals {
  a = upper(var.base)
  b = "${local.a}-x"
}
fake "x" {
  version = 1
  color   = local.b
}
`
	cfg, err := Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := cfg.Lookup("x").Spec.(*fakeConfig).Color; got != "RED-x" {
		t.Errorf("color = %q, want RED-x", got)
	}
}

func TestLocalsCycleDetected(t *testing.T) {
	src := `
locals {
  a = local.b
  b = local.a
}
fake "x" { version = 1 }
`
	_, err := Parse([]byte(src), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected locals cycle error, got %v", err)
	}
}

func TestRequiredVariableMissing(t *testing.T) {
	src := `
variable "needed" {}
fake "x" {
  version = 1
  color   = var.needed
}
`
	_, err := Parse([]byte(src), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "required variable") || !strings.Contains(err.Error(), "needed") {
		t.Errorf("expected required-variable error, got %v", err)
	}
}

func TestOutputSensitiveFlag(t *testing.T) {
	src := `
fake "a" { version = 1 }
output "secret" {
  value     = fake.a.name
  sensitive = true
}
`
	cfg, err := Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.Outputs["secret"].Sensitive {
		t.Error("output should be marked sensitive")
	}
}

func TestLoadWithVarsPrecedenceAndAutoFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "doze.hcl", `
variable "env" { default = "dev" }
variable "n" {
  type    = number
  default = 1
}
fake "a" {
  version = 1
  port    = 7001
  color   = "${var.env}-${var.n}"
}
`)
	writeFile(t, dir, "x.auto.doze.vars", `env = "fromfile"`)

	// Auto-vars file beats the default.
	cfg, err := LoadWithVars(dir+"/doze.hcl", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Lookup("a").Spec.(*fakeConfig).Color; got != "fromfile-1" {
		t.Errorf("auto-file: color = %q, want fromfile-1", got)
	}

	// --var beats the auto-vars file, and a number var converts from a string.
	cfg, err = LoadWithVars(dir+"/doze.hcl", map[string]string{"env": "fromcli", "n": "7"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Lookup("a").Spec.(*fakeConfig).Color; got != "fromcli-7" {
		t.Errorf("cli override: color = %q, want fromcli-7", got)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestForEachStampsInstances(t *testing.T) {
	src := `
fake "node" {
  for_each = toset(["x", "y"])
  version  = 1
  color    = each.key
}
`
	cfg, err := Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.Instances) != 2 {
		t.Fatalf("got %d instances, want 2: %+v", len(cfg.Instances), cfg.Instances)
	}
	for _, want := range []struct{ name, color string }{{"node_x", "x"}, {"node_y", "y"}} {
		inst := cfg.Lookup(want.name)
		if inst == nil {
			t.Fatalf("instance %q missing", want.name)
		}
		if got := inst.Spec.(*fakeConfig).Color; got != want.color {
			t.Errorf("%s color = %q, want %q (each.key)", want.name, got, want.color)
		}
	}
}

func TestForEachMapEachValue(t *testing.T) {
	src := `
fake "n" {
  for_each = { a = "red", b = "blue" }
  version  = 1
  color    = each.value
}
`
	cfg, err := Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := cfg.Lookup("n_a").Spec.(*fakeConfig).Color; got != "red" {
		t.Errorf("n_a color = %q, want red", got)
	}
	if got := cfg.Lookup("n_b").Spec.(*fakeConfig).Color; got != "blue" {
		t.Errorf("n_b color = %q, want blue", got)
	}
}

func TestCountStampsInstances(t *testing.T) {
	src := `
fake "rep" {
  count   = 3
  version = 1
  color   = "c${count.index}"
}
`
	cfg, err := Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.Instances) != 3 {
		t.Fatalf("got %d instances, want 3", len(cfg.Instances))
	}
	for i, name := range []string{"rep_0", "rep_1", "rep_2"} {
		inst := cfg.Lookup(name)
		if inst == nil {
			t.Fatalf("instance %q missing", name)
		}
		want := "c" + string(rune('0'+i))
		if got := inst.Spec.(*fakeConfig).Color; got != want {
			t.Errorf("%s color = %q, want %q (count.index)", name, got, want)
		}
		if inst.Index != i {
			t.Errorf("%s Index = %d, want %d (distinct ports)", name, inst.Index, i)
		}
	}
}

func TestCountZeroProducesNoInstances(t *testing.T) {
	cfg, err := Parse([]byte(`fake "none" { count = 0 }`), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.Instances) != 0 {
		t.Errorf("count = 0 should produce no instances, got %d", len(cfg.Instances))
	}
}

func TestCountAndForEachConflict(t *testing.T) {
	src := `
fake "x" {
  count    = 2
  for_each = toset(["a"])
}
`
	_, err := Parse([]byte(src), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "count or for_each") {
		t.Errorf("expected count/for_each conflict error, got %v", err)
	}
}

func TestReferenceToStampedInstance(t *testing.T) {
	src := `
fake "node" {
  for_each = toset(["a"])
  version  = 1
  color    = "x"
}
fake "client" {
  version = 1
  ref     = fake.node_a.name
}
`
	cfg, err := Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := cfg.Lookup("client")
	if got := c.Spec.(*fakeConfig).Ref; got != "node_a" {
		t.Errorf("ref = %q, want node_a", got)
	}
	if len(c.Deps) != 1 || c.Deps[0].Name != "node_a" {
		t.Errorf("client.Deps = %v, want [node_a]", c.Deps)
	}
}

func TestReferenceDepIsHealthy(t *testing.T) {
	cfg, err := Parse([]byte(`
fake "a" { version = 1 }
fake "b" {
  version = 1
  ref     = fake.a.name
}
`), "doze.hcl", nil)
	if err != nil {
		t.Fatal(err)
	}
	deps := cfg.Lookup("b").Deps
	if len(deps) != 1 || deps[0].Name != "a" || deps[0].Condition != engine.Healthy {
		t.Errorf("reference dep = %+v, want {a healthy}", deps)
	}
}

func TestDependsOnAddsConditionedEdge(t *testing.T) {
	cfg, err := Parse([]byte(`
fake "a" { version = 1 }
fake "b" {
  version    = 1
  depends_on = { "fake.a" = "started" }
}
`), "doze.hcl", nil)
	if err != nil {
		t.Fatal(err)
	}
	deps := cfg.Lookup("b").Deps
	if len(deps) != 1 || deps[0].Name != "a" || deps[0].Condition != engine.Started {
		t.Errorf("depends_on dep = %+v, want {a started}", deps)
	}
}

func TestDependsOnOverridesReferenceCondition(t *testing.T) {
	cfg, err := Parse([]byte(`
fake "a" { version = 1 }
fake "b" {
  version    = 1
  ref        = fake.a.name
  depends_on = { "fake.a" = "started" }
}
`), "doze.hcl", nil)
	if err != nil {
		t.Fatal(err)
	}
	deps := cfg.Lookup("b").Deps
	if len(deps) != 1 || deps[0].Condition != engine.Started {
		t.Errorf("explicit condition should override the reference default: %+v", deps)
	}
}

func TestDependsOnUndeclared(t *testing.T) {
	_, err := Parse([]byte(`
fake "b" {
  version    = 1
  depends_on = { "fake.ghost" = "healthy" }
}
`), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "depends_on") || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("expected depends_on undeclared error, got %v", err)
	}
}

func TestDependsOnInvalidCondition(t *testing.T) {
	_, err := Parse([]byte(`
fake "a" { version = 1 }
fake "b" {
  version    = 1
  depends_on = { "fake.a" = "whenever" }
}
`), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "condition") {
		t.Errorf("expected invalid-condition error, got %v", err)
	}
}

func TestSiblingDozeHCLFilesAreMerged(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "doze.hcl", `
defaults { idle_timeout = "1m" }
fake "anchor" {
  version = 1
  port    = 7001
}
`)
	writeFile(t, dir, "extra.doze.hcl", `fake "sibling" {
  version = 1
  port    = 7002
}`)
	writeFile(t, dir, "ignored.hcl", `fake "nope" { version = 1 }`) // not *.doze.hcl

	cfg, err := Load(dir+"/doze.hcl", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Lookup("anchor") == nil || cfg.Lookup("sibling") == nil {
		t.Errorf("anchor + sibling should both be loaded: %d instances", len(cfg.Instances))
	}
	if cfg.Lookup("nope") != nil {
		t.Error("a plain *.hcl sibling must NOT be auto-merged (only *.doze.hcl)")
	}
}

func TestDomainForSanitizes(t *testing.T) {
	cases := map[string]string{
		"orders_pg":   "orders-pg",
		"Cache":       "cache",
		"api.v2":      "api-v2",
		"_x_":         "x",
		"sessions123": "sessions123",
	}
	for in, want := range cases {
		if got := DomainLabel(in); got != want {
			t.Errorf("DomainLabel(%q) = %q, want %q", in, got, want)
		}
	}
	cfg := &Config{StackName: "My Demo"}
	if got := cfg.DomainFor("orders_pg"); got != "orders-pg.my-demo.doze" {
		t.Errorf("DomainFor = %q, want orders-pg.my-demo.doze", got)
	}
	// Unset name falls back to the config directory's label.
	dir := &Config{path: "/home/x/shop-api/doze.hcl"}
	if got := dir.Stack(); got != "shop-api" {
		t.Errorf("Stack() = %q, want shop-api", got)
	}
}

func TestValidateDomainsCollision(t *testing.T) {
	src := `
defaults { domains = true }
fake "orders_pg" {
  version = 1
  port    = 1001
}
fake "orders-pg" {
  version = 1
  port    = 1002
}
`
	_, err := Parse([]byte(src), "doze.hcl", nil)
	// Parse doesn't run the CLI-only port/domain validation; call it directly.
	cfg, perr := Parse([]byte(src), "doze.hcl", nil)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	_ = err
	if verr := cfg.validateDomains(); verr == nil || !strings.Contains(verr.Error(), "domain conflict") {
		t.Fatalf("expected a domain conflict error, got %v", verr)
	}
}

func TestHooksReceiveDeclaredEngines(t *testing.T) {
	// The Hooks integration is explicit (no package globals), so a caller's
	// RequireEngine must be told each declared (type, version) before the
	// driver lookup — verify the capture fires with the right values.
	type req struct{ engineType, version string }
	var got []req
	hooks := &Hooks{
		RequireEngine: func(engineType, version string) {
			got = append(got, req{engineType, version})
		},
		EngineNames: func() []string { return []string{"postgres"} },
	}
	src := `
fake "a" { version = 16 }
fake "b" { version = "17.2" }
`
	if _, err := Parse([]byte(src), "doze.hcl", hooks); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []req{{"fake", "16"}, {"fake", "17.2"}}
	if len(got) != len(want) {
		t.Fatalf("RequireEngine fired %d times (%+v), want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("RequireEngine[%d] = %+v, want %+v", i, got[i], w)
		}
	}
}
