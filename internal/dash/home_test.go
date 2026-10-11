package dash

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

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
		"go run ./cmd/api", "postgres 17", "valkey 9 · sleeps in 3:12", "kafka · woken by worker", "waiting on events",
		"84 MB", "212 MB", "96 MB", "11 MB",
		"api  awake", "up 14m · pid 48211 · 0 restarts", "docker run", "url", "http://api.shop.doze → :8080", "needs", "✓ db", "cpu", "mem",
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
	for _, want := range []string{"1 needs you", "api  crashing", "next try in 4 s", "panic: STRIPE_KEY is required", "main.loadConfig", "try 3 of 5"} {
		if !strings.Contains(got, want) {
			t.Errorf("Crash lacks %q", want)
		}
	}
}
