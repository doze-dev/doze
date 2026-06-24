package process

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"

	"github.com/nerdmenot/doze/internal/engine"
)

// Logf is the sink for toolchain-mismatch warnings; cmd/doze points it at stderr.
var Logf = func(string, ...any) {}

// Config is the decoded `process "<name>" { … }` block.
type Config struct {
	Cwd     string            // absolute working directory (resolved against the declaring file)
	Command string            // the shell command line, run via `sh -c`
	Port    int               // the port the app binds; 0 if it has none (a worker)
	Env     map[string]string // explicit env, highest precedence (may hold typed refs)
	EnvFile string            // absolute path to an env file, "" if none
	Hooks   Hooks
	Health  *Health // nil when no health block is declared
	Restart Restart
}

// Hooks are the lifecycle command lists run around start/stop, each via `sh -c`.
type Hooks struct {
	PreStart  []string
	PostStart []string
	PreStop   []string
}

// Health is the readiness/liveness probe. Exactly one of the probe kinds is set.
type Health struct {
	Kind     string // "http" | "tcp" | "exec" | "log_line"
	Target   string // the URL / address / command / regex, per Kind
	Interval time.Duration
	Timeout  time.Duration
	Retries  int // readiness budget = Interval*Retries
}

// Restart is the supervisor restart policy for an unexpected exit.
type Restart struct {
	Policy     engine.RestartPolicy
	Backoff    time.Duration
	MaxRetries int
}

// hclConfig is the gohcl decode target for a process block.
type hclConfig struct {
	Cwd     string            `hcl:"cwd,optional"`
	Command string            `hcl:"command"`
	Port    int               `hcl:"port,optional"`
	Env     map[string]string `hcl:"env,optional"`
	EnvFile string            `hcl:"env_file,optional"`
	Hooks   *hooksBlock       `hcl:"hooks,block"`
	Health  *healthBlock      `hcl:"health,block"`
	Restart *restartBlock     `hcl:"restart,block"`
}

type hooksBlock struct {
	PreStart  []string `hcl:"pre_start,optional"`
	PostStart []string `hcl:"post_start,optional"`
	PreStop   []string `hcl:"pre_stop,optional"`
}

type healthBlock struct {
	HTTP     string `hcl:"http,optional"`
	TCP      string `hcl:"tcp,optional"`
	Exec     string `hcl:"exec,optional"`
	LogLine  string `hcl:"log_line,optional"`
	Interval string `hcl:"interval,optional"`
	Timeout  string `hcl:"timeout,optional"`
	Retries  int    `hcl:"retries,optional"`
}

type restartBlock struct {
	Policy     string `hcl:"policy,optional"`
	Backoff    string `hcl:"backoff,optional"`
	MaxRetries int    `hcl:"max_retries,optional"`
}

// Defaults for unset fields.
const (
	defaultHealthInterval = 2 * time.Second
	defaultHealthTimeout  = 3 * time.Second
	defaultHealthRetries  = 30
	defaultBackoff        = time.Second
	defaultMaxRetries     = 5
)

// DecodeConfig implements engine.ConfigDecoder. baseDir is the directory of the
// file that declared the block; cwd and env_file are resolved against it.
func (Driver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, baseDir string) (engine.EngineConfig, error) {
	var raw hclConfig
	if d := gohcl.DecodeBody(body, ctx, &raw); d.HasErrors() {
		return nil, fmt.Errorf("%s", d.Error())
	}
	if strings.TrimSpace(raw.Command) == "" {
		return nil, fmt.Errorf("process needs a non-empty command")
	}

	c := &Config{
		Command: raw.Command,
		Port:    raw.Port,
		Env:     raw.Env,
		Cwd:     resolveDir(baseDir, raw.Cwd),
	}
	if raw.Port < 0 || raw.Port > 65535 {
		return nil, fmt.Errorf("process port %d is out of range", raw.Port)
	}
	if raw.EnvFile != "" {
		c.EnvFile = resolveDir(baseDir, raw.EnvFile)
	}
	if h := raw.Hooks; h != nil {
		c.Hooks = Hooks{PreStart: h.PreStart, PostStart: h.PostStart, PreStop: h.PreStop}
	}

	if h := raw.Health; h != nil {
		hc, err := decodeHealth(h)
		if err != nil {
			return nil, err
		}
		c.Health = hc
	}

	rs, err := decodeRestart(raw.Restart)
	if err != nil {
		return nil, err
	}
	c.Restart = rs

	warnToolchain(c.Cwd)
	return c, nil
}

// decodeHealth validates and normalizes the health block: exactly one probe kind.
func decodeHealth(h *healthBlock) (*Health, error) {
	kinds := []struct {
		kind, target string
	}{
		{"http", h.HTTP}, {"tcp", h.TCP}, {"exec", h.Exec}, {"log_line", h.LogLine},
	}
	out := &Health{Interval: defaultHealthInterval, Timeout: defaultHealthTimeout, Retries: defaultHealthRetries}
	for _, k := range kinds {
		if k.target == "" {
			continue
		}
		if out.Kind != "" {
			return nil, fmt.Errorf("health block sets both %q and %q — choose one probe kind", out.Kind, k.kind)
		}
		out.Kind, out.Target = k.kind, k.target
	}
	if out.Kind == "" {
		return nil, fmt.Errorf("health block needs one of http, tcp, exec, or log_line")
	}
	if h.Interval != "" {
		d, err := time.ParseDuration(h.Interval)
		if err != nil {
			return nil, fmt.Errorf("invalid health interval %q: %w", h.Interval, err)
		}
		out.Interval = d
	}
	if h.Timeout != "" {
		d, err := time.ParseDuration(h.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid health timeout %q: %w", h.Timeout, err)
		}
		out.Timeout = d
	}
	if h.Retries > 0 {
		out.Retries = h.Retries
	}
	return out, nil
}

// decodeRestart validates the restart block, applying defaults. A nil block means
// no restart (today's crashed-backend behavior).
func decodeRestart(r *restartBlock) (Restart, error) {
	out := Restart{Policy: engine.RestartNo, Backoff: defaultBackoff}
	if r == nil {
		return out, nil
	}
	if r.Policy != "" {
		switch engine.RestartPolicy(r.Policy) {
		case engine.RestartNo, engine.RestartOnFailure, engine.RestartAlways:
			out.Policy = engine.RestartPolicy(r.Policy)
		default:
			return out, fmt.Errorf("invalid restart policy %q (want no, on_failure, or always)", r.Policy)
		}
	}
	if r.Backoff != "" {
		d, err := time.ParseDuration(r.Backoff)
		if err != nil {
			return out, fmt.Errorf("invalid restart backoff %q: %w", r.Backoff, err)
		}
		if d < 0 {
			return out, fmt.Errorf("restart backoff must not be negative")
		}
		out.Backoff = d
	}
	// A positive retry budget is required for a restarting policy (the runtime treats
	// MaxRetries==0 as "no attempts"); default it when the user left it unset.
	if out.Policy != engine.RestartNo {
		out.MaxRetries = defaultMaxRetries
		if r.MaxRetries > 0 {
			out.MaxRetries = r.MaxRetries
		}
	}
	return out, nil
}

// resolveDir resolves p against baseDir, returning an absolute path. An empty p
// resolves to baseDir itself (the default working directory).
func resolveDir(baseDir, p string) string {
	if p == "" {
		p = "."
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if baseDir == "" {
		baseDir = "."
	}
	abs, err := filepath.Abs(filepath.Join(baseDir, p))
	if err != nil {
		return filepath.Join(baseDir, p)
	}
	return abs
}

// warnToolchain reads .go-version / .prototools in cwd and warns (no enforcement)
// when the tool on PATH differs from the pinned version. Best-effort.
func warnToolchain(cwd string) {
	if v, ok := readTrim(filepath.Join(cwd, ".go-version")); ok {
		if got, ok := goVersion(); ok && !versionMatches(v, got) {
			Logf("process toolchain: %s pins Go %s but `go` on PATH is %s", cwd, v, got)
		}
	}
	for tool, want := range protoTools(cwd) {
		if got, ok := toolVersion(tool); ok && !versionMatches(want, got) {
			Logf("process toolchain: %s pins %s %s but `%s` on PATH is %s", cwd, tool, want, tool, got)
		}
	}
}

// readTrim reads a file and returns its trimmed contents, ok=false if absent.
func readTrim(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// protoTools parses the tool pins from a .prototools file (lines like
// `go = "1.26"`, `bun = "1.3.10"`). Only go/bun/node are checked. Best-effort.
func protoTools(cwd string) map[string]string {
	out := map[string]string{}
	body, ok := readTrim(filepath.Join(cwd, ".prototools"))
	if !ok {
		return out
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		tool := strings.TrimSpace(k)
		switch tool {
		case "go", "bun", "node":
			out[tool] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return out
}

// goVersion returns the major.minor of the `go` on PATH (e.g. "1.26").
func goVersion() (string, bool) {
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return "", false
	}
	// "go version go1.26.0 linux/amd64" -> "1.26.0"
	for _, f := range strings.Fields(string(out)) {
		if v, ok := strings.CutPrefix(f, "go"); ok && len(v) > 0 && v[0] >= '0' && v[0] <= '9' {
			return v, true
		}
	}
	return "", false
}

// toolVersion returns the reported version string of a tool on PATH (bun/node).
func toolVersion(tool string) (string, bool) {
	out, err := exec.Command(tool, "--version").Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(string(out), "v")), true
}

// versionMatches reports whether got satisfies the want pin, comparing only the
// dotted components want specifies (so "1.26" matches "1.26.0").
func versionMatches(want, got string) bool {
	wp, gp := strings.Split(want, "."), strings.Split(got, ".")
	if len(wp) > len(gp) {
		return false
	}
	for i := range wp {
		if wp[i] != gp[i] {
			return false
		}
	}
	return true
}
