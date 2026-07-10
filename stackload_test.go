package doze_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	doze "github.com/doze-dev/doze"
)

// TestStackRoundTrip proves the read → edit → persist flow: load an existing
// config into a Stack, mutate it (add + remove services), and re-render — with
// existing blocks preserved verbatim.
func TestStackRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doze.hcl")
	original := `name = "shop"

defaults {
  domains = true
}

process "api" {
  command = "./api"
  port    = 8080
}

process "legacy" {
  command = "./legacy"
}
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	sb, err := doze.LoadStack(path)
	if err != nil {
		t.Fatalf("LoadStack: %v", err)
	}

	// The loaded stack round-trips the existing blocks verbatim.
	out := sb.HCL()
	for _, want := range []string{`name = "shop"`, "domains = true", `process "api"`, "port    = 8080", `process "legacy"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("round-trip HCL missing %q:\n%s", want, out)
		}
	}

	// Edit: add a new service, remove an old one.
	sb.AddModule("valkey", "cache").Version("9").Port(6379)
	if !sb.Remove("legacy") {
		t.Fatal("Remove(legacy) returned false")
	}

	out = sb.HCL()
	if !strings.Contains(out, `valkey "cache"`) {
		t.Fatalf("added service missing:\n%s", out)
	}
	if strings.Contains(out, `process "legacy"`) {
		t.Fatalf("removed service still present:\n%s", out)
	}
	// The untouched api block survives verbatim.
	if !strings.Contains(out, "port    = 8080") {
		t.Fatalf("existing block not preserved verbatim:\n%s", out)
	}
}

// TestStackValidate proves a programmatic (or loaded) stack can be validated
// daemon-less before serving.
func TestStackValidate(t *testing.T) {
	base, err := os.MkdirTemp("/tmp", "dz")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	t.Setenv("DOZE_HOME", base)

	good := doze.NewStack("ok")
	good.AddProcess("w", doze.Process{Command: "sh -c 'sleep 1'"})
	if err := good.Validate(doze.Options{}); err != nil {
		t.Fatalf("valid stack failed Validate: %v", err)
	}

	bad := doze.NewStack("bad")
	bad.AddModule("nonsense", "x").Port(1)
	if err := bad.Validate(doze.Options{}); err == nil {
		t.Fatal("invalid stack passed Validate")
	}
}
