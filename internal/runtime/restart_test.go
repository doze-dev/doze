package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/registry"
)

// testRuntime is a minimal Runtime for exercising the restart-cancellation logic
// without a real config/driver (Stop short-circuits before touching cfg when there
// is no live process or held dep).
func testRuntime() *Runtime {
	return &Runtime{
		reg:      registry.New(),
		procs:    map[string]engine.Process{},
		deps:     map[string][]string{},
		restarts: map[string]*restartEntry{},
		logf:     func(string, ...any) {},
	}
}

func (r *Runtime) pendingRestart(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.restarts[name]
	return ok
}

// An intentional Stop during a restart backoff window must cancel the pending
// restart — never respawn an instance the user stopped (the zombie-respawn bug).
func TestStopCancelsPendingRestart(t *testing.T) {
	r := testRuntime()
	r.scheduleRestart("api", time.Hour) // far enough out that it never fires
	if !r.pendingRestart("api") {
		t.Fatal("expected a pending restart after scheduleRestart")
	}
	if err := r.Stop(context.Background(), "api"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if r.pendingRestart("api") {
		t.Fatal("Stop must cancel the pending restart")
	}
	if got := r.reg.Snapshot(); len(got) == 1 && got[0].RestartCount != 0 {
		t.Errorf("Stop must reset the restart budget, got count=%d", got[0].RestartCount)
	}
}

// Daemon shutdown (StopAll) must cancel a restart pending for an instance that has
// no live process — otherwise a context.Background() boot outlives the daemon.
func TestStopAllCancelsPendingRestart(t *testing.T) {
	r := testRuntime()
	r.scheduleRestart("api", time.Hour)
	r.StopAll(context.Background())
	if r.pendingRestart("api") {
		t.Fatal("StopAll must cancel pending restarts (no leaked respawn past shutdown)")
	}
}

// A second scheduleRestart for the same instance supersedes the first.
func TestScheduleRestartSupersedes(t *testing.T) {
	r := testRuntime()
	r.scheduleRestart("api", time.Hour)
	r.scheduleRestart("api", time.Hour)
	if !r.pendingRestart("api") {
		t.Fatal("expected exactly one pending restart")
	}
	if err := r.Stop(context.Background(), "api"); err != nil {
		t.Fatal(err)
	}
	if r.pendingRestart("api") {
		t.Fatal("Stop must clear the superseding restart too")
	}
}

func TestShouldRestart(t *testing.T) {
	cases := []struct {
		policy  engine.RestartPolicy
		exitErr error
		want    bool
	}{
		{engine.RestartNo, nil, false},
		{engine.RestartNo, errors.New("boom"), false},
		{engine.RestartOnFailure, nil, false},
		{engine.RestartOnFailure, errors.New("boom"), true},
		{engine.RestartAlways, nil, true},
		{engine.RestartAlways, errors.New("boom"), true},
	}
	for _, c := range cases {
		if got := shouldRestart(c.policy, c.exitErr); got != c.want {
			t.Errorf("shouldRestart(%q, err=%v) = %v, want %v", c.policy, c.exitErr != nil, got, c.want)
		}
	}
}

func TestBackoffFor(t *testing.T) {
	base := time.Second
	want := []time.Duration{0, 1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	for attempt := 1; attempt <= 4; attempt++ {
		if got := backoffFor(base, attempt); got != want[attempt] {
			t.Errorf("backoffFor(1s, %d) = %s, want %s", attempt, got, want[attempt])
		}
	}
	// Exponential growth is capped at 30s.
	if got := backoffFor(base, 20); got != 30*time.Second {
		t.Errorf("backoffFor(1s, 20) = %s, want 30s cap", got)
	}
	// A zero base falls back to 1s.
	if got := backoffFor(0, 1); got != time.Second {
		t.Errorf("backoffFor(0, 1) = %s, want 1s", got)
	}
}
