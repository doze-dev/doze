//go:build integration

package integration

import (
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
