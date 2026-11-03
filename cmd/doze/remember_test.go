package main

import (
	"slices"
	"testing"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/projects"
)

// The shop: api needs db; worker needs db; mail stands alone.
func rememberStack(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("DOZE_HOME", t.TempDir())
	cfg, err := config.Parse([]byte(`
app "db" {
  run = "sleep 60"
}
app "api" {
  run   = "sleep 60"
  after = [app.db]
}
app "worker" {
  run   = "sleep 60"
  after = [app.db]
}
app "mail" {
  run = "sleep 60"
}
`), t.TempDir()+"/doze.hcl", nil)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDownRemembersTheRootsOfWhatWasAwake(t *testing.T) {
	cfg := rememberStack(t)
	snap := &control.Snapshot{Instances: []control.InstanceView{
		{Name: "db", State: "active"}, {Name: "api", State: "idle"}, {Name: "worker"}, {Name: "mail"},
	}}
	rememberDown(cfg, snap)
	if got := lastSet(cfg); !slices.Equal(got, []string{"api"}) {
		t.Fatalf("last set = %v, want [api]: db came with it", got)
	}
	// A down with the daemon not running keeps what was remembered.
	rememberDown(cfg, nil)
	if got := lastSet(cfg); !slices.Equal(got, []string{"api"}) {
		t.Fatalf("after a down with nothing running: %v", got)
	}
	// Everything awake is remembered as everything.
	for i := range snap.Instances {
		snap.Instances[i].State = "active"
	}
	rememberDown(cfg, snap)
	if got := lastSet(cfg); got != nil {
		t.Fatalf("everything awake should read as everything, got %v", got)
	}
}

func TestUpOffersLastTimesSet(t *testing.T) {
	cfg := rememberStack(t)
	saved := assumeYes
	t.Cleanup(func() { assumeYes = saved })

	rememberUp(cfg, []string{"api", "mail"})
	assumeYes = false
	// No terminal in a test: everything, with a hint.
	if got, _ := chooseUpTargets(cfg, false, false); len(got) != 4 {
		t.Fatalf("no terminal: %v, want everything", got)
	}
	if got, _ := chooseUpTargets(cfg, true, false); !slices.Equal(got, []string{"api", "mail"}) {
		t.Fatalf("--last: %v", got)
	}
	if got, _ := chooseUpTargets(cfg, false, true); len(got) != 4 {
		t.Fatalf("--all: %v", got)
	}
	assumeYes = true
	if got, _ := chooseUpTargets(cfg, false, false); !slices.Equal(got, []string{"api", "mail"}) {
		t.Fatalf("-y takes last time's set: %v", got)
	}
	// A name no longer declared is dropped, not started.
	_ = projects.Update(cfg.ProjectDir(), func(p *projects.Project) { p.LastSet = []string{"gone", "mail"} })
	if got := lastSet(cfg); !slices.Equal(got, []string{"mail"}) {
		t.Fatalf("with a removed name: %v", got)
	}
	// Starting everything records everything.
	rememberUp(cfg, enabledNames(cfg))
	if got := lastSet(cfg); got != nil {
		t.Fatalf("after up everything: %v", got)
	}
}
