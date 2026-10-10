//go:build bench

// Package bench measures the two numbers doze's pitch rests on, against the
// fixture stack in examples/shop: how much memory the daemon holds when every
// service is asleep, and how long a sleeping module takes to wake.
//
// Run:
//
//	make bench            (or: go test -tags bench -count=1 -v ./test/bench/)
//
// It boots the real CLI, real module plugins and real engine binaries in an
// isolated DOZE_HOME, so the first run downloads them. It needs the fixture's
// ports free (5432, 6379, 9092, 4566, 8080, 8090) and skips when they are not.
//
// The idle figure is enforced against budget.json. The wake figures are
// recorded, not enforced: they depend on the machine. Set DOZE_BENCH_OUT to a
// path to keep the full result as JSON.
package bench

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
)

// fixturePorts are the ports examples/shop declares.
var fixturePorts = []int{5432, 6379, 9092, 4566, 8080, 8090}

// modules are the fixture's modules, in the order they are woken.
var modules = []string{"db", "cache", "events", "cloud"}

const wakeRounds = 5

// Result is one run of the bench.
type Result struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	// Idle is the daemon with every service asleep and no client connected.
	Idle Memory `json:"idle"`
	// Wake is the time a `boot` request takes for a sleeping module whose data
	// is already provisioned: spawn, readiness, reply.
	Wake map[string]Latency `json:"wake"`
}

// Memory is resident memory in kilobytes.
type Memory struct {
	DaemonKB int `json:"daemon_kb"` // the doze daemon process alone
	// TreeKB adds every process the daemon keeps alive while idle — the module
	// plugins it holds warm. It is what "at rest" actually costs.
	TreeKB    int    `json:"tree_kb"`
	Processes []Proc `json:"processes"`
}

// Proc is one process counted in the idle figure.
type Proc struct {
	Name string `json:"name"`
	KB   int    `json:"kb"`
}

// Latency summarises wakeRounds wakes of one module, in milliseconds.
type Latency struct {
	MedianMS float64 `json:"median_ms"`
	MinMS    float64 `json:"min_ms"`
	MaxMS    float64 `json:"max_ms"`
}

// Budget is the committed ceiling for the idle figure on one OS: the first
// measurement plus about a tenth. budget.json holds one per OS; an OS with no
// entry is measured and reported but not enforced.
type Budget struct {
	IdleDaemonKB int `json:"idle_daemon_kb"`
	IdleTreeKB   int `json:"idle_tree_kb"`
}

func TestFixtureStack(t *testing.T) {
	for _, p := range fixturePorts {
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err != nil {
			t.Skipf("port %d is in use; the fixture stack cannot start here", p)
		}
		ln.Close()
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// A short home: the control socket lives under it, and a unix socket path
	// has a hard length limit that the usual temp dir on macOS exceeds.
	home, err := os.MkdirTemp("/tmp", "dzb")
	if err != nil {
		t.Fatal(err)
	}
	doze := filepath.Join(home, "doze")
	build := exec.Command("go", "build", "-o", doze, "./cmd/doze")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building doze: %v\n%s", err, out)
	}
	// A copy of the fixture, so the run writes nothing into the repo.
	stack := filepath.Join(home, "shop")
	if out, err := exec.Command("cp", "-R", filepath.Join(root, "examples", "shop"), stack).CombinedOutput(); err != nil {
		t.Fatalf("copying the fixture: %v\n%s", err, out)
	}
	env := append(os.Environ(),
		"DOZE_HOME="+filepath.Join(home, "h"),
		"DOZE_VAR_domains=false", // bind on 127.0.0.1 and leave the shared .doze zone alone
		"NO_COLOR=1",
	)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(doze, args...)
		cmd.Dir, cmd.Env = stack, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("doze %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	t.Cleanup(func() {
		cmd := exec.Command(doze, "down")
		cmd.Dir, cmd.Env = stack, env
		_ = cmd.Run()
		_ = os.RemoveAll(home)
	})

	// Bring everything up once: downloads, provisions data, compiles the apps.
	run("up")

	socks, _ := filepath.Glob(filepath.Join(home, "h", "projects", "*", "run", "doze.admin.sock"))
	if len(socks) != 1 {
		t.Fatalf("expected one control socket under the bench home, found %v", socks)
	}
	client := control.NewClient(socks[0])
	do := func(op, name string) time.Duration {
		t.Helper()
		start := time.Now()
		resp, err := client.Do(control.Request{Op: op, DB: name})
		if err != nil || resp.Error != "" {
			t.Fatalf("%s %q: %v %s", op, name, err, resp.Error)
		}
		return time.Since(start)
	}

	// Everything to sleep; the daemon stays.
	do("down", "")
	pidRaw, err := os.ReadFile(filepath.Join(filepath.Dir(socks[0]), "doze.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(pidRaw), "\n", 2)[0]))
	if err != nil {
		t.Fatalf("daemon pid: %v", err)
	}
	res := Result{OS: runtime.GOOS, Arch: runtime.GOARCH, Wake: map[string]Latency{}}

	// Let the Go heap settle, then take the lowest of a few samples: the
	// figure is what the daemon holds at rest, not a momentary peak.
	time.Sleep(5 * time.Second)
	for i := 0; i < 5; i++ {
		m := measure(t, pid)
		if i == 0 || m.TreeKB < res.Idle.TreeKB {
			res.Idle = m
		}
		time.Sleep(time.Second)
	}
	if resp, err := client.Do(control.Request{Op: "status"}); err == nil {
		for _, inst := range resp.Instances {
			if inst.PID != 0 {
				t.Errorf("%s is still running (pid %d): the idle figure is not an idle figure", inst.Name, inst.PID)
			}
		}
	}

	for _, name := range modules {
		var took []float64
		for i := 0; i < wakeRounds; i++ {
			took = append(took, float64(do("boot", name).Microseconds())/1000)
			do("down", name)
		}
		sort.Float64s(took)
		res.Wake[name] = Latency{MedianMS: took[len(took)/2], MinMS: took[0], MaxMS: took[len(took)-1]}
	}

	report(t, res)

	budgets := map[string]Budget{}
	raw, err := os.ReadFile("budget.json")
	if err != nil {
		t.Fatalf("reading the budget: %v", err)
	}
	if err := json.Unmarshal(raw, &budgets); err != nil {
		t.Fatalf("budget.json: %v", err)
	}
	budget, ok := budgets[runtime.GOOS]
	if !ok {
		t.Logf("budget.json has no entry for %s: measured, not enforced", runtime.GOOS)
		return
	}
	if res.Idle.DaemonKB > budget.IdleDaemonKB {
		t.Errorf("the idle daemon holds %d kB; the budget is %d kB", res.Idle.DaemonKB, budget.IdleDaemonKB)
	}
	if res.Idle.TreeKB > budget.IdleTreeKB {
		t.Errorf("the idle daemon and what it keeps alive hold %d kB; the budget is %d kB", res.Idle.TreeKB, budget.IdleTreeKB)
	}
}

// measure reads the resident memory of pid and of every descendant.
func measure(t *testing.T, pid int) Memory {
	t.Helper()
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,rss=,comm=").Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	rss := map[int]int{}
	name := map[int]string{}
	children := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		p, _ := strconv.Atoi(f[0])
		pp, _ := strconv.Atoi(f[1])
		kb, _ := strconv.Atoi(f[2])
		rss[p] = kb
		name[p] = filepath.Base(f[3])
		children[pp] = append(children[pp], p)
	}
	if _, ok := rss[pid]; !ok {
		t.Fatalf("the daemon (pid %d) is not running", pid)
	}
	m := Memory{DaemonKB: rss[pid]}
	var walk func(p int)
	walk = func(p int) {
		m.TreeKB += rss[p]
		m.Processes = append(m.Processes, Proc{Name: name[p], KB: rss[p]})
		for _, c := range children[p] {
			walk(c)
		}
	}
	walk(pid)
	return m
}

func report(t *testing.T, res Result) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s/%s\n", res.OS, res.Arch)
	fmt.Fprintf(&b, "idle, everything asleep:  daemon %.1f MB   with what it keeps alive %.1f MB\n",
		float64(res.Idle.DaemonKB)/1024, float64(res.Idle.TreeKB)/1024)
	for _, p := range res.Idle.Processes {
		fmt.Fprintf(&b, "  %-22s %6.1f MB\n", p.Name, float64(p.KB)/1024)
	}
	for _, name := range modules {
		l := res.Wake[name]
		fmt.Fprintf(&b, "wake %-7s median %7.1f ms   (min %.1f, max %.1f, n=%d)\n", name, l.MedianMS, l.MinMS, l.MaxMS, wakeRounds)
	}
	t.Log(b.String())
	if path := os.Getenv("DOZE_BENCH_OUT"); path != "" {
		raw, _ := json.MarshalIndent(res, "", "  ")
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
}
