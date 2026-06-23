package kvrocks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/engine"
)

func TestKvrocksBlockDecode(t *testing.T) {
	cfg, err := config.Parse([]byte(`kvrocks "store" {
  version  = "2.15.0"
  password = "pw"
}`), "doze.hcl")
	if err != nil {
		t.Fatal(err)
	}
	inst := cfg.Lookup("store")
	if inst == nil || inst.Type != "kvrocks" || inst.Version != "2.15.0" {
		t.Fatalf("inst = %+v", inst)
	}
	kc, ok := inst.Spec.(*Config)
	if !ok || kc.Password != "pw" {
		t.Errorf("spec = %+v", inst.Spec)
	}
}

func TestKvrocksNamespacesAndSettings(t *testing.T) {
	cfg, err := config.Parse([]byte(`kvrocks "store" {
  version  = "2.15.0"
  password = "default-token"
  workers  = 4
  namespace "tenant_a" { token = "tok-a" }
  settings = { "rocksdb.block_size" = "16384" }
}`), "doze.hcl")
	if err != nil {
		t.Fatal(err)
	}
	kc := cfg.Lookup("store").Spec.(*Config)
	if kc.Workers != 4 || len(kc.Namespaces) != 1 || kc.Namespaces[0].Token != "tok-a" {
		t.Errorf("decoded = %+v", kc)
	}
	// writeConf must emit the namespace and passthrough directives.
	path := filepath.Join(t.TempDir(), "kvrocks.conf")
	if err := writeConf(path, engine.Instance{Name: "store", DataDir: "/d", Spec: kc}, "/run/s.sock"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for _, want := range []string{"workers 4", "namespace.tenant_a tok-a", "rocksdb.block_size 16384"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("conf missing %q:\n%s", want, b)
		}
	}
}

func TestKvrocksNamespaceRequiresPassword(t *testing.T) {
	_, err := config.Parse([]byte(`kvrocks "store" {
  version = "2.15.0"
  namespace "a" { token = "t" }
}`), "doze.hcl")
	if err == nil || !strings.Contains(err.Error(), "password") {
		t.Errorf("expected password-required error, got %v", err)
	}
}

func TestKvrocksUnknownKey(t *testing.T) {
	_, err := config.Parse([]byte(`kvrocks "s" {
  version = "2.15.0"
  bogus   = "x"
}`), "doze.hcl")
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("expected unknown-key error, got %v", err)
	}
}

func TestWriteConf(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kvrocks.conf")
	inst := engine.Instance{Name: "store", DataDir: "/data/store", Spec: &Config{Password: "pw"}}
	if err := writeConf(path, inst, "/run/store/kvrocks.sock"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	conf := string(b)
	for _, want := range []string{"dir /data/store", "bind\n", "port 6666", "unixsocket /run/store/kvrocks.sock", "requirepass pw"} {
		if !strings.Contains(conf, want) {
			t.Errorf("conf missing %q:\n%s", want, conf)
		}
	}
	if strings.Contains(conf, "port 0") {
		t.Errorf("conf must not set port 0 (kvrocks rejects it):\n%s", conf)
	}
}

func TestConnString(t *testing.T) {
	d := Driver{}
	ep := engine.Endpoint{TCPAddr: "127.0.0.1:6500"}
	if v, url := d.ConnString(engine.Instance{Name: "store"}, ep); v != "REDIS_URL" || url != "redis://127.0.0.1:6500/0" {
		t.Errorf("no-auth = %q %q", v, url)
	}
	if _, url := d.ConnString(engine.Instance{Name: "store", Spec: &Config{Password: "pw"}}, ep); url != "redis://:pw@127.0.0.1:6500/0" {
		t.Errorf("auth = %q", url)
	}
}
