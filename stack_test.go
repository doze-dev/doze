package doze_test

import (
	"context"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	doze "github.com/doze-dev/doze"
)

// TestStackHCLRender checks the builder renders faithful, parseable HCL for both
// AddProcess and AddModule (including a raw Body fragment).
func TestStackHCLRender(t *testing.T) {
	sb := doze.NewStack("shop").Domains(true).IdleTimeout(5 * time.Minute)
	sb.AddProcess("worker", doze.Process{
		Command: "python worker.py",
		Env:     map[string]string{"LOG": "debug"},
		Ingress: true,
		Health:  &doze.Health{HTTP: "http://localhost:8080/health", Interval: "2s"},
	})
	sb.AddModule("kafka", "events").Version("4").Port(9092).
		Set("auto_create_topics", true).
		Body("topic \"orders\" {\n  partitions = 3\n}")

	got := sb.HCL()
	for _, want := range []string{
		`name = "shop"`,
		"domains = true",
		`process "worker"`,
		`command = "python worker.py"`,
		"ingress = true",
		"health {",
		`kafka "events"`,
		"version = \"4\"",
		"auto_create_topics = true",
		`topic "orders"`,
		"partitions = 3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered HCL missing %q\n---\n%s", want, got)
		}
	}
}

// TestServeFromStack proves the config-less path: build a stack in Go, Serve it
// with NO HCL file on disk, bring it up, and read its state.
func TestServeFromStack(t *testing.T) {
	if testing.Short() {
		t.Skip("boots a real supervised process; skipped in -short")
	}
	// Short DOZE_HOME so the derived control-socket path stays under the macOS
	// unix-socket length cap.
	base, err := os.MkdirTemp("/tmp", "dz")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	t.Setenv("DOZE_HOME", base)

	sb := doze.NewStack("embedbuilt")
	sb.AddProcess("ticker", doze.Process{
		Command: "sh -c 'while true; do echo tick; sleep 1; done'",
	})

	ctx := context.Background()
	sess, err := doze.Serve(ctx, doze.Options{Stack: sb})
	if err != nil {
		t.Fatalf("Serve from stack: %v", err)
	}
	defer sess.Close()

	if sess.StackName() != "embedbuilt" {
		t.Fatalf("StackName = %q", sess.StackName())
	}
	upCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := sess.Up(upCtx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	inst, ok, err := sess.Instance(ctx, "ticker")
	if err != nil || !ok || inst.PID == 0 {
		t.Fatalf("ticker not running: ok=%v err=%v inst=%+v", ok, err, inst)
	}
	pid := inst.PID
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Fatalf("ticker child (pid %d) survived shutdown", pid)
	}
}
