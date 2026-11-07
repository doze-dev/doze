package fixture

import (
	_ "embed"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
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
// has not. Up 18 minutes; the storefront is dozing.
func Larder(now time.Time) *fakedaemon.Fake {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	up := ago(18 * time.Minute)
	f := &fakedaemon.Fake{Hold: true}
	app := func(name, run string, mb int64, needs ...string) control.InstanceView {
		return control.InstanceView{Name: name, Kind: "app", Engine: "process", State: "active", Run: run, RAM: mb << 20,
			PID: 40000 + int(mb), StartedAt: up, Needs: needs, Domain: name + ".larder.doze"}
	}
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
	notifications := app("notifications", "bun src/index.ts", 45, "install-notifications", "kafka", "mailpit")
	notifications.CPU = 0.9
	storefront := app("storefront", "bun run dev", 61, "install-storefront", "gateway")
	storefront.State, storefront.IdleSince = "idle", ago(70*time.Second)
	orders := app("orders", "go run ./services/orders", 47, "migrate-orders", "postgres", "kafka", "catalog")
	orders.Holders = dependants("gateway", "fulfilment")
	catalog := app("catalog", "go run ./services/catalog", 47, "migrate-catalog", "postgres", "aws")
	catalog.Holders = dependants("gateway", "orders", "recommendations")
	f.Snap = control.Snapshot{Stack: "larder", Dir: "~/Code/doze-dev/doze-examples/larder", IdleTimeout: 2 * time.Minute, Loaded: up, Instances: []control.InstanceView{
		app("identity", "go run ./services/identity", 42, "migrate-identity", "postgres", "kafka"),
		catalog,
		orders,
		app("payments", "go run ./services/payments", 47, "migrate-payments", "postgres", "kafka", "redis"),
		app("fulfilment", "uv run --frozen python -m fulfilment", 52, "install-fulfilment", "kafka", "aws", "orders"),
		app("recommendations", "uv run --frozen python -m recommendations", 58, "migrate-recommendations", "install-recommendations", "postgres", "kafka", "redis", "catalog"),
		notifications,
		app("gateway", "go run ./apps/gateway", 46, "identity", "catalog", "orders", "fulfilment", "recommendations", "redis"),
		storefront,
		{Name: "postgres", Kind: "module", Engine: "postgres", Version: "18", State: "active", RAM: 13 << 20, PID: 701, Conns: 9, StartedAt: up,
			Endpoint: "127.0.0.1:5432", Domain: "postgres.larder.doze", Holders: append(dependants("identity", "catalog", "orders", "payments", "recommendations"), connections(9)...)},
		{Name: "redis", Kind: "module", Engine: "valkey", Version: "9", State: "active", RAM: 5 << 20, PID: 702, Conns: 3, StartedAt: up,
			Endpoint: "127.0.0.1:6379", Domain: "redis.larder.doze", Holders: connections(3)},
		{Name: "kafka", Kind: "module", Engine: "kafka", Version: "4", State: "active", RAM: 24 << 20, PID: 703, Conns: 7, StartedAt: up,
			Endpoint: "127.0.0.1:9092", Domain: "kafka.larder.doze", Holders: connections(7)},
		{Name: "aws", Kind: "module", Engine: "aws", State: "active", RAM: 25 << 20, PID: 704, Conns: 1, StartedAt: up,
			Endpoint: "127.0.0.1:4566", Domain: "aws.larder.doze", Holders: connections(1)},
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
	var stats []control.StatView
	for _, v := range f.Snap.Instances {
		if v.RAM > 0 {
			stats = append(stats, control.StatView{Name: v.Name, PID: v.PID, RAM: v.RAM, CPU: v.CPU, Conns: v.Conns})
		}
	}
	f.Frames = append(f.Frames, control.Frame{Topic: control.TopicStats, Stats: stats})
	return f
}
