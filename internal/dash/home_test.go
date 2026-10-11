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
		"doze  shop ~/code/shop", "4 awake", "1 dozing", "551 MB resident", "stack",
		"APPS", "your code", "MODULES", "from the doze registry", "OUTSIDE", "brought in by you", "TASKS",
		"runs", "go run ./cmd/api",
		"84 MB", "212 MB", "96 MB", "11 MB",
		"api  awake", "up 14m · pid 48211 · 0 restarts", "url", "http://api.shop.doze → :8080", "needs", "✓ db", "cpu", "mem",
		"logs · following", "INF listening addr=:8080", "WRN slow query", "zzz cache woke (api needed it)",
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
	for _, want := range []string{"doze  shop", "APPS", "MODULES", "OUTSIDE", "TASKS", "zzz cache woke", "move"} {
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
	for _, want := range []string{"doze  shop › api app", "following · info and up", "lines · this run", "needs", "✓  db", "this run", "levels", "ERR", "INF", "DBG", "hidden", "zzz cache woke"} {
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

// The list is a third of the screen and says only what each is and how it is;
// what a thing runs, and what it waits for, is on the right for the selected.
func TestTheListIsAThirdAndTheRightSaysWhatItIs(t *testing.T) {
	m := board()
	got := frame(m, 136, 32)
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "go run ./cmd/api") && strings.Index(l, "go run ./cmd/api") < 48 {
			t.Errorf("the list still carries the command: %q", l)
		}
	}
	for name, want := range map[string][]string{
		"cache":  {"is", "valkey 9", "now", "sleeps in 3:12"},
		"events": {"kafka", "woken by worker"},
		"worker": {"waiting on events"},
	} {
		m.sel = name
		out := frame(m, 136, 32)
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: the right lacks %q", name, w)
			}
		}
	}
}
