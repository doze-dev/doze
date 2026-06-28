package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/doze-dev/doze/internal/control"
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

// sqsActs mirrors the sqs engine's published actions.
func sqsActs() []control.ActionView {
	return []control.ActionView{
		{ID: "peek", Label: "Peek", Kind: "queue"},
		{ID: "send", Label: "Send", Kind: "queue", InputHint: "message body"},
		{ID: "purge", Label: "Purge", Kind: "queue", Destructive: true},
		{ID: "redrive", Label: "Redrive", Kind: "queue"},
	}
}

// consoleModel builds a console scoped to a sqs instance with one queue selected.
func consoleModel() model {
	return model{
		width: 110, height: 30,
		cmd:         textinput.New(),
		adminMode:   true,
		adminLoaded: true,
		adminName:   "jobs_sqs",
		adminActs:   sqsActs(),
		adminRes:    []control.ResourceView{{Kind: "queue", Name: "emails", Status: "3 msgs"}, {Kind: "queue", Name: "dlq"}},
		adminCursor: 0,
	}
}

func run(m model, line string) model {
	m.cmd.SetValue(line)
	next, _ := m.runConsoleCommand()
	return next.(model)
}

func lastTx(m model) txBlock { return m.adminTx[len(m.adminTx)-1] }

func TestConsoleCommandParsing(t *testing.T) {
	// A destructive verb stages a y/n confirm rather than running immediately.
	m := run(consoleModel(), "purge")
	if m.adminPending != "purge" {
		t.Fatalf("purge should stage a confirm, got pending=%q", m.adminPending)
	}
	// An input verb with no body errors with a usage hint.
	m = run(consoleModel(), "send")
	if b := lastTx(m); b.kind != txErr || !strings.Contains(b.text, "usage") {
		t.Fatalf("send with no body should error with usage, got %+v", b)
	}
	// An unknown verb errors.
	m = run(consoleModel(), "frobnicate")
	if b := lastTx(m); b.kind != txErr {
		t.Fatalf("unknown verb should error, got %+v", b)
	}
	// A known non-destructive verb with an arg dispatches (no error block; the echo
	// is the last block, the result arrives async).
	m = run(consoleModel(), "peek 5")
	if b := lastTx(m); b.kind != txEcho || b.text != "peek 5" {
		t.Fatalf("peek 5 should echo and dispatch, got %+v", b)
	}
	// `use` switches the active resource.
	m = run(consoleModel(), "use dlq")
	if r, _ := m.selectedResource(); r.Name != "dlq" {
		t.Fatalf("use dlq should select dlq, got %q", r.Name)
	}
}

func TestConsoleHistoryAndCompletion(t *testing.T) {
	m := run(consoleModel(), "peek 3")
	m = run(m, "send hi")
	// ↑ recalls the most recent command.
	m.recallHistory(-1)
	if m.cmd.Value() != "send hi" {
		t.Fatalf("history up = %q, want 'send hi'", m.cmd.Value())
	}
	m.recallHistory(-1)
	if m.cmd.Value() != "peek 3" {
		t.Fatalf("history up×2 = %q, want 'peek 3'", m.cmd.Value())
	}
	// Tab completes a verb prefix.
	m2 := consoleModel()
	m2.cmd.SetValue("pu")
	m2.completeVerb()
	if m2.cmd.Value() != "purge " {
		t.Fatalf("completion of 'pu' = %q, want 'purge '", m2.cmd.Value())
	}
}

func TestCharSelectionText(t *testing.T) {
	m := model{copyCharMode: true, copyLines: []string{"hello world", "second line"}}
	// single line: [0,0)→[0,5) = "hello"
	m.copyAnchor, m.copyAnchorColCh = 0, 0
	m.copyCursor, m.copyColCh = 0, 5
	if got := m.selectedCharText(); got != "hello" {
		t.Fatalf("single-line char select = %q, want hello", got)
	}
	// reversed drag (cursor before anchor) still orders correctly
	m.copyAnchor, m.copyAnchorColCh = 0, 5
	m.copyCursor, m.copyColCh = 0, 0
	if got := m.selectedCharText(); got != "hello" {
		t.Fatalf("reversed char select = %q, want hello", got)
	}
	// multi-line: from (0,6) to (1,6) = "world\nsecond"
	m.copyAnchor, m.copyAnchorColCh = 0, 6
	m.copyCursor, m.copyColCh = 1, 6
	if got := m.selectedCharText(); got != "world\nsecond" {
		t.Fatalf("multi-line char select = %q, want %q", got, "world\nsecond")
	}
}

func TestConsoleRenders(t *testing.T) {
	m := threeInstances()
	m.cursor = 2 // media (s3)
	m.cmd = textinput.New()
	m.adminMode = true
	m.adminLoaded = true
	m.adminName = "media"
	m.adminActs = []control.ActionView{
		{ID: "browse", Label: "Browse", Kind: "bucket"},
		{ID: "empty", Label: "Empty", Kind: "bucket", Destructive: true},
	}
	m.adminRes = []control.ResourceView{{Kind: "bucket", Name: "uploads", Status: "3 objects"}}
	m.adminVP = viewport.New(60, 8)
	m.adminTx = []txBlock{{kind: txInfo, text: "connected to media"}}
	m.adminVP.SetContent(m.renderTranscript(m.adminVP.Width))
	out := m.View()
	// header instance, resource rail, verb hint, and the exit affordance.
	for _, want := range []string{"media", "uploads", "browse", "empty", "esc"} {
		if !strings.Contains(out, want) {
			t.Fatalf("console view missing %q:\n%s", want, out)
		}
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
