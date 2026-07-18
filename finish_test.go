package doze_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	doze "github.com/doze-dev/doze"
)

// TestUpWithProgress proves the progress feed delivers state transitions while
// the stack comes up.
func TestUpWithProgress(t *testing.T) {
	if testing.Short() {
		t.Skip("boots a process")
	}
	sb := doze.NewStack("prog")
	sb.AddProcess("w", doze.Process{Command: "sh -c 'while true; do sleep 1; done'"})
	sess := serveTestStack(t, sb)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The progress callback runs on the event-feed goroutine; guard the slice.
	var mu sync.Mutex
	var got []string
	err := sess.UpWithProgress(ctx, nil, func(p doze.Progress) {
		mu.Lock()
		got = append(got, p.Instance+":"+p.State)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("UpWithProgress: %v", err)
	}
	// Give the async feed a beat to flush the final transition.
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	n := len(got)
	mu.Unlock()
	if n == 0 {
		t.Fatal("no progress events delivered during Up")
	}
}

// TestManyStacksOneProcess proves the reframed multi-tenancy story: many stacks
// under one DOZE_HOME coexist in a single process (hostboot ref-counts the
// shared host).
func TestManyStacksOneProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("boots daemons")
	}
	base, err := os.MkdirTemp("/tmp", "dz")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	t.Setenv("DOZE_HOME", base)

	ctx := context.Background()
	mk := func(name string) *doze.Session {
		sb := doze.NewStack(name)
		sb.AddProcess("w", doze.Process{Command: "sh -c 'while true; do sleep 1; done'"})
		s, err := doze.Serve(ctx, doze.Options{Stack: sb})
		if err != nil {
			t.Fatalf("Serve(%s): %v", name, err)
		}
		return s
	}
	a := mk("alpha")
	defer a.Close()
	b := mk("beta")
	defer b.Close()

	if a.StackName() != "alpha" || b.StackName() != "beta" {
		t.Fatalf("stacks = %q, %q", a.StackName(), b.StackName())
	}
	// Both are independently controllable.
	uctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := a.Up(uctx); err != nil {
		t.Fatalf("alpha Up: %v", err)
	}
	if err := b.Up(uctx); err != nil {
		t.Fatalf("beta Up: %v", err)
	}
}

// TestLiveAddCrossRefClearError proves a live-added block that references a
// sibling fails with a clear, actionable message (not a raw HCL error).
func TestLiveAddCrossRefClearError(t *testing.T) {
	if testing.Short() {
		t.Skip("boots a daemon")
	}
	sb := doze.NewStack("refs")
	sb.AddProcess("w", doze.Process{Command: "sh -c 'while true; do sleep 1; done'"})
	sess := serveTestStack(t, sb)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := sess.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// A process block (built-in, so hermetic) with a reference to another
	// service by HCL traversal, injected via a raw body.
	m := doze.NewModule("process", "x").Body("command = other.svc.arn")
	_, err := sess.AddModule(ctx, m)
	if err == nil {
		t.Fatal("expected an error for a cross-referencing live add")
	}
	if !strings.Contains(err.Error(), "can't reference other services") {
		t.Fatalf("error not actionable: %v", err)
	}
}
