package registry

import (
	"testing"
	"time"
)

func TestConnLifecycle(t *testing.T) {
	r := New()
	r.MarkBooting("db")
	if inst, _ := r.Get("db"); inst.State != Booting {
		t.Fatalf("state = %s, want booting", inst.State)
	}
	r.MarkRunning("db", "/sock", 5432, 100)
	if inst, _ := r.Get("db"); inst.State != Idle {
		t.Fatalf("fresh running with no conns should be idle, got %s", inst.State)
	}

	r.Acquire("db")
	if inst, _ := r.Get("db"); inst.State != Active || inst.Conns != 1 {
		t.Fatalf("after acquire: state=%s conns=%d", inst.State, inst.Conns)
	}
	r.Acquire("db")
	r.Release("db")
	if inst, _ := r.Get("db"); inst.State != Active || inst.Conns != 1 {
		t.Fatalf("one conn left should stay active: state=%s conns=%d", inst.State, inst.Conns)
	}
	r.Release("db")
	if inst, _ := r.Get("db"); inst.State != Idle || inst.Conns != 0 {
		t.Fatalf("zero conns should be idle: state=%s conns=%d", inst.State, inst.Conns)
	}
}

func TestReapableOnlyAfterTimeout(t *testing.T) {
	now := time.Now()
	r := New()
	r.now = func() time.Time { return now }

	r.MarkRunning("db", "/sock", 5432, 100) // idle since `now`
	if got := r.Reapable(time.Minute); len(got) != 0 {
		t.Fatalf("should not be reapable immediately, got %v", got)
	}

	// Advance past the timeout.
	now = now.Add(2 * time.Minute)
	if got := r.Reapable(time.Minute); len(got) != 1 || got[0] != "db" {
		t.Fatalf("should be reapable after timeout, got %v", got)
	}
}

func TestKeepAwakeExemptsFromReaper(t *testing.T) {
	now := time.Now()
	r := New()
	r.now = func() time.Time { return now }
	r.MarkRunning("db", "/sock", 5432, 100)
	now = now.Add(2 * time.Minute) // aged past the timeout

	if got := r.Reapable(time.Minute); len(got) != 1 {
		t.Fatalf("idle past timeout should be reapable, got %v", got)
	}
	if !r.ToggleKeepAwake("db") {
		t.Fatal("toggle should report kept-awake (true)")
	}
	if got := r.Reapable(time.Minute); len(got) != 0 {
		t.Fatalf("a kept-awake db must never be reaped, got %v", got)
	}
	if r.ToggleKeepAwake("db") {
		t.Fatal("toggle should report auto-sleep restored (false)")
	}
	if got := r.Reapable(time.Minute); len(got) != 1 {
		t.Fatalf("after un-pinning it should be reapable again, got %v", got)
	}
}

func TestReapableNotWhenConnected(t *testing.T) {
	now := time.Now()
	r := New()
	r.now = func() time.Time { return now }
	r.MarkRunning("db", "/sock", 5432, 100)
	r.Acquire("db") // a pool holds a connection open

	now = now.Add(time.Hour)
	if got := r.Reapable(time.Minute); len(got) != 0 {
		t.Fatalf("a connected db must never be reaped, got %v", got)
	}

	// Once released and aged out, it becomes reapable.
	r.Release("db")
	now = now.Add(time.Hour)
	if got := r.Reapable(time.Minute); len(got) != 1 {
		t.Fatalf("should be reapable after release+timeout, got %v", got)
	}
}

func TestSnapshotSorted(t *testing.T) {
	r := New()
	r.MarkRunning("zeta", "/z", 5432, 1)
	r.MarkRunning("alpha", "/a", 5432, 2)
	snap := r.Snapshot()
	if len(snap) != 2 || snap[0].Name != "alpha" || snap[1].Name != "zeta" {
		t.Fatalf("snapshot not sorted: %+v", snap)
	}
}

func TestMarkReapedClears(t *testing.T) {
	r := New()
	r.MarkRunning("db", "/sock", 5432, 100)
	r.Acquire("db")
	r.MarkReaped("db")
	inst, _ := r.Get("db")
	if inst.State != Reaped || inst.PID != 0 || inst.Conns != 0 || inst.SocketDir != "" {
		t.Fatalf("reaped instance not cleared: %+v", inst)
	}
}

// Taint must persist across boot attempts (MarkBooting/MarkRunning) and reaps —
// a half-converged instance can never look healthy until a converge clears it.
func TestTaintPersistsUntilCleared(t *testing.T) {
	r := New()
	r.SetTainted("db")

	// A fresh boot attempt clears LastError but must NOT clear the taint.
	r.SetError("db", "boom")
	r.MarkBooting("db")
	if inst, _ := r.Get("db"); inst.LastError != "" {
		t.Fatalf("MarkBooting should clear LastError, got %q", inst.LastError)
	}
	if inst, _ := r.Get("db"); !inst.Tainted {
		t.Fatal("MarkBooting must not clear taint")
	}

	// A successful boot also must not clear the taint (only a converge does).
	r.MarkRunning("db", "/sock", 5432, 100)
	if inst, _ := r.Get("db"); !inst.Tainted {
		t.Fatal("MarkRunning must not clear taint")
	}

	// A reap leaves the structure incomplete — taint survives.
	r.MarkReaped("db")
	if inst, _ := r.Get("db"); !inst.Tainted {
		t.Fatal("MarkReaped must not clear taint")
	}

	// Only a successful converge clears it.
	r.ClearTainted("db")
	if inst, _ := r.Get("db"); inst.Tainted {
		t.Fatal("ClearTainted should clear taint")
	}
}
