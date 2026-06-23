package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nerdmenot/doze/internal/control"
)

func key(s string) tea.KeyMsg {
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func threeInstances() model {
	fi := textinput.New()
	fi.Prompt = "/"
	return model{
		width: 110, height: 30,
		follow: true,
		filter: fi,
		hist:   map[string]*history{},
		logVP:  viewport.New(40, 8),
		resp: control.Response{
			Listen: "127.0.0.1:6432",
			Instances: []control.InstanceView{
				{Name: "app", Engine: "postgres", State: "active", Conns: 1},
				{Name: "cache", Engine: "valkey", State: "idle"},
				{Name: "media", Engine: "s3", State: "reaped", LastError: "boom"},
			},
		},
	}
}

func send(m model, msg tea.Msg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

func TestCursorNavigationClamps(t *testing.T) {
	m := threeInstances()
	m = send(m, key("j"))
	m = send(m, key("j"))
	m = send(m, key("j")) // clamp at last
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (clamped)", m.cursor)
	}
	m = send(m, key("k"))
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
}

func TestSelectionIsNameSorted(t *testing.T) {
	m := threeInstances()
	// cursor 0 should be the alphabetically-first instance regardless of input order.
	if v, _ := m.selected(); v.Name != "app" {
		t.Fatalf("first selection = %q, want app", v.Name)
	}
	m.cursor = 2
	if v, _ := m.selected(); v.Name != "media" {
		t.Fatalf("last selection = %q, want media", v.Name)
	}
}

func TestFilterToggleAndClear(t *testing.T) {
	m := threeInstances()
	m = send(m, key("/"))
	if !m.filtering {
		t.Fatal("'/' should enter filter mode")
	}
	m = send(m, key("z")) // matches nothing
	if len(m.visible()) != 0 {
		t.Fatalf("filter 'z' should hide all, got %d", len(m.visible()))
	}
	m = send(m, key("esc"))
	if m.filtering {
		t.Fatal("esc should leave filter mode")
	}
	if len(m.visible()) != 3 {
		t.Fatalf("esc should clear the filter, got %d visible", len(m.visible()))
	}
}

func TestViewRendersInstancesAndKeys(t *testing.T) {
	m := threeInstances()
	out := m.View()
	for _, want := range []string{"app", "cache", "media", "boot", "reap", "follow", "doze"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q:\n%s", want, out)
		}
	}
	// The full key set (incl. restart) and the mouse/state legend live in `?` help.
	m.showHelp = true
	help := m.View()
	for _, want := range []string{"restart", "Mouse", "asleep", "cycle theme"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help overlay missing %q:\n%s", want, help)
		}
	}
	m.showHelp = false
	// Selecting the failed instance surfaces its error detail.
	m.cursor = 2
	if !strings.Contains(m.View(), "boom") {
		t.Fatalf("view should surface the selected instance's error:\n%s", m.View())
	}
}
