// Package fixture is the stack of the canvas boards, as a scripted daemon: the
// data Main.dc.html, Focus.dc.html and the rest are drawn from. The dash's tests
// and the look tool (tools/look) both use it, so a screen is checked against the
// same shop everywhere.
package fixture

import (
	"math"
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
		{Name: "worker", Kind: "app", Engine: "process", State: "booting", Run: "go run ./cmd/worker", Needs: []string{"events"}},
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
	// four minutes of them at a second each, as a daemon holds: a wave with the
	// odd spike for cpu, a slow climb with a collection now and then for memory
	// (mongo, an outside process, has none, so the empty chart is seen too)
	for i := 0; i < 240; i++ {
		f.Frames = append(f.Frames, control.Frame{Topic: control.TopicStats, Stats: []control.StatView{
			{Name: "api", PID: 48211, RAM: climb(i, 70, 84, 55), CPU: wave(i, 2.2, 1.6, 17, 41, 9), Conns: 2},
			{Name: "db", PID: 612, RAM: climb(i, 88, 96, 80), CPU: wave(i, 1.2, 0.9, 23, 59, 6), Conns: 2 + i/80},
			{Name: "web", PID: 48230, RAM: climb(i, 205, 214, 70), CPU: wave(i, 0.6, 0.4, 13, 0, 0)},
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
	// the task's runs, oldest first: a clean one, a failure after an edit, a retry
	ev(26*time.Hour, journal.Wake, "migrate", journal.Cause{Kind: journal.ByCommand}, 0)
	ev(26*time.Hour-900*time.Millisecond, journal.Ready, "migrate", journal.Cause{}, 900*time.Millisecond)
	ev(3*time.Hour, journal.Wake, "migrate", journal.Cause{Kind: journal.ByWatch, By: "0014_carts.sql"}, 0)
	ev(3*time.Hour-400*time.Millisecond, journal.Failed, "migrate", journal.Cause{}, 400*time.Millisecond)
	ev(2*time.Minute, journal.Wake, "migrate", journal.Cause{Kind: journal.ByWatch, By: "0015_orders_index.sql"}, 0)
	ev(2*time.Minute-1200*time.Millisecond, journal.Ready, "migrate", journal.Cause{}, 1200*time.Millisecond)

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
	line("web", 14*time.Minute, 1, "  VITE v5.4.8  ready in 412 ms")
	line("web", 14*time.Minute, 1, "  ➜  Local:   http://web.shop.doze/")
	line("web", 3*time.Minute, 1, "[vite] hmr update /src/pages/Cart.tsx")
	line("mongo", 14*time.Minute, 1, `{"s":"I","c":"CONTROL","msg":"Process Details","attr":{"pid":48350,"port":27017}}`)
	line("mongo", 14*time.Minute, 1, `{"s":"I","c":"NETWORK","msg":"Waiting for connections","attr":{"port":27017}}`)
	line("mongo", 2*time.Minute, 1, `{"s":"I","c":"STORAGE","msg":"WiredTiger checkpoint","attr":{"ms":41}}`)
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

// Modules is a stack of one of each kind of database module, as the daemon
// describes them: valkey, postgres, ferret (MongoDB wire), temporal (which
// ships a web UI) and mariadb. The connection strings and variables are the
// ones the modules produce.
func Modules(now time.Time) *fakedaemon.Fake {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	f := &fakedaemon.Fake{Hold: true}
	f.Snap = control.Snapshot{Stack: "data", Dir: "~/code/data", IdleTimeout: 5 * time.Minute, Instances: []control.InstanceView{
		{Name: "cache", Kind: "module", Engine: "valkey", Version: "9", State: "idle", RAM: 11 << 20, CPU: 0.2, PID: 701, Conns: 0,
			StartedAt: ago(9 * time.Minute), IdleSince: ago(100 * time.Second), URL: "redis://cache.data.doze:6379/0", EnvVar: "REDIS_URL",
			Domain: "cache.data.doze", Endpoint: "127.0.0.1:6379", Bind: "127.0.0.1:6379", DataDir: "~/.doze/data/cache"},
		{Name: "db", Kind: "module", Engine: "postgres", Version: "17", State: "active", RAM: 96 << 20, CPU: 0.8, PID: 612, Conns: 3,
			StartedAt: ago(9 * time.Minute), URL: "postgres://postgres@db.data.doze:5432/db?sslmode=disable", EnvVar: "DATABASE_URL",
			Domain: "db.data.doze", Endpoint: "127.0.0.1:5432", Bind: "127.0.0.1:5432", DataDir: "~/.doze/data/db",
			Holders: []control.HolderView{{Kind: "connection", Count: 3}}, HasConsole: true, ConsoleFront: "overview"},
		{Name: "docs", Kind: "module", Engine: "ferret", Version: "2", State: "reaped", URL: "mongodb://docs.data.doze:27017/", EnvVar: "MONGODB_URI",
			Domain: "docs.data.doze", Endpoint: "127.0.0.1:27017", DataDir: "~/.doze/data/docs"},
		{Name: "flows", Kind: "module", Engine: "temporal", Version: "1.1", State: "active", RAM: 148 << 20, CPU: 1.4, PID: 733, Conns: 1,
			StartedAt: ago(4 * time.Minute), URL: "flows.data.doze:7233", EnvVar: "TEMPORAL_ADDRESS", Domain: "flows.data.doze",
			Endpoint: "127.0.0.1:7233", Bind: "127.0.0.1:7233", WebURL: "http://127.0.0.1:8233", DataDir: "~/.doze/data/flows",
			Holders: []control.HolderView{{Kind: "connection", Count: 1}}},
		{Name: "legacy", Kind: "module", Engine: "mariadb", Version: "11.4", State: "reaped", URL: "mysql://root@legacy.data.doze:3306/legacy", EnvVar: "DATABASE_URL",
			Domain: "legacy.data.doze", Endpoint: "127.0.0.1:3306", DataDir: "~/.doze/data/legacy"},
		{Name: "events", Kind: "module", Engine: "kafka", Version: "4", State: "active", RAM: 188 << 20, CPU: 1.1, PID: 741, Conns: 2,
			StartedAt: ago(9 * time.Minute), URL: "events.data.doze:9092", EnvVar: "KAFKA_BROKERS", Domain: "events.data.doze",
			Endpoint: "127.0.0.1:9092", Bind: "127.0.0.1:9092", DataDir: "~/.doze/data/events", HasConsole: true, ConsoleFront: "console"},
		{Name: "cloud", Kind: "module", Engine: "aws", State: "active", RAM: 64 << 20, CPU: 0.6, PID: 752, Conns: 1,
			StartedAt: ago(9 * time.Minute), URL: "http://cloud.data.doze", EnvVar: "AWS_ENDPOINT_URL", Domain: "cloud.data.doze",
			Endpoint: "127.0.0.1:4566", Bind: "127.0.0.1:4566", DataDir: "~/.doze/data/cloud", HasConsole: true, ConsoleFront: "console"},
	}}
	for i := 0; i < 240; i++ {
		f.Frames = append(f.Frames, control.Frame{Topic: control.TopicStats, Stats: []control.StatView{
			{Name: "db", PID: 612, RAM: climb(i, 88, 96, 80), CPU: wave(i, 1.2, 0.9, 23, 59, 6), Conns: 2 + i/120},
			{Name: "cache", PID: 701, RAM: climb(i, 10, 11, 120), CPU: wave(i, 0.1, 0.1, 19, 0, 0)},
			{Name: "flows", PID: 733, RAM: climb(i, 130, 148, 90), CPU: wave(i, 0.8, 0.5, 29, 71, 3), Conns: 1},
		}})
	}
	pgSpec, pgActions := PostgresConsole()
	f.Frames = append(f.Frames, consoleFrames("db", pgSpec, pgActions, map[string][]map[string]string{
		"activity": {{"active": "2", "idle": "2", "waiting": "1", "of": "100", "longest": "4.2 s"}},
		"sessions": {
			{"pid": "4412", "user": "shop", "database": "db", "app": "api", "for": "4.2 s", "state": "waiting", "query": "UPDATE orders SET status = $1 WHERE id = $2"},
			{"pid": "4410", "user": "shop", "database": "db", "app": "api", "for": "1.1 s", "state": "active", "query": "SELECT * FROM orders WHERE customer_id = $1"},
			{"pid": "4398", "user": "shop", "database": "db", "app": "worker", "for": "0.2 s", "state": "active", "query": "INSERT INTO events (kind) VALUES ($1)"},
			{"pid": "4377", "user": "shop", "database": "db", "app": "api", "for": "31 s", "state": "idle", "query": "COMMIT"},
		},
		"databases": {
			{"database": "db", "size": "38 MB", "connections": "4", "owner": "postgres"},
			{"database": "shop_test", "size": "9 MB", "connections": "0", "owner": "postgres"},
			{"database": "postgres", "size": "8 MB", "connections": "0", "owner": "postgres"},
		},
	}, nil)...)
	kSpec, kActions := KafkaConsole()
	f.Frames = append(f.Frames, consoleFrames("events", kSpec, kActions, map[string][]map[string]string{
		"topics": {
			{"name": "orders", "partitions": "3", "high": "1043"},
			{"name": "emails", "partitions": "1", "high": "87"},
			{"name": "orders.dlq", "partitions": "1", "high": "0", control.CellRuntime: "1"},
		},
		"groups": {
			{"name": "worker", "topic": "orders", "lag": "0", "members": "1"},
			{"name": "mailer", "topic": "emails", "lag": "12", "members": "0"},
		},
	}, map[string][]map[string]string{
		"tail": {
			{"_for": "orders", "time": "14:22:10", "partition": "1", "offset": "347", "key": "ord_1043", "value": `{"total": 4200, "items": 2}`},
			{"_for": "orders", "time": "14:21:02", "partition": "0", "offset": "351", "key": "ord_1042", "value": `{"total": 1850, "items": 1}`},
			{"_for": "emails", "time": "14:20:11", "partition": "0", "offset": "86", "key": "c_88", "value": `{"template": "receipt"}`},
		},
	})...)
	aSpec, aActions := AwsConsole()
	f.Frames = append(f.Frames, consoleFrames("cloud", aSpec, aActions, map[string][]map[string]string{
		"resources": {
			{"type": "S3", "name": "uploads", "now": "12 objects · 3.1 MB", "from": "doze.hcl"},
			{"type": "SQS", "name": "orders", "now": "0 waiting · 2 in flight", "from": "doze.hcl"},
			{"type": "SQS", "name": "orders-dlq", "now": "1 waiting", "from": "runtime", control.CellRuntime: "1"},
		},
	}, map[string][]map[string]string{
		"wire": {
			{"time": "14:22:31", "svc": "SQS", "action": "ReceiveMessage", "code": "ok", "target": "orders"},
			{"time": "14:22:25", "svc": "S3", "action": "PutObject", "code": "failed", "target": "upload/receipts/1043.pdf"},
			{"time": "14:22:10", "svc": "SQS", "action": "SendMessage", "code": "ok", "target": "orders"},
		},
	})...)
	line := func(inst string, d time.Duration, s string) {
		l := control.LogFrame{Instance: inst, Line: s, Time: ago(d), Run: 1}
		f.Frames = append(f.Frames, control.Frame{Topic: control.TopicLogs, Log: &l})
	}
	line("db", 9*time.Minute, "LOG: database system is ready to accept connections")
	line("db", 2*time.Minute, "LOG: checkpoint complete: wrote 41 buffers")
	line("cache", 9*time.Minute, "Ready to accept connections tcp")
	line("flows", 4*time.Minute, "INFO Temporal server started address=127.0.0.1:7233")
	line("flows", 4*time.Minute, "INFO Web UI listening on http://127.0.0.1:8233")
	return f
}

// consoleFrames scripts what a module's console sends: its spec and actions,
// then rows for each table and stream. A row's "_for" says which row of its
// table a stream row belongs to.
func consoleFrames(inst string, spec control.ConsoleSpec, actions []control.ActionView, tables map[string][]map[string]string, streams map[string][]map[string]string) []control.Frame {
	var out []control.Frame
	push := func(c control.ConsoleFrame) {
		c.Instance = inst
		out = append(out, control.Frame{Topic: control.TopicConsole, Console: &c})
	}
	push(control.ConsoleFrame{State: "active", Spec: &spec, Actions: actions})
	for panel, rows := range tables {
		var rs []control.ConsoleRow
		for _, r := range rows {
			rs = append(rs, control.ConsoleRow{Cells: r})
		}
		push(control.ConsoleFrame{Panel: panel, Rows: rs})
	}
	for panel, rows := range streams {
		for _, r := range rows {
			row := control.ConsoleRow{Cells: r}
			push(control.ConsoleFrame{Panel: panel, Row: &row})
		}
	}
	return out
}

// PostgresConsole is what the postgres module would publish: an activity tab
// of sessions with the selected one's query and the actions on it, and a
// databases table added to the generic overview.
func PostgresConsole() (control.ConsoleSpec, []control.ActionView) {
	spec := control.ConsoleSpec{
		Front: "overview",
		Panels: []control.ConsolePanel{
			{ID: "activity", Title: "", Kind: "counters", Columns: []control.ConsoleColumn{
				{Key: "active", Title: "active"}, {Key: "idle", Title: "idle"}, {Key: "waiting", Title: "waiting"},
				{Key: "of", Title: "of"}, {Key: "longest", Title: "longest"}}},
			{ID: "sessions", Title: "sessions", Kind: "table", Source: "session", Columns: []control.ConsoleColumn{
				{Key: "pid", Title: "pid", Align: "right"}, {Key: "user", Title: "user"}, {Key: "database", Title: "database"},
				{Key: "app", Title: "app"}, {Key: "for", Title: "for", Align: "right"}, {Key: "state", Title: "state", Role: "state"},
				{Key: "query", Title: "query", Role: "detail"}}},
			{ID: "databases", Title: "databases", Kind: "table", Source: "database", Columns: []control.ConsoleColumn{
				{Key: "database", Title: "database"}, {Key: "size", Title: "size", Align: "right"},
				{Key: "connections", Title: "connections", Align: "right", Role: "count"}, {Key: "owner", Title: "owner"}}},
		},
		Tabs: []control.ConsoleTab{{ID: "activity", Title: "activity", Body: control.ConsoleNode{Kind: "col", Children: []control.ConsoleNode{
			{Kind: "stat", Panel: "activity"},
			{Kind: "row", Weight: 1, Children: []control.ConsoleNode{
				{Kind: "table", Panel: "sessions", ID: "sessions", Weight: 2},
				{Kind: "col", Weight: 1, Children: []control.ConsoleNode{
					{Kind: "detail", Panel: "sessions", From: "sessions", Title: "query"},
					{Kind: "actions"},
				}},
			}},
		}}}},
		Extend: []control.ConsoleExtension{{After: "vitals", Node: control.ConsoleNode{Kind: "table", Panel: "databases", Title: "databases"}}},
	}
	actions := []control.ActionView{
		{ID: "cancel", Label: "Cancel query", Kind: "session"},
		{ID: "terminate", Label: "Terminate session", Kind: "session", Destructive: true},
	}
	return spec, actions
}

// KafkaConsole is the console the kafka module publishes today: panels and no
// layout, so the dash lays out its tables side by side and its stream below.
func KafkaConsole() (control.ConsoleSpec, []control.ActionView) {
	spec := control.ConsoleSpec{Front: "console", Panels: []control.ConsolePanel{
		{ID: "topics", Title: "topics", Kind: "table", Source: "topic", Columns: []control.ConsoleColumn{
			{Key: "name", Title: "name"}, {Key: "partitions", Title: "parts", Align: "right", Role: "count"},
			{Key: "high", Title: "messages", Align: "right", Role: "count"}, {Key: "high", Title: "rate", Align: "right", Role: "rate"}}},
		{ID: "groups", Title: "consumer groups", Kind: "table", Source: "group", Columns: []control.ConsoleColumn{
			{Key: "name", Title: "group"}, {Key: "topic", Title: "topic"}, {Key: "lag", Title: "lag", Align: "right", Role: "lag"},
			{Key: "members", Title: "members"}}},
		{ID: "tail", Title: "tail", Kind: "stream", Select: "topics", Columns: []control.ConsoleColumn{
			{Key: "time", Title: "time", Role: "time"}, {Key: "partition", Title: "p"}, {Key: "offset", Title: "offset", Align: "right"},
			{Key: "key", Title: "key"}, {Key: "value", Title: "value"}}},
	}}
	actions := []control.ActionView{
		{ID: "produce", Label: "Produce a message", Kind: "topic", InputHint: "message value"},
		{ID: "keep", Label: "Keep topic in doze.hcl", Kind: "topic"},
	}
	return spec, actions
}

// AwsConsole is the aws module's: a table of resources and the wire.
func AwsConsole() (control.ConsoleSpec, []control.ActionView) {
	spec := control.ConsoleSpec{Front: "console", Panels: []control.ConsolePanel{
		{ID: "resources", Title: "resources", Kind: "table", Source: "resource", Columns: []control.ConsoleColumn{
			{Key: "type", Title: "type"}, {Key: "name", Title: "name"}, {Key: "now", Title: "now"}, {Key: "from", Title: "from"}}},
		{ID: "wire", Title: "wire · every API call, newest first", Kind: "stream", Columns: []control.ConsoleColumn{
			{Key: "time", Title: "time", Role: "time"}, {Key: "svc", Title: "svc"}, {Key: "action", Title: "action"},
			{Key: "code", Title: "code", Align: "right", Role: "state"}, {Key: "target", Title: "target"}}},
	}}
	return spec, []control.ActionView{{ID: "keep", Label: "Keep in doze.hcl", Kind: "resource"}}
}

// wave is a cpu reading at second i: base plus a swing of amp over period
// seconds, and a spike of spike every spikeEvery seconds (0 for none).
func wave(i int, base, amp float64, period, spikeEvery int, spike float64) float64 {
	v := base + amp*math.Sin(float64(i)*2*math.Pi/float64(period))*math.Cos(float64(i)/11)
	if spikeEvery > 0 && i%spikeEvery == 0 {
		v += spike
	}
	return math.Max(v, 0)
}

// climb is a resident size in bytes at second i: it rises from lo to hi MB over
// the run, and drops back by a tenth of that span every collect seconds.
func climb(i int, lo, hi float64, collect int) int64 {
	span := hi - lo
	v := lo + span*float64(i)/240 - span*0.1*float64(i/collect%2)
	return int64(v * (1 << 20))
}
