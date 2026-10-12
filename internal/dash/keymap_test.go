package dash

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/control"
)

func press(m *Model, display string) (tea.Cmd, bool) {
	var msg tea.KeyPressMsg
	switch display {
	case "↑↓":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "⏎":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab}
	case "space":
		msg = tea.KeyPressMsg{Code: ' ', Text: " "}
	case "[ ]":
		msg = tea.KeyPressMsg{Code: '[', Text: "["}
	default:
		r := []rune(display)
		if len(r) != 1 {
			return nil, false
		}
		msg = tea.KeyPressMsg{Code: r[0], Text: display}
	}
	_, cmd := m.Update(msg)
	return cmd, true
}

func stateOf(m *Model) string {
	return fmt.Sprint(m.page, m.modal != nil, m.sel, m.sortBy, m.tree, m.filtering, m.searching, m.selecting, m.paused, m.timeRel, m.level, m.tab, m.pinned, m.notice, m.runSel)
}

// Every key the bar offers does something: a key in the table that nothing
// handles is a label that promises what the page does not do.
func TestEveryKeyOfTheTableIsHandled(t *testing.T) {
	type setup struct {
		name string
		mk   func() *Model
	}
	for _, c := range []setup{
		{"home", func() *Model { return board() }},
		{"focus app logs", func() *Model {
			m := focused("api")
			m.followed = "api"
			for i := 1; i <= 60; i++ {
				m.logs = append(m.logs, control.LogFrame{Instance: "api", Line: fmt.Sprintf("INFO l%d", i), Time: epoch, Run: 1})
				m.logs[len(m.logs)-1].Run = 1 + i/30
			}
			m.cache = map[string][]control.LogFrame{"api": m.logs}
			frame(m, 140, 40)
			return m
		}},
		{"focus module", func() *Model { return focused("db") }},
		{"stack logs", func() *Model {
			m := board()
			m.page = pageLogs
			m.openLogs()
			for i := 0; i < 80; i++ {
				m.all = append(m.all, control.LogFrame{Instance: "api", Line: fmt.Sprintf("INFO line %d", i), Time: epoch.Add(time.Duration(i) * time.Second)})
			}
			frame(m, 140, 40)
			return m
		}},
	} {
		hs := c.mk().pageHints()
		if len(hs) == 0 {
			t.Fatalf("%s: no keys", c.name)
		}
		for _, h := range hs {
			m := c.mk()
			m.w, m.h = 140, 40
			before := stateOf(m)
			cmd, ok := press(m, h.key)
			if !ok {
				continue // a key the table draws as a pair, such as ↑↓
			}
			if h.key == "↑↓" && cmd == nil && stateOf(m) == before {
				m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // at the top, up has nowhere to go
			}
			if h.key == "q" || h.key == "esc" && c.name == "home" {
				continue // leaves
			}
			if cmd == nil && stateOf(m) == before {
				t.Errorf("%s: %q (%s) is in the bar and does nothing", c.name, h.key, h.label)
			}
		}
	}
}

// The bar and the overlay are one table: every key and label of the page is in
// both, and the overlay explains it.
func TestTheBarAndTheHelpAgree(t *testing.T) {
	for _, mk := range []func() *Model{
		func() *Model { return board() },
		func() *Model { return focused("api") },
		func() *Model { m := board(); m.page = pageLogs; return m },
	} {
		m := mk()
		help := strings.Join(m.helpLines(100), "\n")
		bar := m.bar(m.pageHints(), 400, "")
		for _, h := range m.pageHints() {
			if !strings.Contains(ansiStrip(bar), h.key) || !strings.Contains(ansiStrip(bar), h.label) {
				t.Errorf("page %d: %q %q missing from the bar", m.page, h.key, h.label)
			}
			if !strings.Contains(ansiStrip(help), h.label) || !strings.Contains(ansiStrip(help), h.help[:min(len(h.help), 20)]) {
				t.Errorf("page %d: %q missing from the help", m.page, h.label)
			}
		}
	}
}

// A narrow bar drops the least wanted and never the way to help.
func TestANarrowBarKeepsHelpAndTheWayBack(t *testing.T) {
	m := focused("api")
	for _, w := range []int{120, 80, 60, 40, 30} {
		bar := ansiStrip(m.bar(m.pageHints(), w, ""))
		if !strings.Contains(bar, "?") || !strings.Contains(bar, "esc") {
			t.Errorf("at %d the bar is %q", w, bar)
		}
		if width(bar) > w && w >= 30 {
			t.Errorf("at %d the bar is %d wide", w, width(bar))
		}
	}
}

// l is this instance's logs, everywhere; L is the whole stack's.
func TestLIsThisInstanceAndCapitalLIsEveryInstance(t *testing.T) {
	m := board()
	m.sel = "db"
	m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if m.page != pageFocus || m.sel != "db" {
		t.Fatalf("l: page=%d sel=%s", m.page, m.sel)
	}
	ts := m.tabs(func() control.InstanceView { v, _ := m.selected(); return v }())
	if ts[m.tab].id != tabLogs {
		t.Errorf("l should land on the logs tab, got %q", ts[m.tab].id)
	}
	m = board()
	m.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	if m.page != pageLogs {
		t.Errorf("L: page=%d", m.page)
	}
}

// ? opens the help on every page and esc closes it.
func TestQuestionMarkOpensHelp(t *testing.T) {
	for name, mk := range map[string]func() *Model{
		"home": func() *Model { return board() }, "focus": func() *Model { return focused("api") },
		"logs": func() *Model { m := board(); m.page = pageLogs; return m },
	} {
		m := mk()
		m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
		if m.modal == nil || m.modal.kind != modalHelp {
			t.Errorf("%s: ? did not open the help", name)
			continue
		}
		out := frame(m, 120, 40)
		if !strings.Contains(out, "keys") || !strings.Contains(out, "Everywhere") || !strings.Contains(out, "What the meters are") {
			t.Errorf("%s: the overlay:\n%s", name, out)
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if m.modal != nil {
			t.Errorf("%s: esc did not close it", name)
		}
	}
}

func ansiStrip(s string) string { return stripAnsi(s) }
