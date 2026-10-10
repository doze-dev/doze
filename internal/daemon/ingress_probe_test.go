package daemon

import (
	"net"
	"testing"

	"github.com/doze-dev/doze/engine/process"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/endpoints"
)

func TestIngressRouteDerivation(t *testing.T) {
	cfg := &config.Config{
		StackName: "demo",
		Defaults:  config.Defaults{Domains: true},
	}
	cfg.Add(&config.InstanceDecl{
		Type: "process", Name: "orders_api", Port: 28080, Enabled: true,
		Spec: appSpec(t, 28080),
	})
	d := &Daemon{cfg: cfg, logf: t.Logf}
	eps := []endpoints.Endpoint{{Name: "orders_api", Engine: "process", Address: "127.0.0.1:28080", Domain: "orders-api.demo.doze"}}
	routes := d.ingressRoutes(eps, &bindPlan{bind: map[string]string{}, resolve: map[string]net.IP{}})
	if len(routes) != 1 {
		t.Fatalf("routes = %v, want 1", routes)
	}
	if target, ok := routes["orders-api.demo.doze"]; !ok || target != "127.0.0.1:28080" {
		t.Fatalf("route = %+v", routes)
	}
}

// An app that declares a port is named automatically: the decode must hand
// the app its port, or the ingress never routes to it.
func TestAppWithAPortIsForwarded(t *testing.T) {
	src := `
stack = "demo"
defaults { domains = true }
app "api" {
  run  = "sleep 1"
  port = 28080
}
app "raw" {
  run    = "sleep 1"
  port   = 28081
  direct = true
}
`
	cfg, err := config.Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	api, ok := cfg.Lookup("api").Spec.(*process.Config)
	if !ok {
		t.Fatalf("Spec is %T, want *process.Config", cfg.Lookup("api").Spec)
	}
	if !api.IngressEnabled() || api.ForwardPort() != 80 {
		t.Fatalf("an app with a port should be forwarded on 80, got %d", api.ForwardPort())
	}
	if raw := cfg.Lookup("raw").Spec.(*process.Config); raw.IngressEnabled() {
		t.Fatal("direct = true keeps an app off the ingress")
	}
	// core consumes `port` before the driver decodes — the DECL carries it.
	if cfg.Lookup("api").Port != 28080 {
		t.Fatalf("decl.Port = %d, want 28080", cfg.Lookup("api").Port)
	}
}

// appSpec is a decoded app that declares port.
func appSpec(t *testing.T, port int) *process.Config {
	t.Helper()
	spec := &process.Config{Run: "x"}
	if err := spec.BindPort(port); err != nil {
		t.Fatal(err)
	}
	return spec
}
