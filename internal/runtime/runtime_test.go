package runtime_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/journal"
	"github.com/doze-dev/doze/internal/registry"
	"github.com/doze-dev/doze/internal/runtime"
)

// fakeProc is a no-op engine.Process for tests. Wait blocks until Stop, like a
// real backend, so the runtime's crash watcher only fires on an actual exit.
type fakeProc struct {
	stopped atomic.Bool
	done    chan struct{}
}

func newFakeProc() *fakeProc { return &fakeProc{done: make(chan struct{})} }

func (p *fakeProc) PID() int       { return 4242 }
func (p *fakeProc) Alive() bool    { return !p.stopped.Load() }
func (p *fakeProc) Logs() []string { return nil }
func (p *fakeProc) Stop(context.Context) error {
	if p.stopped.CompareAndSwap(false, true) {
		close(p.done)
	}
	return nil
}
func (p *fakeProc) Wait() error { <-p.done; return nil }

// leafDriver is a trivial engine that provisions/boots without real work and can
// serve as a backend dependency.
type leafDriver struct{}

func (leafDriver) Type() string { return "tleaf" }
func (leafDriver) Resolve(context.Context, engine.VersionSpec, engine.Platform, engine.Locker, engine.Fetcher) (engine.Toolchain, error) {
	return engine.Toolchain{Engine: "tleaf"}, nil
}
func (leafDriver) Provision(context.Context, engine.Instance, engine.Toolchain) error { return nil }
func (leafDriver) Provisioned(string) bool                                            { return true }
func (leafDriver) Spawn(context.Context, engine.Instance, engine.Toolchain) (engine.Process, error) {
	return newFakeProc(), nil
}
func (leafDriver) WaitReady(context.Context, engine.Instance, engine.Toolchain, engine.Process) error {
	return nil
}
func (leafDriver) BackendSocket(dir string, _ int) string                       { return dir + "/leaf.sock" }
func (leafDriver) ConnString(engine.Instance, engine.Endpoint) (string, string) { return "", "" }
func (leafDriver) BackendURL(inst engine.Instance) string                       { return "fake://" + inst.Name }

// depDriver is a second engine type used to verify that a reference to another
// instance (tleaf.base) is what makes the runtime boot and hold that dependency —
// no hand-declared DependsOn; the edge is derived from the config graph.
type depDriver struct{ leafDriver }

func (depDriver) Type() string { return "tdep" }

func TestDependencyBootAndHold(t *testing.T) {
	t.Setenv("DOZE_HOME", t.TempDir())
	engine.Register(leafDriver{})
	engine.Register(depDriver{})

	cfg, err := config.Parse([]byte(`
module tleaf "base" { version = 1 }
module tdep "app" {
  version = 1
  base    = module.base.name
}
`), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := runtime.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.EnsureDataRoot(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := rt.Boot(ctx, "app"); err != nil {
		t.Fatalf("boot app: %v", err)
	}
	// The dependency was booted and is held running (a connection count from app).
	base, ok := rt.Registry().Get("base")
	if !ok || base.State != registry.Active || base.Conns < 1 {
		t.Fatalf("base should be booted and held active: %+v", base)
	}
	if app, _ := rt.Registry().Get("app"); app.State == registry.Reaped {
		t.Fatalf("app should be running: %+v", app)
	}

	// Stopping the dependent releases its hold on the dependency.
	if err := rt.Stop(ctx, "app"); err != nil {
		t.Fatal(err)
	}
	if base, _ = rt.Registry().Get("base"); base.Conns != 0 {
		t.Fatalf("base hold should be released after app stops: %+v", base)
	}
}

// TestBackendCrashDetected verifies the crash watcher: an unexpected backend
// exit (not an intentional Stop) flips the instance to reaped and records the
// failure, so the next connect re-boots cleanly.
func TestBackendCrashDetected(t *testing.T) {
	t.Setenv("DOZE_HOME", t.TempDir())
	engine.Register(leafDriver{})

	cfg, err := config.Parse([]byte("module tleaf \"solo\" { version = 1 }\n"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := runtime.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := rt.Boot(ctx, "solo"); err != nil {
		t.Fatal(err)
	}
	fp, ok := rt.Backend("solo").(*fakeProc)
	if !ok {
		t.Fatalf("expected a fakeProc backend, got %T", rt.Backend("solo"))
	}
	close(fp.done) // simulate an unexpected exit (a crash), not a Stop

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if inst, _ := rt.Registry().Get("solo"); inst.State == registry.Failed && inst.LastError != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("crash was not detected: instance not marked failed with an error")
}

// loopProc is a process that exits by itself, with status 1, as soon as it starts.
type loopProc struct{ fakeProc }

func (p *loopProc) Wait() error { return errCrash }

var errCrash = errorString("exit status 1")

type errorString string

func (e errorString) Error() string { return string(e) }

// loopDriver is an always-on engine whose process crashes at once, with a
// restart budget of two.
type loopDriver struct{ leafDriver }

func (loopDriver) Type() string                    { return "tloop" }
func (loopDriver) Supervised(engine.Instance) bool { return true }
func (loopDriver) Spawn(context.Context, engine.Instance, engine.Toolchain) (engine.Process, error) {
	return &loopProc{}, nil
}
func (loopDriver) RestartPolicy(engine.Instance) engine.RestartSpec {
	return engine.RestartSpec{Policy: engine.RestartOnFailure, Backoff: 20 * time.Millisecond, MaxRetries: 2}
}

// A process that keeps dying is `crashing`, with the attempt and the budget,
// while a restart is pending — and `failed` once the budget is spent.
func TestCrashLoopIsCrashingThenFailed(t *testing.T) {
	t.Setenv("DOZE_HOME", t.TempDir())
	engine.Register(loopDriver{})
	cfg, err := config.Parse([]byte("module tloop \"api\" { version = 1 }\n"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := runtime.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.EnsureDataRoot(); err != nil {
		t.Fatal(err)
	}
	events, cancel := rt.Registry().Subscribe(64)
	defer cancel()
	if _, err := rt.Boot(context.Background(), "api"); err != nil {
		t.Fatalf("boot: %v", err)
	}

	var sawCrashing registry.Instance
	deadline := time.After(5 * time.Second)
	for {
		select {
		case inst := <-events:
			switch inst.State {
			case registry.Crashing:
				if sawCrashing.Name == "" {
					sawCrashing = inst
				}
			case registry.Failed:
				if sawCrashing.Name == "" {
					t.Fatal("failed without ever being crashing")
				}
				if sawCrashing.RestartCount != 1 || sawCrashing.RestartMax != 2 || sawCrashing.NextRestart.IsZero() {
					t.Errorf("first crash = attempt %d of %d, next %v; want 1 of 2 with a time",
						sawCrashing.RestartCount, sawCrashing.RestartMax, sawCrashing.NextRestart)
				}
				if inst.PID != 0 || inst.LastError == "" {
					t.Errorf("failed instance = %+v", inst)
				}
				return
			}
		case <-deadline:
			got, _ := rt.Registry().Get("api")
			t.Fatalf("never reached failed; last state %s (%s)", got.State, got.LastError)
		}
	}
}

// An always-on instance with nobody connected is active, not idle: nothing is
// counting down to stop it.
func TestAlwaysOnIsActiveWithNoConnections(t *testing.T) {
	t.Setenv("DOZE_HOME", t.TempDir())
	engine.Register(steadyDriver{})
	engine.Register(leafDriver{})
	cfg, err := config.Parse([]byte("module tsteady \"web\" { version = 1 }\nmodule tleaf \"db\" { version = 1 }\n"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := runtime.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.EnsureDataRoot(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"web", "db"} {
		if _, err := rt.Boot(context.Background(), name); err != nil {
			t.Fatalf("boot %s: %v", name, err)
		}
	}
	if web, _ := rt.Registry().Get("web"); web.State != registry.Active {
		t.Errorf("an always-on instance with no connections is %s, want active", web.State)
	}
	if db, _ := rt.Registry().Get("db"); db.State != registry.Idle {
		t.Errorf("a module with no connections is %s, want idle", db.State)
	}
	_ = rt.Stop(context.Background(), "web")
	_ = rt.Stop(context.Background(), "db")
}

type steadyDriver struct{ leafDriver }

func (steadyDriver) Type() string                    { return "tsteady" }
func (steadyDriver) Supervised(engine.Instance) bool { return true }

// Why something is awake: its dependants by name, a pin, and client
// connections — with the dependants' own holds not counted as clients.
func TestHolders(t *testing.T) {
	t.Setenv("DOZE_HOME", t.TempDir())
	engine.Register(leafDriver{})
	engine.Register(depDriver{})
	cfg, err := config.Parse([]byte(`
module tleaf "db" { version = 1 }
module tdep "api" {
  version = 1
  base    = module.db.name
}
`), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := runtime.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.EnsureDataRoot(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if got := rt.Holders("db"); got != nil {
		t.Fatalf("an instance that is not running has no holders, got %+v", got)
	}
	if _, err := rt.Boot(ctx, "api"); err != nil {
		t.Fatalf("boot: %v", err)
	}
	want := func(when string, holders ...runtime.Holder) {
		t.Helper()
		got := rt.Holders("db")
		if len(got) != len(holders) {
			t.Fatalf("%s: holders = %+v, want %+v", when, got, holders)
		}
		for i := range got {
			if got[i] != holders[i] {
				t.Fatalf("%s: holders = %+v, want %+v", when, got, holders)
			}
		}
	}
	want("held by a dependant", runtime.Holder{Kind: runtime.HolderDependant, Name: "api"})

	rt.Acquire("db")
	rt.Acquire("db")
	rt.ToggleKeepAwake("db")
	want("plus two clients and a pin",
		runtime.Holder{Kind: runtime.HolderDependant, Name: "api"},
		runtime.Holder{Kind: runtime.HolderPin},
		runtime.Holder{Kind: runtime.HolderConnection, Count: 2})

	if err := rt.Stop(ctx, "api"); err != nil {
		t.Fatal(err)
	}
	want("the dependant gone",
		runtime.Holder{Kind: runtime.HolderPin},
		runtime.Holder{Kind: runtime.HolderConnection, Count: 2})
	if got := rt.Holders("api"); got != nil {
		t.Fatalf("a stopped instance has no holders, got %+v", got)
	}
	_ = rt.Stop(ctx, "db")
}

// The journal says what woke each instance and how long it took: the wake
// trace, as far as declared references go.
func TestJournalRecordsCauseAndDuration(t *testing.T) {
	t.Setenv("DOZE_HOME", t.TempDir())
	engine.Register(leafDriver{})
	engine.Register(depDriver{})
	cfg, err := config.Parse([]byte(`
module tleaf "db" { version = 1 }
module tdep "api" {
  version = 1
  base    = module.db.name
}
`), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := runtime.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.EnsureDataRoot(); err != nil {
		t.Fatal(err)
	}
	j, err := journal.Open(filepath.Join(t.TempDir(), "journal.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	rt.SetJournal(j)

	ctx := journal.WithCause(context.Background(), journal.ByConnection, "")
	if _, err := rt.Boot(ctx, "api"); err != nil {
		t.Fatalf("boot: %v", err)
	}
	rt.ToggleKeepAwake("db")
	if err := rt.Stop(journal.WithCause(context.Background(), journal.ByIdle, ""), "api"); err != nil {
		t.Fatal(err)
	}

	events, err := j.Since(0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range events {
		line := e.Kind + " " + e.Instance
		if e.Cause.Kind != "" {
			line += " by " + e.Cause.Kind
			if e.Cause.By != "" {
				line += ":" + e.Cause.By
			}
		}
		if e.Detail != "" {
			line += " (" + e.Detail + ")"
		}
		got = append(got, line)
		if e.Kind == journal.Ready && e.Took <= 0 {
			t.Errorf("%s has no duration", line)
		}
	}
	want := []string{
		"wake api by connection",
		"wake db by dependency:api",
		"ready db",
		"ready api",
		"pin db by command (on)",
		"sleep api by idle",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("journal:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	_ = rt.Stop(context.Background(), "db")
}
