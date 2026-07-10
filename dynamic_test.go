package doze_test

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	doze "github.com/doze-dev/doze"
)

// TestLiveAddRemoveProcess brings up a stack, then dynamically adds a second
// process at runtime, confirms it's running, and removes it again — the
// "dynamically add infra to a live stack" path.
func TestLiveAddRemoveProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("boots real processes; skipped in -short")
	}
	base, err := os.MkdirTemp("/tmp", "dz")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	t.Setenv("DOZE_HOME", base)

	sb := doze.NewStack("livetest")
	sb.AddProcess("first", doze.Process{
		Command: "sh -c 'while true; do echo one; sleep 1; done'",
	})

	ctx := context.Background()
	sess, err := doze.Serve(ctx, doze.Options{Stack: sb})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer sess.Close()

	upCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := sess.Up(upCtx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// The stack started with one service.
	if svcs := sess.Services(); len(svcs) != 1 {
		t.Fatalf("Services before add = %v", svcs)
	}

	// Dynamically add a second process — no config file, no restart.
	addCtx, addCancel := context.WithTimeout(ctx, 30*time.Second)
	defer addCancel()
	inst, err := sess.AddProcess(addCtx, "second", doze.Process{
		Command: "sh -c 'while true; do echo two; sleep 1; done'",
	})
	if err != nil {
		t.Fatalf("AddProcess: %v", err)
	}
	if inst.PID == 0 {
		t.Fatalf("added process not running: %+v", inst)
	}
	addedPID := inst.PID

	// It's now part of the live stack.
	found := false
	for _, n := range sess.Services() {
		if n == "second" {
			found = true
		}
	}
	if !found {
		t.Fatalf("added service not in Services(): %v", sess.Services())
	}

	// Remove it — its process must die and it must leave the stack.
	if err := sess.Remove(ctx, "second", true); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && syscall.Kill(addedPID, 0) == nil {
		time.Sleep(100 * time.Millisecond)
	}
	if syscall.Kill(addedPID, 0) == nil {
		t.Fatalf("removed process (pid %d) still alive", addedPID)
	}
	for _, n := range sess.Services() {
		if n == "second" {
			t.Fatalf("removed service still in Services()")
		}
	}
}
