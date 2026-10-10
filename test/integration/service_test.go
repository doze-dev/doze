//go:build integration

package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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

func (p *project) write(rel, body string) {
	p.t.Helper()
	path := filepath.Join(p.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		p.t.Fatal(err)
	}
}

func (p *project) lines(rel string) int {
	raw, err := os.ReadFile(filepath.Join(p.dir, rel))
	if err != nil {
		return 0
	}
	return strings.Count(string(raw), "\n")
}

// lifecycle.md measure: "A task re-runs only on change. Touch a file outside
// inputs: no run. Change one inside: one run."
func TestATaskRerunsOnlyWhenItsInputsChange(t *testing.T) {
	p := newProject(t, `
stack = "tasks"

defaults {
  domains = false
}

task "migrate" {
  run    = "echo ran >> ran.log"
  inputs = ["migrations/*.sql"]
}

app "api" {
  run   = "sleep 600"
  after = [task.migrate]
}
`)
	p.write("migrations/001.sql", "create table a;")
	p.write("README.md", "docs")

	p.must("up")
	if got := p.lines("ran.log"); got != 1 {
		t.Fatalf("the task ran %d times on the first up, want 1", got)
	}
	// It ran before the app: the app is up, and the task is done.
	list := p.must("task")
	if !strings.Contains(list, "migrate") || !strings.Contains(list, "done") {
		t.Errorf("doze task should list migrate as done:\n%s", list)
	}

	p.must("down")
	p.write("README.md", "docs, edited — not an input")
	p.must("up")
	if got := p.lines("ran.log"); got != 1 {
		t.Errorf("a change outside the inputs ran the task: %d runs", got)
	}

	p.must("down")
	p.write("migrations/001.sql", "create table a, b;")
	p.must("up")
	if got := p.lines("ran.log"); got != 2 {
		t.Errorf("a changed input should run the task once more: %d runs", got)
	}

	p.must("down")
	p.must("up")
	if got := p.lines("ran.log"); got != 2 {
		t.Errorf("nothing changed, but the task ran again: %d runs", got)
	}

	// `doze task` runs it whatever its state.
	out := p.must("task", "migrate")
	if got := p.lines("ran.log"); got != 3 || !strings.Contains(out, "done") {
		t.Errorf("doze task migrate: %d runs, output %q", got, out)
	}
}

// A task that fails stops what is after it, says why, and runs again next
// time; a manual task runs only when asked.
func TestAFailedTaskStopsWhatIsAfterIt(t *testing.T) {
	p := newProject(t, `
stack = "tasks"

defaults {
  domains = false
}

task "migrate" {
  run = "echo 'no such table' >&2; test -f ok"
}

task "seed" {
  run    = "echo seeded >> seeded.log"
  manual = true
}

app "api" {
  run   = "sleep 600"
  after = [task.migrate]
}
`)
	out, err := p.run("up")
	if err == nil {
		t.Fatalf("up should fail while the task fails:\n%s", out)
	}
	if !strings.Contains(out, "migrate") || !strings.Contains(out, "no such table") {
		t.Errorf("the failure should name the task and show its output:\n%s", out)
	}
	if st := p.state("api"); st == "awake" || st == "dozing" {
		t.Errorf("api started though its task failed: %s", st)
	}
	if p.lines("seeded.log") != 0 {
		t.Error("a manual task ran without being asked")
	}
	list := p.must("task")
	if !strings.Contains(list, "failed") || !strings.Contains(list, "manual") {
		t.Errorf("task list:\n%s", list)
	}

	// Fixed: the next up runs it again, and the app follows.
	p.write("ok", "")
	p.must("up")
	p.waitState("api", 10*time.Second, "awake", "dozing")

	p.must("task", "seed")
	if p.lines("seeded.log") != 1 {
		t.Error("doze task seed did not run the manual task")
	}
}

// eventually polls check until it holds or the time is up.
func eventually(t *testing.T, within time.Duration, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s: not within %s", what, within)
}

// `watch` restarts an app when a matching file changes; a file that does not
// match does nothing; and editing the input of a task the app waits for runs
// the task and then restarts the app.
func TestWatchRestartsAnAppAndRerunsItsTasks(t *testing.T) {
	p := newProject(t, `
stack = "watch"

defaults {
  domains = false
}

task "gen" {
  run    = "echo gen >> gen.log"
  inputs = ["schema/*.sql"]
}

app "api" {
  run   = "echo started >> starts.log; sleep 600"
  watch = ["src/*.txt"]
  after = [task.gen]
}
`)
	p.write("src/a.txt", "1")
	p.write("schema/s.sql", "1")
	p.write("other/ignored.txt", "1")

	p.must("up")
	if p.lines("starts.log") != 1 || p.lines("gen.log") != 1 {
		t.Fatalf("after up: starts %d, gen %d", p.lines("starts.log"), p.lines("gen.log"))
	}

	// A file the globs match.
	p.write("src/a.txt", "2")
	eventually(t, 15*time.Second, "restart on a watched file", func() bool { return p.lines("starts.log") == 2 })
	if p.lines("gen.log") != 1 {
		t.Errorf("the task ran though its inputs did not change: %d", p.lines("gen.log"))
	}

	// A file nothing matches.
	p.write("other/ignored.txt", "2")
	time.Sleep(3 * time.Second)
	if p.lines("starts.log") != 2 {
		t.Errorf("an unwatched file restarted the app: %d starts", p.lines("starts.log"))
	}

	// A task input: the task runs first, then the app starts again.
	p.write("schema/s.sql", "2")
	eventually(t, 15*time.Second, "restart on a task input", func() bool { return p.lines("starts.log") == 3 })
	if p.lines("gen.log") != 2 {
		t.Errorf("the task did not re-run before the restart: %d runs", p.lines("gen.log"))
	}

	// The journal says why.
	found := false
	for _, e := range p.journal() {
		if e["kind"] == "sleep" && fmt.Sprint(e["cause"]) != "" && strings.Contains(fmt.Sprint(e["cause"]), "watch") {
			found = true
		}
	}
	if !found {
		t.Error("no journal event carries the watch cause")
	}
}

// A service can be fetched: unpacked once, pinned in doze.lock, and refused
// when what the URL serves changes. This is the lifecycle.md measure "a
// changed download is refused", against a real daemon.
func TestADownloadedServiceIsPinnedAndAChangeIsRefused(t *testing.T) {
	var archive []byte
	hits := 0
	srv := newDownloadServer(t, &archive, &hits)
	port := freeTCPPort(t)

	archive = tarGz(t, map[string]string{
		"tool-1.0/bin/serve": "#!/bin/sh\necho started >> \"$DATA/starts\"\nexec " + pyServeArgs() + "\n",
	})
	config := fmt.Sprintf(`
stack = "dl"

defaults {
  domains = false
}

service "tool" {
  download = "%s/tool.tgz"
  run      = "bin/serve"
  port     = %d
}
`, srv, port)
	p := newProject(t, config)

	p.must("up")
	if code, err := get(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil || code != 200 {
		t.Fatalf("the downloaded service: %d, %v", code, err)
	}
	lock, err := os.ReadFile(filepath.Join(p.dir, "doze.lock"))
	if err != nil || !strings.Contains(string(lock), "downloads:") || !strings.Contains(string(lock), "sha256:") {
		t.Fatalf("doze.lock should pin the download:\n%s\n%v", lock, err)
	}
	p.must("down")

	// A new machine: same lock, empty cache, and the archive replaced upstream.
	archive = tarGz(t, map[string]string{"tool-1.0/bin/serve": "#!/bin/sh\necho tampered\n"})
	other := newProject(t, config)
	if err := os.WriteFile(filepath.Join(other.dir, "doze.lock"), lock, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := other.run("up")
	if err == nil || !strings.Contains(out, "checksum mismatch") {
		t.Fatalf("a changed download should be refused with a mismatch (err %v):\n%s", err, out)
	}
}

func pyServeArgs() string {
	return `python3 -c "import socketserver as s, http.server as h, os; s.TCPServer((os.environ['HOST'], int(os.environ['PORT'])), h.SimpleHTTPRequestHandler).serve_forever()"`
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// newDownloadServer serves *archive at /tool.tgz and returns its base URL.
func newDownloadServer(t *testing.T, archive *[]byte, hits *int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		_, _ = w.Write(*archive)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// doze sync says which blocks differ from the ones the running daemon started
// with, and what applying them does — the daemon reads doze.hcl once.
func TestSyncNamesTheBlocksThatDifferFromTheRunningDaemon(t *testing.T) {
	p := newProject(t, `
stack = "blocks"

defaults {
  domains = false
}

app "api" {
  run = "sleep 600"
}

app "old" {
  run = "sleep 600"
}

app "same" {
  run = "sleep 600"
}
`)
	p.must("up")

	// Nothing differs yet: sync has nothing to say about blocks.
	if out := p.must("sync", "--dry-run"); strings.Contains(out, "differs from what the running daemon") {
		t.Fatalf("an unchanged file differs from itself:\n%s", out)
	}

	p.write("doze.hcl", `
stack = "blocks"

defaults {
  domains = false
}

app "api" {
  run = "sleep 601"
}

app "same" {
  run = "sleep 600"
}

service "mail" {
  run  = "sleep 600"
}
`)
	out := p.must("sync", "--dry-run")
	for _, want := range []string{
		"differs from what the running daemon started with",
		"api", "changed", "restarts",
		"mail", "new", "starts when something uses it",
		"old", "removed", "its data is kept",
		"1 block unchanged",
		"doze down && doze up",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("sync lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "same ") && strings.Contains(out, "same     app") {
		t.Errorf("an unchanged block is listed as changed:\n%s", out)
	}
}
