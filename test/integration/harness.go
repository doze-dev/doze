//go:build integration

// Package integration is doze's end-to-end suite: it boots the REAL doze CLI
// against REAL engine plugins and REAL backend binaries, then drives a real client
// and asserts behaviour. Unlike the unit tests (which use fake drivers), this is
// the layer that proves core -> plugin -> binary -> client works together — and
// that it works on Linux, when run there in CI.
//
// Run:
//
//	go test -tags integration ./test/integration/...
//
// Requirements (documented, not magic):
//   - A sibling doze-modules checkout, so plugins can be built from source.
//     Override with DOZE_MODULES_DIR; defaults to ../../../doze-modules.
//   - Each engine's backend binary. To avoid a network fetch, point
//     DOZE_<ENGINE>_BINDIR at a local build (e.g. DOZE_POSTGRES_BINDIR); otherwise
//     doze fetches it from the registry, which needs network.
package integration

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/netutil"
)

// FreePort returns an available loopback TCP port for an instance's client-facing
// listener (declared as `port =` in config).
func FreePort(t *testing.T) int {
	port, err := netutil.FreePort()
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	return port
}

// modulesDir locates the doze-modules checkout (for building plugins from source).
func modulesDir(t *testing.T) string {
	if d := os.Getenv("DOZE_MODULES_DIR"); d != "" {
		return d
	}
	// test/integration -> repo root is ../.. ; doze-modules is a sibling of the repo.
	d, err := filepath.Abs(filepath.Join("..", "..", "..", "doze-modules"))
	if err != nil {
		t.Fatalf("resolving doze-modules dir: %v", err)
	}
	if _, err := os.Stat(d); err != nil {
		t.Skipf("doze-modules not found at %s (set DOZE_MODULES_DIR): %v", d, err)
	}
	return d
}

var (
	dozeBinOnce sync.Once
	dozeBinPath string
	dozeBinErr  error
)

// buildDoze compiles the doze CLI once per test binary and returns its path.
func buildDoze(t *testing.T) string {
	dozeBinOnce.Do(func() {
		out := filepath.Join(t.TempDir(), "doze")
		// The repo root is two levels up from test/integration.
		cmd := exec.Command("go", "build", "-o", out, "./cmd/doze")
		cmd.Dir = repoRoot(t)
		if b, err := cmd.CombinedOutput(); err != nil {
			dozeBinErr = err
			t.Logf("building doze: %v\n%s", err, b)
			return
		}
		dozeBinPath = out
	})
	if dozeBinErr != nil {
		t.Fatalf("building doze CLI failed: %v", dozeBinErr)
	}
	return dozeBinPath
}

func repoRoot(t *testing.T) string {
	d, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	return d
}

// buildPlugin compiles a module plugin from the doze-modules checkout and returns
// the binary path, for the DOZE_<ENGINE>_PLUGIN override.
func buildPlugin(t *testing.T, module string) string {
	out := filepath.Join(t.TempDir(), module+"-plugin")
	cmd := exec.Command("go", "build", "-o", out, "./modules/"+module+"/plugin")
	cmd.Dir = modulesDir(t)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building %s plugin: %v\n%s", module, err, b)
	}
	return out
}

// Project is one isolated doze project: its own config dir + DOZE_HOME, so the
// daemon, state, and data never collide with the developer's real setup.
type Project struct {
	t      *testing.T
	dir    string // holds doze.hcl
	home   string // DOZE_HOME (daemon socket, state, data)
	doze   string // the built doze CLI
	env    []string
	engine string
	name   string // instance name
	port   int
}

// NewProject writes cfg to a temp project, builds doze + the engine's plugin, and
// wires the plugin override. The caller boots it with Up and tears it down with
// Cleanup (registered via t.Cleanup).
func NewProject(t *testing.T, engine, name string, port int, cfg string) *Project {
	dir := t.TempDir()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doze.hcl"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	p := &Project{
		t: t, dir: dir, home: home, doze: buildDoze(t),
		engine: engine, name: name, port: port,
	}
	p.env = append(os.Environ(),
		"DOZE_HOME="+home,
		"HOME="+home,
		"DOZE_"+strings.ToUpper(engine)+"_PLUGIN="+buildPlugin(t, engine),
	)
	t.Cleanup(p.Cleanup)
	return p
}

// SetEnv adds an environment variable for the doze process (e.g. a BINDIR override).
func (p *Project) SetEnv(k, v string) { p.env = append(p.env, k+"="+v) }

// WriteConfig rewrites the project's doze.hcl — used to simulate a config edit
// between boots (e.g. adding a role) in drift/reconverge tests.
func (p *Project) WriteConfig(cfg string) {
	if err := os.WriteFile(filepath.Join(p.dir, "doze.hcl"), []byte(cfg), 0o644); err != nil {
		p.t.Fatalf("writing config: %v", err)
	}
}

// WriteFile writes an auxiliary file (e.g. a seed) into the project dir, at a path
// the config can reference relative to doze.hcl.
func (p *Project) WriteFile(name, content string) {
	if err := os.WriteFile(filepath.Join(p.dir, name), []byte(content), 0o644); err != nil {
		p.t.Fatalf("writing %s: %v", name, err)
	}
}

// doze runs a doze subcommand in the project, returning combined output.
func (p *Project) dozeCmd(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, p.doze, args...)
	cmd.Dir = p.dir
	cmd.Env = p.env
	b, err := cmd.CombinedOutput()
	return string(b), err
}

// Up boots (and converges) the project, failing the test on error.
func (p *Project) Up() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if out, err := p.dozeCmd(ctx, "up"); err != nil {
		p.t.Fatalf("doze up failed: %v\n%s", err, out)
	}
}

// Cleanup tears the project down (best-effort) so the daemon does not linger.
func (p *Project) Cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = p.dozeCmd(ctx, "down")
}

// ProxyAddr is the client-facing address doze binds for the instance (the port
// declared in config). Clients connect here; doze proxies to the backend.
func (p *Project) ProxyAddr() string { return "127.0.0.1:" + strconv.Itoa(p.port) }

// run executes an external client tool (psql, mariadb, …), returning its output.
func run(t *testing.T, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	b, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(b)), err
}

// tool resolves a client binary from an engine BINDIR (…/bin/<tool>) if set, else
// falls back to PATH — so the suite prefers the exact toolchain doze uses.
func tool(binDirEnv, name string) string {
	if d := os.Getenv(binDirEnv); d != "" {
		p := filepath.Join(d, "bin", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return name
}

// mustTool is tool, but skips the test if the client isn't in the BINDIR or on
// PATH — so a machine lacking a client (e.g. mongosh) skips cleanly rather than
// failing spuriously.
func mustTool(t *testing.T, binDirEnv, name string) string {
	if d := os.Getenv(binDirEnv); d != "" {
		if p := filepath.Join(d, "bin", name); fileExists(p) {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	t.Skipf("client tool %q not found (set %s or install it)", name, binDirEnv)
	return ""
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// splitHostPort splits "host:port" into its parts for client tools that take them
// as separate flags (--host/--port).
func splitHostPort(t *testing.T, addr string) (host, port string) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("splitting %q: %v", addr, err)
	}
	return h, p
}
