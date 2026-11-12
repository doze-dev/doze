package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/fault"
	"github.com/doze-dev/doze/internal/modules"
)

// captureStdout runs f and returns what it printed on stdout.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	defer func() { os.Stdout = old }()
	f()
	_ = w.Close()
	os.Stdout = old
	return <-done
}

// unreachableValkey is the load error a stack gets when the valkey module is
// not cached and the registry has no DNS record, as config wraps it.
func unreachableValkey() error {
	un := &modules.UnreachableError{Module: "valkey", Host: "registry.doze-dev.com", Registry: true, Cause: "no DNS answer"}
	return fault.With(fault.Invalid, &config.ModuleUnreachableError{Err: un})
}

// lint gave "✓ doze.hcl is valid" and exit 0 having checked none of the
// module's arguments, because the registry could not be reached: a typo
// passed (F4). A block it could not check is not a pass: no tick, which
// blocks and why, and exit 5 (the code status/env/url give the same cause).
func TestLintWithTheRegistryDownIsNotAPass(t *testing.T) {
	inProject(t, "module valkey \"cache\" {\n  version = 8\n  port    = 6390\n  maxmemory_policy = \"allkeys-lur\"\n}\n")
	shape, err := config.LoadShape(configPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	var verdict error
	out := captureStdout(t, func() { verdict = lintVerdict(shape, unreachableValkey()) })
	if strings.Contains(out, "✓") || strings.Contains(out, "is valid") {
		t.Errorf("a tick for work not done:\n%s", out)
	}
	for _, want := range []string{
		"not checked: valkey",
		"the registry can't be reached (registry.doze-dev.com: no DNS answer)",
		"would pass unseen",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "registry.doze-dev.com") != 1 {
		t.Errorf("the cause is said more than once:\n%s", out)
	}
	if code := codeOf(verdict); code != 5 {
		t.Errorf("exit %d, want 5", code)
	}

	// Checked in full, it is still a tick and exit 0.
	out = captureStdout(t, func() { verdict = lintVerdict(shape, nil) })
	if !strings.Contains(out, "✓") || codeOf(verdict) != 0 {
		t.Errorf("a full pass: exit %d\n%s", codeOf(verdict), out)
	}
}

type fakeProbe struct{ err error }

func (f fakeProbe) CheckRegistry(context.Context, time.Duration) error { return f.err }

// doctor said "✓ all checks passed" without ever asking the registry (F5).
// It now has a registry row, failing when the registry does not answer.
func TestDoctorChecksTheRegistry(t *testing.T) {
	t.Setenv("DOZE_MODULES", "")
	un := &modules.UnreachableError{Module: "valkey", Host: "registry.doze-dev.com", Registry: true, Cause: "no DNS answer"}
	c := registryCheck(context.Background(), fakeProbe{un})
	if c.OK || c.Name != "registry" || !strings.Contains(c.Detail, "can't be reached (registry.doze-dev.com: no DNS answer)") {
		t.Errorf("registry down: %+v", c)
	}
	if c := registryCheck(context.Background(), fakeProbe{}); !c.OK || !strings.Contains(c.Detail, "answered") {
		t.Errorf("registry up: %+v", c)
	}
	t.Setenv("DOZE_MODULES", "off")
	if c := registryCheck(context.Background(), fakeProbe{un}); !c.OK || !strings.Contains(c.Detail, "not checked") {
		t.Errorf("modules off: %+v", c)
	}
}
