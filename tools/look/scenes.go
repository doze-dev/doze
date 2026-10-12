package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/dash"
	"github.com/doze-dev/doze/internal/dash/fixture"
	"github.com/doze-dev/doze/internal/journal"
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
	{name: "logs-select", board: "View-App", fixture: "shop", keys: []string{"enter", "v", "up", "up"}, wpx: 1200, hpx: 760, cols: 136, rows: 40},
	{name: "home-mongo", board: "V-C", fixture: "shop", keys: downs(8), wpx: 1640, hpx: 900, cols: 150, rows: 40},
	{name: "metrics-nt", board: "V-Focus", fixture: "shop", keys: cat(downs(5), []string{"enter", "m"}), wpx: 1640, hpx: 900, cols: 150, rows: 44},
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
	// A person trying the dash sees it move; the scenes freeze it, so a screen is
	// the same image every run.
	frozen := len(args) > 1 && args[1] == "frozen"
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
	now := func() time.Time { return epoch }
	if !frozen {
		start := time.Now()
		now = func() time.Time { return epoch.Add(time.Since(start)) }
		// Five minutes is the real idle time; to be seen, it is half a minute here,
		// and what is already dozing has twenty seconds left of it.
		d.Mutate(func(snap *control.Snapshot) {
			snap.IdleTimeout = 30 * time.Second
			for i := range snap.Instances {
				if in := &snap.Instances[i]; in.State == "idle" {
					in.IdleSince = epoch.Add(-10 * time.Second)
				}
			}
		})
		go drive(ctx, d, now)
	}
	if err := dash.Run(ctx, d, dash.Options{Now: now, Loc: time.UTC}); err != nil {
		os.Stderr.WriteString("look host: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func awakeState(s string) bool { return s == "active" || s == "idle" }

// drive makes the fixture's daemon behave like one: every second each awake
// instance's memory drifts and its cpu moves, now and then it spikes, and apps
// print a line. The dash must follow all of it without being asked.
func drive(ctx context.Context, d *fakedaemon.Fake, now func() time.Time) {
	rng := rand.New(rand.NewSource(7))
	// what each kind of thing says: a web server, a job runner, a database that
	// is run as an outside service. %d is a number that varies.
	says := map[string][]string{
		"api":    {"INF GET /products 200 %dms", "INF GET /cart 200 %dms", "INF POST /orders 201 %dms", "INF GET /healthz 200 %dms", "WRN slow query %dms SELECT * FROM orders"},
		"web":    {"[vite] hmr update /src/pages/Cart.tsx (%d ms)", "[vite] page reload src/App.tsx", "INF GET /assets/app.%dkb.js 200", "WRN deprecated prop `onClick` on <Link>"},
		"worker": {"INF picked up job ord_%d from orders/1", "INF sent receipt to c_%d@example.com", "INF job done in %dms", "WRN retrying job ord_%d (attempt 2 of 5)"},
		"mongo":  {"{\"t\":{\"$date\":\"now\"},\"s\":\"I\",\"c\":\"NETWORK\",\"msg\":\"Connection accepted\",\"attr\":{\"connectionCount\":%d}}", "{\"s\":\"I\",\"c\":\"STORAGE\",\"msg\":\"WiredTiger checkpoint\",\"attr\":{\"ms\":%d}}", "{\"s\":\"I\",\"c\":\"COMMAND\",\"msg\":\"Slow query\",\"attr\":{\"ns\":\"shop.orders\",\"durationMillis\":%d}}"},
	}
	generic := []string{"INF handled request in %dms", "INF heartbeat ok (%d)"}
	tk := time.NewTicker(time.Second)
	defer tk.Stop()
	life := &lifecycle{booted: map[string]int{}}
	for tick := 0; ; tick++ {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
		life.step(d, now(), tick)
		var stats []control.StatView
		var apps []string
		d.Mutate(func(snap *control.Snapshot) {
			for i := range snap.Instances {
				in := &snap.Instances[i]
				if in.State != "active" && in.State != "idle" {
					continue
				}
				// a walk that stays near where it began, and a spike now and then
				in.RAM += int64(rng.Intn(5)-2) << 20
				in.RAM = max(in.RAM, 4<<20)
				in.CPU = 0.3 + rng.Float64()*2.2
				if rng.Intn(14) == 0 {
					in.CPU += 8 + rng.Float64()*12
				}
				if in.Kind == "module" && rng.Intn(6) == 0 {
					in.Conns = max(0, in.Conns+rng.Intn(3)-1)
				}
				stats = append(stats, control.StatView{Name: in.Name, PID: in.PID, RAM: in.RAM, CPU: in.CPU, Conns: in.Conns})
				if in.Kind == "app" || in.Kind == "service" {
					apps = append(apps, in.Name)
				}
			}
		})
		d.Push(control.Frame{Topic: control.TopicStats, Stats: stats})
		for _, name := range apps {
			if rng.Intn(2) == 0 {
				pool := says[name]
				if pool == nil {
					pool = generic
				}
				d.PushLog(control.LogFrame{Instance: name, Line: fmt.Sprintf(pool[rng.Intn(len(pool))], 2+rng.Intn(60)), Time: now(), Run: 1})
			}
		}
	}
}

// lifecycle moves the fixture's instances through what a daemon moves them
// through: a booting one becomes awake once what it needs is awake, an idle one
// goes to sleep when its timer is spent, and now and then a connection wakes a
// sleeping module. Each change is told to the dash as an instances frame and a
// journal event, as the daemon does.
type lifecycle struct {
	booted map[string]int // the tick an instance began booting
	wakeAt int            // the next tick a sleeping module is woken by a connection
}

func (l *lifecycle) step(d *fakedaemon.Fake, now time.Time, tick int) {
	var changed []control.InstanceView
	var events []journal.Event
	ev := func(kind, inst string, c journal.Cause, took time.Duration) {
		events = append(events, journal.Event{Time: now, Kind: kind, Instance: inst, Cause: c, Took: took})
	}
	d.Mutate(func(snap *control.Snapshot) {
		up := func(name string) bool {
			for _, o := range snap.Instances {
				if o.Name == name {
					return o.State == "active" || o.State == "idle"
				}
			}
			return true
		}
		// a sleeping module is woken by a connection every so often
		if l.wakeAt == 0 {
			l.wakeAt = tick + 50 // after the dozing one has had time to be seen asleep
		}
		if tick >= l.wakeAt {
			l.wakeAt = tick + 60
			for i := range snap.Instances {
				in := &snap.Instances[i]
				if in.Kind == "module" && in.State == "reaped" && !in.Disabled {
					in.State, in.Holders = "booting", []control.HolderView{{Kind: "connection", Count: 1}}
					ev(journal.Wake, in.Name, journal.Cause{Kind: journal.ByConnection}, 0)
					changed = append(changed, *in)
					break
				}
			}
		}
		for i := range snap.Instances {
			in := &snap.Instances[i]
			switch in.State {
			case "booting":
				if _, ok := l.booted[in.Name]; !ok {
					l.booted[in.Name] = tick
				}
				ready := true
				for _, n := range in.Needs {
					ready = ready && up(n)
				}
				// a module takes a couple of seconds; what waits on one, a moment after
				if ready && tick-l.booted[in.Name] >= 3 {
					in.State, in.StartedAt, in.PID = "active", now, 41000+i
					in.RAM = int64(30+i*9) << 20
					in.IdleSince = time.Time{}
					took := time.Duration(tick-l.booted[in.Name]) * time.Second
					delete(l.booted, in.Name)
					ev(journal.Ready, in.Name, journal.Cause{}, took)
					changed = append(changed, *in)
				}
			case "active":
				// a module nothing holds goes idle, with its timer set
				if in.Kind == "module" && in.Conns == 0 && len(in.Holders) == 0 && rand.Intn(25) == 0 {
					in.State, in.IdleSince = "idle", now
					changed = append(changed, *in)
				}
			case "idle":
				after := snap.IdleTimeout
				if in.IdleAfter > 0 {
					after = in.IdleAfter
				}
				if in.IdleSince.IsZero() {
					in.IdleSince = now
				}
				if now.Sub(in.IdleSince) >= after {
					in.State, in.RAM, in.CPU, in.PID, in.StartedAt, in.IdleSince = "reaped", 0, 0, 0, time.Time{}, time.Time{}
					ev(journal.Sleep, in.Name, journal.Cause{Kind: journal.ByIdle}, 0)
					changed = append(changed, *in)
				}
			}
		}
	})
	if len(changed) > 0 {
		d.Push(control.Frame{Topic: control.TopicInstances, Instances: changed})
	}
	for _, e := range events {
		e := e
		d.Push(control.Frame{Topic: control.TopicJournal, Event: &e})
	}
}
