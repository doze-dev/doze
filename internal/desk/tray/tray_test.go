package tray

import (
	"fmt"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
)

var now = time.Date(2026, 11, 5, 14, 5, 0, 0, time.UTC)

func TestBuild(t *testing.T) {
	stacks := []hub.StackView{
		{Key: "k-shop", Name: "shop", Dir: "/code/shop", Running: true, Conn: hub.ConnLive, Instances: []control.InstanceView{
			{Name: "api", Kind: "app", State: "active", RAM: 48 << 20},
			{Name: "web", Kind: "app", State: "active", RAM: 212 << 20, HTTPS: "https://shop.acme.dev"},
			{Name: "db", Kind: "module", State: "idle", RAM: 90 << 20},
			{Name: "migrate", Kind: "task", State: "done"},
			{Name: "cache", Kind: "module", State: "reaped"},
		}},
		{Key: "k-data", Name: "data", Dir: "/code/data", Running: true, Conn: hub.ConnLive, Instances: []control.InstanceView{
			{Name: "search", Kind: "service", State: "crashing", RestartCount: 2, RestartMax: 5},
		}},
		{Key: "k-old", Name: "old", Dir: "/code/old", LastUp: now.Add(-14 * 24 * time.Hour)},
		{Key: "k-acme", Name: "acme", Dir: "/code/acme", LastUp: now.Add(-3 * 24 * time.Hour)},
	}
	m := Build(stacks, now)
	if !m.NeedsYou {
		t.Error("a crashing service turns the red dot on")
	}
	want := []Line{
		{Key: "k-data", Name: "data", Dir: "/code/data", Running: true, Sub: "search is crashing · try 2 of 5", Trouble: true},
		{Key: "k-shop", Name: "shop", Dir: "/code/shop", Running: true, Sub: "2 awake · 1 dozing · 350 MB"},
		{Key: "k-acme", Name: "acme", Dir: "/code/acme", Sub: "up 3d ago"},
		{Key: "k-old", Name: "old", Dir: "/code/old", Sub: "up 14d ago"},
	}
	if len(m.Lines) != len(want) {
		t.Fatalf("lines = %+v", m.Lines)
	}
	for i, l := range m.Lines {
		if l != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, l, want[i])
		}
	}
	if m.Tooltip != "doze · 2 awake" {
		t.Errorf("tooltip = %q", m.Tooltip)
	}
}

func TestBuildWithNothingRunning(t *testing.T) {
	m := Build(nil, now)
	if m.NeedsYou || len(m.Lines) != 0 || m.Tooltip != "doze" {
		t.Errorf("empty model = %+v", m)
	}
	m = Build([]hub.StackView{{Key: "k", Name: "shop", Running: true, Conn: hub.ConnDown}}, now)
	if len(m.Lines) != 1 || m.Lines[0].Sub != "daemon not answering" {
		t.Errorf("down daemon = %+v", m.Lines)
	}
}

func TestSplit(t *testing.T) {
	var lines []Line
	for i := 0; i < 10; i++ {
		lines = append(lines, Line{Name: fmt.Sprintf("up%d", i), Running: true, Trouble: i == 9})
	}
	for i := 0; i < 7; i++ {
		lines = append(lines, Line{Name: fmt.Sprintf("down%d", i)})
	}
	shown, more := Split(lines)
	if len(shown) != 8+1+5 || len(more) != 1+2 {
		t.Fatalf("shown %d, more %d", len(shown), len(more))
	}
	if more[0].Name != "up8" || more[1].Name != "down5" {
		t.Errorf("more = %v", more)
	}
	for _, l := range more {
		if l.Trouble {
			t.Errorf("a stack in trouble went under More: %v", l)
		}
	}
	if s, m := Split(lines[:3]); len(s) != 3 || len(m) != 0 {
		t.Errorf("a short list is all shown: %d, %d", len(s), len(m))
	}
}
