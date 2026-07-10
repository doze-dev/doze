package doze_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	doze "github.com/doze-dev/doze"
)

// TestLoadDaemonless proves lint/tree/plan can be built on the library WITHOUT
// booting a daemon: Load parses + validates + exposes topology and the plan,
// with nothing spawned.
func TestLoadDaemonless(t *testing.T) {
	base, err := os.MkdirTemp("/tmp", "dz")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	t.Setenv("DOZE_HOME", base)

	sb := doze.NewStack("static")
	sb.AddProcess("api", doze.Process{Command: "sh -c 'sleep 1'", Port: 8080})
	sb.AddProcess("worker", doze.Process{Command: "sh -c 'sleep 1'"})

	in, err := doze.Load(doze.Options{Stack: sb})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer in.Close()

	if in.StackName() != "static" {
		t.Fatalf("StackName = %q", in.StackName())
	}
	nodes := in.Topology()
	if len(nodes) != 2 {
		t.Fatalf("Topology len = %d", len(nodes))
	}
	// A process-only stack declares no structural objects, so the plan is empty —
	// but computing it must succeed with no daemon and no error.
	plan, err := in.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.Empty() {
		t.Fatalf("expected empty plan, got %+v", plan)
	}
}

// TestLoadInvalidIsLint proves the lint result: an invalid config makes Load
// return the validation error (no daemon involved).
func TestLoadInvalidIsLint(t *testing.T) {
	base, err := os.MkdirTemp("/tmp", "dz")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	t.Setenv("DOZE_HOME", base)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "doze.hcl")
	// `nonsense` is not a known engine — a validation error.
	if err := os.WriteFile(cfgPath, []byte("nonsense \"x\" {\n  foo = 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in, err := doze.Load(doze.Options{ConfigPath: cfgPath})
	if err == nil {
		in.Close()
		t.Fatal("Load accepted an invalid config; want a lint error")
	}
}

// TestMissingConfig proves a clear error (not a panic) when neither Stack nor a
// findable config is provided.
func TestMissingConfig(t *testing.T) {
	base, err := os.MkdirTemp("/tmp", "dz")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	t.Setenv("DOZE_HOME", base)

	_, err = doze.Load(doze.Options{ConfigPath: filepath.Join(base, "does-not-exist.hcl")})
	if err == nil {
		t.Fatal("expected an error for a missing config")
	}
	// Not a typed sentinel necessarily, but must be a real error.
	if errors.Is(err, doze.ErrNotFound) {
		// fine if it maps, but not required
	}
}
