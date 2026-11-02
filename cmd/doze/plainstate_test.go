package main

import (
	"testing"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
)

// status --json must say what actually happened to an instance: a crash, a
// failure or a finished task is not "asleep".
func TestPlainStateKeepsCrashAndFailure(t *testing.T) {
	on := &config.InstanceDecl{Enabled: true}
	for _, tc := range []struct {
		state   string
		running bool
		want    string
	}{
		{"active", true, "active"},
		{"idle", true, "idle"},
		{"booting", true, "booting"},
		{"crashing", true, "crashing"},
		{"failed", true, "failed"},
		{"done", true, "done"},
		{"reaped", true, "asleep"},
		{"active", false, "asleep"},
	} {
		got := plainState(control.InstanceView{State: tc.state}, tc.running, on)
		if got != tc.want {
			t.Errorf("plainState(%q, running=%v) = %q, want %q", tc.state, tc.running, got, tc.want)
		}
	}
	if got := plainState(control.InstanceView{State: "active"}, true, &config.InstanceDecl{}); got != "disabled" {
		t.Errorf("disabled instance = %q, want disabled", got)
	}
}
