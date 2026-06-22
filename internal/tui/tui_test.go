package tui

import (
	"strings"
	"testing"

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
	return model{
		width: 100, height: 24,
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

func TestLogsModeReturnsOnEsc(t *testing.T) {
	m := threeInstances()
	m.mode = modeLogs
	m.logs = []string{"line1", "line2"}
	m = send(m, key("esc"))
	if m.mode != modeList {
		t.Fatal("esc in logs mode should return to the list")
	}
}

func TestViewRendersStateAndKeys(t *testing.T) {
	m := threeInstances()
	out := m.View()
	for _, want := range []string{"app", "cache", "media", "boot", "reap", "restart"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q:\n%s", want, out)
		}
	}
	// Selecting the failed instance surfaces its error detail.
	m.cursor = 2
	if !strings.Contains(m.View(), "boom") {
		t.Fatalf("view should surface the selected instance's error:\n%s", m.View())
	}
}
