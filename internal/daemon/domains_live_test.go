package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	names "github.com/doze-dev/doze-names"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/runtime"
)

// A domain added to a running stack gets what one declared at the start
// gets: its record for the routes, the authority, and the authority in the
// environment of every process spawned from then on. A stack with no
// domain makes no authority.
func TestADomainAddedLiveGetsTheRecordAndTheAuthority(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DOZE_HOME", home)
	project := t.TempDir()
	path := filepath.Join(project, "doze.hcl")
	if err := os.WriteFile(path, []byte("stack = \"shop\"\ndefaults { names = true }\napp \"web\" {\n  run  = \"sleep 60\"\n  port = 28081\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Home = home
	rt, err := runtime.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{cfg: cfg, rt: rt, logf: t.Logf, zone: names.Open(home, "doze")}

	d.ensureDomains(false)
	if _, err := os.Stat(filepath.Join(names.CADir(home), "root.crt")); err == nil {
		t.Fatal("a stack with no domain must not make an authority")
	}
	if len(rt.ExtraEnv()) != 0 {
		t.Fatalf("no domain, no authority in the environment: %v", rt.ExtraEnv())
	}

	// doze add, with a domain, on the running stack.
	web := cfg.Lookup("web")
	web.Domains = []string{"dev.acme.com", "*.dev.acme.com"}
	d.ensureDomains(true)

	if c := names.Claimants(home, "dev.acme.com"); len(c) != 1 || c[0] != d.projectDir() {
		t.Errorf("the record names this project for the route: %v", c)
	}
	if _, err := os.Stat(filepath.Join(names.CADir(home), "root.crt")); err != nil {
		t.Error("the authority is made for the added domain")
	}
	env := strings.Join(rt.ExtraEnv(), " ")
	if !strings.Contains(env, "DOZE_CA_FILE="+filepath.Join(names.CADir(home), "root.crt")) || !strings.Contains(env, "SSL_CERT_FILE="+filepath.Join(names.CADir(home), "bundle.pem")) {
		t.Errorf("processes started from now on are told where the authority is: %v", rt.ExtraEnv())
	}
	// Dropping the domain again updates the record; the authority stays
	// (its trust is the person's to remove with doze trust --remove).
	web.Domains = nil
	d.ensureDomains(true)
	if c := names.Claimants(home, "dev.acme.com"); len(c) != 0 {
		t.Errorf("a dropped domain loses this project as its reason: %v", c)
	}
}
