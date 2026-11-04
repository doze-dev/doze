package notify

import (
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
)

var epoch = time.Date(2026, 10, 11, 14, 4, 40, 0, time.UTC)

func stack(vs ...control.InstanceView) []hub.StackView {
	return []hub.StackView{{Key: "shop-1", Name: "shop", Dir: "~/code/shop", Instances: vs}}
}

func hidden(string, string) bool { return false }
func shown(string, string) bool  { return true }

func TestACrashIsSaidOncePerEpisodeAndNeverWhileOnScreen(t *testing.T) {
	r := New()
	fine := control.InstanceView{Name: "api", Kind: "app", State: "active"}
	crash := control.InstanceView{Name: "api", Kind: "app", State: "crashing", LastError: "panic: STRIPE_KEY is required\n  main.loadConfig", RestartCount: 3, RestartMax: 5, NextRestart: epoch.Add(4 * time.Second)}
	// The first look says nothing: what is already so is on screen, not news.
	if n := r.Next(stack(crash), hidden, epoch); len(n) != 0 {
		t.Fatalf("first look said %+v", n)
	}
	if n := r.Next(stack(fine), hidden, epoch); len(n) != 0 {
		t.Fatalf("recovery said %+v", n)
	}
	n := r.Next(stack(crash), hidden, epoch)
	if len(n) != 1 || n[0].Title != "api crashed · shop" || n[0].Body != "panic: STRIPE_KEY is required" || n[0].Sub != "try 3 of 5 · next in 4s" || n[0].ID != "shop-1/api/crash" {
		t.Fatalf("crash note = %+v", n)
	}
	// Still crashing, still the same episode: nothing more.
	if n := r.Next(stack(crash), hidden, epoch.Add(time.Second)); len(n) != 0 {
		t.Fatalf("second look said %+v", n)
	}
	// Up again, then down again: a new episode.
	r.Next(stack(fine), hidden, epoch)
	if n := r.Next(stack(crash), hidden, epoch); len(n) != 1 {
		t.Fatalf("new episode said %+v", n)
	}
	// On screen: seen, but not said; and not said later either.
	r.Next(stack(fine), hidden, epoch)
	if n := r.Next(stack(crash), shown, epoch); len(n) != 0 {
		t.Fatalf("on screen said %+v", n)
	}
	if n := r.Next(stack(crash), hidden, epoch); len(n) != 0 {
		t.Fatalf("after looking away said %+v", n)
	}
	// Gave up: a failed instance says so.
	r.Next(stack(fine), hidden, epoch)
	gaveUp := control.InstanceView{Name: "api", Kind: "app", State: "failed", LastError: "exit status 2"}
	if n := r.Next(stack(gaveUp), hidden, epoch); len(n) != 1 || n[0].Title != "api failed · shop" || n[0].Sub != "it stopped and waits for you" {
		t.Fatalf("failed note = %+v", n)
	}
}

func TestAFailedTaskIsSaidOncePerRun(t *testing.T) {
	r := New()
	ok := control.InstanceView{Name: "migrate", Kind: "task", State: "done", Task: &control.TaskView{Result: "done", RanAt: epoch.Add(-time.Hour), TookMS: 1200}}
	failed := control.InstanceView{Name: "migrate", Kind: "task", State: "failed", Task: &control.TaskView{Result: "failed", ExitCode: 1, RanAt: epoch, TookMS: 400}}
	if n := r.Next(stack(failed), hidden, epoch); len(n) != 0 {
		t.Fatalf("first look said %+v", n)
	}
	r.Next(stack(ok), hidden, epoch)
	n := r.Next(stack(failed), hidden, epoch)
	if len(n) != 1 || n[0].Title != "migrate failed (exit 1) after 400ms · shop" || n[0].Instance != "migrate" {
		t.Fatalf("task note = %+v", n)
	}
	if n := r.Next(stack(failed), hidden, epoch); len(n) != 0 {
		t.Fatalf("same run said again %+v", n)
	}
	again := failed
	again.Task = &control.TaskView{Result: "failed", ExitCode: 1, RanAt: epoch.Add(time.Minute), TookMS: 400}
	if n := r.Next(stack(again), hidden, epoch); len(n) != 1 {
		t.Fatalf("a new failed run said %+v", n)
	}
	done := failed
	done.Task = &control.TaskView{Result: "done", RanAt: epoch.Add(2 * time.Minute), TookMS: 900}
	if n := r.Next(stack(done), hidden, epoch); len(n) != 0 {
		t.Fatalf("a run that went well said %+v", n)
	}
}
