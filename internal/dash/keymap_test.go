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

// A failure stays through other keys, goes on esc, and is kept to be read again.
func TestAFailureStaysUntilEscOrTime(t *testing.T) {
	m := board()
	frame(m, 120, 30)
	m.Update(doneMsg{verb: "wake", target: "db", err: fmt.Errorf("dial unix /x/y.sock: connect: connection refused")})
	if !m.noticeBad || !strings.Contains(m.notice, "wake db failed: the daemon is not reachable") {
		t.Fatalf("notice = %q bad=%v", m.notice, m.noticeBad)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.notice == "" {
		t.Fatal("a failure should survive other keys")
	}
	if out := frame(m, 120, 30); !strings.Contains(out, "wake db failed") {
		t.Errorf("not on screen:\n%s", out)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.notice != "" {
		t.Error("esc should dismiss it")
	}
	m.Update(failMsg("restart api failed: boom"))
	later := m.now.Add(13 * time.Second)
	m.nowf = func() time.Time { return later }
	m.Update(tickMsg(later))
	if m.notice != "" {
		t.Error("a failure should fade after 12s")
	}
	if len(m.history) != 2 || !m.history[1].bad {
		t.Errorf("history = %+v", m.history)
	}
}

func TestASuccessFadesAndIsKept(t *testing.T) {
	m := board()
	m.say("sent 3 lines to the clipboard")
	later := m.now.Add(5 * time.Second)
	m.nowf = func() time.Time { return later }
	m.Update(tickMsg(later))
	if m.notice != "" {
		t.Error("a success should fade after 4s")
	}
	m.recentMessages()
	if m.modal == nil || !strings.Contains(strings.Join(m.modal.lines, "\n"), "sent 3 lines") {
		t.Errorf("recent messages: %+v", m.modal)
	}
}

func TestALongMessageNeverWidensTheBar(t *testing.T) {
	m := board()
	m.fail(strings.Repeat("something went badly wrong ", 12))
	for _, w := range []int{120, 80, 50} {
		if got := width(m.bar(m.homeHints(), w, m.noticeText())); got > w {
			t.Errorf("at %d the bar is %d wide", w, got)
		}
	}
}

func TestTooSmallSaysSoAndKeysStillWork(t *testing.T) {
	m := board()
	for _, sz := range [][2]int{{59, 30}, {120, 15}, {24, 8}, {5, 3}} {
		out := stripAnsi(m.Render(sz[0], sz[1]))
		lines := strings.Split(out, "\n")
		if len(lines) > sz[1] {
			t.Errorf("%v: %d lines", sz, len(lines))
		}
		for _, l := range lines {
			if width(l) > sz[0] {
				t.Errorf("%v: line %q is %d wide", sz, l, width(l))
			}
		}
		if sz[0] >= 24 && !strings.Contains(out, "larger") {
			t.Errorf("%v: no message:\n%s", sz, out)
		}
	}
	if out := stripAnsi(m.Render(60, 16)); strings.Contains(out, "make the window larger") {
		t.Error("60x16 is big enough")
	}
	sel := m.sel
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.sel == sel {
		t.Error("keys should still move the selection")
	}
}

func TestTheMouseHintIsSaidOnce(t *testing.T) {
	m := board()
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if !strings.Contains(m.notice, "shift-drag") {
		t.Fatalf("notice = %q", m.notice)
	}
	m.clearNotice()
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if m.notice != "" {
		t.Errorf("said again: %q", m.notice)
	}
}

func TestTheTopOfTheLogsIsMarked(t *testing.T) {
	m := focused("api")
	m.followed = "api"
	for i := 0; i < 80; i++ {
		m.logs = append(m.logs, control.LogFrame{Instance: "api", Line: fmt.Sprintf("INFO l%d", i), Time: epoch.Add(time.Duration(i) * time.Second), Run: 1})
	}
	m.cache = map[string][]control.LogFrame{"api": m.logs}
	frame(m, 140, 40)
	m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	if out := frame(m, 140, 40); !strings.Contains(out, "older: doze logs api") {
		t.Errorf("top of the logs not marked:\n%s", out)
	}
}

// Sorted by memory the order holds for a few seconds after a key, so a click
// lands on the row that was seen; then it follows the numbers again.
func TestSortByMemoryHoldsStillAfterAKey(t *testing.T) {
	m := board()
	m.sortBy = sortMem
	names := func() string {
		var out []string
		for _, r := range m.ordered() {
			out = append(out, r.v.Name)
		}
		return strings.Join(out, ",")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	before := names()
	for i := range m.snap.Instances {
		m.snap.Instances[i].RAM = int64(len(m.snap.Instances)-i) << 20 // the reverse of any order it had
	}
	if names() != before {
		t.Fatalf("the order moved under the cursor:\n%s\n%s", before, names())
	}
	later := m.now.Add(holdFor + time.Second)
	m.nowf = func() time.Time { return later }
	m.Update(tickMsg(later))
	if names() == before {
		t.Error("the order should follow the numbers again once the hold is over")
	}
}

// An instance added while the dash is open has its logs asked for.
func TestANewInstanceGetsItsLogs(t *testing.T) {
	m := board()
	asked := 0
	m.resubLogs = func() { asked++ }
	m.known = nil
	m.apply(control.Frame{Topic: control.TopicInstances, Snapshot: true, Instances: m.snap.Instances})
	if asked != 0 {
		t.Fatal("the first set is the baseline")
	}
	m.apply(control.Frame{Topic: control.TopicInstances, Snapshot: true, Instances: m.snap.Instances})
	if asked != 0 {
		t.Fatal("nothing new")
	}
	added := append(append([]control.InstanceView(nil), m.snap.Instances...), control.InstanceView{Name: "fresh", Kind: "app", State: "active"})
	m.apply(control.Frame{Topic: control.TopicInstances, Snapshot: true, Instances: added})
	if asked != 1 {
		t.Errorf("asked %d times for a new instance", asked)
	}
}

func TestTheTotalSaysItIsAnEstimate(t *testing.T) {
	out := stripAnsi(frame(board(), 150, 40))
	if !strings.Contains(out, "≈ ") || !strings.Contains(out, "resident") {
		t.Errorf("header:\n%s", out)
	}
}

// The clock on log lines says which zone it is in, and not when it is a duration.
func TestLogTimesNameTheirZone(t *testing.T) {
	m := focused("api")
	m.loc = time.FixedZone("IST", 5*3600+1800)
	m.followed = "api"
	m.logs = []control.LogFrame{{Instance: "api", Line: "INFO hi", Time: epoch, Run: 1}}
	m.cache = map[string][]control.LogFrame{"api": m.logs}
	m.page = pageFocus
	for i, tb := range m.tabs(func() control.InstanceView { v, _ := m.selected(); return v }()) {
		if tb.id == tabLogs {
			m.tab = i
		}
	}
	if out := stripAnsi(frame(m, 140, 40)); !strings.Contains(out, "· IST") {
		t.Errorf("focus logs do not name the zone:\n%s", out)
	}
	m.timeRel = true
	if out := stripAnsi(frame(m, 140, 40)); strings.Contains(out, "· IST") {
		t.Error("a duration has no zone")
	}
	m = board()
	m.loc = time.FixedZone("CEST", 7200)
	m.page = pageLogs
	m.openLogs()
	if out := stripAnsi(frame(m, 140, 40)); !strings.Contains(out, "kept · CEST") {
		t.Errorf("stack logs do not name the zone:\n%s", out)
	}
}

// Asking for a task to run is answered at once; while it runs the row says
// running, not waking; and the end says how it went.
func TestRerunningATaskSaysSoThroughout(t *testing.T) {
	m := board()
	m.sel = "migrate"
	frame(m, 150, 40)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil || !strings.Contains(m.notice, "running migrate") {
		t.Fatalf("no answer at once: cmd=%v notice=%q", cmd != nil, m.notice)
	}
	// the daemon marks it booting while it runs
	for i := range m.snap.Instances {
		if m.snap.Instances[i].Name == "migrate" {
			m.snap.Instances[i].State = "booting"
			m.snap.Instances[i].StartedAt = m.now.Add(-3 * time.Second)
		}
	}
	out := stripAnsi(frame(m, 150, 40))
	if !strings.Contains(out, "running") || !strings.Contains(out, "running now") || !strings.Contains(out, "1 running") {
		t.Errorf("a running task should say so in the row, BECAUSE and the header:\n%s", out)
	}
	if strings.Contains(out, "migrate") && strings.Contains(strings.Split(out, "migrate")[1], "waking") {
		t.Error("a task is not waking")
	}
	// still running after the message's time: it stays
	later := m.now.Add(10 * time.Second)
	m.nowf = func() time.Time { return later }
	m.Update(tickMsg(later))
	if !strings.Contains(m.notice, "running migrate") {
		t.Error("the message should stay while it runs")
	}
	// it finishes: the snapshot carries the record
	m.Update(doneMsg{verb: control.VerbTask, target: "migrate"})
	snap := m.snap
	snap.Instances = append([]control.InstanceView(nil), snap.Instances...)
	for i := range snap.Instances {
		if snap.Instances[i].Name == "migrate" {
			snap.Instances[i].State = "idle"
			snap.Instances[i].Task = &control.TaskView{Result: "done", TookMS: 1234, RanAt: later}
		}
	}
	m.Update(snapshotMsg{snap: snap})
	if !strings.Contains(m.notice, "migrate done in 1.2s") || m.noticeBad {
		t.Errorf("notice = %q", m.notice)
	}
	// and a failure is red
	m.Update(doneMsg{verb: control.VerbTask, target: "migrate"})
	snap.Instances = append([]control.InstanceView(nil), snap.Instances...)
	for i := range snap.Instances {
		if snap.Instances[i].Name == "migrate" {
			snap.Instances[i].Task = &control.TaskView{Result: "failed", ExitCode: 2, TookMS: 800, RanAt: later}
		}
	}
	m.Update(snapshotMsg{snap: snap})
	if !m.noticeBad || !strings.Contains(m.notice, "migrate failed (exit 2) after 800ms") {
		t.Errorf("notice = %q bad=%v", m.notice, m.noticeBad)
	}
}

// The meters say what is consumed and by whom, and never "of" something that is
// only a guess.
func TestMetersNameWhoConsumesAndSayOfNothing(t *testing.T) {
	m := board()
	out := frame(m, 150, 40)
	if strings.Contains(out, "of ~") {
		t.Errorf("an estimated ceiling is back:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	var mem, memKey, cpu string
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "mem"):
			mem, memKey = l, lines[i+1]
		case strings.HasPrefix(l, "cpu"):
			cpu = l
		}
	}
	for _, want := range []string{"web 212", "mongo 148"} {
		if !strings.Contains(memKey, want) {
			t.Errorf("the key under mem lacks %q: %q", want, memKey)
		}
	}
	if !strings.Contains(mem, "MB") || !strings.Contains(cpu, "cores") && !strings.Contains(cpu, "core") {
		t.Errorf("totals: %q / %q", mem, cpu)
	}
	// the bar is the whole of what is consumed: it is full
	bar := func(l string) string { return l[strings.Index(l, "[")+1 : strings.Index(l, "]")] }
	if b := bar(mem); b == "" || strings.Contains(b, " ") { // plain: runs differ by character
		t.Errorf("the memory bar is not full: %q", b)
	}
	// short windows have the bars without the key, so the table keeps its rows
	if short := frame(m, 150, 30); strings.Contains(short, "web 212") {
		t.Errorf("a 30-row window has no room for the key")
	}
}

func TestCoresSaysWhatTheNumberIs(t *testing.T) {
	for pct, want := range map[float64]string{0: "0 cores", 0.1: "<0.01 cores", 22.6: "0.23 cores", 100: "1 core", 250: "2.5 cores", 1020: "10.2 cores"} {
		if got := cores(pct); got != want {
			t.Errorf("cores(%v) = %q, want %q", pct, got, want)
		}
	}
}
