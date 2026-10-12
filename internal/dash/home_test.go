package dash

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/journal"
)

var epoch = time.Date(2026, 10, 11, 14, 4, 40, 0, time.UTC)

// board is the data of Main.dc.html: the shop stack, api selected.
func board() *Model {
	m := New(&fakedaemon.Fake{}, Options{Plain: true, Now: func() time.Time { return epoch }, Loc: time.UTC})
	m.now = epoch
	m.snap = control.Snapshot{Stack: "shop", Dir: homeDir() + "/code/shop", IdleTimeout: 5 * time.Minute, Instances: []control.InstanceView{
		{Name: "api", Kind: "app", Engine: "process", State: "active", Run: "go run ./cmd/api", RAM: 84 << 20, CPU: 2.1, PID: 48211,
			StartedAt: epoch.Add(-14 * time.Minute), Domain: "api.shop.doze", Bind: "127.0.0.1:8080", Endpoint: "127.0.0.1:8080",
			Needs: []string{"db", "mongo", "cache", "migrate"}},
		{Name: "web", Kind: "app", Engine: "process", State: "active", Run: "pnpm dev", RAM: 212 << 20},
		{Name: "worker", Kind: "app", Engine: "process", State: "booting", Needs: []string{"events"}},
		{Name: "db", Kind: "module", Engine: "postgres", Version: "17", State: "active", RAM: 96 << 20},
		{Name: "cache", Kind: "module", Engine: "valkey", Version: "9", State: "idle", RAM: 11 << 20, IdleSince: epoch.Add(-(5*time.Minute - 3*time.Minute - 12*time.Second))},
		{Name: "events", Kind: "module", Engine: "kafka", State: "booting", Holders: []control.HolderView{{Kind: "dependant", Name: "worker"}}},
		{Name: "cloud", Kind: "module", Engine: "aws", State: "reaped"},
		{Name: "mongo", Kind: "service", Engine: "process", State: "active", Run: "bin/mongod --dbpath $DATA", RAM: 148 << 20},
		{Name: "mailpit", Kind: "service", Engine: "process", State: "reaped", Run: "mailpit --smtp 127.0.0.1:$PORT"},
		{Name: "search", Kind: "service", Engine: "process", State: "reaped", Run: "docker run --rm -p 7700:7700 getmeili/meilisearch"},
		{Name: "migrate", Kind: "task", Engine: "process", State: "reaped", Task: &control.TaskView{Result: "done", TookMS: 1200, RanAt: epoch.Add(-2 * time.Minute)}},
		{Name: "seed", Kind: "task", Engine: "process", State: "reaped"},
	}}
	m.sel = "api"
	for i := 0; i < 26; i++ {
		m.cpu["api"] = append(m.cpu["api"], float64(1+i%5))
		m.ram["api"] = append(m.ram["api"], float64((80+i/6)<<20))
	}
	at := func(s string) time.Time {
		t, _ := time.Parse("15:04:05", s)
		return time.Date(2026, 10, 11, t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
	}
	for _, l := range []struct{ t, s string }{
		{"14:02:11", "INFO listening addr=:8080"}, {"14:03:40", "INFO GET /products 200 12ms"},
		{"14:03:52", "INFO POST /orders 201 31ms"}, {"14:04:07", "WARN slow query 212ms SELECT * FROM orders"},
	} {
		m.logs = append(m.logs, control.LogFrame{Instance: "api", Line: l.s, Time: at(l.t)})
	}
	m.events = []journal.Event{
		{Time: at("14:03:41"), Kind: journal.Wake, Instance: "cache", Cause: journal.Cause{Kind: journal.ByDependency, By: "api"}},
	}
	return m
}

func frame(m *Model, w, h int) string {
	var out []string
	for _, l := range strings.Split(m.Render(w, h), "\n") {
		out = append(out, strings.TrimRight(ansi.Strip(l), " "))
	}
	return strings.Join(out, "\n")
}

// TestHomeAgainstTheBoard renders Main.dc.html's data at the board's size
// (1200 x 760 px is 136 x 32 cells at 14px) so the frame can be put beside it.
func TestHomeAgainstTheBoard(t *testing.T) {
	m := board()
	got := frame(m, 136, 32)
	if os.Getenv("DOZE_SHOW") != "" {
		fmt.Println(got)
	}
	for _, want := range []string{
		"doze  shop ~/code/shop", "mem", "cpu", "conns", "4 awake", "1 dozing", "2 waking", "3 asleep",
		"NAME", "KIND", "STATE", "MEM", "WHY AWAKE · WHAT IT RUNS",
		"APPS", "your code", "MODULES", "from the doze registry", "OUTSIDE", "brought in by you", "TASKS",
		"go run ./cmd/api", "postgres 17", "valkey 9", "sleeps in 3:12", "woken by worker", "waiting on events",
		"84 MB", "212 MB", "96 MB", "11 MB",
		"api · logs · following", "INF listening addr=:8080", "WRN slow query", "zzz cache woke (api needed it)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Home lacks %q", want)
		}
	}
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// Narrow.dc.html: 80 x 24, one column.
func TestNarrowAgainstTheBoard(t *testing.T) {
	m := board()
	got := frame(m, 80, 24)
	if os.Getenv("DOZE_SHOW") != "" {
		fmt.Println(got)
	}
	for _, want := range []string{"doze  shop", "APPS", "MODULES", "OUTSIDE", "zzz cache woke", "move"} {
		if !strings.Contains(got, want) {
			t.Errorf("Narrow lacks %q", want)
		}
	}
	for i, l := range strings.Split(got, "\n") {
		if width(l) > 80 {
			t.Errorf("line %d is %d cells wide", i, width(l))
		}
	}
}

// Crash.dc.html: api is crash-looping.
func TestCrashAgainstTheBoard(t *testing.T) {
	m := board()
	for i := range m.snap.Instances {
		if m.snap.Instances[i].Name == "api" {
			v := &m.snap.Instances[i]
			v.State, v.RAM, v.PID = "crashing", 0, 0
			v.LastError = "panic: STRIPE_KEY is required\n  main.loadConfig  cmd/api/config.go:41"
			v.RestartCount, v.RestartMax, v.NextRestart = 3, 5, epoch.Add(4*time.Second)
		}
	}
	got := frame(m, 136, 32)
	if os.Getenv("DOZE_SHOW") != "" {
		fmt.Println(got)
	}
	for _, want := range []string{"1 needs you", "api  crashing", "next try in 4 s", "panic: STRIPE_KEY is required", "main.loadConfig", "try", "3 of 5"} {
		if !strings.Contains(got, want) {
			t.Errorf("Crash lacks %q", want)
		}
	}
}

// focused opens name in the frame, with what the board's stack knows of it.
func focused(name string) *Model {
	m := board()
	for i := range m.snap.Instances {
		v := &m.snap.Instances[i]
		switch v.Name {
		case "db":
			v.URL, v.Domain, v.Endpoint, v.Bind = "postgres://shop@db.shop.doze:5432/shop", "db.shop.doze", "127.0.0.1:5432", "127.0.0.1:5432"
			v.EnvVar, v.Conns, v.DataDir, v.PID, v.CPU = "DATABASE_URL", 4, homeDir()+"/.doze/shop/db", 612, 0.8
			v.StartedAt = epoch.Add(-14 * time.Minute)
			v.Holders = []control.HolderView{{Kind: "dependant", Name: "api"}, {Kind: "connection", Count: 4}}
		case "mailpit":
			v.Console, v.Domain, v.Endpoint = "http://mailpit.shop.doze:8025", "mailpit.shop.doze", "127.0.0.1:1025"
		}
	}
	for i := 0; i < 40; i++ {
		m.cpu["db"] = append(m.cpu["db"], float64(1+i%4))
		m.ram["db"] = append(m.ram["db"], float64((90+i/10)<<20))
		m.conns["db"] = append(m.conns["db"], float64(2+i/12))
	}
	m.sel = name
	m.open()
	return m
}

// Focus.dc.html: a database module opens on its overview.
func TestFocusDatabaseAgainstTheBoard(t *testing.T) {
	m := focused("db")
	m.logs = []control.LogFrame{
		{Instance: "db", Line: "LOG: database system is ready to accept connections", Time: epoch.Add(-14 * time.Minute)},
		{Instance: "db", Line: "LOG: checkpoint complete: wrote 41 buffers", Time: epoch.Add(-20 * time.Second)},
	}
	got := frame(m, 136, 32)
	if os.Getenv("DOZE_SHOW") != "" {
		fmt.Println(got)
	}
	for _, want := range []string{"doze  shop › db postgres 17", "awake", "overview", "logs", "connect", "postgres://shop@db.shop.doze:5432/shop",
		"module.db.url in doze.hcl", "awake because", "api", "4 connections", "sleeps 5:00 after the last closes", "vitals", "cpu", "conns", "recent"} {
		if !strings.Contains(got, want) {
			t.Errorf("Focus on a module lacks %q", want)
		}
	}
}

// View-App.dc.html: an app opens on its logs.
func TestFocusAppAgainstTheBoard(t *testing.T) {
	m := focused("api")
	got := frame(m, 136, 32)
	if os.Getenv("DOZE_SHOW") != "" {
		fmt.Println(got)
	}
	for _, want := range []string{"doze  shop › api app", "following · info and up", "lines · this run", "needs", "✓  db", "this run", "levels", "err", "warn", "info", "zzz cache woke"} {
		if !strings.Contains(got, want) {
			t.Errorf("Focus on an app lacks %q", want)
		}
	}
}

// View-Task.dc.html: a task opens on its last run.
func TestFocusTaskAgainstTheBoard(t *testing.T) {
	m := focused("migrate")
	m.logs = []control.LogFrame{
		{Instance: "migrate", Line: "14 migrations already applied", Time: epoch.Add(-2 * time.Minute), Run: 1},
		{Instance: "migrate", Line: "ok 0015_orders_index.sql (212 ms)", Time: epoch.Add(-2*time.Minute + time.Second), Run: 1},
	}
	got := frame(m, 136, 32)
	if os.Getenv("DOZE_SHOW") != "" {
		fmt.Println(got)
	}
	for _, want := range []string{"migrate task", "last run", "exit 0", "14 migrations already applied", "runs when", "doze task migrate"} {
		if !strings.Contains(got, want) {
			t.Errorf("Focus on a task lacks %q", want)
		}
	}
}

// View-Web.dc.html: an outside service with a console of its own.
func TestFocusWebConsoleAgainstTheBoard(t *testing.T) {
	m := focused("mailpit")
	got := frame(m, 136, 32)
	if os.Getenv("DOZE_SHOW") != "" {
		fmt.Println(got)
	}
	for _, want := range []string{"mailpit service", "web console", "mailpit has its own console", "http://mailpit.shop.doze:8025", "Opening the console wakes it first", "Open in browser", "what doze can still tell you"} {
		if !strings.Contains(got, want) {
			t.Errorf("Focus on a web console lacks %q", want)
		}
	}
}

// Enter opens the frame on the front tab, tab moves along, esc comes back.
func TestEnterOpensTheFrameAndEscComesBack(t *testing.T) {
	m := board()
	m.sel = "db"
	if got := frame(m, 136, 32); !strings.Contains(got, "stack") {
		t.Fatalf("not on home:\n%s", got)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.page != pageFocus || !strings.Contains(frame(m, 136, 32), "connect") {
		t.Fatalf("enter did not open db on its overview (page %d)", m.page)
	}
	m.key("tab")
	if got := frame(m, 136, 32); !strings.Contains(got, "following · info and up") {
		t.Fatalf("tab did not move to logs:\n%s", got)
	}
	m.key("f")
	if got := frame(m, 136, 32); !strings.Contains(got, "warnings and up") {
		t.Fatalf("f did not raise the level:\n%s", got)
	}
	m.key("esc")
	if m.page != pageHome {
		t.Fatalf("esc did not go back (page %d)", m.page)
	}
	// a task has one tab; tab does not wander off it
	m.sel = "migrate"
	m.key("enter")
	m.key("tab")
	if !strings.Contains(frame(m, 136, 32), "last run") {
		t.Fatalf("a task left its last run tab")
	}
}

// typing feeds the palette the way the terminal does: a key and its text.
func typeText(m *Model, s string) {
	for _, r := range s {
		m.keyText(string(r), string(r))
	}
}

// Palette.dc.html: ":" opens it over the dimmed stack, typing narrows it, and
// every row has its command beside it.
func TestPaletteAgainstTheBoard(t *testing.T) {
	m := board()
	m.sel = "api"
	m.key(":")
	typeText(m, "re")
	got := frame(m, 136, 32)
	if os.Getenv("DOZE_SHOW") != "" {
		fmt.Println(got)
	}
	for _, want := range []string{": re", "restart api", "doze restart api", "reset api", "wipes its data · asks first", "doze reset api",
		"Every row is a shell command", "run", "copy command"} {
		if !strings.Contains(got, want) {
			t.Errorf("Palette lacks %q", want)
		}
	}
	if strings.Contains(got, "wake cache") {
		t.Errorf("Palette shows a row that does not match \"re\"")
	}
	// enter runs the first row: restart api
	fd := m.d.(*fakedaemon.Fake)
	_, cmd := m.key("enter")
	if m.modal != nil || cmd == nil {
		t.Fatalf("enter did not close the palette and run (modal %v)", m.modal)
	}
	cmd()
	if done := fd.Done(); len(done) != 1 || done[0].Name != control.VerbRestart || done[0].Target != "api" {
		t.Errorf("verbs run = %+v, want restart api", done)
	}
}

// Confirm-Sleep.dc.html: sleeping what something uses asks first.
func TestSleepInUseAsksFirst(t *testing.T) {
	m := focusedDB()
	m.key("s")
	if m.modal == nil || m.modal.kind != modalSleep {
		t.Fatalf("s on something in use did not ask")
	}
	got := frame(m, 136, 32)
	if os.Getenv("DOZE_SHOW") != "" {
		fmt.Println(got)
	}
	for _, want := range []string{"sleep db?", "api is running and needs it, and 4 connections are open to it right now.", "Kept. Nothing is deleted.",
		"Sleep db and api", "Sleep db only", "Cancel", "Sleeping an idle service never asks"} {
		if !strings.Contains(got, want) {
			t.Errorf("Confirm-Sleep lacks %q", want)
		}
	}
	fd := m.d.(*fakedaemon.Fake)
	m.key("right") // db only
	_, cmd := m.key("enter")
	cmd()
	if done := fd.Done(); len(done) != 1 || done[0].Target != "db" || done[0].Name != control.VerbSleep {
		t.Errorf("verbs run = %+v, want sleep db only", done)
	}
}

// An idle thing is slept without asking.
func TestSleepIdleNeverAsks(t *testing.T) {
	m := board()
	m.sel = "web"
	_, cmd := m.key("s")
	if m.modal != nil || cmd == nil {
		t.Fatalf("sleeping an idle service asked")
	}
}

// Confirm-Reset.dc.html: reset needs the name typed.
func TestResetNeedsTheName(t *testing.T) {
	m := focusedDB()
	m.key(":")
	typeText(m, "reset db")
	_, _ = m.key("enter")
	if m.modal == nil || m.modal.kind != modalReset {
		t.Fatalf("reset did not ask")
	}
	got := frame(m, 136, 32)
	if os.Getenv("DOZE_SHOW") != "" {
		fmt.Println(got)
	}
	for _, want := range []string{"reset db · this deletes data", "everything in ~/.doze/shop/db", "none. There is no snapshot.", "Type db to confirm", "Cancel is the default button", "doze reset db --yes"} {
		if !strings.Contains(got, want) {
			t.Errorf("Confirm-Reset lacks %q", want)
		}
	}
	fd := m.d.(*fakedaemon.Fake)
	typeText(m, "d")
	m.key("enter") // typed "d": not the name
	if m.modal == nil || len(fd.Done()) != 0 {
		t.Fatalf("reset ran without the name")
	}
	m.key("backspace")
	typeText(m, "db")
	_, cmd := m.key("enter")
	if m.modal != nil || cmd == nil {
		t.Fatalf("typing the name and enter did not reset")
	}
	cmd()
	if done := fd.Done(); len(done) != 1 || done[0].Name != control.VerbReset || done[0].Target != "db" {
		t.Errorf("verbs run = %+v, want reset db", done)
	}
	// esc cancels
	m = focusedDB()
	m.key(":")
	typeText(m, "reset db")
	m.key("enter")
	m.key("esc")
	if m.modal != nil {
		t.Errorf("esc did not cancel the confirmation")
	}
}

// focusedDB is the db of the Focus board, opened.
func focusedDB() *Model {
	m := focused("db")
	return m
}

// Every line of every screen is exactly the width asked for: a border that is
// a cell off shows as a ragged edge in a terminal.
func TestEveryFrameIsExactlyTheWidthAsked(t *testing.T) {
	cases := map[string]*Model{}
	cases["home"] = board()
	cases["focus-db"] = focused("db")
	cases["focus-api"] = focused("api")
	cases["focus-task"] = focused("migrate")
	cases["focus-web"] = focused("mailpit")
	pal := board()
	pal.key(":")
	cases["palette"] = pal
	slp := focused("db")
	slp.key("s")
	cases["sleep"] = slp
	rst := focused("db")
	rst.modal = &modal{kind: modalReset, target: "db", btn: 1}
	cases["reset"] = rst
	for name, m := range cases {
		for _, sz := range [][2]int{{136, 32}, {100, 30}, {80, 24}} {
			for i, l := range strings.Split(m.Render(sz[0], sz[1]), "\n") {
				if got := width(l); got != sz[0] && width(ansi.Strip(l)) != 0 && got > sz[0] {
					t.Errorf("%s at %dx%d: line %d is %d cells wide", name, sz[0], sz[1], i, got)
				}
			}
		}
	}
}

// The palette matches word starts, not scattered letters.
func TestPaletteMatchesWordStarts(t *testing.T) {
	cases := []struct {
		query, label string
		want         bool
	}{
		{"re", "restart api", true},
		{"re", "reset db", true},
		{"re", "rerun task migrate", true},
		{"re", "wake worker", false},
		{"reset d", "reset db", true},
		{"reset db", "reset db", true},
		{"db", "reset db", true},
		{"rdb", "reset db", false},
		{"", "anything", true},
		{"res", "restart api", true},
		{"resx", "restart api", false},
	}
	for _, c := range cases {
		if _, got := match(c.query, c.label); got != c.want {
			t.Errorf("match(%q, %q) = %v, want %v", c.query, c.label, got, c.want)
		}
	}
}

// The palette is drawn for the board's ground, so the dash asks the terminal
// for it rather than taking the theme's, unless colour is off.
func TestTheDashAsksForItsGround(t *testing.T) {
	m := board()
	m.st = newStyles(false) // the board fixture is plain, for the text tests
	m.w, m.h = 136, 32
	if m.View().BackgroundColor == nil || m.View().ForegroundColor == nil {
		t.Error("the dash does not set the terminal's ground")
	}
	p := board()
	p.st = newStyles(true)
	p.w, p.h = 136, 32
	if p.View().BackgroundColor != nil {
		t.Error("plain output must leave the terminal's colours alone")
	}
}

// Rows of the stack can be clicked: one click selects, a second opens.
func TestClickingTheStack(t *testing.T) {
	m := board()
	m.w, m.h = 136, 32
	m.Render(m.w, m.h) // lays out the stack, which records where each row is
	lineOf := func(name string) int {
		for i, n := range m.hit {
			if n == name {
				return m.hitY0 + i
			}
		}
		t.Fatalf("no row for %s", name)
		return 0
	}
	click := func(name string, x int) {
		m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: lineOf(name), Button: tea.MouseLeft}))
		m.Render(m.w, m.h)
	}
	click("db", 10)
	if m.sel != "db" || m.page != pageHome {
		t.Fatalf("one click: sel=%q page=%d", m.sel, m.page)
	}
	click("db", 10)
	if m.page != pageFocus {
		t.Errorf("a second click on the same row did not open it")
	}

	m = board()
	m.w, m.h = 136, 32
	m.Render(m.w, m.h)
	before := m.sel
	m.Update(tea.MouseClickMsg(tea.Mouse{X: 100, Y: m.hitY0, Button: tea.MouseLeft})) // on the right pane
	if m.sel != before {
		t.Error("a click outside the stack moved the selection")
	}
	m.Update(tea.MouseClickMsg(tea.Mouse{X: 5, Y: m.hitY0, Button: tea.MouseLeft})) // a group heading
	if m.sel != before {
		t.Error("a click on a heading moved the selection")
	}
}

// More instances than rows: the list scrolls to keep the selection in view, and
// a click lands on the row that is drawn there.
func TestTheListScrollsToTheSelection(t *testing.T) {
	m := board()
	var many []control.InstanceView
	for i := 0; i < 40; i++ {
		many = append(many, control.InstanceView{Name: fmt.Sprintf("svc%02d", i), Kind: "service", Engine: "process", State: "reaped"})
	}
	m.snap.Instances = many
	m.w, m.h = 136, 20
	m.sel = "svc39"
	out := frame(m, 136, 20)
	if !strings.Contains(out, "svc39") {
		t.Fatalf("the selected row is not drawn:\n%s", out)
	}
	m.sel = "svc00"
	if out = frame(m, 136, 20); !strings.Contains(out, "svc00") {
		t.Fatalf("scrolling back up lost the selection:\n%s", out)
	}
	// the hit map is for what is drawn
	for i, n := range m.hit {
		if n != "" && !strings.Contains(frame(m, 136, 20), n) {
			t.Errorf("hit line %d names %s, which is not drawn", i, n)
		}
	}
}

// The header names the columns where the rows put them: the last column's
// header starts where its cells do, and a right-aligned column ends together.
func TestTableColumnsAlign(t *testing.T) {
	m := board()
	lines := strings.Split(frame(m, 150, 36), "\n")
	var head string
	for _, l := range lines {
		if strings.Contains(l, "WHY AWAKE") {
			head = l
		}
	}
	if head == "" {
		t.Fatal("no header row")
	}
	why := strings.Index(head, "WHY AWAKE")
	up := strings.Index(head, "UP") + len("UP")
	for _, l := range lines {
		if i := strings.Index(l, "go run ./cmd/api"); i >= 0 && i != why && !strings.Contains(l, "│") {
			t.Errorf("why cell starts at %d, header at %d:\n%s", i, why, l)
		}
		if i := strings.Index(l, " 14m "); i >= 0 && !strings.Contains(l, "│") && i+4 != up {
			t.Errorf("up cell ends at %d, header at %d:\n%s", i+4, up, l)
		}
	}
}

// t shows what needs what, and / narrows the table to what matches.
func TestTreeAndFilter(t *testing.T) {
	m := board()
	m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	out := frame(m, 150, 36)
	if strings.Contains(out, "APPS") {
		t.Error("the tree should have no group bands")
	}
	if !strings.Contains(out, "└ db") {
		t.Errorf("api's needs are not under it:\n%s", out)
	}
	m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "pg" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if out := frame(m, 150, 36); strings.Contains(out, "mailpit") {
		t.Errorf("the filter left a row that does not match:\n%s", out)
	}
	m.filter = ""
	for _, r := range "kafka" {
		m.filter += string(r)
	}
	out = frame(m, 150, 36)
	if !strings.Contains(out, "events") || strings.Contains(out, "mongo") {
		t.Errorf("filter kafka:\n%s", out)
	}
	if m.sel != "events" {
		t.Errorf("the selection stayed on a row the filter hid: %q", m.sel)
	}
}

// y copies: on Home the address of the selected instance, over OSC 52, and it
// says what it copied; an instance with nothing to copy says so.
func TestYankTheAddress(t *testing.T) {
	m := board()
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatalf("y did nothing; notice %q", m.notice)
	}
	if !strings.Contains(m.notice, "copied") || !strings.Contains(m.notice, "api.shop.doze") {
		t.Errorf("notice = %q", m.notice)
	}
	m.sel = "seed" // a task: no address
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil || !strings.Contains(m.notice, "no address") {
		t.Errorf("a task has nothing to copy: cmd=%v notice=%q", cmd != nil, m.notice)
	}
}

// The logs tab: / narrows to the lines that hold what was typed and marks it,
// and [ ] move between the runs the daemon has kept.
func TestLogsSearchAndRuns(t *testing.T) {
	m := focused("api")
	m.followed = "api" // already following, so the lines set here are kept
	m.logs = []control.LogFrame{
		{Instance: "api", Line: "INFO listening addr=:8080", Time: epoch.Add(-time.Hour), Run: 1},
		{Instance: "api", Line: "ERROR boom in the old run", Time: epoch.Add(-50 * time.Minute), Run: 1},
		{Instance: "api", Line: "INFO listening addr=:8080", Time: epoch.Add(-time.Minute), Run: 2},
		{Instance: "api", Line: "WARN slow query 212ms", Time: epoch.Add(-30 * time.Second), Run: 2},
	}
	press := func(k, text string) { m.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: text}) }

	if out := frame(m, 136, 32); !strings.Contains(out, "boom") || !strings.Contains(out, "slow query") {
		t.Fatalf("both runs should show at first:\n%s", out)
	}
	press("/", "/")
	for _, r := range "slow" {
		press(string(r), string(r))
	}
	out := frame(m, 136, 32)
	if !strings.Contains(out, "slow query") || strings.Contains(out, "boom") || strings.Contains(out, "listening") {
		t.Errorf("search slow should leave one line:\n%s", out)
	}
	if !strings.Contains(out, "search slow") {
		t.Errorf("the search is not named:\n%s", out)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.search != "" || m.page != pageFocus {
		t.Errorf("esc should clear the search and stay: search=%q page=%d", m.search, m.page)
	}

	press("[", "[")
	if m.runSel == 0 {
		t.Fatal("[ did not move to an earlier run")
	}
	out = frame(m, 136, 32)
	if !strings.Contains(out, "boom") || strings.Contains(out, "slow query") || !strings.Contains(out, "run 1 of 2") {
		t.Errorf("run 1 only:\n%s", out)
	}
	press("]", "]")
	press("]", "]")
	if m.runSel != 0 {
		t.Errorf("] past the newest should follow again, runSel=%d", m.runSel)
	}
}

// A task lists the runs the journal holds: when, why, how long, how it ended.
func TestTaskHistory(t *testing.T) {
	m := focused("migrate")
	m.events = []journal.Event{
		{Time: epoch.Add(-26 * time.Hour), Kind: journal.Wake, Instance: "migrate", Cause: journal.Cause{Kind: journal.ByCommand}},
		{Time: epoch.Add(-26 * time.Hour), Kind: journal.Ready, Instance: "migrate", Took: 900 * time.Millisecond},
		{Time: epoch.Add(-3 * time.Hour), Kind: journal.Wake, Instance: "migrate", Cause: journal.Cause{Kind: journal.ByWatch, By: "0014_carts.sql"}},
		{Time: epoch.Add(-3 * time.Hour), Kind: journal.Failed, Instance: "migrate", Took: 400 * time.Millisecond, Detail: "exit 1"},
	}
	out := frame(m, 136, 32)
	for _, want := range []string{"history", "0014_carts.sql changed", "you asked", "✗", "✓", "0.4s", "0.9s"} {
		if !strings.Contains(out, want) {
			t.Errorf("history lacks %q:\n%s", want, out)
		}
	}
}

// The page of the whole stack's logs: l opens it, / narrows it, esc goes back.
func TestStackLogsPage(t *testing.T) {
	m := board()
	m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if m.page != pageLogs {
		t.Fatalf("l did not open the logs page: page=%d", m.page)
	}
	m.all = []control.LogFrame{
		{Instance: "api", Line: "INFO GET /products 200", Time: epoch.Add(-3 * time.Minute)},
		{Instance: "db", Line: "ERROR relation does not exist", Time: epoch.Add(-2 * time.Minute)},
		{Instance: "web", Line: "INFO hmr update", Time: epoch.Add(-time.Minute)},
	}
	out := frame(m, 120, 30)
	for _, want := range []string{"all logs", "api", "db", "web", "relation does not exist", "GET /products"} {
		if !strings.Contains(out, want) {
			t.Errorf("logs page lacks %q:\n%s", want, out)
		}
	}
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "relation" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if out := frame(m, 120, 30); strings.Contains(out, "hmr update") || !strings.Contains(out, "relation does not exist") {
		t.Errorf("search should leave the db line:\n%s", out)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.page != pageHome {
		t.Errorf("esc, esc should be back on Home: page=%d", m.page)
	}
}

// Part of the logs can be copied: v, ↑↓ and y by keyboard, or a drag.
func TestCopyPartOfTheLogs(t *testing.T) {
	setup := func() *Model {
		m := focused("api")
		m.followed = "api"
		m.w, m.h = 140, 30
		m.logs = nil
		for i := 1; i <= 6; i++ {
			m.logs = append(m.logs, control.LogFrame{Instance: "api", Line: fmt.Sprintf("INFO line number %d", i), Time: epoch.Add(time.Duration(i-10) * time.Minute), Run: 1})
		}
		frame(m, 140, 30) // draws, which is what v selects from
		return m
	}

	m := setup()
	m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if !m.selecting {
		t.Fatalf("v did not start a selection; notice %q", m.notice)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	out := frame(m, 140, 30)
	if !strings.Contains(out, "3 lines") || !strings.Contains(out, "copy") {
		t.Errorf("the key bar should say what is selected:\n%s", out)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil || !strings.Contains(m.notice, "copied 3 lines") || m.selecting {
		t.Errorf("y should copy three lines and leave: cmd=%v notice=%q selecting=%v", cmd != nil, m.notice, m.selecting)
	}

	m = setup()
	m.Update(tea.MouseClickMsg(tea.Mouse{X: 10, Y: 2 + 1, Button: tea.MouseLeft}))
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: 2 + 3, Button: tea.MouseLeft}))
	_, cmd = m.Update(tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: 2 + 3, Button: tea.MouseLeft}))
	if cmd == nil || !strings.Contains(m.notice, "copied 3 lines") {
		t.Errorf("a drag over three lines should copy them: cmd=%v notice=%q", cmd != nil, m.notice)
	}

	m = setup()
	m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.selecting || m.page != pageFocus {
		t.Errorf("esc should cancel the selection and stay: selecting=%v page=%d", m.selecting, m.page)
	}
}

// A running instance shows the same vitals box at the top of its page, on every
// tab but the overview (which has its own) and not for a task or a sleeping one.
func TestVitalsAcrossThePlatform(t *testing.T) {
	m := focused("api")
	out := frame(m, 140, 42)
	if !strings.Contains(out, "vitals") || !strings.Contains(out, "now") {
		t.Errorf("a running app's logs tab lacks the vitals box:\n%s", out)
	}
	m = focused("seed")
	if out := frame(m, 140, 42); strings.Contains(out, "vitals") {
		t.Errorf("a task has no vitals:\n%s", out)
	}
	m = focused("api")
	if out := frame(m, 140, 24); strings.Contains(out, "vitals") {
		t.Errorf("a short window should keep its room for the logs:\n%s", out)
	}
}

// Looking at another instance and back loses nothing: every instance's lines
// are kept as they arrive, including those that came while it was not shown.
func TestLogsAreKeptWhenYouLookAway(t *testing.T) {
	m := board()
	line := func(inst, s string, sec int) logMsg {
		return logMsg{inst, control.LogFrame{Instance: inst, Line: s, Time: epoch.Add(time.Duration(sec) * time.Second), Run: 1}}
	}
	m.sel = "api"
	m.follow()
	m.Update(line("api", "INFO first", 1))
	m.Update(line("web", "INFO web while api shown", 2))
	m.sel = "web"
	m.follow()
	if len(m.logs) != 1 || !strings.Contains(m.logs[0].Line, "web while api shown") {
		t.Fatalf("web's lines that came while api was shown are lost: %+v", m.logs)
	}
	m.Update(line("api", "INFO api while web shown", 3))
	m.sel = "api"
	m.follow()
	if len(m.logs) != 2 || m.logs[0].Line != "INFO first" || m.logs[1].Line != "INFO api while web shown" {
		t.Errorf("api's lines were not kept: %+v", m.logs)
	}
	// the stack's logs are the same lines, merged
	m.openLogs()
	if len(m.all) != 3 || m.all[0].Line != "INFO first" || m.all[2].Line != "INFO api while web shown" {
		t.Errorf("the stack's logs are not the kept lines in time order: %+v", m.all)
	}
}
