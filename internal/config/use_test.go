package config

import (
	"strings"
	"testing"
)

// The `use <type> "<name>"` form desugars to the bare `<type> "<name>"` block, so
// it must parse identically — same type, name, and decoded body.
func TestUsePrefixEquivalentToBare(t *testing.T) {
	useSrc := `
use fake "x" {
  version = 16
  color   = "red"
}
`
	bareSrc := `
fake "x" {
  version = 16
  color   = "red"
}
`
	useCfg, err := Parse([]byte(useSrc), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("use form: unexpected error: %v", err)
	}
	bareCfg, err := Parse([]byte(bareSrc), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("bare form: unexpected error: %v", err)
	}
	u, b := useCfg.Lookup("x"), bareCfg.Lookup("x")
	if u == nil || b == nil {
		t.Fatalf("missing instance: use=%v bare=%v", u, b)
	}
	if u.Type != b.Type || u.Name != b.Name || u.Version != b.Version {
		t.Errorf("identity differs: use=%+v bare=%+v", u, b)
	}
	uc, ok1 := u.Spec.(*fakeConfig)
	bc, ok2 := b.Spec.(*fakeConfig)
	if !ok1 || !ok2 || uc.Color != bc.Color || uc.Color != "red" {
		t.Errorf("decoded body differs: use=%+v bare=%+v", u.Spec, b.Spec)
	}
}

// `use` and the bare form may coexist in one file; the built-in `process` block
// stays bare.
func TestUseAndBareCoexist(t *testing.T) {
	src := `
use fake "a" { version = 1 }
fake "b"     { version = 1 }
`
	cfg, err := Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Instances) != 2 || cfg.Lookup("a") == nil || cfg.Lookup("b") == nil {
		t.Fatalf("want instances a and b, got %+v", cfg.Instances)
	}
}

func TestUseRejectsBadForms(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"too few labels", `use fake { version = 1 }`, "engine type and a name"},
		{"too many labels", `use fake "x" "y" { version = 1 }`, "engine type and a name"},
		{"process via use", `use process "w" { command = "x" }`, "built in"},
		{"reserved type", `use modules "x" {}`, "reserved block"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.src), "doze.hcl", nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want error containing %q", err, tc.want)
			}
		})
	}
}
