package serve

import (
	"strings"
	"testing"

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
