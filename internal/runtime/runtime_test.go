package runtime_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/registry"
	"github.com/nerdmenot/doze/internal/runtime"
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

// depDriver depends on the instance named "base".
type depDriver struct{ leafDriver }

func (depDriver) Type() string                       { return "tdep" }
func (depDriver) DependsOn(engine.Instance) []string { return []string{"base"} }

func TestDependencyBootAndHold(t *testing.T) {
	t.Setenv("DOZE_HOME", t.TempDir())
	engine.Register(leafDriver{})
	engine.Register(depDriver{})

	cfg, err := config.Parse([]byte("tleaf \"base\" { version = 1 }\ntdep \"app\" { version = 1 }\n"), "")
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

	cfg, err := config.Parse([]byte("tleaf \"solo\" { version = 1 }\n"), "")
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
		if inst, _ := rt.Registry().Get("solo"); inst.State == registry.Reaped && inst.LastError != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("crash was not detected: instance not marked reaped with an error")
}
