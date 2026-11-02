package daemonctl

import (
	"reflect"
	"testing"
)

// The daemon must load the config with the same --var overrides as the command
// that started it, or it runs a different stack.
func TestDaemonArgsCarryVars(t *testing.T) {
	got := daemonArgs("/p/doze.hcl", map[string]string{"domains": "false", "pg": "16"})
	want := []string{"__daemon", "--config", "/p/doze.hcl", "--var", "domains=false", "--var", "pg=16"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("daemonArgs = %q, want %q", got, want)
	}
	if got := daemonArgs("/p/doze.hcl", nil); !reflect.DeepEqual(got, []string{"__daemon", "--config", "/p/doze.hcl"}) {
		t.Fatalf("no vars: %q", got)
	}
}
