package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/fault"
	"github.com/doze-dev/doze/internal/modules"
)

// A module that could not be fetched because the registry did not answer is
// not a mistake in doze.hcl. status, env and url used to say "the stack's
// config has a mistake — no module called valkey" (F3); now the load error
// names the module, the host and why, once, and still exits 5.
func TestUnreachableModuleIsNotAConfigMistake(t *testing.T) {
	un := &modules.UnreachableError{Module: "widgetdb", Host: "registry.doze-dev.com", Registry: true, Cause: "no DNS answer"}
	hooks := &config.Hooks{LookupError: func(engineType string) error {
		if engineType == "widgetdb" {
			return un
		}
		return nil
	}}
	src := "module widgetdb \"cache\" {\n  version = 8\n  port    = 6390\n}\n"
	_, err := config.Parse([]byte(src), "doze.hcl", hooks)
	if err == nil {
		t.Fatal("a config whose module cannot be fetched loaded")
	}
	msg := err.Error()
	for _, bad := range []string{"has a mistake", "no module called", "Error:"} {
		if strings.Contains(msg, bad) {
			t.Errorf("message blames the config (%q):\n%s", bad, msg)
		}
	}
	want := "module widgetdb could not be fetched: the module registry can't be reached (registry.doze-dev.com: no DNS answer)"
	if !strings.Contains(msg, want) {
		t.Errorf("message = %q, want it to hold %q", msg, want)
	}
	if strings.Count(msg, "registry.doze-dev.com") != 1 {
		t.Errorf("the cause is said more than once:\n%s", msg)
	}
	if !modules.IsUnreachable(err) || !errors.Is(err, un) {
		t.Errorf("the cause is lost: %v", err)
	}
	if fault.Of(err) != fault.Invalid {
		t.Errorf("kind = %q, want %q (exit 5: a module it names won't fetch)", fault.Of(err), fault.Invalid)
	}
}
