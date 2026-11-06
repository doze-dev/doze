package serve

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/doze-dev/doze/engine/process" // apps load in doze.hcl
	"github.com/doze-dev/doze/internal/control/fixture"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/config"
)

func TestStartingSet(t *testing.T) {
	d := func(name, kind string, deps ...string) *config.InstanceDecl {
		x := &config.InstanceDecl{Name: name, Kind: kind, Enabled: true}
		for _, n := range deps {
			x.Deps = append(x.Deps, engine.Dependency{Name: n})
		}
		return x
	}
	decls := []*config.InstanceDecl{d("api", "app", "db"), d("web", "app", "api"), d("worker", "app"), d("db", "module"), d("migrate", "task", "db")}
	names := func(s starting) string {
		var out []string
		for _, x := range s.decls {
			out = append(out, x.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names(startingSet(decls, []string{"web"}, "up-last", nil)); got != "web,api,db" {
		t.Errorf("up-last web = %s", got)
	}
	if got := names(startingSet(decls, nil, "up-last", nil)); got != "api,db,web,worker,migrate" {
		t.Errorf("up-last with nothing remembered = %s", got)
	}
	if got := names(startingSet(decls, []string{"gone"}, "up-last", nil)); got != "api,db,web,worker,migrate" {
		t.Errorf("up-last with nothing still declared = %s", got)
	}
	s := startingSet(decls, nil, "up", []string{"web", "worker"})
	if !s.needed["api"] || s.needed["worker"] || s.needed["web"] {
		t.Errorf("needed = %v", s.needed)
	}
}

// Waking one service whose port another program holds asks first, naming
// the program; nothing holds it, nothing to say.
func TestPreflightInstance(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	dir := t.TempDir()
	hcl := fmt.Sprintf("stack = \"shop\"\n\napp \"search\" {\n  run  = \"true\"\n  port = %d\n}\n", port)
	if err := os.WriteFile(filepath.Join(dir, "doze.hcl"), []byte(hcl), 0o644); err != nil {
		t.Fatal(err)
	}
	f := fixture.Shop(epoch)
	f.Snap.Dir = dir
	h, key := newHub(t, f)
	s := New(h, nil)
	p := s.PreflightInstance(key, "search")
	if !p.Blocked || len(p.Checks) != 1 || !strings.Contains(p.Checks[0].Title, fmt.Sprintf("Port %d for search is taken", port)) {
		t.Fatalf("held port: %+v", p)
	}
	if fx := p.Checks[0].Fixes[0]; fx.Kind != "stop" || fx.PID != os.Getpid() {
		t.Errorf("first fix = %+v, want stop pid %d", fx, os.Getpid())
	}
	_ = l.Close()
	if p := s.PreflightInstance(key, "search"); p.Blocked {
		t.Errorf("free port, yet blocked: %+v", p)
	}
	// One already awake is not looked at.
	if p := s.PreflightInstance(key, "db"); p.Blocked || len(p.Checks) != 0 {
		t.Errorf("awake db: %+v", p)
	}
}
