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

// A restart and a pin are things a watcher needs to hear about, even though
// neither changes the state.
func TestRestartCountAndPinEmit(t *testing.T) {
	r := New()
	r.MarkRunning("api", "", 0, 1)
	ch, cancel := r.Subscribe(8)
	defer cancel()

	r.IncRestart("api")
	if got := recv(t, ch); got.RestartCount != 1 {
		t.Fatalf("after a restart: %+v", got)
	}
	r.ToggleKeepAwake("api")
	if got := recv(t, ch); !got.KeepAwake {
		t.Fatalf("after a pin: %+v", got)
	}
	r.ResetRestart("api")
	if got := recv(t, ch); got.RestartCount != 0 {
		t.Fatalf("after a reset: %+v", got)
	}
}

func TestCrashingFailedDone(t *testing.T) {
	r := New()
	r.MarkRunning("api", "/sock", 1, 99)
	next := time.Now().Add(time.Second)
	r.IncRestart("api")
	r.MarkCrashing("api", 5, next, "exited with status 1; restarting (1/5) in 1s")
	got, _ := r.Get("api")
	if got.State != Crashing || got.PID != 0 || got.RestartMax != 5 || !got.NextRestart.Equal(next) || got.RestartCount != 1 {
		t.Fatalf("crashing = %+v", got)
	}
	// A new attempt is booting, and a successful one clears the schedule.
	r.MarkBooting("api")
	r.MarkRunning("api", "/sock", 1, 100)
	if got, _ := r.Get("api"); got.RestartMax != 0 || !got.NextRestart.IsZero() || got.LastError != "" {
		t.Fatalf("running again = %+v", got)
	}
	r.MarkFailed("api", "exited with status 1; gave up after 5 restarts")
	if got, _ := r.Get("api"); got.State != Failed || got.PID != 0 || got.LastError == "" {
		t.Fatalf("failed = %+v", got)
	}
	// Neither is reapable, and a connection does not make either look awake.
	r.Acquire("api")
	if got, _ := r.Get("api"); got.State != Failed {
		t.Fatalf("a connection moved a failed instance to %s", got.State)
	}
	r.MarkDone("migrate")
	if got, _ := r.Get("migrate"); got.State != Done || got.LastError != "" {
		t.Fatalf("done = %+v", got)
	}
}

func recv(t *testing.T, ch <-chan Instance) Instance {
	t.Helper()
	select {
	case inst := <-ch:
		return inst
	case <-time.After(time.Second):
		t.Fatal("no event")
		return Instance{}
	}
}
