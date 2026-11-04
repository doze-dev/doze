package hub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/journal"
	"github.com/doze-dev/doze/internal/projects"
)

// fakeHome lays out a doze home with one project folder per fake daemon,
// each with a control socket path (a plain file: the hub only globs for
// it) and hands the fakes out through Dial.
func fakeHome(t *testing.T, fakes map[string]*fakedaemon.Fake) (string, func(string) control.Daemon) {
	t.Helper()
	home := t.TempDir()
	bySock := map[string]control.Daemon{}
	for slug, f := range fakes {
		run := filepath.Join(home, "projects", slug, "run")
		if err := os.MkdirAll(run, 0o755); err != nil {
			t.Fatal(err)
		}
		sock := filepath.Join(run, "doze.admin.sock")
		if err := os.WriteFile(sock, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		bySock[sock] = f
	}
	return home, func(sock string) control.Daemon { return bySock[sock] }
}

func wait(t *testing.T, h *Hub, cond func([]StackView) bool) []StackView {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st := h.Stacks(); cond(st) {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met; stacks: %+v", h.Stacks())
	return nil
}

func TestRunningStacksAreFoundAndFollowed(t *testing.T) {
	shop := &fakedaemon.Fake{
		Snap: control.Snapshot{Stack: "shop", Dir: "/code/shop", Instances: []control.InstanceView{
			{Name: "db", Kind: "module", State: "active"},
			{Name: "api", Kind: "app", State: "reaped"},
		}},
		Frames: []control.Frame{
			{Topic: control.TopicInstances, Instances: []control.InstanceView{{Name: "api", Kind: "app", State: "booting"}}},
			{Topic: control.TopicJournal, Event: &journal.Event{Seq: 7, Kind: "wake", Instance: "api"}},
		},
		Hold: true,
	}
	home, dial := fakeHome(t, map[string]*fakedaemon.Fake{"shop-1": shop})
	h := New(Options{Home: home, Dial: dial, Every: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)

	st := wait(t, h, func(st []StackView) bool {
		return len(st) == 1 && st[0].Conn == ConnLive && len(st[0].Events) == 1
	})
	s := st[0]
	if s.Name != "shop" || s.Dir != "/code/shop" || !s.Running {
		t.Errorf("stack = %+v", s)
	}
	if len(s.Instances) != 2 || s.Instances[0].Name != "api" || s.Instances[0].State != "booting" {
		t.Errorf("instances after the frame = %+v", s.Instances)
	}
	if s.Events[0].Seq != 7 {
		t.Errorf("events = %+v", s.Events)
	}
	if err := h.Do(ctx, s.Key, control.Verb{Name: control.VerbWake, Target: "api"}); err != nil {
		t.Fatal(err)
	}
	if done := shop.Done(); len(done) != 1 || done[0].Target != "api" {
		t.Errorf("verbs sent = %+v", done)
	}
}

func TestADaemonThatStopsAnsweringIsDownThenRetried(t *testing.T) {
	// A real daemon answers a subscription with a snapshot frame at once; the
	// connection counts as live from that first frame, as the dash's did.
	shop := &fakedaemon.Fake{
		Snap:   control.Snapshot{Stack: "shop"},
		Frames: []control.Frame{{Topic: control.TopicInstances, Snapshot: true}},
		Hold:   true,
	}
	home, dial := fakeHome(t, map[string]*fakedaemon.Fake{"shop-1": shop})
	h := New(Options{Home: home, Dial: dial, Every: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)
	st := wait(t, h, func(st []StackView) bool { return len(st) == 1 && st[0].Conn == ConnLive })

	shop.Drop()
	st = wait(t, h, func(st []StackView) bool { return st[0].Conn == ConnDown })
	if st[0].RetryAt.IsZero() {
		t.Error("a down stack says when it retries")
	}
	if err := h.Do(ctx, st[0].Key, control.Verb{Name: control.VerbSleep, Target: "db"}); err != ErrNotResponding {
		t.Errorf("a verb while down = %v, want ErrNotResponding", err)
	}
	// The daemon comes back; the hub's retry finds it without being told.
	shop.Refuse(false)
	wait(t, h, func(st []StackView) bool { return st[0].Conn == ConnLive })
}

func TestStoppedStacksComeFromTheProjectRecord(t *testing.T) {
	home, dial := fakeHome(t, nil)
	dir := filepath.Join(home, "projects", "old-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := projects.Save(dir, projects.Project{Config: "/code/old/doze.hcl", Name: "old", LastDown: time.Unix(100, 0)}); err != nil {
		t.Fatal(err)
	}
	h := New(Options{Home: home, Dial: dial})
	h.Refresh()
	st := h.Stacks()
	if len(st) != 1 || st[0].Running || st[0].Name != "old" || st[0].Dir != "/code/old" {
		t.Fatalf("stacks = %+v", st)
	}
	if err := h.Do(context.Background(), st[0].Key, control.Verb{Name: control.VerbWake}); err != ErrNotRunning {
		t.Errorf("a verb on a stopped stack = %v, want ErrNotRunning", err)
	}
}

func TestAStackWhoseSocketVanishesIsDropped(t *testing.T) {
	shop := &fakedaemon.Fake{
		Snap:   control.Snapshot{Stack: "shop"},
		Frames: []control.Frame{{Topic: control.TopicInstances, Snapshot: true}},
		Hold:   true,
	}
	home, dial := fakeHome(t, map[string]*fakedaemon.Fake{"shop-1": shop})
	h := New(Options{Home: home, Dial: dial, Every: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes := 0
	h.OnChange(func() { changes++ })
	go h.Run(ctx)
	wait(t, h, func(st []StackView) bool { return len(st) == 1 })
	if err := os.Remove(filepath.Join(home, "projects", "shop-1", "run", "doze.admin.sock")); err != nil {
		t.Fatal(err)
	}
	wait(t, h, func(st []StackView) bool { return len(st) == 0 })
	if changes == 0 {
		t.Error("OnChange was never called")
	}
}
