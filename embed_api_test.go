package doze_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	doze "github.com/doze-dev/doze"
)

func serveTestStack(t *testing.T, sb *doze.Stack) *doze.Session {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "dz")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	t.Setenv("DOZE_HOME", base)
	sess, err := doze.Serve(context.Background(), doze.Options{Stack: sb})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { sess.Close() })
	return sess
}

// TestTopology proves the declared graph is readable as data without the daemon
// doing anything — engine, version, port, deps.
func TestTopology(t *testing.T) {
	// Built-in process engine only, so the test is hermetic (no module fetch).
	sb := doze.NewStack("topo")
	sb.AddProcess("api", doze.Process{Command: "sh -c 'sleep 1000'", Port: 8080})
	sb.AddProcess("worker", doze.Process{Command: "sh -c 'sleep 1000'"})

	if testing.Short() {
		// Topology is a pure config projection; still needs a Serve to build cfg.
		t.Skip("needs Serve to construct the config")
	}
	sess := serveTestStack(t, sb)

	nodes := sess.Topology()
	if len(nodes) != 2 {
		t.Fatalf("Topology len = %d, want 2", len(nodes))
	}
	byName := map[string]doze.Node{}
	for _, n := range nodes {
		byName[n.Name] = n
	}
	if a := byName["api"]; a.Engine != "process" || a.Port != 8080 || !a.Enabled {
		t.Fatalf("api node = %+v", a)
	}
	if _, ok := byName["worker"]; !ok {
		t.Fatalf("worker missing from topology: %+v", nodes)
	}
}

// TestTypedErrors proves failures come back as sentinel errors, not strings, on
// the direct (in-process) Serve backend.
func TestTypedErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("boots the daemon")
	}
	sb := doze.NewStack("errs")
	sb.AddProcess("only", doze.Process{Command: "sh -c 'while true; do sleep 1; done'"})
	sess := serveTestStack(t, sb)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := sess.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Removing a service that doesn't exist → ErrNotFound.
	if err := sess.Remove(ctx, "ghost", false); !errors.Is(err, doze.ErrNotFound) {
		t.Fatalf("Remove(ghost) err = %v, want ErrNotFound", err)
	}

	// Adding a name that already exists → ErrAlreadyExists.
	_, err := sess.AddProcess(ctx, "only", doze.Process{Command: "sh -c 'sleep 1'"})
	if !errors.Is(err, doze.ErrAlreadyExists) {
		t.Fatalf("AddProcess(dup) err = %v, want ErrAlreadyExists", err)
	}

	// NOTE: live-adding the aws engine is gated (ErrUnsupported), but reaching
	// that gate requires decoding the block through the aws module — a registry
	// fetch — so it has no hermetic unit test here. The sentinel mapping itself
	// is covered by the cases above.
}
