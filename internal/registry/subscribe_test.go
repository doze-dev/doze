package registry

import (
	"testing"
	"time"
)

func TestSubscribeEmitsOnTransitions(t *testing.T) {
	r := New()
	ch, cancel := r.Subscribe(16)
	defer cancel()

	r.MarkBooting("db")             // Reaped->Booting: emit
	r.MarkRunning("db", "/s", 5, 9) // Booting->Idle: emit
	r.SetHealthy("db", true)        // Healthy nil->true: emit

	got := drain(t, ch, 3)
	if got[0].State != Booting {
		t.Fatalf("first = %v, want booting", got[0].State)
	}
	if got[1].State != Idle {
		t.Fatalf("second = %v, want idle", got[1].State)
	}
	if got[2].Healthy == nil || !*got[2].Healthy {
		t.Fatalf("third healthy = %v, want true", got[2].Healthy)
	}
}

func TestSubscribeIgnoresConnChurn(t *testing.T) {
	r := New()
	r.MarkRunning("db", "/s", 5, 9) // Idle (no subscriber yet)
	ch, cancel := r.Subscribe(16)
	defer cancel()

	// Acquire moves Idle->Active (a transition: 1 emit). A second Acquire is
	// pure Conns churn with no state change: no emit. Release back to zero
	// moves Active->Idle: 1 emit.
	r.Acquire("db")
	r.Acquire("db")
	r.Release("db")
	r.Release("db")

	got := drain(t, ch, 2)
	if got[0].State != Active || got[1].State != Idle {
		t.Fatalf("states = %v,%v want active,idle", got[0].State, got[1].State)
	}
	select {
	case extra := <-ch:
		t.Fatalf("unexpected extra emit: %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func drain(t *testing.T, ch <-chan Instance, n int) []Instance {
	t.Helper()
	out := make([]Instance, 0, n)
	for i := 0; i < n; i++ {
		select {
		case inst := <-ch:
			out = append(out, inst)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for emit %d/%d", i+1, n)
		}
	}
	return out
}
