package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2/hclparse"

	"github.com/doze-dev/doze-sdk/engine"
)

func decode(t *testing.T, src string) (*Config, error) {
	t.Helper()
	f, diags := hclparse.NewParser().ParseHCL([]byte(src), "test.hcl")
	if diags.HasErrors() {
		t.Fatalf("parsing test HCL: %s", diags.Error())
	}
	c, err := Driver{}.DecodeConfig(f.Body, nil, ".", "")
	if err != nil {
		return nil, err
	}
	return c.(*Config), nil
}

func TestDecodeMinimal(t *testing.T) {
	c, err := decode(t, `run = "go run ./..."`)
	if err != nil {
		t.Fatalf("minimal app should decode: %v", err)
	}
	if c.Run != "go run ./..." {
		t.Fatalf("run = %q", c.Run)
	}
	if c.Restart.Policy != engine.RestartOnFailure {
		t.Fatalf("default restart policy = %q, want on_failure", c.Restart.Policy)
	}
	if c.Restart.MaxRetries <= 0 {
		t.Fatalf("default restart should have a positive retry budget, got %d", c.Restart.MaxRetries)
	}
	if c.Ready != nil || c.readySpec(0) != nil {
		t.Fatalf("a portless app with no `ready` should fall back to the liveness check")
	}
}

func TestDecodeRequiresRun(t *testing.T) {
	if _, err := decode(t, `dir = "."`); err == nil {
		t.Fatal("an app without `run` should be rejected")
	}
}

func TestReady(t *testing.T) {
	for _, tc := range []struct {
		name, src    string
		port         int
		kind, target string
		retries      int
	}{
		{"default with a port is tcp", `run = "x"`, 8080, "tcp", "127.0.0.1:8080", 60},
		{"tcp", `run = "x"
			ready = tcp()`, 8080, "tcp", "127.0.0.1:8080", 60},
		{"http", `run = "x"
			ready = http("/healthz")
			ready_timeout = "90s"`, 8080, "http", "http://127.0.0.1:8080/healthz", 90},
		{"log is matched literally", `run = "x"
			ready = log("listening on [::]")`, 0, "log_line", `listening on \[::\]`, 60},
		{"exec", `run = "x"
			ready = exec("pg_isready")`, 0, "exec", "pg_isready", 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := decode(t, tc.src)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if err := c.BindPort(tc.port); err != nil {
				t.Fatalf("BindPort: %v", err)
			}
			r := c.readySpec(tc.port)
			if r == nil || r.Kind != tc.kind || r.Target != tc.target || r.Retries != tc.retries {
				t.Fatalf("ready = %+v, want %s %s ×%d", r, tc.kind, tc.target, tc.retries)
			}
		})
	}

	for name, src := range map[string]string{
		"a bare string":       `ready = "/healthz"`,
		"http without a path": `ready = http("healthz")`,
		"an empty log":        `ready = log("")`,
		"a bad timeout":       `ready_timeout = "soon"`,
	} {
		if _, err := decode(t, "run = \"x\"\n"+src); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}

	c, err := decode(t, `run = "x"
		ready = http("/healthz")`)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.BindPort(0); err == nil {
		t.Error("http() with no port to probe should be rejected")
	}
}

func TestSteps(t *testing.T) {
	c, err := decode(t, `
		run      = "x"
		setup    = "make migrate"
		on_ready = ["make seed", "make warm"]
		stop     = "make drain"
	`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(c.Setup) != 1 || c.Setup[0] != "make migrate" || len(c.OnReady) != 2 || len(c.Stop) != 1 {
		t.Fatalf("steps = %v / %v / %v", c.Setup, c.OnReady, c.Stop)
	}
	if _, err := decode(t, `run = "x"
		setup = 3`); err == nil {
		t.Error("a non-string step should be rejected")
	}
}

func TestSettings(t *testing.T) {
	c, err := decode(t, `
		run     = "x"
		sleep   = "10m"
		logs    = "quiet"
		console = "http://localhost:9000"
		front   = "console"
		watch   = ["**/*.go"]
		direct  = true
	`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if c.Sleep.After != 10*time.Minute || !c.QuietLogs || !c.Direct || c.Front != "console" || len(c.Watch) != 1 {
		t.Fatalf("settings = %+v", c)
	}
	for name, src := range map[string]string{
		"a bad sleep":   `sleep = "later"`,
		"a bad logs":    `logs = "loud"`,
		"a bad console": `console = "localhost:9000"`,
	} {
		if _, err := decode(t, "run = \"x\"\n"+src); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
}

func TestForwardPort(t *testing.T) {
	c, _ := decode(t, `run = "x"`)
	if c.ForwardPort() != 0 {
		t.Fatal("a portless app is not forwarded")
	}
	_ = c.BindPort(8080)
	if c.ForwardPort() != 80 {
		t.Fatalf("an app with a port is named automatically, got %d", c.ForwardPort())
	}
	c.Direct = true
	if c.ForwardPort() != 0 {
		t.Fatal("direct = true opts out")
	}
}

func TestOldFormsNameTheirReplacement(t *testing.T) {
	for src, want := range map[string]string{
		`command = "x"`:               "use `run`",
		"run = \"x\"\ncwd = \"..\"":   "use `dir`",
		"run = \"x\"\nforward = 80":   "named automatically",
		"run = \"x\"\nhooks {}":       "`setup`, `on_ready` and `stop`",
		"run = \"x\"\nhealth {}":      "`ready`",
		"run = \"x\"\ningress = true": "named automatically",
	} {
		_, err := decode(t, src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error = %v, want it to mention %q", src, err, want)
		}
	}
}

func TestDecodeRestartDefaults(t *testing.T) {
	c, err := decode(t, `
		run = "x"
		restart { policy = "always" }
	`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if c.Restart.Policy != engine.RestartAlways {
		t.Fatalf("policy = %q", c.Restart.Policy)
	}
	if c.Restart.MaxRetries != defaultMaxRetries {
		t.Fatalf("max_retries default = %d, want %d", c.Restart.MaxRetries, defaultMaxRetries)
	}

	// `policy = "no"` opts out: zero retry budget so the runtime never restarts.
	off, err := decode(t, `
		run = "x"
		restart { policy = "no" }
	`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if off.Restart.Policy != engine.RestartNo || off.Restart.MaxRetries != 0 {
		t.Fatalf("policy=no should zero retries, got policy=%q retries=%d", off.Restart.Policy, off.Restart.MaxRetries)
	}
	if _, err := decode(t, `run = "x"
		restart { policy = "sometimes" }`); err == nil {
		t.Fatal("an invalid restart policy should be rejected")
	}
}

func TestAdvertisedAddr(t *testing.T) {
	inst := engine.Instance{Spec: &Config{Port: 8080}}
	addr, ok := Driver{}.AdvertisedAddr(inst)
	if !ok || addr != "127.0.0.1:8080" {
		t.Fatalf("AdvertisedAddr = %q, %v", addr, ok)
	}
	if _, ok := (Driver{}).AdvertisedAddr(engine.Instance{Spec: &Config{}}); ok {
		t.Fatal("a portless process should not advertise an address")
	}
}

func TestMergedEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("FROM_FILE=file\nSHARED=file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROC_TEST_BASE", "base")
	c := &Config{
		EnvFile: envFile,
		Env:     map[string]string{"SHARED": "explicit", "FROM_ENV": "env"},
	}
	got := map[string]string{}
	for _, kv := range c.mergedEnv() {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	// explicit env{} beats env_file; os.Environ is the floor. doze injects nothing.
	checks := map[string]string{
		"PROC_TEST_BASE": "base",
		"FROM_FILE":      "file",
		"FROM_ENV":       "env",
		"SHARED":         "explicit",
	}
	for k, want := range checks {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
}

func envMap(env []string) map[string]string {
	out := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out
}

// An app with a port is started on a private one, which is what it is told to
// listen on and what its readiness is probed at. The declared port is doze's.
func TestAProxiedAppListensOnItsPrivatePort(t *testing.T) {
	c, err := decode(t, `run = "x"`)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.BindPort(8080); err != nil {
		t.Fatal(err)
	}
	inst := engine.Instance{Name: "api", Port: 51234, DataDir: "/data/api", Spec: c}
	if !(Driver{}).Proxied(inst) {
		t.Fatal("an app with a port is proxied")
	}
	plan, err := Driver{}.Plan(nil, engine.Instance{Name: "api", Port: 51234, DataDir: t.TempDir(), Spec: c}, engine.Toolchain{})
	if err != nil {
		t.Fatal(err)
	}
	env := envMap(plan.Specs[0].Env)
	if env["PORT"] != "51234" || env["HOST"] != "127.0.0.1" || env["DATA"] == "" {
		t.Fatalf("PORT=%q HOST=%q DATA=%q; want the private port, loopback and a data folder", env["PORT"], env["HOST"], env["DATA"])
	}
	if r := plan.Specs[0].Ready; r == nil || r.Target != "127.0.0.1:51234" {
		t.Fatalf("readiness is probed at %+v, want the private port", r)
	}
	if got := (Driver{}).BackendSocket("", 51234); got != "127.0.0.1:51234" {
		t.Fatalf("doze forwards to %q, want the private port", got)
	}
}

// direct = true is today's behaviour: the app binds the declared port itself,
// is told nothing about where to listen, and is probed where it said it would be.
func TestADirectAppBindsItsOwnPort(t *testing.T) {
	c, err := decode(t, "run = \"x\"\ndirect = true\nenv = { PORT = \"8080\" }")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.BindPort(8080); err != nil {
		t.Fatalf("a direct app may set PORT itself: %v", err)
	}
	inst := engine.Instance{Name: "api", Port: 5432, DataDir: t.TempDir(), Spec: c}
	if (Driver{}).Proxied(inst) {
		t.Fatal("direct = true opts out of the proxy")
	}
	plan, err := Driver{}.Plan(nil, inst, engine.Toolchain{})
	if err != nil {
		t.Fatal(err)
	}
	if env := envMap(plan.Specs[0].Env); env["PORT"] != "8080" || env["HOST"] != "" {
		t.Fatalf("a direct app's env is its own: PORT=%q HOST=%q", env["PORT"], env["HOST"])
	}
	if r := plan.Specs[0].Ready; r == nil || r.Target != "127.0.0.1:8080" {
		t.Fatalf("readiness is probed at %+v, want the declared port", r)
	}
}

// An env that sets PORT on a proxied app would send it to the port doze holds.
func TestAProxiedAppMayNotSetItsOwnPort(t *testing.T) {
	c, err := decode(t, "run = \"x\"\nenv = { PORT = \"8080\" }")
	if err != nil {
		t.Fatal(err)
	}
	err = c.BindPort(8080)
	if err == nil || !strings.Contains(err.Error(), "env sets PORT") || !strings.Contains(err.Error(), "direct = true") {
		t.Fatalf("BindPort = %v, want an error naming PORT and the way out", err)
	}
	// With no port there is nothing for doze to hold, and PORT is the app's business.
	worker, _ := decode(t, "run = \"x\"\nenv = { PORT = \"9\" }")
	if err := worker.BindPort(0); err != nil {
		t.Fatalf("a portless app may set PORT: %v", err)
	}
}

// An app can sleep when idle only if a connection can wake it again: only if
// doze holds its port.
func TestSleepNeedsDozeToHoldThePort(t *testing.T) {
	c, _ := decode(t, "run = \"x\"\nsleep = \"10m\"")
	if d, set := c.IdleAfter(); !set || d != 10*time.Minute {
		t.Fatalf("IdleAfter = %s, %v", d, set)
	}
	if err := c.BindPort(8080); err != nil {
		t.Fatalf("a fronted app may sleep: %v", err)
	}
	if err := c.BindPort(0); err == nil || !strings.Contains(err.Error(), "declare a `port`") {
		t.Fatalf("a portless app that sleeps could never wake: %v", err)
	}
	direct, _ := decode(t, "run = \"x\"\nsleep = \"10m\"\ndirect = true")
	if err := direct.BindPort(8080); err == nil {
		t.Fatal("a direct app that sleeps could never wake")
	}
	never, _ := decode(t, "run = \"x\"\nsleep = \"never\"")
	if _, set := never.IdleAfter(); set {
		t.Fatal(`sleep = "never" is the default: always on`)
	}
	if err := never.BindPort(0); err != nil {
		t.Fatalf(`a portless app with sleep = "never": %v`, err)
	}
}
