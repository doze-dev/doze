package dash

import (
	"fmt"
	"math/rand"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/journal"
)

// The screens meet things the boards never drew: names that run on, sixty
// instances, a log line with no spaces and a thousand cells wide, escape codes
// in the output, wide characters, nothing at all. These tests draw every page of
// such a stack at a spread of window sizes and hold each frame to what a
// terminal needs of it: no panic, no line wider than the window, no more lines
// than it has.

type stackShape struct {
	name  string
	build func() *Model
}

var hostile = []string{
	"INFO " + strings.Repeat("x", 1200),                   // no spaces, far wider than any window
	"INFO \x1b[31mred\x1b[0m and \x1b]0;title\x07 bell\a", // colour, a title-setting OSC, a bell
	"INFO tabs\there\tand\ra carriage return",
	"ERROR 日本語のログ行 — wide characters ✓ 🙂 and a combining é",
	"WARN " + strings.Repeat("ab ", 400), // a long line of words
	"",                                   // an empty line
	"\x00\x01\x02 control bytes \x7f",
	`{"level":"error","msg":"` + strings.Repeat("y", 300) + `"}`,
}

func longName(i int) string {
	return fmt.Sprintf("customer-notifications-email-dispatcher-worker-%02d-with-an-absurdly-long-suffix", i)
}

func stressModel(n int, names func(int) string, logs []string) *Model {
	f := &fakedaemon.Fake{}
	m := New(f, Options{Plain: true, Now: func() time.Time { return epoch }, Loc: time.UTC})
	m.now = epoch
	snap := control.Snapshot{Stack: strings.Repeat("platform-", 6) + "monorepo", Dir: "~/code/" + strings.Repeat("deeply/nested/", 8) + "project", IdleTimeout: 5 * time.Minute}
	kinds := []string{"app", "module", "service", "task"}
	states := []string{"active", "idle", "reaped", "booting", "crashing", "failed", "disabled"}
	for i := 0; i < n; i++ {
		v := control.InstanceView{Name: names(i), Kind: kinds[i%len(kinds)], Engine: "process", State: states[i%len(states)],
			Run: "node --enable-source-maps ./services/" + names(i) + "/dist/main.js --concurrency 16 --queue email,sms,push,webhook", RAM: int64(10+i) << 20,
			CPU: float64(i%40) + 0.5, PID: 1000 + i, Conns: i % 7, StartedAt: epoch.Add(-time.Duration(i+1) * time.Minute)}
		if v.Kind == "module" {
			v.Engine, v.Version = "postgres", "17"
			v.URL = "postgres://app:secret@" + names(i) + ".platform.doze:5432/" + strings.Repeat("database_", 8)
		}
		if v.Kind == "task" {
			v.Task = &control.TaskView{Result: "failed", ExitCode: 3, TookMS: 4120, RanAt: epoch.Add(-time.Hour), Why: strings.Repeat("a long reason ", 12)}
		}
		if v.State == "crashing" || v.State == "failed" {
			v.LastError = "panic: " + strings.Repeat("something went badly wrong ", 20) + "\n\tat " + strings.Repeat("pkg/", 40) + "main.go:1"
			v.RestartCount, v.RestartMax, v.NextRestart = 2, 5, epoch.Add(9*time.Second)
		}
		if i%5 == 0 && i > 0 {
			v.Needs = []string{names((i + 1) % n), names((i + 2) % n)}
		}
		snap.Instances = append(snap.Instances, v)
	}
	m.snap = snap
	if n > 0 {
		m.sel = snap.Instances[0].Name
	}
	// the lines arrive the way they do, so they are cleaned the way they are
	for _, v := range snap.Instances {
		for k, l := range logs {
			m.Update(logMsg{v.Name, control.LogFrame{Instance: v.Name, Line: l, Time: epoch.Add(time.Duration(k-len(logs)) * time.Second), Run: 1}})
		}
	}
	m.events = []journal.Event{{Time: epoch, Kind: journal.Wake, Instance: names(0), Cause: journal.Cause{Kind: journal.ByDependency, By: names(1 % max(n, 1))}}}
	for i := 0; i < 240; i++ {
		for _, v := range snap.Instances {
			m.ram[v.Name] = append(m.ram[v.Name], float64(v.RAM)+float64(i%9<<20))
			m.cpu[v.Name] = append(m.cpu[v.Name], v.CPU+float64(i%5))
			m.conns[v.Name] = append(m.conns[v.Name], float64(v.Conns))
		}
	}
	return m
}

func shapes() []stackShape {
	short := func(i int) string { return fmt.Sprintf("svc%d", i) }
	return []stackShape{
		{"none", func() *Model { return stressModel(0, short, nil) }},
		{"one", func() *Model { return stressModel(1, short, hostile) }},
		{"long names", func() *Model { return stressModel(12, longName, hostile) }},
		{"sixty", func() *Model { return stressModel(60, short, hostile) }},
		{"two hundred long", func() *Model { return stressModel(200, longName, hostile) }},
		{"unicode names", func() *Model {
			return stressModel(9, func(i int) string { return fmt.Sprintf("サービス-%d-🙂-ñandú", i) }, hostile)
		}},
		{"no logs", func() *Model { return stressModel(15, short, nil) }},
	}
}

var sizes = [][2]int{{200, 60}, {150, 40}, {120, 30}, {100, 24}, {80, 24}, {60, 20}, {40, 12}, {24, 8}}

// pages visits each screen of m once, returning a name and a way to draw it.
func pages(m *Model) []struct {
	name string
	set  func(*Model)
} {
	type pg = struct {
		name string
		set  func(*Model)
	}
	out := []pg{{"home", func(m *Model) { m.page = pageHome }}}
	for i, v := range m.snap.Instances {
		if i > 7 && i%11 != 0 {
			continue // a spread of kinds and states, not all two hundred
		}
		name, kind := v.Name, v.Kind
		out = append(out, pg{fmt.Sprintf("focus %s [%s]", name, kind), func(m *Model) {
			m.sel, m.page, m.tab = name, pageFocus, 0
			m.follow()
		}})
	}
	out = append(out,
		pg{"stack logs", func(m *Model) { m.page = pageLogs; m.openLogs() }},
		pg{"palette", func(m *Model) { m.page = pageHome; m.modal = &modal{kind: modalPalette} }},
		pg{"sleep confirm", func(m *Model) {
			m.page = pageHome
			m.modal = &modal{kind: modalSleep, target: m.snap.Instances[0].Name}
		}},
		pg{"reset confirm", func(m *Model) {
			m.page = pageHome
			m.modal = &modal{kind: modalReset, target: m.snap.Instances[0].Name}
		}},
	)
	return out
}

func TestEveryPageHoldsUpAgainstTheStackNobodyDrew(t *testing.T) {
	for _, sh := range shapes() {
		m0 := sh.build()
		if len(m0.snap.Instances) == 0 && sh.name != "none" {
			t.Fatal("empty shape")
		}
		var pgs []struct {
			name string
			set  func(*Model)
		}
		if len(m0.snap.Instances) == 0 {
			pgs = append(pgs, struct {
				name string
				set  func(*Model)
			}{"home", func(m *Model) {}})
		} else {
			pgs = pages(m0)
		}
		for _, pg := range pgs {
			for _, sz := range sizes {
				m := sh.build()
				pg.set(m)
				m.w, m.h = sz[0], sz[1]
				var got string
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Errorf("%s / %s at %dx%d panicked: %v", sh.name, pg.name, sz[0], sz[1], r)
						}
					}()
					got = m.Render(sz[0], sz[1])
				}()
				lines := strings.Split(got, "\n")
				if len(lines) > sz[1] {
					t.Errorf("%s / %s at %dx%d: %d lines for a window %d tall", sh.name, pg.name, sz[0], sz[1], len(lines), sz[1])
				}
				for i, l := range lines {
					if w := ansi.StringWidth(l); w > sz[0] {
						t.Errorf("%s / %s at %dx%d: line %d is %d cells wide: %q", sh.name, pg.name, sz[0], sz[1], i, w, ansi.Strip(l))
						break
					}
					for _, r := range ansi.Strip(l) {
						if r < 0x20 || r == 0x7f {
							t.Errorf("%s / %s at %dx%d: line %d carries control byte %U: %q", sh.name, pg.name, sz[0], sz[1], i, r, ansi.Strip(l))
							break
						}
					}
				}
			}
		}
	}
}

var _ = tea.KeyPressMsg{}

// With many instances the table scrolls inside its share of the window, the
// selected row stays in view, it says what is out of sight, and the logs keep
// room to be read.
func TestManyInstancesLeaveRoomForTheLogs(t *testing.T) {
	short := func(i int) string { return fmt.Sprintf("svc%02d", i) }
	for _, sz := range [][2]int{{150, 40}, {120, 30}, {100, 24}} {
		m := stressModel(60, short, []string{"INFO one", "INFO two", "INFO three", "INFO four", "INFO five", "INFO six", "INFO seven", "INFO eight", "INFO nine", "INFO ten", "INFO eleven", "INFO twelve"})
		// an instance that is not troubled, so its logs are what the strip shows
		m.sel = "svc02"
		for i := range m.snap.Instances {
			if m.snap.Instances[i].Name == "svc02" {
				m.snap.Instances[i].State, m.snap.Instances[i].LastError = "active", ""
			}
		}
		m.follow()
		out := frame(m, sz[0], sz[1])
		if !strings.Contains(out, "svc02") {
			t.Errorf("%dx%d: the selected row is out of sight:\n%s", sz[0], sz[1], out)
		}
		if !strings.Contains(out, "more") || !strings.Contains(out, "filter to narrow") {
			t.Errorf("%dx%d: the table scrolls and does not say so:\n%s", sz[0], sz[1], out)
		}
		logLines := 0
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "INFO ") || strings.Contains(l, "INF ") {
				logLines++
			}
		}
		want := min(8, 12)
		if sz[1] < 30 {
			want = 4
		}
		if logLines < want {
			t.Errorf("%dx%d: only %d log lines are readable, want at least %d:\n%s", sz[0], sz[1], logLines, want, out)
		}
	}
}

// A line with no spaces, a thousand cells wide, ends in an ellipsis at the edge
// of its box and costs one line, not several.
func TestALongLineIsOneLineEndingInAnEllipsis(t *testing.T) {
	m := stressModel(1, func(int) string { return "svc" }, []string{"INFO " + strings.Repeat("x", 1200), "INFO after"})
	m.sel, m.page = "svc", pageFocus
	m.snap.Instances[0].State = "active"
	m.follow()
	out := frame(m, 100, 30)
	longs, after := 0, 0
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "xxxx") {
			longs++
			if !strings.Contains(l, "…") {
				t.Errorf("the long line is cut without a mark:\n%s", l)
			}
		}
		if strings.Contains(l, "INF after") {
			after++
		}
	}
	if longs != 1 || after != 1 {
		t.Errorf("the long line took %d lines and the next appeared %d times:\n%s", longs, after, out)
	}
}

// Wide characters are measured in cells, so a column of them lines up and
// nothing is cut in half.
func TestWideCharactersStayInTheirColumns(t *testing.T) {
	m := stressModel(4, func(i int) string { return fmt.Sprintf("サービス%d", i) }, nil)
	out := frame(m, 120, 30)
	for i, l := range strings.Split(out, "\n") {
		if w := width(l); w > 120 {
			t.Errorf("line %d is %d cells: %q", i, w, l)
		}
		if strings.ContainsRune(l, '�') {
			t.Errorf("line %d has a broken character: %q", i, l)
		}
	}
	if !strings.Contains(out, "サービス0") {
		t.Errorf("the name is gone:\n%s", out)
	}
}

// A line wider than the screen can be read whole: select it, press enter, and it
// is wrapped in a box with nothing cut off; esc closes it.
func TestAWideLineCanBeReadWhole(t *testing.T) {
	long := "INFO " + strings.Repeat("0123456789", 60)
	m := stressModel(1, func(int) string { return "svc" }, []string{"INFO before", long})
	m.sel, m.page = "svc", pageFocus
	m.snap.Instances[0].State = "active"
	m.follow()
	m.w, m.h = 100, 30
	frame(m, 100, 30)
	m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp}) // the newest line is doze's own: one up is the long one
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modal == nil || m.modal.kind != modalText {
		t.Fatalf("enter on a selection should open the line: modal=%v selecting=%v", m.modal, m.selecting)
	}
	out := frame(m, 100, 30)
	digits := 0
	for _, l := range strings.Split(out, "\n") {
		digits += strings.Count(l, "0123456789")
	}
	if digits < 55 {
		t.Errorf("only %d of the 60 runs of digits are readable:\n%s", digits, out)
	}
	if !strings.Contains(out, "whole line") {
		t.Errorf("the box is not named:\n%s", out)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.modal != nil {
		t.Error("esc should close it")
	}
}

// Whatever is pressed, in whatever order, at whatever size, the screen stays a
// screen: no panic, no line wider than the window, no more lines than it has.
func TestMashingKeysAndTheMouseNeverBreaksAFrame(t *testing.T) {
	keys := []tea.KeyPressMsg{
		{Code: tea.KeyUp}, {Code: tea.KeyDown}, {Code: tea.KeyEnter}, {Code: tea.KeyEscape}, {Code: tea.KeyTab},
		{Code: tea.KeyBackspace}, {Code: tea.KeyPgUp}, {Code: tea.KeyPgDown}, {Code: tea.KeyLeft}, {Code: tea.KeyRight},
		{Code: tea.KeyHome}, {Code: tea.KeyEnd}, {Code: ' ', Text: " "},
	}
	for _, r := range "lwsrytovfgG/:[]hjkcpe1234567890abcdxz?- qR" {
		keys = append(keys, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	for _, sh := range shapes() {
		for seed := int64(1); seed <= 4; seed++ {
			rng := rand.New(rand.NewSource(seed*7919 + int64(len(sh.name))))
			m := sh.build()
			w, h := 120, 30
			m.w, m.h = w, h
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s seed %d panicked: %v\n%s", sh.name, seed, r, debug.Stack())
					}
				}()
				for step := 0; step < 250; step++ {
					switch n := rng.Intn(23); {
					case n < 14:
						_, _ = m.Update(keys[rng.Intn(len(keys))])
					case n < 16:
						_, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: rng.Intn(w + 5), Y: rng.Intn(h + 5), Button: tea.MouseLeft}))
					case n < 18:
						b := tea.MouseWheelUp
						if rng.Intn(2) == 0 {
							b = tea.MouseWheelDown
						}
						_, _ = m.Update(tea.MouseWheelMsg(tea.Mouse{X: rng.Intn(w), Y: rng.Intn(h), Button: b}))
					case n < 19:
						_, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{X: rng.Intn(w), Y: rng.Intn(h), Button: tea.MouseLeft}))
					case n == 20:
						_, _ = m.Update(connMsg{up: false, err: fmt.Errorf("gone"), retry: time.Second})
					case n == 21:
						_, _ = m.Update(connMsg{up: true})
					default:
						sz := sizes[rng.Intn(len(sizes))]
						w, h = sz[0], sz[1]
						_, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
					}
					if m.snap.Instances == nil && sh.name != "none" {
						t.Fatalf("%s seed %d step %d: the instances vanished", sh.name, seed, step)
					}
					lines := strings.Split(m.Render(w, h), "\n")
					if len(lines) > h {
						t.Fatalf("%s seed %d step %d: %d lines for %d (page %d, modal %v)", sh.name, seed, step, len(lines), h, m.page, m.modal != nil)
					}
					for i, l := range lines {
						if got := ansi.StringWidth(l); got > w {
							t.Fatalf("%s seed %d step %d: line %d is %d wide in %d (page %d): %q", sh.name, seed, step, i, got, w, m.page, ansi.Strip(l))
						}
					}
				}
			}()
		}
	}
}
