package runtime_test

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/runtime"
)

// deafDriver starts an app that is given $PORT and never listens on it.
type deafDriver struct{ leafDriver }

// deafPort is a port nothing listens on, chosen once for the test binary.
var deafPort = func() int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 1
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}()

func (deafDriver) Type() string { return "tdeaf" }
func (deafDriver) Plan(context.Context, engine.Instance, engine.Toolchain) (engine.SpawnPlan, error) {
	addr := "127.0.0.1:" + strconv.Itoa(deafPort)
	return engine.SpawnPlan{Specs: []engine.SpawnSpec{{
		Name: "web", Bin: "sh", Args: []string{"-c", "sleep 30"},
		Env:  []string{"PORT=" + strconv.Itoa(deafPort)},
		Tree: true,
		Ready: &engine.Ready{
			Kind: "tcp", Target: addr,
			Interval: 50 * time.Millisecond, Timeout: 50 * time.Millisecond, Retries: 4,
		},
	}}}, nil
}

// An app that never listens on $PORT fails its start with a sentence that
// says so, not just a timeout.
func TestAnAppDeafToPortIsToldWhy(t *testing.T) {
	t.Setenv("DOZE_HOME", t.TempDir())
	cfg, err := config.Parse([]byte("module tdeaf \"web\" { version = 1 }\n"), "", nil)
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
	_, err = rt.Boot(context.Background(), "web")
	want := "web started but listens on no port; doze waited for $PORT (" + strconv.Itoa(deafPort) + ")"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("boot error = %v, want it to say %q", err, want)
	}
}
