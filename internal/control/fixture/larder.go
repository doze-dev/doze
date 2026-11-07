package fixture

import (
	_ "embed"
	"strconv"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/journal"
)

// LarderHCL is the doze.hcl of doze-examples/larder, which the Larder
// fixture is up from: the map and grid tooltips quote its lines.
//
//go:embed larder.hcl
var LarderHCL string

// Larder is the stack the MapFlow, MapFlowSelected and MapGrid boards are
// drawn on: services that call services, which Shop has none of. A
// storefront calls a gateway, which calls identity, catalog, orders,
// fulfilment and recommendations; fulfilment calls orders, orders and
// recommendations call catalog; payments and notifications are called by
// nothing and work from Kafka. Postgres, Redis, Kafka, AWS and Mailpit are
// shared. Every service's setup task has run; seed-catalog, run by hand,
// has not. Up 18 minutes; the storefront is dozing. It is also the stack
// the docs' screenshots are taken of, so it has four minutes of meters,
// what each service printed, what the daemon did, and the consoles of
// postgres, kafka and aws.
func Larder(now time.Time) *fakedaemon.Fake {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	up := ago(18 * time.Minute)
	f := &fakedaemon.Fake{Hold: true}
	app := func(name, run string, port int, mb int64, needs ...string) control.InstanceView {
		v := control.InstanceView{Name: name, Kind: "app", Engine: "process", State: "active", Run: run, RAM: mb << 20,
			PID: 40000 + int(mb), StartedAt: up, Needs: needs, Domain: name + ".larder.doze"}
		if port > 0 {
			addr := "127.0.0.1:" + strconv.Itoa(port)
			v.Endpoint, v.Bind, v.URL = addr, addr, "http://"+name+".larder.doze:"+strconv.Itoa(port)
		} else {
			v.AlwaysOn = true
		}
		return v
	}
	never := func(v control.InstanceView) control.InstanceView { v.AlwaysOn = true; return v }
	task := func(name, run string, ran time.Duration, needs ...string) control.InstanceView {
		return control.InstanceView{Name: name, Kind: "task", Engine: "process", State: "reaped", Run: run, Needs: needs,
			Task: &control.TaskView{Result: "done", TookMS: 900, RanAt: ago(ran)}}
	}
	connections := func(n int) []control.HolderView { return []control.HolderView{{Kind: "connection", Count: n}} }
	dependants := func(names ...string) []control.HolderView {
		var out []control.HolderView
		for _, n := range names {
			out = append(out, control.HolderView{Kind: "dependant", Name: n})
		}
		return out
	}
	notifications := app("notifications", "bun src/index.ts", 0, 45, "install-notifications", "kafka", "mailpit")
	notifications.CPU = 0.9
	storefront := app("storefront", "bun run dev", 5173, 61, "install-storefront", "gateway")
	storefront.State, storefront.IdleSince = "idle", ago(70*time.Second)
	orders := never(app("orders", "go run ./services/orders", 8003, 47, "migrate-orders", "postgres", "kafka", "catalog"))
	orders.Holders = dependants("gateway", "fulfilment")
	catalog := app("catalog", "go run ./services/catalog", 8002, 47, "migrate-catalog", "postgres", "aws")
	catalog.Holders = dependants("gateway", "orders", "recommendations")
	f.Snap = control.Snapshot{Stack: "larder", Dir: "~/Code/doze-dev/doze-examples/larder", IdleTimeout: 2 * time.Minute, Loaded: up, Instances: []control.InstanceView{
		app("identity", "go run ./services/identity", 8001, 42, "migrate-identity", "postgres", "kafka"),
		catalog,
		orders,
		never(app("payments", "go run ./services/payments", 8004, 47, "migrate-payments", "postgres", "kafka", "redis")),
		never(app("fulfilment", "uv run --frozen python -m fulfilment", 8005, 52, "install-fulfilment", "kafka", "aws", "orders")),
		never(app("recommendations", "uv run --frozen python -m recommendations", 8006, 58, "migrate-recommendations", "install-recommendations", "postgres", "kafka", "redis", "catalog")),
		notifications,
		app("gateway", "go run ./apps/gateway", 8000, 46, "identity", "catalog", "orders", "fulfilment", "recommendations", "redis"),
		storefront,
		{Name: "postgres", Kind: "module", Engine: "postgres", Version: "18", State: "active", RAM: 13 << 20, PID: 701, Conns: 9, StartedAt: up,
			Endpoint: "127.0.0.1:5432", Bind: "127.0.0.1:5432", Domain: "postgres.larder.doze", URL: "postgres://postgres@postgres.larder.doze:5432/postgres?sslmode=disable",
			EnvVar: "DATABASE_URL", DataDir: "~/.doze/larder/postgres", HasConsole: true, ConsoleFront: "overview",
			Holders: append(dependants("identity", "catalog", "orders", "payments", "recommendations"), connections(9)...)},
		{Name: "redis", Kind: "module", Engine: "valkey", Version: "9", State: "active", RAM: 5 << 20, PID: 702, Conns: 3, StartedAt: up,
			Endpoint: "127.0.0.1:6379", Bind: "127.0.0.1:6379", Domain: "redis.larder.doze", URL: "redis://redis.larder.doze:6379/0", EnvVar: "REDIS_URL",
			DataDir: "~/.doze/larder/redis", Holders: connections(3)},
		{Name: "kafka", Kind: "module", Engine: "kafka", Version: "4", State: "active", RAM: 24 << 20, PID: 703, Conns: 7, StartedAt: up,
			Endpoint: "127.0.0.1:9092", Bind: "127.0.0.1:9092", Domain: "kafka.larder.doze", URL: "kafka.larder.doze:9092", EnvVar: "KAFKA_BROKERS",
			DataDir: "~/.doze/larder/kafka", HasConsole: true, ConsoleFront: "console", Holders: connections(7)},
		{Name: "aws", Kind: "module", Engine: "aws", State: "active", RAM: 25 << 20, PID: 704, Conns: 1, StartedAt: up,
			Endpoint: "127.0.0.1:4566", Bind: "127.0.0.1:4566", Domain: "aws.larder.doze", URL: "http://aws.larder.doze:4566", EnvVar: "AWS_ENDPOINT_URL",
			DataDir: "~/.doze/larder/aws", HasConsole: true, ConsoleFront: "console", Holders: connections(1)},
		{Name: "mailpit", Kind: "service", Engine: "process", State: "active", Run: "./mailpit --smtp $HOST:$PORT --listen 127.0.0.1:8025 --database $DATA/mail.db",
			RAM: 24 << 20, PID: 705, Conns: 1, StartedAt: up, Console: "http://127.0.0.1:8025", Endpoint: "127.0.0.1:2525", Holders: connections(1)},
		task("migrate-identity", "go run ./platform/cmd/migrate services/identity/migrations", 17*time.Minute, "postgres"),
		task("migrate-catalog", "go run ./platform/cmd/migrate services/catalog/migrations", 17*time.Minute, "postgres"),
		task("migrate-orders", "go run ./platform/cmd/migrate services/orders/migrations", 16*time.Minute, "postgres"),
		task("migrate-payments", "go run ./platform/cmd/migrate services/payments/migrations", 16*time.Minute, "postgres"),
		task("migrate-recommendations", "go run ./platform/cmd/migrate services/recommendations/migrations", 16*time.Minute, "postgres"),
		{Name: "seed-catalog", Kind: "task", Engine: "process", State: "reaped", Run: "go run ./platform/cmd/migrate services/catalog/seed", Needs: []string{"migrate-catalog", "postgres"}},
		task("install-notifications", "bun install --frozen-lockfile", 18*time.Minute),
		task("install-fulfilment", "uv sync --frozen", 18*time.Minute),
		task("install-recommendations", "uv sync --frozen", 18*time.Minute),
		task("install-storefront", "bun install --frozen-lockfile", 18*time.Minute),
	}}

	// four minutes of meters, a second each: each process climbs to the
	// size the snapshot says, and its cpu waves
	var meters []control.StatView
	for _, v := range f.Snap.Instances {
		if v.RAM > 0 {
			meters = append(meters, control.StatView{Name: v.Name, PID: v.PID, RAM: v.RAM, CPU: v.CPU, Conns: v.Conns})
		}
	}
	for i := 0; i < 240; i++ {
		var stats []control.StatView
		for n, s := range meters {
			mb := float64(s.RAM >> 20)
			if i < 239 { // the last second is the size the snapshot says
				s.RAM = climb(i, mb*0.85, mb, 50+n*7)
			}
			s.CPU = wave(i, 0.3+s.CPU, 0.25, 13+n*2, 37+n*5, 1.5)
			stats = append(stats, s)
		}
		f.Frames = append(f.Frames, control.Frame{Topic: control.TopicStats, Stats: stats})
	}
	// and the snapshot's cpu is the last second's, so a page shows the same
	// numbers whether or not it is metered yet
	last := f.Frames[len(f.Frames)-1].Stats
	for i := range f.Snap.Instances {
		for _, s := range last {
			if v := &f.Snap.Instances[i]; v.Name == s.Name {
				v.CPU = s.CPU
			}
		}
	}

	// what the daemon did: the first up, then the storefront's last wake
	ev := func(d time.Duration, kind, inst string, c journal.Cause, took time.Duration) {
		e := journal.Event{Time: ago(d), Kind: kind, Instance: inst, Cause: c, Took: took}
		f.Frames = append(f.Frames, control.Frame{Topic: control.TopicJournal, Event: &e})
	}
	dep := func(by string) journal.Cause { return journal.Cause{Kind: journal.ByDependency, By: by} }
	// migrate-catalog's runs, oldest first: a clean one two days ago, a
	// failure after an edit yesterday, and this up's
	ev(50*time.Hour, journal.Wake, "migrate-catalog", journal.Cause{Kind: journal.ByCommand}, 0)
	ev(50*time.Hour-700*time.Millisecond, journal.Ready, "migrate-catalog", journal.Cause{}, 700*time.Millisecond)
	ev(26*time.Hour, journal.Wake, "migrate-catalog", journal.Cause{Kind: journal.ByWatch, By: "0012_product_search.sql"}, 0)
	ev(26*time.Hour-400*time.Millisecond, journal.Failed, "migrate-catalog", journal.Cause{}, 400*time.Millisecond)
	ev(18*time.Minute, journal.Wake, "postgres", dep("migrate-identity"), 0)
	ev(18*time.Minute-210*time.Millisecond, journal.Ready, "postgres", journal.Cause{}, 210*time.Millisecond)
	ev(18*time.Minute-time.Second, journal.Wake, "kafka", dep("identity"), 0)
	ev(18*time.Minute-3*time.Second, journal.Ready, "kafka", journal.Cause{}, 2100*time.Millisecond)
	ev(18*time.Minute-time.Second, journal.Wake, "aws", dep("catalog"), 0)
	ev(18*time.Minute-2*time.Second, journal.Ready, "aws", journal.Cause{}, 640*time.Millisecond)
	ev(17*time.Minute+20*time.Second, journal.Wake, "migrate-catalog", dep("catalog"), 0)
	ev(17*time.Minute+19*time.Second, journal.Ready, "migrate-catalog", journal.Cause{}, 900*time.Millisecond)
	ev(17*time.Minute, journal.Wake, "catalog", journal.Cause{Kind: journal.ByCommand}, 0)
	ev(17*time.Minute-41*time.Second, journal.Ready, "catalog", journal.Cause{}, 41*time.Second)
	ev(16*time.Minute, journal.Wake, "orders", journal.Cause{Kind: journal.ByCommand}, 0)
	ev(16*time.Minute-38*time.Second, journal.Ready, "orders", journal.Cause{}, 38*time.Second)
	ev(9*time.Minute, journal.Sleep, "storefront", journal.Cause{Kind: journal.ByIdle}, 0)
	ev(5*time.Minute, journal.Wake, "storefront", journal.Cause{Kind: journal.ByConnection}, 0)
	ev(5*time.Minute-900*time.Millisecond, journal.Ready, "storefront", journal.Cause{}, 900*time.Millisecond)
	ev(4*time.Minute, journal.Wake, "catalog", journal.Cause{Kind: journal.ByWatch, By: "services/catalog/search.go"}, 0)
	ev(4*time.Minute-6*time.Second, journal.Ready, "catalog", journal.Cause{}, 6*time.Second)

	// what each service printed, oldest first
	type said = struct {
		d time.Duration
		s string
	}
	for _, p := range []struct {
		inst  string
		lines []said
	}{
		{"storefront", []said{
			{5 * time.Minute, "$ vite --host 127.0.0.1 --port 5173"},
			{5 * time.Minute, "  VITE v6.0.7  ready in 388 ms"},
			{5 * time.Minute, "  ➜  Local:   http://storefront.larder.doze:5173/"},
			{4 * time.Minute, "[vite] hmr update /src/pages/Basket.tsx"},
			{3 * time.Minute, "[vite] hmr update /src/components/AisleGrid.tsx, /src/styles/aisle.css"},
			{2 * time.Minute, "[vite] page reload src/api/client.ts"},
		}},
		{"gateway", gatewayLines()},
		{"identity", []said{
			{17 * time.Minute, "INFO migrations current schema=identity version=7"},
			{17 * time.Minute, "INFO listening addr=127.0.0.1:8001"},
			{4 * time.Minute, "INFO session ok user=u_2817"},
			{3 * time.Minute, "INFO signed up user=u_9134 email=r.mensah@example.com"},
			{3 * time.Minute, "INFO published topic=users.signed-up key=u_9134"},
		}},
		{"catalog", []said{
			{17 * time.Minute, "INFO migrations current schema=catalog version=12"},
			{17 * time.Minute, "INFO bucket ready bucket=product-images objects=412"},
			{4 * time.Minute, "INFO restarting: services/catalog/search.go changed"},
			{4 * time.Minute, "INFO listening addr=127.0.0.1:8002"},
			{4 * time.Minute, "INFO GET /products?aisle=fruit-veg 200 11ms rows=48"},
			{3 * time.Minute, "INFO GET /search?q=oat+milk 200 14ms rows=6"},
			{2*time.Minute + 40*time.Second, "INFO reserved order=ord_5521 lines=6"},
		}},
		{"orders", []said{
			{16 * time.Minute, "INFO migrations current schema=orders version=9"},
			{16 * time.Minute, "INFO listening addr=127.0.0.1:8003"},
			{16 * time.Minute, "INFO consumer joined group=orders topics=payments.settled,payments.failed"},
			{2*time.Minute + 40*time.Second, "INFO order placed order=ord_5521 lines=6 total=38.40 GBP"},
			{2*time.Minute + 40*time.Second, "INFO published topic=orders.placed key=ord_5521 partition=1"},
			{2*time.Minute + 38*time.Second, "INFO payment settled order=ord_5521 status=paid"},
			{90 * time.Second, "WARN payment failed order=ord_5519 reason=card_declined status=payment_failed"},
		}},
		{"payments", []said{
			{16 * time.Minute, "INFO migrations current schema=payments version=4"},
			{16 * time.Minute, "INFO consumer joined group=payments topic=orders.placed"},
			{2*time.Minute + 39*time.Second, "INFO charged order=ord_5521 amount=38.40 GBP card=•••• 4242"},
			{2*time.Minute + 39*time.Second, "INFO published topic=payments.settled key=ord_5521"},
			{90 * time.Second, "WARN declined order=ord_5519 amount=12.75 GBP card=•••• 0002 reason=card_declined"},
			{90 * time.Second, "INFO published topic=payments.failed key=ord_5519"},
		}},
		{"fulfilment", []said{
			{17 * time.Minute, "INFO:     Started server process [40052]"},
			{17 * time.Minute, "INFO:     Uvicorn running on http://127.0.0.1:8005 (Press CTRL+C to quit)"},
			{17 * time.Minute, "INFO:fulfilment:consuming payments.settled as group fulfilment"},
			{2*time.Minute + 37*time.Second, "INFO:fulfilment:pick list queued order=ord_5521 queue=pick-lists lines=6"},
			{2 * time.Minute, "INFO:fulfilment:packed order=ord_5520 picker=bay-3"},
			{2 * time.Minute, "INFO:fulfilment:receipt written s3://receipts/2026/11/ord_5520.pdf (41 KB)"},
		}},
		{"recommendations", []said{
			{16 * time.Minute, "INFO:     Uvicorn running on http://127.0.0.1:8006 (Press CTRL+C to quit)"},
			{16 * time.Minute, "INFO:recommendations:warmed 1,208 product pairs from orders.placed"},
			{3*time.Minute + 30*time.Second, "INFO:     127.0.0.1:52144 - \"GET /goes-well-with/p_0412 HTTP/1.1\" 200 OK"},
			{2*time.Minute + 40*time.Second, "INFO:recommendations:counted order=ord_5521 pairs=15"},
		}},
		{"notifications", []said{
			{17 * time.Minute, "connected to kafka.larder.doze:9092"},
			{17 * time.Minute, "consuming users.signed-up, orders.placed, payments.failed"},
			{3 * time.Minute, "sent welcome to r.mensah@example.com (u_9134)"},
			{2*time.Minute + 39*time.Second, "sent order-confirmation to amira.h@example.com (ord_5521)"},
			{89 * time.Second, "sent payment-failed to j.okafor@example.com (ord_5519)"},
		}},
		{"migrate-catalog", []said{
			{17*time.Minute + 20*time.Second, "11 migrations already applied (schema catalog)"},
			{17*time.Minute + 20*time.Second, "applying 0012_product_search.sql"},
			{17*time.Minute + 19*time.Second, "ok 0012_product_search.sql (318 ms)"},
		}},
		{"postgres", []said{
			{18 * time.Minute, "LOG:  database system is ready to accept connections"},
			{17 * time.Minute, "LOG:  connection authorized: user=catalog database=postgres application_name=catalog"},
			{16 * time.Minute, "LOG:  connection authorized: user=orders database=postgres application_name=orders"},
			{3 * time.Minute, "LOG:  duration: 118.204 ms  statement: SELECT p.* FROM catalog.products p WHERE p.search @@ plainto_tsquery($1)"},
			{40 * time.Second, "LOG:  checkpoint complete: wrote 63 buffers (0.4%); 0 WAL file(s) added"},
		}},
		{"kafka", []said{
			{18 * time.Minute, "[KafkaRaftServer nodeId=1] Kafka Server started"},
			{18 * time.Minute, "[Partition orders.placed-0 broker=1] Log loaded for partition orders.placed-0 with initial high watermark 0"},
			{16 * time.Minute, "[GroupCoordinator 1]: Stabilized group payments generation 1 (__consumer_offsets-12) with 1 members"},
			{16 * time.Minute, "[GroupCoordinator 1]: Stabilized group fulfilment generation 1 (__consumer_offsets-31) with 1 members"},
		}},
		{"aws", []said{
			{18 * time.Minute, "listening on 127.0.0.1:4566 (s3, sqs)"},
			{18 * time.Minute, "created bucket product-images, bucket receipts (versioned)"},
			{18 * time.Minute, "created queue pick-lists (dead letters to pick-lists-dlq after 5 receives)"},
			{2*time.Minute + 37*time.Second, "SQS SendMessage pick-lists 200"},
			{2 * time.Minute, "S3 PutObject receipts/2026/11/ord_5520.pdf 200"},
		}},
	} {
		for _, l := range p.lines {
			larderLine(f, p.inst, ago(l.d), 1, l.s)
		}
	}

	pgSpec, pgActions := PostgresConsole()
	f.Frames = append(f.Frames, consoleFrames("postgres", pgSpec, pgActions, map[string][]map[string]string{
		"activity": {{"active": "2", "idle": "7", "waiting": "0", "of": "100", "longest": "0.4 s"}},
		"sessions": {
			{"pid": "5120", "user": "catalog", "database": "postgres", "app": "catalog", "for": "0.4 s", "state": "active", "query": "SELECT p.* FROM catalog.products p WHERE p.aisle = $1 ORDER BY p.rank"},
			{"pid": "5133", "user": "orders", "database": "postgres", "app": "orders", "for": "0.1 s", "state": "active", "query": "UPDATE orders.orders SET status = $1 WHERE id = $2"},
			{"pid": "5118", "user": "identity", "database": "postgres", "app": "identity", "for": "12 s", "state": "idle", "query": "SELECT id, user_id FROM identity.sessions WHERE token = $1"},
			{"pid": "5141", "user": "payments", "database": "postgres", "app": "payments", "for": "38 s", "state": "idle", "query": "COMMIT"},
		},
		"databases": {
			{"database": "postgres", "size": "46 MB", "connections": "9", "owner": "postgres"},
		},
	}, nil)...)
	kSpec, kActions := KafkaConsole()
	f.Frames = append(f.Frames, consoleFrames("kafka", kSpec, kActions, map[string][]map[string]string{
		"topics": {
			{"name": "orders.placed", "partitions": "3", "high": "5521"},
			{"name": "payments.settled", "partitions": "3", "high": "5384"},
			{"name": "payments.failed", "partitions": "3", "high": "137"},
			{"name": "users.signed-up", "partitions": "3", "high": "9134"},
		},
		"groups": {
			{"name": "orders", "topic": "payments.settled", "lag": "0", "members": "1"},
			{"name": "payments", "topic": "orders.placed", "lag": "0", "members": "1"},
			{"name": "fulfilment", "topic": "payments.settled", "lag": "0", "members": "1"},
			{"name": "recommendations", "topic": "orders.placed", "lag": "2", "members": "1"},
			{"name": "notifications", "topic": "orders.placed", "lag": "0", "members": "1"},
		},
	}, map[string][]map[string]string{
		"tail": {
			{"_for": "orders.placed", "time": "14:19:50", "partition": "1", "offset": "1840", "key": "ord_5521", "value": `{"user": "u_2817", "lines": 6, "total": 3840}`},
			{"_for": "orders.placed", "time": "14:17:02", "partition": "0", "offset": "1852", "key": "ord_5520", "value": `{"user": "u_4410", "lines": 11, "total": 7215}`},
			{"_for": "orders.placed", "time": "14:14:47", "partition": "2", "offset": "1826", "key": "ord_5519", "value": `{"user": "u_0937", "lines": 2, "total": 1275}`},
		},
	})...)
	aSpec, aActions := AwsConsole()
	f.Frames = append(f.Frames, consoleFrames("aws", aSpec, aActions, map[string][]map[string]string{
		"resources": {
			{"type": "S3", "name": "product-images", "now": "412 objects · 38 MB", "from": "doze.hcl"},
			{"type": "S3", "name": "receipts", "now": "1,204 objects · 61 MB · versioned", "from": "doze.hcl"},
			{"type": "SQS", "name": "pick-lists", "now": "2 waiting · 1 in flight", "from": "doze.hcl"},
			{"type": "SQS", "name": "pick-lists-dlq", "now": "0 waiting", "from": "doze.hcl"},
		},
	}, map[string][]map[string]string{
		"wire": {
			{"time": "14:20:30", "svc": "S3", "action": "PutObject", "code": "ok", "target": "receipts/2026/11/ord_5520.pdf"},
			{"time": "14:20:05", "svc": "SQS", "action": "ReceiveMessage", "code": "ok", "target": "pick-lists"},
			{"time": "14:19:53", "svc": "SQS", "action": "SendMessage", "code": "ok", "target": "pick-lists"},
			{"time": "14:18:58", "svc": "S3", "action": "GetObject", "code": "ok", "target": "product-images/p_0412.webp"},
		},
	})...)
	return f
}

// gatewayLines are the gateway's: it started, then the storefront's
// requests, a few a minute, through one order's checkout and a rate limit.
func gatewayLines() []struct {
	d time.Duration
	s string
} {
	type said = struct {
		d time.Duration
		s string
	}
	out := []said{
		{17 * time.Minute, "INFO listening addr=127.0.0.1:8000"},
		{17 * time.Minute, "INFO redis connected addr=redis.larder.doze:6379"},
	}
	for i, r := range []string{
		"GET /api/catalog/aisles 200 9ms",
		"GET /api/catalog/products?aisle=fruit-veg 200 18ms",
		"GET /api/recommendations/p_0412 200 7ms",
		"GET /api/catalog/products/p_0412 200 6ms",
		"POST /api/basket/items 200 22ms user=u_2817",
		"GET /api/catalog/products?aisle=dairy 200 15ms",
		"GET /api/catalog/search?q=oat+milk 200 21ms",
		"POST /api/basket/items 200 19ms user=u_2817",
		"GET /api/basket 200 8ms user=u_2817",
		"POST /api/identity/sessions 201 41ms user=u_9134",
		"GET /api/catalog/products?aisle=bakery 200 14ms",
		"POST /api/basket/items 200 17ms user=u_2817",
		"GET /api/basket 200 7ms user=u_2817",
		"POST /api/orders 201 64ms user=u_2817 order=ord_5521",
		"GET /api/orders/ord_5521 200 6ms user=u_2817",
		"WARN rate limited user=u_9120 path=/api/catalog/search limit=20/s",
		"GET /api/catalog/search?q=sourdough 200 12ms",
		"GET /api/orders/ord_5521 200 5ms user=u_2817",
		"GET /api/catalog/aisles 200 4ms",
		"GET /api/recommendations/p_0977 200 9ms",
	} {
		if !strings.HasPrefix(r, "WARN ") {
			r = "INFO " + r
		}
		out = append(out, said{4*time.Minute + 50*time.Second - time.Duration(i)*13*time.Second, r})
	}
	return out
}

// LarderCrash is Larder with orders in a crash loop: its role's password
// in doze.hcl no longer matches the one postgres has, so every start
// fails to connect. Run 3 of 5 just failed; the next try is in 4 seconds.
func LarderCrash(now time.Time) *fakedaemon.Fake {
	f := Larder(now)
	larderCrashing(f, now, "orders", `pq: password authentication failed for user "orders"`, []string{
		"started go run ./services/orders",
		"INFO config loaded port=8003",
		"INFO connecting to postgres.larder.doze:5432 as orders",
		`ERROR pq: password authentication failed for user "orders"`,
		"exit status 1 · ran 1.4s",
	})
	larderLine(f, "postgres", now.Add(-5*time.Second), 1, `FATAL:  password authentication failed for user "orders"`)
	return f
}

// LarderPortClash is Larder with the gateway unable to listen: another
// program has 127.0.0.1:8000. Whatever holds it is found by the page, as
// in the app; the docs' screenshot holds the port while it shoots.
func LarderPortClash(now time.Time) *fakedaemon.Fake {
	f := Larder(now)
	larderCrashing(f, now, "gateway", "listen tcp 127.0.0.1:8000: bind: address already in use", []string{
		"started go run ./apps/gateway",
		"INFO redis connected addr=redis.larder.doze:6379",
		"ERROR listen tcp 127.0.0.1:8000: bind: address already in use",
		"exit status 1 · ran 0.6s",
	})
	return f
}

// LarderDomains is Larder with the storefront on a domain of its own,
// dev.larder.example, served over https: the connect card's Domain row.
func LarderDomains(now time.Time) *fakedaemon.Fake {
	f := Larder(now)
	for i := range f.Snap.Instances {
		if v := &f.Snap.Instances[i]; v.Name == "storefront" {
			v.State, v.IdleSince = "active", time.Time{}
			v.Domains, v.HTTPS = []string{"dev.larder.example"}, "https://dev.larder.example"
		}
	}
	return f
}

// LarderDomainsHCL is the doze.hcl LarderDomains runs: the storefront with
// its domain.
var LarderDomainsHCL = strings.Replace(LarderHCL, "  port  = 5173\n", "  port    = 5173\n  domains = [\"dev.larder.example\"]\n", 1)

// larderCrashing puts name in a crash loop, try 3 of 5, with err as its
// last error and lines as what its last run printed.
func larderCrashing(f *fakedaemon.Fake, now time.Time, name, err string, lines []string) {
	for i := range f.Snap.Instances {
		if v := &f.Snap.Instances[i]; v.Name == name {
			v.State, v.RAM, v.CPU, v.PID, v.Holders = "crashing", 0, 0, 0, nil
			v.LastError = err
			v.RestartCount, v.RestartMax, v.NextRestart = 3, 5, now.Add(4*time.Second)
		}
	}
	for i, fr := range f.Frames {
		if fr.Topic == control.TopicStats {
			var s []control.StatView
			for _, st := range fr.Stats {
				if st.Name != name {
					s = append(s, st)
				}
			}
			f.Frames[i].Stats = s
		}
	}
	e := journal.Event{Time: now.Add(-5 * time.Second), Kind: journal.Crash, Instance: name, Detail: strings.Split(lines[len(lines)-1], " · ")[0]}
	f.Frames = append(f.Frames, control.Frame{Topic: control.TopicJournal, Event: &e})
	for i, s := range lines {
		larderLine(f, name, now.Add(-6*time.Second+time.Duration(i)*100*time.Millisecond), 3, s)
	}
}

// larderLine is one line a Larder service printed, in the log stream and
// in what the daemon answers for its logs.
func larderLine(f *fakedaemon.Fake, inst string, at time.Time, run int, s string) {
	l := control.LogFrame{Instance: inst, Line: s, Time: at, Run: run}
	f.Frames = append(f.Frames, control.Frame{Topic: control.TopicLogs, Log: &l})
	if f.Lines == nil {
		f.Lines = map[string][]control.LogFrame{}
	}
	f.Lines[inst] = append(f.Lines[inst], l)
}
