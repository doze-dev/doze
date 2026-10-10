package supervisor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// A child a command starts with `&` ignores SIGINT and SIGQUIT, so it survives
// a stop its parent obeyed. Stopping a tree must not leave it behind.
func TestStopLeavesNoOrphans(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	// The child records its pid and sleeps; the parent waits in the foreground.
	script := "sleep 300 & echo $! > " + pidFile + "; exec sleep 300"
	p, err := StartTree(exec.Command("sh", "-c", script), Sink{})
	if err != nil {
		t.Fatal(err)
	}
	var child int
	deadline := time.Now().Add(3 * time.Second)
	for child == 0 && time.Now().Before(deadline) {
		if raw, err := os.ReadFile(pidFile); err == nil {
			child, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if child == 0 || !alive(child) {
		t.Fatal("the background child never started")
	}
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })

	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for alive(child) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if alive(child) {
		t.Fatalf("the background child (pid %d) outlived the stop", child)
	}
}

// The same when the command ends by itself: what it leaves running goes too.
func TestACommandThatExitsTakesItsChildrenWithIt(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	p, err := StartTree(exec.Command("sh", "-c", "sleep 300 & echo $! > "+pidFile+"; sleep 0.2; exit 3"), Sink{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err == nil {
		t.Fatal("the command exits 3")
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	child, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	deadline := time.Now().Add(2 * time.Second)
	for alive(child) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if alive(child) {
		t.Fatalf("the child (pid %d) outlived the command that started it", child)
	}
}
