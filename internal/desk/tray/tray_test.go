package tray

import (
	"testing"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
)

func TestBuild(t *testing.T) {
	stacks := []hub.StackView{
		{Key: "k-shop", Name: "shop", Dir: "/code/shop", Running: true, Conn: hub.ConnLive, Instances: []control.InstanceView{
			{Name: "api", Kind: "app", State: "active", RAM: 48 << 20},
			{Name: "db", Kind: "module", State: "idle", RAM: 90 << 20},
			{Name: "migrate", Kind: "task", State: "done"},
			{Name: "worker", Kind: "app", State: "crashing"},
			{Name: "cache", Kind: "module", State: "reaped"},
		}},
		{Key: "k-data", Name: "data", Dir: "/code/data", Running: true, Conn: hub.ConnDown},
		{Key: "k-old", Name: "old", Dir: "/code/old"},
	}
	m := Build(stacks)
	if m.Label != "2" {
		t.Errorf("label = %q, want 2 (api and db are awake; a task is not counted)", m.Label)
	}
	if !m.NeedsYou {
		t.Error("a crashing app turns the red dot on")
	}
	if len(m.Sections) != 2 || m.Sections[0].Title != "data · daemon not responding" || m.Sections[1].Title != "shop · 2 awake" {
		t.Errorf("sections = %+v", m.Sections)
	}
	shop := m.Sections[1]
	want := []string{
		"● api  awake  48 MB",
		"· cache  asleep",
		"◐ db  dozing  90 MB",
		"✗ worker  crashing",
	}
	if len(shop.Rows) != len(want) {
		t.Fatalf("rows = %+v", shop.Rows)
	}
	for i, r := range shop.Rows {
		if r.Label != want[i] {
			t.Errorf("row %d = %q, want %q", i, r.Label, want[i])
		}
	}
	if !shop.Rows[0].Awake || shop.Rows[1].Awake || !shop.Rows[3].Trouble {
		t.Errorf("row flags = %+v", shop.Rows)
	}
	if len(m.Stopped) != 1 || m.Stopped[0].Name != "old" {
		t.Errorf("stopped = %+v", m.Stopped)
	}
	if m.Tooltip != "doze · 2 awake" {
		t.Errorf("tooltip = %q", m.Tooltip)
	}
}

func TestBuildWithNothingRunning(t *testing.T) {
	m := Build(nil)
	if m.Label != "" || m.NeedsYou || len(m.Sections) != 0 || m.Tooltip != "doze" {
		t.Errorf("empty model = %+v", m)
	}
}
