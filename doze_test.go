package doze_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	doze "github.com/doze-dev/doze"
)

// TestServeLifecycle boots a hermetic single-process stack in-process via Serve,
// brings it up, reads its state and logs, and tears it down — no plugins, no
// network, no external binaries beyond /bin/sh.
func TestServeLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("boots a real supervised process; skipped in -short")
	}
	// Unix socket paths are capped near 104 bytes on macOS, and the daemon's
	// control socket lives several dirs deep under DOZE_HOME — so the default
	// (long) /var/folders temp path overflows the bind. Use a short /tmp base.
	base, err := os.MkdirTemp("/tmp", "dz")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	dir := filepath.Join(base, "p")
	home := filepath.Join(base, "h")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOZE_HOME", home)

	cfgPath := filepath.Join(dir, "doze.hcl")
	if err := os.WriteFile(cfgPath, []byte(`
name = "embedtest"

process "ticker" {
  command = "sh -c 'while true; do echo tick; sleep 1; done'"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	sess, err := doze.Serve(ctx, doze.Options{ConfigPath: cfgPath})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer sess.Close()

	if got := sess.StackName(); got != "embedtest" {
		t.Fatalf("StackName = %q, want embedtest", got)
	}
	if svcs := sess.Services(); len(svcs) != 1 || svcs[0] != "ticker" {
		t.Fatalf("Services = %v, want [ticker]", svcs)
	}

	upCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := sess.Up(upCtx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	inst, ok, err := sess.Instance(ctx, "ticker")
	if err != nil || !ok {
		t.Fatalf("Instance: ok=%v err=%v", ok, err)
	}
	if inst.PID == 0 {
		t.Fatalf("ticker has no pid after Up: %+v", inst)
	}
	pid := inst.PID

	// Logs should carry the ticker's output within a couple of seconds.
	deadline := time.Now().Add(5 * time.Second)
	var sawTick bool
	for time.Now().Before(deadline) && !sawTick {
		lines, err := sess.Logs(ctx, "ticker")
		if err != nil {
			t.Fatalf("Logs: %v", err)
		}
		for _, l := range lines {
			if l == "tick" {
				sawTick = true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !sawTick {
		t.Fatalf("never saw a 'tick' log line")
	}

	if err := sess.Down(ctx, ""); err != nil {
		t.Fatalf("Down: %v", err)
	}

	// After Down + Close the supervised child must be gone.
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if processAlive(pid) {
		t.Fatalf("ticker child (pid %d) survived shutdown", pid)
	}
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil // signal 0: liveness probe
}
