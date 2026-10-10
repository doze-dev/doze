//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
)

// TestTemporalNamespace proves core -> plugin -> real Temporal dev server works:
// boot with a declared namespace and confirm the running frontend knows it.
//
// Temporal is a PortBinder (it binds its own frontend port; doze opens no proxy),
// so clients connect straight to the advertised address. The assertion uses the
// `temporal` CLI against that address.
func TestTemporalNamespace(t *testing.T) {
	temporal := mustTool(t, "DOZE_TEMPORAL_BINDIR", "temporal")

	port := FreePort(t)
	uiPort := FreePort(t)
	cfg := fmt.Sprintf(`module temporal "dev" {
  version = "1.1"
  port    = %d
  ui_port = %d

  namespace "orders" {}
}
`, port, uiPort)
	p := NewProject(t, "temporal", "dev", port, cfg)
	p.Up()

	addr := p.ProxyAddr() // for a PortBinder this is the app's own address
	out, err := run(t, temporal, "operator", "namespace", "describe", "orders", "--address", addr)
	if err != nil {
		t.Fatalf("describing namespace 'orders' at %s: %v\n%s", addr, err, out)
	}
	if !strings.Contains(out, "orders") {
		t.Fatalf("namespace 'orders' not registered; describe output:\n%s", out)
	}
}
