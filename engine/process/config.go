package process

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/zclconf/go-cty/cty"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/runspec"
)

// Logf is the sink for toolchain-mismatch warnings; cmd/doze points it at stderr.
var Logf = func(string, ...any) {}

// Config is the decoded `app "<name>" { … }` block.
type Config struct {
	Dir     string            // absolute working directory (resolved against the declaring file)
	Run     string            // the shell command line, run via `sh -c`
	Port    int               // the port the app binds itself, for AdvertisedAddr; 0 from a decode
	Env     map[string]string // explicit env, highest precedence (may hold typed refs)
	EnvFile string            // absolute path to an env file, "" if none

	// Setup runs before the app starts, OnReady once it is ready, Stop before it
	// is signalled. Until the lifecycle work lands (docs/design/next/lifecycle.md)
	// Setup runs on every start, not once per data folder, and Stop runs ahead
	// of the signals instead of replacing them.
	Setup   []string
	OnReady []string
	Stop    []string

	Ready        *runspec.Probe // nil: tcp() when a port is declared, else a brief liveness check
	ReadyTimeout time.Duration
	Restart      Restart

	// Sleep is the app's own idle time (see IdleAfter) and Direct opts out of
	// the proxy. The rest are parsed and validated, not acted on yet: the dash's
	// front tab, console link and log volume, and file watching.
	Sleep     runspec.Sleep
	Front     string
	Console   string
	QuietLogs bool
	Watch     []string
	Direct    bool

	declaredPort int // the block's `port`, handed over by config (see BindPort)
}

// Restart is the supervisor restart policy for an unexpected exit.
type Restart struct {
	Policy     engine.RestartPolicy
	Backoff    time.Duration
	MaxRetries int
}

// hclConfig is the gohcl decode target for an app block.
type hclConfig struct {
	Run          string            `hcl:"run"`
	Dir          string            `hcl:"dir,optional"`
	Env          map[string]string `hcl:"env,optional"`
	EnvFile      string            `hcl:"env_file,optional"`
	Ready        cty.Value         `hcl:"ready,optional"`
	ReadyTimeout string            `hcl:"ready_timeout,optional"`
	Setup        cty.Value         `hcl:"setup,optional"`
	OnReady      cty.Value         `hcl:"on_ready,optional"`
	Stop         cty.Value         `hcl:"stop,optional"`
	Restart      *restartBlock     `hcl:"restart,block"`
	Sleep        string            `hcl:"sleep,optional"`
	Front        string            `hcl:"front,optional"`
	Console      string            `hcl:"console,optional"`
	Logs         string            `hcl:"logs,optional"`
	Watch        []string          `hcl:"watch,optional"`
	Direct       bool              `hcl:"direct,optional"`
}

type restartBlock struct {
	Policy     string `hcl:"policy,optional"`
	Backoff    string `hcl:"backoff,optional"`
	MaxRetries int    `hcl:"max_retries,optional"`
}

// Defaults for unset fields.
const (
	readyInterval     = time.Second
	readyProbeTimeout = 3 * time.Second
	defaultBackoff    = time.Second
	defaultMaxRetries = 5
)

// removed maps each argument and block of the old `process` form to what
// replaces it, so a stale config fails with the fix instead of HCL's bare
// "unsupported argument".
var removed = []struct {
	name, now string
	block     bool
}{
	{name: "command", now: "`run`"},
	{name: "cwd", now: "`dir`"},
	{name: "forward", now: "nothing: an app with a `port` is named automatically"},
	{name: "ingress", now: "nothing: an app with a `port` is named automatically"},
	{name: "hooks", now: "`setup`, `on_ready` and `stop`", block: true},
	{name: "health", now: "`ready`, e.g. ready = http(\"/healthz\")", block: true},
}

func checkRemoved(body hcl.Body) error {
	schema := &hcl.BodySchema{}
	for _, r := range removed {
		if r.block {
			schema.Blocks = append(schema.Blocks, hcl.BlockHeaderSchema{Type: r.name})
		} else {
			schema.Attributes = append(schema.Attributes, hcl.AttributeSchema{Name: r.name})
		}
	}
	content, _, _ := body.PartialContent(schema)
	if content == nil {
		return nil
	}
	for _, r := range removed {
		if attr, ok := content.Attributes[r.name]; ok {
			return fmt.Errorf("%s: `%s` is no longer an argument; use %s", attr.NameRange, r.name, r.now)
		}
		for _, blk := range content.Blocks {
			if blk.Type == r.name {
				return fmt.Errorf("%s: the `%s` block is gone; use %s", blk.DefRange, r.name, r.now)
			}
		}
	}
	return nil
}

// DecodeConfig implements engine.ConfigDecoder. baseDir is the directory of the
// file that declared the block; dir and env_file are resolved against it.
func (Driver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, baseDir string, _ engine.VersionSpec) (engine.EngineConfig, error) {
	if err := checkRemoved(body); err != nil {
		return nil, err
	}
	// The probe constructors (tcp, http, log, exec) exist only while an app
	// block decodes, so they cannot shadow anything elsewhere in a config.
	ectx := &hcl.EvalContext{}
	if ctx != nil {
		ectx = ctx.NewChild()
	}
	ectx.Functions = runspec.Functions()

	var raw hclConfig
	if d := gohcl.DecodeBody(body, ectx, &raw); d.HasErrors() {
		return nil, fmt.Errorf("%s", d.Error())
	}
	if strings.TrimSpace(raw.Run) == "" {
		return nil, fmt.Errorf("an app needs a non-empty `run`")
	}

	c := &Config{
		Run:     raw.Run,
		Env:     raw.Env,
		Dir:     resolveDir(baseDir, raw.Dir),
		Front:   raw.Front,
		Console: raw.Console,
		Watch:   raw.Watch,
		Direct:  raw.Direct,
	}
	if raw.EnvFile != "" {
		c.EnvFile = resolveDir(baseDir, raw.EnvFile)
	}
	var err error
	if c.Setup, err = runspec.Steps("setup", raw.Setup); err != nil {
		return nil, err
	}
	if c.OnReady, err = runspec.Steps("on_ready", raw.OnReady); err != nil {
		return nil, err
	}
	if c.Stop, err = runspec.Steps("stop", raw.Stop); err != nil {
		return nil, err
	}
	if c.Ready, err = runspec.ParseReady(raw.Ready); err != nil {
		return nil, err
	}
	if c.ReadyTimeout, err = runspec.ParseReadyTimeout(raw.ReadyTimeout); err != nil {
		return nil, err
	}
	if c.Sleep, err = runspec.ParseSleep(raw.Sleep); err != nil {
		return nil, err
	}
	if c.QuietLogs, err = runspec.ParseLogs(raw.Logs); err != nil {
		return nil, err
	}
	if err := runspec.CheckConsole(raw.Console); err != nil {
		return nil, err
	}
	if c.Restart, err = decodeRestart(raw.Restart); err != nil {
		return nil, err
	}

	warnToolchain(c.Dir)
	return c, nil
}

// BindPort hands the app the `port` its block declares. Config reads `port`
// off every block before the driver decodes the rest, so the driver never sees
// it; config calls this straight after the decode. The port is what tcp() and
// http() probe.
func (c *Config) BindPort(port int) error {
	c.declaredPort = port
	if _, sleeps := c.IdleAfter(); sleeps && !c.Proxied() {
		// Only a connection through doze's listener can wake an app again.
		return fmt.Errorf("sleep = %q needs doze to hold the app's port, so that a connection can wake it: declare a `port`, and do not set `direct`", c.Sleep.After)
	}
	if c.Proxied() {
		// doze holds the declared port and tells the app where to listen. An
		// env that sets these itself would send the app somewhere doze is not
		// forwarding to — usually straight onto the port doze already holds.
		for _, name := range []string{"PORT", "HOST"} {
			if _, set := c.Env[name]; set {
				return fmt.Errorf("env sets %s, but doze chooses it for an app with a `port`: remove it from env (the app is started with $%s), or set `direct = true` to have the app bind %d itself", name, name, port)
			}
		}
	}
	if port == 0 && c.Ready != nil && c.Ready.NeedsPort() {
		return fmt.Errorf("ready = %s(…) needs a `port` to probe", c.Ready.Kind)
	}
	return nil
}

// decodeRestart validates the restart block, applying defaults. A process is a
// supervised service, so the default is to restart it when it crashes (a non-zero
// exit), with a backoff + bounded retries to guard against a crash loop — a clean
// exit 0 (a one-shot command) is left down. Override per process with a
// `restart { policy = "always" | "no" }` block. A nil block keeps the defaults.
func decodeRestart(r *restartBlock) (Restart, error) {
	out := Restart{Policy: engine.RestartOnFailure, Backoff: defaultBackoff, MaxRetries: defaultMaxRetries}
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
	if r.MaxRetries > 0 {
		out.MaxRetries = r.MaxRetries
	}
	// `policy = "no"` opts out entirely: a zero retry budget satisfies the runtime's
	// "MaxRetries == 0 means no attempts" contract.
	if out.Policy == engine.RestartNo {
		out.MaxRetries = 0
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

// ConsoleTarget is the address of the app's web console when `console` names
// a second port on the app itself — a loopback URL such as
// http://127.0.0.1:8025. doze then fronts that port too, on the app's own
// address. A console hosted anywhere else is only a link, and ok is false.
func (c *Config) ConsoleTarget() (hostport, path string, ok bool) {
	u, err := url.Parse(c.Console)
	if err != nil || c.Console == "" || u.Port() == "" {
		return "", "", false
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost":
		return net.JoinHostPort("127.0.0.1", u.Port()), u.RequestURI(), true
	}
	return "", "", false
}

// IdleAfter is how long the app may sit with nothing holding it before doze
// puts it to sleep, when it asked for that with `sleep = "10m"`. An app that
// did not, or said "never", runs until it is stopped.
func (c *Config) IdleAfter() (time.Duration, bool) {
	if c.Sleep.Set && !c.Sleep.Never {
		return c.Sleep.After, true
	}
	return 0, false
}

// Proxied reports whether doze fronts this app: it declares a port and has not
// opted out with `direct`.
func (c *Config) Proxied() bool { return c.declaredPort > 0 && !c.Direct }

// ForwardPort is the public port this app is served on through the shared
// ingress (http://<name>.<stack>.doze), or 0 if it isn't. An app that declares a
// `port` is named automatically; `direct = true` opts out. The daemon
// type-asserts for this, keeping engine specs opaque to it otherwise.
func (c *Config) ForwardPort() int {
	if c.declaredPort > 0 && !c.Direct {
		return 80
	}
	return 0
}

// IngressEnabled reports whether this app is forwarded through the shared
// ingress.
func (c *Config) IngressEnabled() bool { return c.ForwardPort() > 0 }
