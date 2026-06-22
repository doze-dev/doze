package ferretdb

import (
	"strings"
	"testing"

	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/engine"
)

func TestFerretdbBlockDecode(t *testing.T) {
	cfg, err := config.Parse([]byte(`ferretdb "events" {
  version = "2.7.0"
  backend = "ferret_pg"
}`), "doze.hcl")
	if err != nil {
		t.Fatal(err)
	}
	inst := cfg.Lookup("events")
	if inst == nil || inst.Type != "ferretdb" || inst.Version != "2.7.0" {
		t.Fatalf("inst = %+v", inst)
	}
	fc, ok := inst.Spec.(*Config)
	if !ok || fc.Backend != "ferret_pg" {
		t.Fatalf("spec = %+v", inst.Spec)
	}
	if deps := (Driver{}).DependsOn(engine.Instance{Spec: inst.Spec}); len(deps) != 1 || deps[0] != "ferret_pg" {
		t.Errorf("DependsOn = %v", deps)
	}
}

func TestBackendRequired(t *testing.T) {
	_, err := config.Parse([]byte(`ferretdb "events" { version = "2.7.0" }`), "doze.hcl")
	if err == nil || !strings.Contains(err.Error(), "backend") {
		t.Errorf("expected backend-required error, got %v", err)
	}
}

func TestConnString(t *testing.T) {
	_, url := (Driver{}).ConnString(engine.Instance{Name: "events"}, engine.Endpoint{TCPAddr: "127.0.0.1:6700"})
	if url != "mongodb://127.0.0.1:6700/" {
		t.Errorf("conn = %q", url)
	}
}
