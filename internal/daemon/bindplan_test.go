package daemon

import (
	"testing"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/endpoints"
	"github.com/doze-dev/doze/internal/loopback"
)

// In fallback mode (domains on, loopback range NOT aliased), two services that
// declare the same port must fail with the actionable dns-setup message rather
// than silently colliding at bind time.
func TestBindPlanFallbackDuplicatePort(t *testing.T) {
	cfg := &config.Config{StackName: "demo", Defaults: config.Defaults{Domains: true}}
	cfg.Add(&config.InstanceDecl{Type: "postgres", Name: "orders_pg", Port: 5432, Enabled: true})
	cfg.Add(&config.InstanceDecl{Type: "postgres", Name: "analytics_pg", Port: 5432, Enabled: true})
	d := &Daemon{cfg: cfg, logf: t.Logf}
	eps := []endpoints.Endpoint{
		{Name: "orders_pg", Engine: "postgres", Address: "127.0.0.1:5432", Domain: "orders-pg.demo.doze"},
		{Name: "analytics_pg", Engine: "postgres", Address: "127.0.0.1:5432", Domain: "analytics-pg.demo.doze"},
	}
	alloc := loopback.NewAllocator(t.TempDir(), 1)
	_, err := d.buildBindPlan(eps, alloc)
	// This test runs where the range isn't aliased (CI/dev macOS) → fallback →
	// dup-port error. On Linux the range IS available, so it'd allocate distinct
	// IPs and succeed; accept either but assert the message when it errors.
	if loopback.Available() {
		if err != nil {
			t.Fatalf("loopback available but plan errored: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatal("expected a duplicate-port error in fallback mode")
	}
	if got := err.Error(); !contains(got, "dns-setup") {
		t.Fatalf("error should point at dns-setup, got %q", got)
	}
}

// Status must show each same-port service at the address it ACTUALLY listens on,
// not the shared canonical 127.0.0.1:<port> — otherwise two Postgres on 5432 both
// read as 127.0.0.1:5432, which is impossible and nothing binds there.
func TestHydrateEndpointShowsRealBind(t *testing.T) {
	cfg := &config.Config{StackName: "demo", Defaults: config.Defaults{Domains: true}}
	cfg.Add(&config.InstanceDecl{Type: "postgres", Name: "orders_pg", Port: 5432, Enabled: true})
	cfg.Add(&config.InstanceDecl{Type: "postgres", Name: "analytics_pg", Port: 5432, Enabled: true})
	d := &Daemon{cfg: cfg, logf: t.Logf, binds: map[string]string{
		"orders_pg":    "127.0.0.11:5432",
		"analytics_pg": "127.0.0.12:5432",
	}}
	h := &handler{d: d}

	orders := control.InstanceView{Name: "orders_pg"}
	analytics := control.InstanceView{Name: "analytics_pg"}
	h.hydrateEndpoint(&orders, endpoints.Endpoint{
		Name: "orders_pg", Engine: "postgres", Address: "127.0.0.1:5432", Domain: "orders-pg.demo.doze"})
	h.hydrateEndpoint(&analytics, endpoints.Endpoint{
		Name: "analytics_pg", Engine: "postgres", Address: "127.0.0.1:5432", Domain: "analytics-pg.demo.doze"})

	if orders.Endpoint != "127.0.0.11:5432" {
		t.Errorf("orders_pg endpoint = %q, want real bind 127.0.0.11:5432", orders.Endpoint)
	}
	if analytics.Endpoint != "127.0.0.12:5432" {
		t.Errorf("analytics_pg endpoint = %q, want real bind 127.0.0.12:5432", analytics.Endpoint)
	}
	if orders.Endpoint == analytics.Endpoint {
		t.Fatal("two same-port services must not share a displayed endpoint")
	}
	// Bind mirrors the dialable truth for proxied services.
	if orders.Bind != "127.0.0.11:5432" || analytics.Bind != "127.0.0.12:5432" {
		t.Errorf("binds = %q / %q, want the real per-instance addresses", orders.Bind, analytics.Bind)
	}
}

// An AWS built-in's Endpoint is prettified to the shared per-type host, but
// Bind must keep the internal backend address — it's the raw line the dash
// shows under the connect address.
func TestHydrateEndpointAWSKeepsRawBind(t *testing.T) {
	cfg := &config.Config{StackName: "demo", Defaults: config.Defaults{Domains: true}}
	cfg.Add(&config.InstanceDecl{Type: "sqs", Name: "jobs", Enabled: true})
	d := &Daemon{cfg: cfg, logf: t.Logf, binds: map[string]string{"jobs": "127.0.0.1:53987"}}
	h := &handler{d: d}
	v := control.InstanceView{Name: "jobs", Engine: "sqs"}
	h.hydrateEndpoint(&v, endpoints.Endpoint{Name: "jobs", Engine: "sqs", Address: "127.0.0.1:53987"})
	if v.Endpoint != "sqs.demo.doze" {
		t.Errorf("endpoint = %q, want the shared host sqs.demo.doze", v.Endpoint)
	}
	if v.Bind != "127.0.0.1:53987" {
		t.Errorf("bind = %q, want the raw backend 127.0.0.1:53987", v.Bind)
	}
}

// A supervised process binds its own port and has no entry in the bind plan; its
// endpoint falls back to the canonical declared address.
func TestHydrateEndpointFallsBackWhenUnproxied(t *testing.T) {
	d := &Daemon{cfg: &config.Config{}, logf: t.Logf, binds: map[string]string{}}
	h := &handler{d: d}
	v := control.InstanceView{Name: "worker"}
	h.hydrateEndpoint(&v, endpoints.Endpoint{Name: "worker", Engine: "process", Address: "127.0.0.1:8080"})
	if v.Endpoint != "127.0.0.1:8080" {
		t.Errorf("endpoint = %q, want canonical 127.0.0.1:8080", v.Endpoint)
	}
	if v.Bind != "127.0.0.1:8080" {
		t.Errorf("bind = %q, want the self-bound address", v.Bind)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
