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
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/nerdmenot/doze/internal/engine"
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
	_ engine.Hooked        = Driver{}
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
// command via `sh -c` in its cwd with the merged environment, killed as a process
// group, gated on its health probe (or a brief liveness check for a worker). Core
// executes and supervises it.
func (Driver) Plan(_ context.Context, inst engine.Instance, _ engine.Toolchain) (engine.SpawnPlan, error) {
	cfg, ok := inst.Spec.(*Config)
	if !ok {
		return engine.SpawnPlan{}, fmt.Errorf("process %q: missing config", inst.Name)
	}
	if fi, err := os.Stat(cfg.Cwd); err != nil || !fi.IsDir() {
		return engine.SpawnPlan{}, fmt.Errorf("process %q: working dir %q does not exist", inst.Name, cfg.Cwd)
	}
	return engine.SpawnPlan{Specs: []engine.SpawnSpec{{
		Name:  inst.Name,
		Dir:   cfg.Cwd,
		Bin:   "sh",
		Args:  []string{"-c", cfg.Command},
		Env:   cfg.mergedEnv(inst.InjectedEnv),
		Tree:  true, // reap `go run`/`bun` and all children as a group
		Ready: cfg.readySpec(),
	}}}, nil
}

// readySpec translates the optional health block into a core readiness probe
// (nil → a brief liveness check, for a worker with no endpoint).
func (c *Config) readySpec() *engine.Ready {
	if c.Health == nil {
		return nil
	}
	return &engine.Ready{
		Kind:     c.Health.Kind,
		Target:   c.Health.Target,
		Interval: c.Health.Interval,
		Timeout:  c.Health.Timeout,
		Retries:  c.Health.Retries,
	}
}

// BackendSocket implements engine.Driver: a process is not proxied, so there is no
// doze backend socket.
func (Driver) BackendSocket(string, int) string { return "" }

// ConnString implements engine.Driver: nothing connects *to* a process through a
// conventional doze env var, so it contributes none. Other instances reference its
// address via the process.<name>.url attribute (see Attributes).
func (Driver) ConnString(engine.Instance, engine.Endpoint) (string, string) { return "", "" }

// mergedEnv builds the child environment, lowest precedence first: the daemon's
// own environment (PATH, HOME, …), then doze-injected connection vars, then the
// env_file, then the explicit env{} block.
func (c *Config) mergedEnv(injected map[string]string) []string {
	merged := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			merged[k] = v
		}
	}
	for k, v := range injected {
		merged[k] = v
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
