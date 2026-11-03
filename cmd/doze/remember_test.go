package main

import (
	"slices"
	"strings"
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

func last(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	got, err := lastSet(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// up api, then api sleeps while idle and only db is awake at down: the set
// is still api (db comes with it), not db.
func TestDownNeverShrinksTheSet(t *testing.T) {
	cfg := rememberStack(t)
	rememberUp(cfg, []string{"api"}, nil)
	rememberDown(cfg, &control.Snapshot{Instances: []control.InstanceView{
		{Name: "db", State: "active"}, {Name: "api", State: "reaped"}, {Name: "worker", State: "reaped"}, {Name: "mail", State: "reaped"},
	}})
	if got := last(t, cfg); !slices.Equal(got, []string{"api"}) {
		t.Fatalf("after api slept: %v, want [api]", got)
	}
	// Woken since: mail joins.
	rememberDown(cfg, &control.Snapshot{Instances: []control.InstanceView{{Name: "db", State: "active"}, {Name: "mail", State: "active"}}})
	if got := last(t, cfg); !slices.Equal(got, []string{"api", "mail"}) {
		t.Fatalf("after mail was woken: %v", got)
	}
	// A down with nothing running keeps it.
	rememberDown(cfg, nil)
	if got := last(t, cfg); !slices.Equal(got, []string{"api", "mail"}) {
		t.Fatalf("after a down with nothing running: %v", got)
	}
}

// up worker on a stack already running api keeps api in the set.
func TestUpAddsToWhatIsRunning(t *testing.T) {
	cfg := rememberStack(t)
	rememberUp(cfg, []string{"worker"}, &control.Snapshot{Instances: []control.InstanceView{{Name: "db", State: "active"}, {Name: "api", State: "idle"}}})
	if got := last(t, cfg); !slices.Equal(got, []string{"api", "worker"}) {
		t.Fatalf("got %v", got)
	}
	// Naming one service twice on a two-root stack is not everything.
	rememberUp(cfg, []string{"api", "api"}, nil)
	if got := last(t, cfg); !slices.Equal(got, []string{"api"}) {
		t.Fatalf("api twice: %v", got)
	}
}

func TestUpOffersLastTimesSet(t *testing.T) {
	cfg := rememberStack(t)
	saved := assumeYes
	t.Cleanup(func() { assumeYes = saved })

	rememberUp(cfg, []string{"api", "mail"}, nil)
	for _, yes := range []bool{false, true} {
		// No terminal in a test: everything, -y or not.
		assumeYes = yes
		if got, _ := chooseUpTargets(cfg, false, false); len(got) != 4 {
			t.Fatalf("no terminal, -y %v: %v, want everything", yes, got)
		}
	}
	if got, _ := chooseUpTargets(cfg, true, false); !slices.Equal(got, []string{"api", "mail"}) {
		t.Fatalf("--last: %v", got)
	}
	if got, _ := chooseUpTargets(cfg, false, true); len(got) != 4 {
		t.Fatalf("--all: %v", got)
	}
	// A name no longer declared is dropped, not started.
	_ = projects.Update(cfg.ProjectDir(), func(p *projects.Project) { p.LastSet = []string{"gone", "mail"} })
	if got := last(t, cfg); !slices.Equal(got, []string{"mail"}) {
		t.Fatalf("with a removed name: %v", got)
	}
	// Starting everything records everything.
	rememberUp(cfg, enabledNames(cfg), nil)
	if got := last(t, cfg); got != nil {
		t.Fatalf("after up everything: %v", got)
	}
}

// End of input at the question is no; an answer is read as yes unless it
// says otherwise.
func TestAskYes(t *testing.T) {
	for in, want := range map[string]bool{"": false, "\n": true, "y\n": true, "n\n": false, "a\n": false} {
		if got, err := askYes(strings.NewReader(in)); err != nil || got != want {
			t.Errorf("answer %q: %v, %v; want %v", in, got, err, want)
		}
	}
}
