package runtime

import (
	"errors"
	"testing"
	"time"

	"github.com/nerdmenot/doze/internal/engine"
)

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
