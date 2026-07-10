package doze_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	doze "github.com/doze-dev/doze"
)

// TestAttachFromStack proves the config-less Attach path (Phase C): build a real
// doze binary, Attach a Go-built stack (materialized to a file the background
// daemon reads), live-add a service, then Shutdown the daemon.
func TestAttachFromStack(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a doze binary and spawns a background daemon; skipped in -short")
	}
	base, err := os.MkdirTemp("/tmp", "dz")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	t.Setenv("DOZE_HOME", filepath.Join(base, "h"))

	// Build the doze binary Attach will spawn as the background daemon.
	bin := filepath.Join(base, "doze")
	build := exec.Command("go", "build", "-o", bin, "./cmd/doze")
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building doze: %v\n%s", err, out)
	}

	sb := doze.NewStack("attachtest")
	sb.AddProcess("first", doze.Process{
		Command: "sh -c 'while true; do echo one; sleep 1; done'",
	})

	ctx := context.Background()
	sess, err := doze.Attach(ctx, doze.Options{
		Stack:      sb,
		WorkDir:    filepath.Join(base, "proj"),
		DozeBinary: bin,
	})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	// Ensure we stop the background daemon even on failure.
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = sess.Shutdown(sctx)
	}()

	// The materialized HCL file must exist for the daemon to have read it.
	if _, err := os.Stat(filepath.Join(base, "proj", "doze.hcl")); err != nil {
		t.Fatalf("stack was not materialized to disk: %v", err)
	}

	upCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if err := sess.Up(upCtx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Live-add a second process against the BACKGROUND daemon (proves Attach mode).
	addCtx, addCancel := context.WithTimeout(ctx, 40*time.Second)
	defer addCancel()
	inst, err := sess.AddProcess(addCtx, "second", doze.Process{
		Command: "sh -c 'while true; do echo two; sleep 1; done'",
	})
	if err != nil {
		t.Fatalf("AddProcess (attach): %v", err)
	}
	if inst.PID == 0 {
		t.Fatalf("live-added process not running: %+v", inst)
	}
	if err := sess.Remove(ctx, "second", true); err != nil {
		t.Fatalf("Remove (attach): %v", err)
	}
}
