package hub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/control/fixture"
)

var epoch = time.Date(2026, 10, 11, 14, 4, 40, 0, time.UTC)

func TestStatsAndLogsStreamOnlyWhileWatched(t *testing.T) {
	shop := fixture.Shop(epoch)
	home, dial := fakeHome(t, map[string]*fakedaemon.Fake{"shop-1": shop})
	h := New(Options{Home: home, Dial: dial, Every: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)
	st := wait(t, h, func(st []StackView) bool { return len(st) == 1 && st[0].Conn == ConnLive })
	key := st[0].Key
	if len(st[0].Series) != 0 || len(h.Logs(key, "api")) != 0 {
		t.Fatalf("nothing watched, yet series=%d logs=%d", len(st[0].Series), len(h.Logs(key, "api")))
	}

	release := h.Watch(key, control.TopicStats)
	st = wait(t, h, func(st []StackView) bool { return len(st[0].Series["api"].RAM) == samplesKept })
	api := st[0].Series["api"]
	if len(api.CPU) != samplesKept || len(api.Conns) != samplesKept {
		t.Errorf("api series lengths: ram %d cpu %d conns %d", len(api.RAM), len(api.CPU), len(api.Conns))
	}
	if _, ok := st[0].Series["mongo"]; ok {
		t.Error("mongo reports no meters; it has no series")
	}
	// The instance rows carry the latest reading too.
	for _, v := range st[0].Instances {
		if v.Name == "api" && v.RAM != int64(api.RAM[len(api.RAM)-1]) {
			t.Errorf("api row RAM %d, series end %v", v.RAM, api.RAM[len(api.RAM)-1])
		}
	}

	stopLogs := h.Watch(key, control.TopicLogs)
	wait(t, h, func([]StackView) bool { return len(h.Logs(key, "api")) == 10 })
	if all := h.Logs(key, ""); len(all) != 23 {
		t.Errorf("all logs = %d lines, want 23", len(all))
	}
	// A reconnect replays the backlog; nothing is doubled.
	shop.Drop()
	wait(t, h, func(st []StackView) bool { return st[0].Conn == ConnDown })
	shop.Refuse(false)
	wait(t, h, func(st []StackView) bool {
		return st[0].Conn == ConnLive && len(st[0].Series["api"].RAM) == samplesKept
	})
	time.Sleep(50 * time.Millisecond)
	if n := len(h.Logs(key, "api")); n != 10 {
		t.Errorf("after a reconnect api has %d lines, want 10", n)
	}
	release()
	stopLogs()
}

func TestAWatchOutlivesTheStackItWaitsFor(t *testing.T) {
	// A page watching a stack that is not running yet, or whose daemon
	// restarts, gets its stats once the daemon is there.
	shop := fixture.Shop(epoch)
	home, dial := fakeHome(t, map[string]*fakedaemon.Fake{"shop-1": shop})
	sock := filepath.Join(home, "projects", "shop-1", "run", "doze.admin.sock")
	if err := os.Remove(sock); err != nil {
		t.Fatal(err)
	}
	h := New(Options{Home: home, Dial: dial, Every: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)
	key := filepath.Join(home, "projects", "shop-1")
	release := h.Watch(key, control.TopicStats)
	defer release()
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(sock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	wait(t, h, func(st []StackView) bool { return len(st) == 1 && len(st[0].Series["api"].RAM) == samplesKept })
	if h.LogsVersion(key, "api") != 0 {
		t.Error("logs were not watched, yet lines were counted")
	}
}

func TestLinesRingDedupsAndCaps(t *testing.T) {
	var l lines
	at := epoch
	for i := 0; i < 5; i++ {
		l, _ = l.add(control.LogFrame{Instance: "a", Line: "x", Time: at.Add(time.Duration(i) * time.Second), Run: 1}, 3)
	}
	if len(l) != 3 || !l[0].Time.Equal(at.Add(2*time.Second)) {
		t.Errorf("ring = %+v", l)
	}
	l, added := l.add(control.LogFrame{Instance: "a", Line: "x", Time: at.Add(4 * time.Second), Run: 1}, 3)
	if len(l) != 3 || added {
		t.Error("a line already held was added again")
	}
}

func TestSeriesSkipsAReadingAlreadyHeld(t *testing.T) {
	var s Series
	s.push(control.StatView{RAM: 1}, epoch)
	s.push(control.StatView{RAM: 2}, epoch)
	s.push(control.StatView{RAM: 3}, epoch.Add(time.Second))
	if len(s.RAM) != 2 || s.RAM[1] != 3 {
		t.Errorf("series = %+v", s.RAM)
	}
}
