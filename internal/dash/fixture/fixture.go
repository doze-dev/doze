// Package fixture is the stack of the canvas boards, as a scripted daemon: the
// data Main.dc.html, Focus.dc.html and the rest are drawn from. The dash's tests
// and the look tool (tools/look) both use it, so a screen is checked against the
// same shop everywhere.
package fixture

import (
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/journal"
)

// Shop is the stack of Main.dc.html at now: api, web and worker; db, cache,
// events and cloud; mailpit, mongo and search; migrate and seed. api is the
// busy one; db is held by api and has four connections open.
func Shop(now time.Time) *fakedaemon.Fake {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	f := &fakedaemon.Fake{Hold: true}
	f.Snap = control.Snapshot{Stack: "shop", Dir: "~/code/shop", IdleTimeout: 5 * time.Minute, Instances: []control.InstanceView{
		{Name: "api", Kind: "app", Engine: "process", State: "active", Run: "go run ./cmd/api", RAM: 84 << 20, CPU: 2.1, PID: 48211,
			StartedAt: ago(14 * time.Minute), Domain: "api.shop.doze", Bind: "127.0.0.1:8080", Endpoint: "127.0.0.1:8080",
			Needs: []string{"db", "mongo", "cache", "migrate"}},
		{Name: "web", Kind: "app", Engine: "process", State: "active", Run: "pnpm dev", RAM: 212 << 20, PID: 48230, StartedAt: ago(14 * time.Minute)},
		{Name: "worker", Kind: "app", Engine: "process", State: "booting", Needs: []string{"events"}},
		{Name: "db", Kind: "module", Engine: "postgres", Version: "17", State: "active", RAM: 96 << 20, CPU: 0.8, PID: 612, Conns: 4,
			StartedAt: ago(14 * time.Minute), URL: "postgres://shop@db.shop.doze:5432/shop", Domain: "db.shop.doze",
			Endpoint: "127.0.0.1:5432", Bind: "127.0.0.1:5432", EnvVar: "DATABASE_URL", DataDir: "~/.doze/shop/db",
			Holders: []control.HolderView{{Kind: "dependant", Name: "api"}, {Kind: "connection", Count: 4}}},
		{Name: "cache", Kind: "module", Engine: "valkey", Version: "9", State: "idle", RAM: 11 << 20, IdleSince: ago(5*time.Minute - 3*time.Minute - 12*time.Second)},
		{Name: "events", Kind: "module", Engine: "kafka", State: "booting", Holders: []control.HolderView{{Kind: "dependant", Name: "worker"}}},
		{Name: "cloud", Kind: "module", Engine: "aws", State: "reaped"},
		{Name: "mongo", Kind: "service", Engine: "process", State: "active", Run: "bin/mongod --dbpath $DATA", RAM: 148 << 20},
		{Name: "mailpit", Kind: "service", Engine: "process", State: "reaped", Run: "mailpit --smtp 127.0.0.1:$PORT",
			Console: "http://mailpit.shop.doze:8025", Domain: "mailpit.shop.doze", Endpoint: "127.0.0.1:1025"},
		{Name: "search", Kind: "service", Engine: "process", State: "reaped", Run: "docker run --rm -p 7700:7700 getmeili/meilisearch"},
		{Name: "migrate", Kind: "task", Engine: "process", State: "reaped",
			Task: &control.TaskView{Result: "done", TookMS: 1200, RanAt: ago(2 * time.Minute), Why: "migrations/0015_orders_index.sql is new"}},
		{Name: "seed", Kind: "task", Engine: "process", State: "reaped"},
	}}

	// the meters, a minute of them, for the charts
	for i := 0; i < 60; i++ {
		f.Frames = append(f.Frames, control.Frame{Topic: control.TopicStats, Stats: []control.StatView{
			{Name: "api", PID: 48211, RAM: int64(80+i/12) << 20, CPU: float64(1 + i%5), Conns: 2},
			{Name: "db", PID: 612, RAM: int64(90+i/10) << 20, CPU: float64(1+i%4) / 2, Conns: 2 + i/20},
			{Name: "web", PID: 48230, RAM: 212 << 20, CPU: 0.4},
		}})
	}

	// what the daemon did
	ev := func(d time.Duration, kind, inst string, c journal.Cause, took time.Duration) {
		e := journal.Event{Time: ago(d), Kind: kind, Instance: inst, Cause: c, Took: took}
		f.Frames = append(f.Frames, control.Frame{Topic: control.TopicJournal, Event: &e})
	}
	ev(15*time.Minute, journal.Wake, "db", journal.Cause{Kind: journal.ByDependency, By: "migrate"}, 0)
	ev(15*time.Minute, journal.Ready, "db", journal.Cause{}, 180*time.Millisecond)
	ev(14*time.Minute-20*time.Second, journal.Wake, "cache", journal.Cause{Kind: journal.ByDependency, By: "api"}, 0)
	ev(14*time.Minute-19*time.Second, journal.Ready, "cache", journal.Cause{}, 38*time.Millisecond)
	ev(3*time.Minute, journal.Wake, "events", journal.Cause{Kind: journal.ByDependency, By: "worker"}, 0)

	// what each instance printed
	line := func(inst string, d time.Duration, run int, s string) {
		l := control.LogFrame{Instance: inst, Line: s, Time: ago(d), Run: run}
		f.Frames = append(f.Frames, control.Frame{Topic: control.TopicLogs, Log: &l})
		if f.Lines == nil {
			f.Lines = map[string][]control.LogFrame{}
		}
		f.Lines[inst] = append(f.Lines[inst], l)
	}
	line("api", 14*time.Minute, 1, "INFO listening addr=:8080")
	line("api", 14*time.Minute, 1, "INFO postgres connected host=db.shop.doze pool=4")
	line("api", 14*time.Minute, 1, "INFO mongo connected host=mongo.shop.doze")
	line("api", 4*time.Minute, 1, "INFO GET /products 200 12ms")
	line("api", 3*time.Minute+40*time.Second, 1, "INFO GET /cart 200 44ms")
	line("api", 3*time.Minute, 1, "INFO POST /orders 201 31ms")
	line("api", 3*time.Minute, 1, "INFO published topic=orders key=ord_1042")
	line("api", 2*time.Minute, 1, "WARN slow query 212ms SELECT * FROM orders WHERE customer_id = $1")
	line("api", time.Minute, 1, "INFO GET /orders/ord_1042 200 6ms")
	line("api", 10*time.Second, 1, "INFO GET /healthz 200 0ms")
	line("db", 15*time.Minute, 1, "LOG: database system is ready to accept connections")
	line("db", 14*time.Minute, 1, "LOG: connection authorized: user=shop database=shop application_name=api")
	line("db", 2*time.Minute, 1, "LOG: duration: 212.4 ms  statement: SELECT * FROM orders WHERE customer_id = $1")
	line("db", 40*time.Second, 1, "LOG: checkpoint complete: wrote 41 buffers")
	line("migrate", 2*time.Minute, 1, "14 migrations already applied")
	line("migrate", 2*time.Minute-time.Second, 1, "applying 0015_orders_index.sql")
	line("migrate", 2*time.Minute-time.Second, 1, "ok 0015_orders_index.sql (212 ms)")
	return f
}

// Crash is Shop with api in a crash loop: Crash.dc.html.
func Crash(now time.Time) *fakedaemon.Fake {
	f := Shop(now)
	for i := range f.Snap.Instances {
		if v := &f.Snap.Instances[i]; v.Name == "api" {
			v.State, v.RAM, v.CPU, v.PID = "crashing", 0, 0, 0
			v.LastError = "panic: STRIPE_KEY is required\n  main.loadConfig  cmd/api/config.go:41"
			v.RestartCount, v.RestartMax, v.NextRestart = 3, 5, now.Add(4*time.Second)
		}
	}
	return f
}
