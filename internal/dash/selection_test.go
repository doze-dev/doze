package dash

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/control"
)

// logsOf gives the model's api instance n plain log lines.
func withManyLogs(m *Model, n int) {
	var ls []control.LogFrame
	for i := 0; i < n; i++ {
		ls = append(ls, control.LogFrame{Instance: "api", Line: fmt.Sprintf("INF GET /orders/ord_%04d 200 %dms", 1000+i, 5+i%9), Time: epoch.Add(time.Duration(i) * time.Second), Run: 1})
	}
	m.logs = ls
	m.cache = map[string][]control.LogFrame{"api": ls}
	m.followed = "api"
}

func mpress(x, y int) tea.MouseClickMsg { return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft} }
func mmove(x, y int) tea.MouseMotionMsg { return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft} }
func mlift(x, y int) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// The wheel acts on what is under the pointer and nothing else: over the logs
// it scrolls them; over the list it does nothing, because the keys move through
// the apps.
func TestTheWheelFollowsThePointerOnHome(t *testing.T) {
	for _, sz := range [][2]int{{150, 44}, {190, 54}} {
		m := board()
		withManyLogs(m, 80)
		out := frame(m, sz[0], sz[1])
		ly, lx := rowOf(out, "GET /orders/ord_")
		if ly < 0 {
			t.Fatalf("%v: no logs on screen:\n%s", sz, out)
		}
		sel := m.sel
		m.Update(tea.MouseWheelMsg{X: lx, Y: ly + 1, Button: tea.MouseWheelUp})
		if !m.pinned || m.sel != sel {
			t.Fatalf("%v: wheel over the logs: pinned=%v, selection %q -> %q", sz, m.pinned, sel, m.sel)
		}
		if got := frame(m, sz[0], sz[1]); !strings.Contains(got, "scrolled back") {
			t.Errorf("%v: it does not say it is scrolled back:\n%s", sz, got)
		}
		// over the list the wheel does nothing: the keys move through the apps
		m.Update(tea.MouseWheelMsg{X: 4, Y: m.hitY0 + 2, Button: tea.MouseWheelDown})
		m.Update(tea.MouseWheelMsg{X: 4, Y: m.hitY0 + 2, Button: tea.MouseWheelUp})
		if m.sel != sel || !m.pinned {
			t.Errorf("%v: the wheel over the list changed something: selection %q, pinned=%v", sz, m.sel, m.pinned)
		}
		// the keys do
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		if m.sel == sel {
			t.Errorf("%v: the down key should move the selection", sz)
		}
		// and the scroll is not carried to the next instance's logs
		withManyLogs(m, 80)
		frame(m, sz[0], sz[1])
		if m.pinned {
			t.Errorf("%v: the scroll followed the selection to another instance", sz)
		}
	}
}

func frameWith(t *testing.T) (*Model, int, int, *string) {
	t.Helper()
	var got string
	withClipboard(t, func(s string) (string, error) { got = s; return "pbcopy", nil }, func(string) error { return nil })
	m := focused("api")
	withManyLogs(m, 40)
	out := frame(m, 140, 50)
	y, x := rowOf(out, "ord_1039")
	if y < 0 {
		t.Fatalf("no log line on screen:\n%s", out)
	}
	return m, x, y, &got
}

// A drag selects characters, not lines: what is between the two points.
func TestADragSelectsCharactersNotLines(t *testing.T) {
	m, x, y, got := frameWith(t)
	from := x - len("GET /orders/") // the G of GET
	m.Update(mpress(from, y))
	m.Update(mmove(x+len("ord_1039"), y))
	m.Update(mlift(x+len("ord_1039"), y))
	if *got != "GET /orders/ord_1039" {
		t.Errorf("copied %q", *got)
	}
	if m.selecting || !strings.Contains(m.notice, "characters") {
		t.Errorf("selecting=%v notice=%q", m.selecting, m.notice)
	}
}

// Backwards and across lines: the first line from the press to its end, the last
// from its start to the release.
func TestADragUpAndAcrossLines(t *testing.T) {
	m, x, y, got := frameWith(t)
	m.Update(mpress(x+4, y))  // on the 1 of 1039, the last line
	m.Update(mmove(x+4, y-1)) // up a line: the 1 of 1038
	m.Update(mlift(x+4, y-1))
	parts := strings.Split(*got, "\n")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "1038 200") || !strings.HasSuffix(parts[1], "/orders/ord_") {
		t.Errorf("copied %q", *got)
	}
}

// Double-click is a word, and a path is one word; triple-click is the line.
func TestDoubleClickAPathAndTripleClickALine(t *testing.T) {
	m, x, y, got := frameWith(t)
	at := x + 2 // inside ord_1039
	m.Update(mpress(at, y))
	m.Update(mlift(at, y))
	if *got != "" || m.selecting {
		t.Errorf("a single click copied %q (selecting=%v)", *got, m.selecting)
	}
	m.Update(mpress(at, y))
	m.Update(mlift(at, y))
	if *got != "/orders/ord_1039" {
		t.Errorf("double-click copied %q, want the whole path", *got)
	}
	m.Update(mpress(at, y))
	m.Update(mlift(at, y))
	if !strings.Contains(*got, "INF GET /orders/ord_1039 200") || strings.Contains(*got, "\n") {
		t.Errorf("triple-click copied %q, want the line", *got)
	}
}

func TestWordsAreBoundedBySpacesQuotesAndBrackets(t *testing.T) {
	for _, c := range []struct {
		s    string
		col  int
		want string
	}{
		{"GET /a/b_c.go 200", 6, "/a/b_c.go"},
		{`msg="hello world" ok`, 6, "hello"},
		{"url=http://x.doze:8080/p?q=1 done", 8, "url=http://x.doze:8080/p?q=1"},
		{"(see /tmp/f.txt)", 8, "/tmp/f.txt"},
		{"日本語 log", 2, "日本語"},
		{"a  b", 1, "  "},
	} {
		a, b := wordAt(c.s, c.col)
		if got := ansiCut(c.s, a, b); got != c.want {
			t.Errorf("wordAt(%q, %d) = %q, want %q", c.s, c.col, got, c.want)
		}
	}
}

func ansiCut(s string, a, b int) string { return ansiCutImpl(s, a, b) }

// On an instance's page the wheel scrolls the logs only when the pointer is on
// them, not on the column beside them.
func TestTheWheelScrollsOnlyTheLogsUnderIt(t *testing.T) {
	m := focused("api")
	withManyLogs(m, 80)
	out := frame(m, 140, 40)
	y, x := rowOf(out, "GET /orders/ord_")
	if y < 0 {
		t.Fatalf("no logs:\n%s", out)
	}
	m.Update(tea.MouseWheelMsg{X: 138, Y: y, Button: tea.MouseWheelUp}) // the column beside
	if m.pinned {
		t.Error("the wheel beside the logs scrolled them")
	}
	m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	if !m.pinned {
		t.Error("the wheel over the logs did not scroll them")
	}
}

func TestTheWheelScrollsTheAllLogsPage(t *testing.T) {
	m := board()
	m.page = pageLogs
	for i := 0; i < 120; i++ {
		m.all = append(m.all, control.LogFrame{Instance: "api", Line: fmt.Sprintf("INF line %03d", i), Time: epoch.Add(time.Duration(i) * time.Second)})
	}
	out := frame(m, 120, 30)
	y, x := rowOf(out, "INF line")
	m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	if !m.pinned {
		t.Errorf("the wheel over the all-logs page did not scroll it:\n%s", out)
	}
}
