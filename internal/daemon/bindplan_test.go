package daemon

import (
	"testing"

	"github.com/doze-dev/doze/internal/config"
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

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
