package main

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/doze-dev/doze/internal/control"
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
	{name: "home", board: "V-C", fixture: "shop", wpx: 1640, hpx: 900, cols: 190, rows: 40},
	{name: "narrow", board: "Narrow", fixture: "shop", wpx: 716, hpx: 564, cols: 80, rows: 30},
	{name: "crash", board: "V-Crash", fixture: "crash", wpx: 1640, hpx: 900, cols: 190, rows: 40},
	{name: "focus-api", board: "View-App", fixture: "shop", keys: []string{"enter"}, wpx: 1200, hpx: 760, cols: 136, rows: 42},
	{name: "focus-db", board: "V-Focus", fixture: "shop", keys: cat(downs(5), []string{"enter"}), wpx: 1640, hpx: 900, cols: 190, rows: 40},
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
	// the dream at a wide window (docs/design/next/boards/W-*.dc.html)

	{name: "w-overview", board: "W-Overview", fixture: "shop", keys: cat(downs(5), []string{"enter"}), wpx: 1640, hpx: 1010, cols: 190, rows: 44},
	{name: "w-activity", board: "W-Activity", fixture: "modules", keys: cat(downs(2), []string{"enter", "tab"}), wpx: 1640, hpx: 800, cols: 190, rows: 34},
	{name: "w-task", board: "W-Task", fixture: "shop", keys: cat(downs(10), []string{"enter"}), wpx: 1640, hpx: 760, cols: 190, rows: 32},
	{name: "w-charts", board: "W-Charts", fixture: "shop", wpx: 1640, hpx: 900, cols: 190, rows: 40},
	{name: "v-a", board: "V-A", fixture: "shop", wpx: 1640, hpx: 900, cols: 190, rows: 40},
	{name: "v-b", board: "V-B", fixture: "shop", wpx: 1640, hpx: 900, cols: 190, rows: 40},
	{name: "v-c", board: "V-C", fixture: "shop", wpx: 1640, hpx: 900, cols: 190, rows: 40},
	{name: "v-focus", board: "V-Focus", fixture: "shop", keys: cat(downs(5), []string{"enter"}), wpx: 1640, hpx: 900, cols: 190, rows: 40},
	// the stack a real project is: names that run on, a crash trace, a failed task
	{name: "mess-home", board: "V-C", fixture: "mess", wpx: 1640, hpx: 900, cols: 150, rows: 40},
	{name: "mess-narrow", board: "Narrow", fixture: "mess", wpx: 716, hpx: 564, cols: 80, rows: 24},
	{name: "mess-crash", board: "V-Crash", fixture: "mess", keys: []string{"down"}, wpx: 1640, hpx: 900, cols: 150, rows: 40},
	{name: "mess-logs", board: "View-App", fixture: "mess", keys: []string{"enter"}, wpx: 1200, hpx: 760, cols: 136, rows: 40},
	{name: "mess-task", board: "View-Task", fixture: "mess", keys: cat(downs(20), []string{"enter"}), wpx: 1200, hpx: 760, cols: 136, rows: 40},
	{name: "logs-all", board: "Logs", fixture: "mess", keys: []string{"l"}, wpx: 1200, hpx: 760, cols: 136, rows: 40},
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
	case "mess":
		d = fixture.Mess(epoch)
	default:
		d = fixture.Shop(epoch)
	}
	// The fixture's daemon only records a verb. Under the look tool, and for a
	// person trying the dash with "look host", a verb should be seen to do what it
	// says: wake an instance and it is awake, sleep it and it is not.
	d.OnDo = func(v control.Verb) {
		d.Mutate(func(snap *control.Snapshot) {
			for i := range snap.Instances {
				in := &snap.Instances[i]
				if v.Target != "" && in.Name != v.Target {
					continue
				}
				switch v.Name {
				case control.VerbWake, control.VerbRestart, control.VerbUp:
					if in.Kind == "task" || in.Disabled {
						continue
					}
					in.State, in.StartedAt, in.PID, in.LastError = "active", epoch, 40000+i, ""
					if in.RAM == 0 {
						in.RAM = int64(40+i*7) << 20
					}
				case control.VerbSleep, control.VerbReset:
					if in.Kind == "task" || (v.Target == "" && !awakeState(in.State)) {
						continue
					}
					in.State, in.RAM, in.CPU, in.PID, in.Conns, in.StartedAt, in.LastError = "reaped", 0, 0, 0, 0, time.Time{}, ""
					in.Holders = nil
				case control.VerbPin:
					pinned := false
					var keep []control.HolderView
					for _, h := range in.Holders {
						if h.Kind == "pin" {
							pinned = true
							continue
						}
						keep = append(keep, h)
					}
					if !pinned {
						keep = append(keep, control.HolderView{Kind: "pin"})
					}
					in.Holders = keep
				case control.VerbTask:
					if in.Kind == "task" {
						in.Task = &control.TaskView{Result: "done", TookMS: 900, RanAt: epoch, Why: "you ran it"}
					}
				}
			}
		})
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := dash.Run(ctx, d, dash.Options{Now: func() time.Time { return epoch }, Loc: time.UTC}); err != nil {
		os.Stderr.WriteString("look host: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func awakeState(s string) bool { return s == "active" || s == "idle" }
