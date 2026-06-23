package valkey

import (
	"strings"
	"testing"

	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/engine"
)

func TestValkeyBlockDecode(t *testing.T) {
	cfg, err := config.Parse([]byte(`
valkey "cache" {
  version   = 8
  password  = "s3cret"
  maxmemory = "64mb"
}
`), "doze.hcl")
	if err != nil {
		t.Fatal(err)
	}
	inst := cfg.Lookup("cache")
	if inst == nil || inst.Type != "valkey" || inst.Version != "8" {
		t.Fatalf("inst = %+v", inst)
	}
	vc, ok := inst.Spec.(*Config)
	if !ok {
		t.Fatalf("spec is %T, want *valkey.Config", inst.Spec)
	}
	if vc.Password != "s3cret" || vc.Maxmemory != "64mb" {
		t.Errorf("decoded config = %+v", vc)
	}
}

func TestValkeyExtendedOptions(t *testing.T) {
	cfg, err := config.Parse([]byte(`
valkey "cache" {
  version          = 8
  maxmemory        = "64mb"
  maxmemory_policy = "allkeys-lru"
  appendonly       = true
  save             = "3600 1"
  settings = { "lazyfree-lazy-eviction" = "yes" }
}
`), "doze.hcl")
	if err != nil {
		t.Fatal(err)
	}
	vc := cfg.Lookup("cache").Spec.(*Config)
	if vc.MaxmemoryPolicy != "allkeys-lru" || !vc.Appendonly || vc.Save != "3600 1" {
		t.Errorf("extended options wrong: %+v", vc)
	}
	if vc.Settings["lazyfree-lazy-eviction"] != "yes" {
		t.Errorf("settings passthrough wrong: %+v", vc.Settings)
	}
}

func TestValkeyUnknownKey(t *testing.T) {
	_, err := config.Parse([]byte(`valkey "cache" {
  version = 8
  bogus   = "x"
}`), "doze.hcl")
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("expected unknown-key error, got %v", err)
	}
}

func TestConnString(t *testing.T) {
	d := Driver{}
	ep := engine.Endpoint{TCPAddr: "127.0.0.1:6400"}
	v, url := d.ConnString(engine.Instance{Name: "cache"}, ep)
	if v != "REDIS_URL" || url != "redis://127.0.0.1:6400/0" {
		t.Errorf("no-auth conn = %q %q", v, url)
	}
	_, url = d.ConnString(engine.Instance{Name: "cache", Spec: &Config{Password: "pw"}}, ep)
	if url != "redis://:pw@127.0.0.1:6400/0" {
		t.Errorf("auth conn = %q", url)
	}
}
