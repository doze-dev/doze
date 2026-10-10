// Package process implements the doze engine.Driver for application processes —
// a Go HTTP API, a Bun consumer, a Temporal dev server — run directly on the host
// with no Docker or virtualization. Unlike the database/AWS engines (which doze
// proxies and idle-reaps), a process is a long-lived, supervised client of those
// backends: it binds its own port, is exempt from the idle reaper, restarts per a
// policy on unexpected exit, and gates readiness on a health probe.
package process

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/runspec"
)

func init() { engine.Register(Driver{}) }

// Compile-time checks that the driver provides the capabilities the runtime and
// config discover by type assertion.
var (
	_ engine.Driver        = Driver{}
	_ engine.Spawner       = Driver{}
	_ engine.Versionless   = Driver{}
	_ engine.Lifecycle     = Driver{}
	_ engine.PortBinder    = Driver{}
	_ engine.Restartable   = Driver{}
	_ engine.HealthChecker = Driver{}
	_ engine.Attributer    = Driver{}
	_ engine.ConfigDecoder = Driver{}
)

// Driver is the process engine driver.
type Driver struct{}

// Type implements engine.Driver.
func (Driver) Type() string { return "process" }

// Versionless implements engine.Versionless: a process has no doze-managed
// toolchain version (v1 runs go/bun/node from PATH).
func (Driver) Versionless() {}

// Supervised implements engine.Lifecycle: a process is long-lived and exempt from
// the idle reaper.
func (Driver) Supervised(engine.Instance) bool { return true }

// Proxied reports whether doze fronts this app: it declares a `port` and has
// not set `direct`. doze then holds the declared port itself, awake or asleep,
// and the app listens on a private one it is given as $PORT.
func (Driver) Proxied(inst engine.Instance) bool {
	cfg, ok := inst.Spec.(*Config)
	return ok && cfg.Proxied()
}

// BootBudget implements engine.SlowBooter. A connection to a sleeping app
// wakes it, and waking a `go run` or a bundler means compiling first: that is
// minutes, not the seconds a database takes.
func (Driver) BootBudget() time.Duration { return 5 * time.Minute }

// AdvertisedAddr implements engine.PortBinder: the app binds its own port, so its
// endpoint address is that port (no doze proxy listener). ok is false for a worker
// with no port.
func (Driver) AdvertisedAddr(inst engine.Instance) (string, bool) {
	cfg, ok := inst.Spec.(*Config)
	if !ok || cfg.Port == 0 {
		return "", false
	}
	return "127.0.0.1:" + strconv.Itoa(cfg.Port), true
}

// RestartPolicy implements engine.Restartable.
func (Driver) RestartPolicy(inst engine.Instance) engine.RestartSpec {
	cfg, ok := inst.Spec.(*Config)
	if !ok {
		return engine.RestartSpec{Policy: engine.RestartNo}
	}
	return engine.RestartSpec{
		Policy:     cfg.Restart.Policy,
		Backoff:    cfg.Restart.Backoff,
		MaxRetries: cfg.Restart.MaxRetries,
	}
}

// Resolve implements engine.Driver: nothing to fetch — the toolchain is on PATH.
func (Driver) Resolve(context.Context, engine.VersionSpec, engine.Platform, engine.Locker, engine.Fetcher) (engine.Toolchain, error) {
	return engine.Toolchain{Engine: "process", Full: "builtin"}, nil
}

// Provision implements engine.Driver: a process has no data store to initialize;
// just ensure the per-instance dir exists (for any future log/pid files).
func (Driver) Provision(_ context.Context, inst engine.Instance, _ engine.Toolchain) error {
	return os.MkdirAll(inst.DataDir, 0o700)
}

// Provisioned implements engine.Driver.
func (Driver) Provisioned(dataDir string) bool {
	fi, err := os.Stat(dataDir)
	return err == nil && fi.IsDir()
}

// Plan implements engine.Spawner: a process is a single supervised spec — run the
// command via `sh -c` in its dir with the merged environment, killed as a process
// group, gated on its health probe (or a brief liveness check for a worker). Core
// executes and supervises it.
func (Driver) Plan(_ context.Context, inst engine.Instance, _ engine.Toolchain) (engine.SpawnPlan, error) {
	cfg, ok := inst.Spec.(*Config)
	if !ok {
		return engine.SpawnPlan{}, fmt.Errorf("app %q: missing config", inst.Name)
	}
	if fi, err := os.Stat(cfg.workDir()); err != nil || !fi.IsDir() {
		return engine.SpawnPlan{}, fmt.Errorf("app %q: dir %q does not exist", inst.Name, cfg.workDir())
	}
	return engine.SpawnPlan{Specs: []engine.SpawnSpec{{
		Name:  inst.Name,
		Dir:   cfg.workDir(),
		Bin:   "sh",
		Args:  []string{"-c", cfg.Run},
		Env:   cfg.envFor(inst),
		Tree:  true, // reap `go run`/`bun` and all children as a group
		Ready: cfg.readySpec(cfg.listenPort(inst)),
	}}}, nil
}

// readySpec translates `ready` into a core readiness probe. With no `ready`, an
// app that declares a port is ready when the port accepts a connection, and a
// portless one (a worker) gets nil: a brief liveness check.
func (c *Config) readySpec(port int) *engine.Ready {
	probe := c.Ready
	if probe == nil {
		if port == 0 {
			return nil
		}
		probe = &runspec.Probe{Kind: "tcp"}
	}
	timeout := c.ReadyTimeout
	if timeout <= 0 {
		timeout = runspec.DefaultReadyTimeout
	}
	kind, target := probe.Target(port)
	return &engine.Ready{
		Kind:     kind,
		Target:   target,
		Interval: readyInterval,
		Timeout:  readyProbeTimeout,
		Retries:  int(timeout / readyInterval),
	}
}

// BackendSocket implements engine.Driver. An app has no unix socket: what doze
// forwards to is the private TCP port the app was started on.
func (Driver) BackendSocket(_ string, port int) string {
	return net.JoinHostPort(PrivateHost, strconv.Itoa(port))
}

// PrivateHost is where a proxied app listens, and what it is given as $HOST.
const PrivateHost = "127.0.0.1"

// ConnString implements engine.Driver: nothing connects *to* a process through a
// conventional doze env var, so it contributes none. Other instances reference its
// address via the process.<name>.url attribute (see Attributes).
func (Driver) ConnString(engine.Instance, engine.Endpoint) (string, string) { return "", "" }

// listenPort is the port this app listens on: the private one doze picked for
// this start when the app is proxied, the declared one when it binds for itself.
func (c *Config) listenPort(inst engine.Instance) int {
	if c.Proxied() {
		return inst.Port
	}
	return c.declaredPort
}

// envFor is the environment an app starts with: what it declares, plus the
// three things doze tells every command. DATA is a folder of the app's own that
// survives restarts. PORT and HOST are where to listen, and are set only when
// doze fronts the app — an app that binds for itself already knows its port.
func (c *Config) envFor(inst engine.Instance) []string {
	env := c.mergedEnv()
	if inst.DataDir != "" {
		env = append(env, "DATA="+inst.DataDir)
	}
	if c.Proxied() {
		env = append(env, "PORT="+strconv.Itoa(inst.Port), "HOST="+PrivateHost)
	}
	return env
}

// mergedEnv builds the declared part of the child environment, lowest
// precedence first: the daemon's own environment (PATH, HOME, …), then the
// env_file, then the explicit env{} block (which can reference peers, e.g.
// DATABASE_URL = module.app.url).
func (c *Config) mergedEnv() []string {
	merged := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			merged[k] = v
		}
	}
	for k, v := range parseEnvFile(c.EnvFile) {
		merged[k] = v
	}
	for k, v := range c.Env {
		merged[k] = v
	}
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// parseEnvFile reads KEY=VALUE lines from path (ignoring blanks and # comments),
// stripping optional surrounding quotes. A missing/empty path yields nothing.
func parseEnvFile(path string) map[string]string {
	out := map[string]string{}
	if path == "" {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out
}
