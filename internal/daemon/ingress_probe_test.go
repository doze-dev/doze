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
		Spec: &process.Config{Command: "x", Port: 28080, Ingress: true},
	})
	d := &Daemon{cfg: cfg, logf: t.Logf}
	eps := []endpoints.Endpoint{{Name: "orders_api", Engine: "process", Address: "127.0.0.1:28080", Domain: "orders-api.demo.doze"}}
	routes := d.ingressRoutes(eps, &bindPlan{bind: map[string]string{}, resolve: map[string]net.IP{}})
	if len(routes) != 1 {
		t.Fatalf("routes = %v, want 1", routes)
	}
	if r, ok := routes["orders-api.demo.doze"]; !ok || r.Target != "127.0.0.1:28080" {
		t.Fatalf("route = %+v", routes)
	}
}

func TestIngressSurvivesFullDecode(t *testing.T) {
	src := `
name = "demo"
defaults { domains = true }
process "api" {
  command = "sleep 1"
  port    = 28080
  ingress = true
}
`
	cfg, err := config.Parse([]byte(src), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	decl := cfg.Lookup("api")
	if decl == nil {
		t.Fatal("api not declared")
	}
	pc, ok := decl.Spec.(*process.Config)
	if !ok {
		t.Fatalf("Spec is %T, want *process.Config", decl.Spec)
	}
	if !pc.Ingress {
		t.Fatal("ingress = true did not survive the decode")
	}
	if !pc.IngressEnabled() {
		t.Fatal("IngressEnabled() = false")
	}
	// core consumes `port` before the driver decodes — the DECL carries it.
	if decl.Port != 28080 {
		t.Fatalf("decl.Port = %d, want 28080", decl.Port)
	}
}
