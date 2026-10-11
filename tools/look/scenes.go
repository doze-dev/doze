package main

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/dash"
	"github.com/doze-dev/doze/internal/dash/fixture"
)

// epoch is the moment every scene is drawn at, so "up 14m" is the same each run.
var epoch = time.Date(2026, 10, 11, 14, 4, 40, 0, time.UTC)

// scene is one screen: a fixture, the keys that get there, and its board.
type scene struct {
	name    string
	board   string // the board's file stem under docs/design/next/boards
	fixture string // "shop" or "crash"
	keys    []string
	wpx     int // the board's size in px
	hpx     int
	cols    int // the terminal's, which is that many px at 14px type and a 17px line
	rows    int
}

// downs is n presses of the down key.
func downs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "down"
	}
	return out
}

func cat(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// The shop's rows, in order: api web worker | cache cloud db events |
// mailpit mongo search | migrate seed. So db is five presses down, mailpit
// seven, migrate ten.
var scenes = []scene{
	{name: "home", board: "Main", fixture: "shop", wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "narrow", board: "Narrow", fixture: "shop", wpx: 716, hpx: 564, cols: 80, rows: 30},
	{name: "crash", board: "Crash", fixture: "crash", wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "focus-api", board: "View-App", fixture: "shop", keys: []string{"enter"}, wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "focus-db", board: "Focus", fixture: "shop", keys: cat(downs(5), []string{"enter"}), wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "focus-task", board: "View-Task", fixture: "shop", keys: cat(downs(10), []string{"enter"}), wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "focus-web", board: "View-Web", fixture: "shop", keys: cat(downs(7), []string{"enter"}), wpx: 1200, hpx: 760, cols: 136, rows: 42},
	// the data modules: cache=0 cloud=1 db=2 docs=3 events=4 flows=5 legacy=6
	{name: "focus-valkey", board: "Focus", fixture: "modules", keys: []string{"enter"}, wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "focus-ferret", board: "Focus", fixture: "modules", keys: cat(downs(3), []string{"enter"}), wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "focus-temporal", board: "View-Web", fixture: "modules", keys: cat(downs(5), []string{"enter"}), wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "focus-mariadb", board: "Focus", fixture: "modules", keys: cat(downs(6), []string{"enter"}), wpx: 1200, hpx: 760, cols: 136, rows: 42},
	// consoles, from the blocks their modules publish
	{name: "pg-overview", board: "Pg-Overview", fixture: "modules", keys: cat(downs(2), []string{"enter"}), wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "pg-activity", board: "Pg-Activity", fixture: "modules", keys: cat(downs(2), []string{"enter", "tab"}), wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "kafka", board: "View-Kafka", fixture: "modules", keys: cat(downs(4), []string{"enter"}), wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "aws", board: "View-Aws", fixture: "modules", keys: cat(downs(1), []string{"enter"}), wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "palette", board: "Palette", fixture: "shop", keys: []string{":", "r", "e"}, wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "sleep", board: "Confirm-Sleep", fixture: "shop", keys: cat(downs(5), []string{"s"}), wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "reset", board: "Confirm-Reset", fixture: "shop", keys: cat(downs(5), []string{":", "r", "e", "s", "e", "t", " ", "d", "b", "enter"}), wpx: 1200, hpx: 760, cols: 136, rows: 42},
}

func find(name string) (scene, bool) {
	for _, s := range scenes {
		if s.name == name {
			return s, true
		}
	}
	return scene{}, false
}

func sceneNames() []string {
	var out []string
	for _, s := range scenes {
		out = append(out, s.name)
	}
	return out
}

// host is what the child process runs: the real dash, on the terminal it was
// given, against a scripted daemon.
func host(args []string) {
	fixtureName := "shop"
	if len(args) > 0 {
		fixtureName = args[0]
	}
	var d *fakedaemon.Fake
	switch fixtureName {
	case "crash":
		d = fixture.Crash(epoch)
	case "modules":
		d = fixture.Modules(epoch)
	default:
		d = fixture.Shop(epoch)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := dash.Run(ctx, d, dash.Options{Now: func() time.Time { return epoch }, Loc: time.UTC}); err != nil {
		os.Stderr.WriteString("look host: " + err.Error() + "\n")
		os.Exit(1)
	}
}
