//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A service is a command doze runs on behalf of the stack: asleep until
// something connects, awake while it is used, asleep again when it is not.
// This is the lifecycle.md measure "a two-line service wakes on connect",
// against a real daemon.

// pyServe serves the working directory on $HOST:$PORT, with no reverse lookup
// (which takes half a minute on a CI Mac).
const pyServe = `python3 -c \"import socketserver as s, http.server as h, os; s.TCPServer((os.environ['HOST'], int(os.environ['PORT'])), h.SimpleHTTPRequestHandler).serve_forever()\"`

type project struct {
	t    *testing.T
	dir  string
	home string
	bin  string
}

func newProject(t *testing.T, config string) *project {
	t.Helper()
	p := &project{t: t, home: shortHome(t), bin: buildDoze(t), dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(p.dir, "doze.hcl"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.run("down") })
	return p
}

func (p *project) cmd(args ...string) *exec.Cmd {
	c := exec.Command(p.bin, args...)
	c.Dir = p.dir
	c.Env = append(os.Environ(), "DOZE_HOME="+p.home, "NO_COLOR=1", "DOZE_MODULES=off", "DOZE_ZONE=off")
	return c
}

func (p *project) run(args ...string) (string, error) {
	out, err := p.cmd(args...).CombinedOutput()
	return string(out), err
}

func (p *project) must(args ...string) string {
	p.t.Helper()
	out, err := p.run(args...)
	if err != nil {
		p.t.Fatalf("doze %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// state is what `doze status` says about one instance: the STATE column.
func (p *project) state(name string) string {
	out, _ := p.run("status")
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		for i, w := range f {
			if w == name && i+2 < len(f) {
				return f[i+2]
			}
		}
	}
	return ""
}

func (p *project) waitState(name string, within time.Duration, want ...string) string {
	p.t.Helper()
	deadline := time.Now().Add(within)
	var got string
	for time.Now().Before(deadline) {
		got = p.state(name)
		for _, w := range want {
			if got == w {
				return got
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	p.t.Fatalf("%s is %q, want one of %v", name, got, want)
	return ""
}

func get(url string) (int, error) {
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(url)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

func TestAServiceWakesOnConnectAndSleepsWhenIdle(t *testing.T) {
	port := freeTCPPort(t)
	p := newProject(t, fmt.Sprintf(`
stack = "svc"

defaults {
  domains = false
}

service "mail" {
  run   = "%s"
  port  = %d
  sleep = "2s"
}
`, pyServe, port))
	p.must("up")

	// `up` wakes everything declared, as it does for a module; with nothing
	// using it, the service goes back to sleep on its own.
	p.waitState("mail", 10*time.Second, "asleep")

	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	start := time.Now()
	code, err := get(url)
	if err != nil || code != 200 {
		t.Fatalf("a connection to the sleeping service: %d, %v", code, err)
	}
	t.Logf("woken and answered in %s", time.Since(start).Round(time.Millisecond))
	p.waitState("mail", 5*time.Second, "awake", "dozing")

	// Nothing holds it now: it sleeps, and the next connection wakes it again.
	p.waitState("mail", 20*time.Second, "asleep")
	if code, err := get(url); err != nil || code != 200 {
		t.Fatalf("the second wake: %d, %v", code, err)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func (p *project) journal() []map[string]any {
	p.t.Helper()
	path := firstMatch(filepath.Join(p.home, "projects", "*", "run", "journal.jsonl"))
	raw, err := os.ReadFile(path)
	if err != nil {
		p.t.Fatalf("reading the journal: %v", err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var e map[string]any
		if json.Unmarshal([]byte(line), &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

// count of journal events of a kind whose detail starts with prefix.
func (p *project) events(kind, prefix string) int {
	n := 0
	for _, e := range p.journal() {
		if e["kind"] == kind && strings.HasPrefix(fmt.Sprint(e["detail"]), prefix) {
			n++
		}
	}
	return n
}

// data is the instance's $DATA folder.
func (p *project) data(name string) string {
	return firstMatch(filepath.Join(p.home, "projects", "*", "clusters", name))
}

// lifecycle.md measure: "Setup runs exactly once. Wake, sleep, wake: one step
// event for setup in the journal."
func TestSetupRunsOncePerDataFolder(t *testing.T) {
	port := freeTCPPort(t)
	p := newProject(t, fmt.Sprintf(`
stack = "svc"

defaults {
  domains = false
}

service "mail" {
  run   = "%s"
  port  = %d
  sleep = "2s"
  setup = ["echo prepared >> $DATA/setup.log", "echo from-setup"]
}
`, pyServe, port))
	p.must("up")
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)

	p.waitState("mail", 20*time.Second, "asleep")
	if code, err := get(url); err != nil || code != 200 {
		t.Fatalf("second wake: %d, %v", code, err)
	}
	p.waitState("mail", 20*time.Second, "asleep")
	if code, err := get(url); err != nil || code != 200 {
		t.Fatalf("third wake: %d, %v", code, err)
	}

	if got := p.events("step", "setup:"); got != 2 { // two commands, run once
		t.Errorf("setup step events = %d, want 2 (its two commands, once)", got)
	}
	if got := p.events("wake", ""); got < 3 {
		t.Errorf("wake events = %d, want at least 3", got)
	}
	raw, _ := os.ReadFile(filepath.Join(p.data("mail"), "setup.log"))
	if strings.Count(string(raw), "prepared") != 1 {
		t.Errorf("setup.log = %q, want one line", raw)
	}

	// The step's output is in the log of the run it ran in — the first — under
	// its tag.
	out := p.must("logs", "mail", "--run", "1")
	if !strings.Contains(out, "from-setup") {
		t.Errorf("the log does not hold the setup step's output:\n%s", out)
	}

	// A reset removes the data folder, so setup runs again.
	p.must("down")
	p.must("reset", "mail", "--yes")
	p.must("up")
	p.waitState("mail", 20*time.Second, "asleep", "awake", "dozing")
	if got := p.events("step", "setup:"); got != 4 {
		t.Errorf("setup step events after a reset = %d, want 4", got)
	}
}

// on_ready that fails leaves the command running and not ready; the next wake
// runs that step again and nothing else.
func TestAFailedOnReadyKeepsTheCommandAndRerunsOnlyTheStep(t *testing.T) {
	port := freeTCPPort(t)
	p := newProject(t, fmt.Sprintf(`
stack = "svc"

defaults {
  domains = false
}

service "mail" {
  run      = "%s"
  port     = %d
  on_ready = ["test -f $DATA/ok"]
}
`, pyServe, port))

	out, err := p.run("up")
	if err == nil {
		t.Fatalf("up should fail while on_ready fails:\n%s", out)
	}
	if !strings.Contains(out, "on_ready") {
		t.Errorf("the failure should name the step:\n%s", out)
	}
	// The command itself is up: its port answers.
	if code, err := get(fmt.Sprintf("http://127.0.0.1:%d/", port)); err == nil {
		t.Logf("the command answers directly: %d", code)
	}
	wakes := p.events("wake", "")
	again := p.events("wake", "on_ready again") // the connection above may have run it

	if err := os.WriteFile(filepath.Join(p.data("mail"), "ok"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	p.must("up", "mail")
	p.waitState("mail", 10*time.Second, "awake", "dozing")
	if got := p.events("wake", "on_ready again"); got != again+1 {
		t.Errorf("on_ready-again events %d → %d, want one more", again, got)
	}
	// Only the step ran: the command was not started a second time. Every wake
	// event after the first is an "on_ready again".
	if got := p.events("wake", "") - p.events("wake", "on_ready again"); got != wakes-again {
		t.Errorf("the command was started %d times, want %d", got, wakes-again)
	}
}

// A command is stopped with SIGTERM, and a `stop` step replaces the signals.
func TestStopStepsAndSIGTERM(t *testing.T) {
	p := newProject(t, `
stack = "svc"

defaults {
  domains = false
}

service "termy" {
  run  = "trap 'touch $DATA/got-term; exit 0' TERM; while :; do sleep 0.2; done"
}

service "polite" {
  run  = "trap '' TERM INT; while :; do sleep 0.2; done"
  stop = ["touch $DATA/stop-ran; pkill -KILL -f 'while :; do sleep 0.2; done' || true"]
}
`)
	p.must("up")
	p.must("down")
	if _, err := os.Stat(filepath.Join(p.data("termy"), "got-term")); err != nil {
		t.Errorf("the command was not asked to stop with SIGTERM: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.data("polite"), "stop-ran")); err != nil {
		t.Errorf("the stop step did not run: %v", err)
	}
}
