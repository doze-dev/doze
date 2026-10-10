//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	names "github.com/doze-dev/doze-names"
)

// The .doze zone is shared: doze, doze-aws and doze-kafka each claim names in
// one registry, and whichever started first answers DNS and :80 for all of
// them. These tests run a real doze daemon beside a second program in the
// zone — this test process, using the same library the standalone tools use —
// in each start order.
//
// They run in a private zone (its own home, DNS address and front-door
// address), so they need no setup and touch nothing of the machine's real
// zone. That also means names are resolved by asking the zone's DNS server
// directly. TestNamesThroughTheSystemResolver is the one that goes through the
// operating system, and it runs only where setup has been done for it.

// zone is a private .doze zone and a doze project inside it.
type zone struct {
	t                *testing.T
	home, dns, front string
	dir              string
	doze             string
	env              []string
}

func freeUDP(t *testing.T) string {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().String()
}

func freeTCP(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func newZone(t *testing.T, config string) *zone {
	t.Helper()
	// Short paths: the daemon's control socket lives under the home, and a unix
	// socket path has a hard length limit the usual temp dir can exceed.
	root, err := os.MkdirTemp("/tmp", "dzn")
	if err != nil {
		t.Fatal(err)
	}
	built := time.Now()
	doze := buildDoze(t)
	t.Logf("building doze took %s", time.Since(built).Round(time.Millisecond))
	z := &zone{t: t, home: filepath.Join(root, "h"), dns: freeUDP(t), front: freeTCP(t), dir: filepath.Join(root, "p"), doze: doze}
	if err := os.MkdirAll(z.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(z.dir, "doze.hcl"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	private := map[string]string{names.EnvHome: z.home, names.EnvResolver: z.dns, names.EnvIngress: z.front}
	z.env = os.Environ()
	for k, v := range private {
		z.env = append(z.env, k+"="+v)
		t.Setenv(k, v) // this process is a peer in the same zone
	}
	z.env = append(z.env, "NO_COLOR=1", "DOZE_MODULES=off")
	t.Cleanup(func() {
		z.run("down")
		_ = os.RemoveAll(root)
	})
	return z
}

func (z *zone) run(args ...string) (string, error) {
	cmd := exec.Command(z.doze, args...)
	cmd.Dir, cmd.Env = z.dir, z.env
	start := time.Now()
	out, err := cmd.CombinedOutput()
	z.t.Logf("doze %s took %s", strings.Join(args, " "), time.Since(start).Round(time.Millisecond))
	return string(out), err
}

func (z *zone) mustRun(args ...string) string {
	z.t.Helper()
	out, err := z.run(args...)
	if err != nil {
		z.t.Fatalf("doze %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// lookup asks the zone's own DNS server for a name.
func (z *zone) lookup(host string) string {
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "udp", z.dns)
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addrs, err := r.LookupHost(ctx, host)
	if err != nil || len(addrs) == 0 {
		return ""
	}
	return addrs[0]
}

// viaFrontDoor asks the zone's front door for a name.
func (z *zone) viaFrontDoor(host string) (string, error) {
	req, _ := http.NewRequest("GET", "http://"+z.front+"/", nil)
	req.Host = host
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return string(body), nil
}

func waitFor(t *testing.T, what string, check func() error) {
	t.Helper()
	start := time.Now()
	defer func() { t.Logf("%s: %s", what, time.Since(start).Round(time.Millisecond)) }()
	deadline := time.Now().Add(10 * time.Second)
	var err error
	for {
		if err = check(); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", what, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// appStack is a stack with one app and names on. The app serves the directory
// it starts in, on the private port doze gives it.
const appStack = `stack = "namestest"

defaults {
  domains = true
}

app "api" {
  run  = "python3 -m http.server $PORT --bind $HOST"
  port = 18431
}
`

// standalone is a second program in the zone: it claims one name, routes it to
// a server of its own, and joins the resolver and the front door.
type standalone struct {
	reg   *names.Registry
	lease *names.Lease
	srv   *names.Server
	front *names.Ingress
	http  *http.Server
}

func startStandalone(t *testing.T, z *zone, owner, service, stack string) *standalone {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &standalone{reg: names.Open(z.home, owner)}
	s.http = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "this is "+owner) })}
	go s.http.Serve(ln)
	if s.lease, err = s.reg.Claim(names.Qualified(service, stack)); err != nil {
		t.Fatal(err)
	}
	if err := s.lease.Route(ln.Addr().String()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.srv = names.Serve(ctx, s.reg, nil)
	s.front = names.ServeIngress(ctx, s.reg, nil)
	t.Cleanup(func() { cancel(); s.srv.Close(); s.front.Close(); _ = s.lease.Release(); _ = s.http.Close() })
	return s
}

// both checks the two programs' names from the outside: each resolves, and
// each is fronted to its own program — whichever of them is serving the zone.
func both(t *testing.T, z *zone, other string) {
	t.Helper()
	waitFor(t, "doze's name resolves and is fronted", func() error {
		if ip := z.lookup("api.namestest.doze"); !names.OnThisMachine(net.ParseIP(ip)) {
			return fmt.Errorf("api.namestest.doze resolved to %q", ip)
		}
		body, err := z.viaFrontDoor("api.namestest.doze")
		if err != nil {
			return err
		}
		if !strings.Contains(body, "doze.hcl") { // the app lists its directory
			return fmt.Errorf("the front door did not reach the app: %.80q", body)
		}
		return nil
	})
	waitFor(t, "the other program's name resolves and is fronted", func() error {
		if ip := z.lookup(other); !names.OnThisMachine(net.ParseIP(ip)) {
			return fmt.Errorf("%s resolved to %q", other, ip)
		}
		body, err := z.viaFrontDoor(other)
		if err != nil || !strings.HasPrefix(body, "this is ") {
			return fmt.Errorf("front door for %s: %q, %v", other, body, err)
		}
		return nil
	})
}

func TestDozeAndAStandaloneToolShareTheZone(t *testing.T) {
	t.Run("the standalone tool first", func(t *testing.T) {
		z := newZone(t, appStack)
		startStandalone(t, z, "doze-aws", "aws", "elsewhere")
		z.mustRun("up")
		// The tool was first, so it is the one serving — doze's names included.
		reg := names.Open(z.home, "test")
		if h, ok := reg.ResolverHolder(); !ok || h.Owner != "doze-aws" {
			t.Fatalf("the tool started first and should be serving DNS, the registry says %+v", h)
		}
		both(t, z, "aws.elsewhere.doze")
	})
	t.Run("doze first", func(t *testing.T) {
		z := newZone(t, appStack)
		z.mustRun("up")
		reg := names.Open(z.home, "test")
		waitFor(t, "doze serves the zone", func() error {
			if h, ok := reg.ResolverHolder(); !ok || h.Owner != "doze" {
				return fmt.Errorf("resolver holder is %+v", h)
			}
			return nil
		})
		startStandalone(t, z, "doze-kafka", "kafka", "elsewhere")
		both(t, z, "kafka.elsewhere.doze")
	})
}

// A stack and a standalone tool that want the same name: first come keeps it,
// and doze says who has it instead of starting on top of it.
func TestANameAStandaloneToolHoldsStopsTheStack(t *testing.T) {
	z := newZone(t, appStack)
	if !names.PoolUsable() {
		// Without an address of its own a service is reached by port, the stack
		// runs, and the lost name is reported, not fatal. On a Mac that is every
		// private zone: the network daemon serves the machine's real one.
		t.Skip("this zone cannot give a service its own address")
	}
	// The tool's instance happens to be called what the stack's app is.
	startStandalone(t, z, "doze-kafka", "api", "namestest")
	out, err := z.run("up")
	if err == nil {
		t.Fatalf("up should fail while another program holds api.namestest.doze:\n%s", out)
	}
	logged, _ := os.ReadFile(firstMatch(filepath.Join(z.home, "projects", "*", "run", "doze.log")))
	all := out + string(logged)
	if !strings.Contains(all, "api.namestest.doze") || !strings.Contains(all, "doze-kafka") {
		t.Fatalf("the failure should name the name and who holds it:\n%s", all)
	}
}

func firstMatch(pattern string) string {
	m, _ := filepath.Glob(pattern)
	if len(m) == 0 {
		return ""
	}
	return m[0]
}

// TestNamesThroughTheSystemResolver is the test that goes all the way: a name
// doze publishes is looked up by the operating system's own resolver, the way
// psql and curl look it up. It uses the machine's real zone, so it runs only
// where that is wanted and setup has been done: a CI runner that ran
// `doze doctor --fix` and set DOZE_TEST_SYSTEM_RESOLVER=1.
func TestNamesThroughTheSystemResolver(t *testing.T) {
	if os.Getenv("DOZE_TEST_SYSTEM_RESOLVER") != "1" {
		t.Skip("uses the machine's real .doze zone; set DOZE_TEST_SYSTEM_RESOLVER=1 where `doze doctor --fix` has been run")
	}
	root, err := os.MkdirTemp("/tmp", "dzr")
	if err != nil {
		t.Fatal(err)
	}
	doze := buildDoze(t)
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	// Two projects, and the same app on the same port in each. Nobody chose a
	// second port: each has an address of its own.
	hosts := []string{"api.namestest.doze", "api.namestest2.doze"}
	addrs := map[string]string{}
	for i, host := range hosts {
		dir := filepath.Join(root, fmt.Sprintf("p%d", i))
		_ = os.MkdirAll(dir, 0o755)
		config := appStack
		if i == 1 {
			config = strings.Replace(appStack, `"namestest"`, `"namestest2"`, 1)
		}
		if err := os.WriteFile(filepath.Join(dir, "doze.hcl"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "which.txt"), []byte(host), 0o644); err != nil {
			t.Fatal(err)
		}
		run := func(args ...string) (string, error) {
			cmd := exec.Command(doze, args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "NO_COLOR=1", "DOZE_MODULES=off")
			out, err := cmd.CombinedOutput()
			return string(out), err
		}
		t.Cleanup(func() { run("down") })
		if out, err := run("up"); err != nil {
			t.Fatalf("doze up in %s: %v\n%s", dir, err, out)
		}
		waitFor(t, host+" resolves through the system resolver", func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			got, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return err
			}
			if len(got) == 0 || !names.OnThisMachine(net.ParseIP(got[0])) || got[0] == "127.0.0.1" {
				return fmt.Errorf("resolved to %v, want an address of its own on this machine", got)
			}
			addrs[host] = got[0]
			return nil
		})
	}
	if addrs[hosts[0]] == addrs[hosts[1]] {
		t.Fatalf("both stacks' apps are at %s", addrs[hosts[0]])
	}
	// The app serves its project's directory, so which.txt says which one answered.
	for _, host := range hosts {
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + host + ":18431/which.txt")
		if err != nil {
			t.Fatalf("reaching %s by name: %v", host, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || string(body) != host {
			t.Fatalf("%s:18431 answered %d %q, want its own project", host, resp.StatusCode, body)
		}
	}
	t.Logf("two stacks, one port: %s at %s and %s at %s", hosts[0], addrs[hosts[0]], hosts[1], addrs[hosts[1]])
}
