//go:build deskbench

// Package deskbench measures the app the way test/bench measures the daemon,
// against the shop fixture, and holds it to budget.json per OS (design D14,
// "light by rule, checked in CI"): how fast the menu bar is ready, what the
// menu bar alone costs, how fast the window paints home and what it costs
// with WebKit, what is given back a minute after the window closes, and
// the CPU at rest.
//
// Run:
//
//	make deskbench        (or: go test -tags deskbench,purego -count=1 -v ./test/deskbench/)
//
// It builds doze with the page, runs it over the fixture (no daemon, no
// ports) and reads the timing lines doze open --measure prints. Set
// DOZE_DESKBENCH_OUT to keep the result as JSON.
package deskbench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Result is one run.
type Result struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	// TrayReadyMS is how long after start the menu bar item is up.
	TrayReadyMS int `json:"tray_ready_ms"`
	// TrayKB is the app's footprint with the menu bar alone.
	TrayKB int `json:"tray_kb"`
	// WindowHomeMS is from the request to open the window to home painted.
	WindowHomeMS int `json:"window_home_ms"`
	// WindowKB is the app plus its web view processes with the window open.
	WindowKB int `json:"window_kb"`
	// AfterCloseKB is the same a minute after the window closed.
	AfterCloseKB int `json:"after_close_kb"`
	// RestCPUPct is the app's CPU over thirty seconds with the window hidden.
	RestCPUPct float64 `json:"rest_cpu_pct"`
	Processes  []Proc  `json:"processes"`
}

// Proc is one process counted with the window open.
type Proc struct {
	Name string `json:"name"`
	KB   int    `json:"kb"`
}

type Budget struct {
	TrayReadyMS  int     `json:"tray_ready_ms"`
	TrayKB       int     `json:"tray_footprint_kb"`
	WindowHomeMS int     `json:"window_home_ms"`
	WindowKB     int     `json:"window_footprint_kb"`
	AfterCloseKB int     `json:"after_close_footprint_kb"`
	RestCPUPct   float64 `json:"rest_cpu_pct"`
}

func TestApp(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the desk bench measures macOS; Linux is measured under Xvfb by desk.yml")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("budget.json")
	if err != nil {
		t.Fatal(err)
	}
	var budgets map[string]Budget
	if err := json.Unmarshal(raw, &budgets); err != nil {
		t.Fatal(err)
	}
	budget := budgets[runtime.GOOS]

	dir := t.TempDir()
	doze := filepath.Join(dir, "doze")
	build := exec.Command("go", "build", "-tags", "purego,production", "-o", doze, "./cmd/doze")
	build.Dir, build.Env = root, append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building doze: %v\n%s", err, out)
	}
	res := Result{OS: runtime.GOOS, Arch: runtime.GOARCH}

	// Scene 1: the menu bar alone, for ten seconds.
	t.Log("menu bar alone")
	run := runApp(t, doze, "open", "--fixture", "shop", "--measure", "--quit-after", "12s")
	res.TrayReadyMS = run.wait(t, "tray ready", 5*time.Second)
	time.Sleep(6 * time.Second)
	res.TrayKB = footprint(t, run.pid)
	run.done(t)

	// Scene 2: the window on home, then closed, then a minute of quiet.
	t.Log("the window, then closed")
	before := webkitPIDs()
	run = runApp(t, doze, "open", "--fixture", "shop", "--window", "--measure", "--close-after", "8s", "--quit-after", "80s")
	run.wait(t, "window shown", 5*time.Second)
	res.WindowHomeMS = run.wait(t, "home painted", 5*time.Second) + run.last("window shown")
	time.Sleep(4 * time.Second)
	res.WindowKB, res.Processes = footprintTree(t, run.pid, before)
	run.wait(t, "window closed", 10*time.Second)
	cpuBefore := cpuTime(t, run.pid)
	time.Sleep(30 * time.Second)
	res.RestCPUPct = float64(cpuTime(t, run.pid)-cpuBefore) / 30 * 100
	time.Sleep(30 * time.Second)
	res.AfterCloseKB, _ = footprintTree(t, run.pid, before)
	run.done(t)

	report(t, res)
	if res.TrayReadyMS > budget.TrayReadyMS {
		t.Errorf("menu bar ready in %d ms; the budget is %d", res.TrayReadyMS, budget.TrayReadyMS)
	}
	if res.TrayKB > budget.TrayKB {
		t.Errorf("menu bar alone is %d kB; the budget is %d", res.TrayKB, budget.TrayKB)
	}
	if res.WindowHomeMS > budget.WindowHomeMS {
		t.Errorf("window to home painted took %d ms; the budget is %d", res.WindowHomeMS, budget.WindowHomeMS)
	}
	if res.WindowKB > budget.WindowKB {
		t.Errorf("the window costs %d kB; the budget is %d", res.WindowKB, budget.WindowKB)
	}
	if res.AfterCloseKB > budget.AfterCloseKB {
		t.Errorf("a minute after closing the app holds %d kB; the budget is %d", res.AfterCloseKB, budget.AfterCloseKB)
	}
	if res.RestCPUPct > budget.RestCPUPct {
		t.Errorf("at rest the app uses %.2f%% CPU; the budget is %.2f", res.RestCPUPct, budget.RestCPUPct)
	}
}

// app is a running doze open with its timing lines.
type app struct {
	cmd   *exec.Cmd
	pid   int
	lines chan string
	seen  map[string]int
}

var measureLine = regexp.MustCompile(`^(?:desk|page): (.+?)(?: (\d+)ms.*)?$`)

func runApp(t *testing.T, doze string, args ...string) *app {
	t.Helper()
	cmd := exec.Command(doze, args...)
	cmd.Env = append(os.Environ(), "DOZE_HOME="+t.TempDir())
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	a := &app{cmd: cmd, pid: cmd.Process.Pid, lines: make(chan string, 64), seen: map[string]int{}}
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			a.lines <- sc.Text()
		}
		close(a.lines)
	}()
	return a
}

// wait blocks until a measure line starting with prefix arrives and returns
// its millisecond figure (0 when it has none).
func (a *app) wait(t *testing.T, prefix string, timeout time.Duration) int {
	t.Helper()
	if ms, ok := a.seen[prefix]; ok {
		return ms
	}
	deadline := time.After(timeout)
	for {
		select {
		case line, ok := <-a.lines:
			if !ok {
				t.Fatalf("doze exited before %q", prefix)
			}
			m := measureLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			ms, _ := strconv.Atoi(m[2])
			for _, p := range []string{"tray ready", "window shown", "home painted", "window closed"} {
				if strings.HasPrefix(m[1], p) {
					a.seen[p] = ms
				}
			}
			if strings.HasPrefix(m[1], prefix) {
				return ms
			}
		case <-deadline:
			t.Fatalf("no %q line within %s", prefix, timeout)
		}
	}
}

func (a *app) last(prefix string) int { return a.seen[prefix] }

func (a *app) done(t *testing.T) {
	t.Helper()
	_ = a.cmd.Process.Signal(os.Interrupt)
	go func() { time.Sleep(3 * time.Second); _ = a.cmd.Process.Kill() }()
	_ = a.cmd.Wait()
	for range a.lines {
	}
}

// footprint is the physical footprint of one process in kB, from macOS's
// own footprint tool.
func footprint(t *testing.T, pid int) int {
	t.Helper()
	out, err := exec.Command("footprint", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatalf("footprint: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "phys_footprint:" {
			return toKB(f[1], f[2])
		}
	}
	t.Fatalf("footprint said nothing for %d:\n%s", pid, out)
	return 0
}

func toKB(n, unit string) int {
	v, _ := strconv.ParseFloat(n, 64)
	switch strings.ToUpper(unit) {
	case "GB":
		return int(v * 1024 * 1024)
	case "MB":
		return int(v * 1024)
	case "KB":
		return int(v)
	}
	return int(v / 1024)
}

// webkitPIDs are the WebKit helper processes on the machine right now; the
// ones that appear after the app starts are its web view.
func webkitPIDs() map[int]bool {
	out, _ := exec.Command("pgrep", "-f", "com.apple.WebKit").Output()
	pids := map[int]bool{}
	for _, s := range strings.Fields(string(out)) {
		p, _ := strconv.Atoi(s)
		pids[p] = true
	}
	return pids
}

// footprintTree is the app plus every WebKit helper that appeared since
// before, in kB.
func footprintTree(t *testing.T, pid int, before map[int]bool) (int, []Proc) {
	t.Helper()
	total := footprint(t, pid)
	procs := []Proc{{Name: "doze", KB: total}}
	for p := range webkitPIDs() {
		if before[p] {
			continue
		}
		out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(p)).Output()
		if err != nil {
			continue
		}
		name := filepath.Base(strings.TrimSpace(string(out)))
		kb := footprint(t, p)
		total += kb
		procs = append(procs, Proc{Name: name, KB: kb})
	}
	return total, procs
}

// cpuTime is the process's CPU seconds so far.
func cpuTime(t *testing.T, pid int) float64 {
	t.Helper()
	out, err := exec.Command("ps", "-o", "cputime=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	// mm:ss.cc
	s := strings.TrimSpace(string(out))
	parts := strings.Split(s, ":")
	var secs float64
	for _, p := range parts {
		v, _ := strconv.ParseFloat(p, 64)
		secs = secs*60 + v
	}
	return secs
}

func report(t *testing.T, res Result) {
	t.Helper()
	t.Logf("menu bar ready %d ms, %d MB; window to home %d ms, %d MB with WebKit; %d MB a minute after close; %.2f%% CPU at rest",
		res.TrayReadyMS, res.TrayKB>>10, res.WindowHomeMS, res.WindowKB>>10, res.AfterCloseKB>>10, res.RestCPUPct)
	for _, p := range res.Processes {
		t.Logf("  %-40s %6d kB", p.Name, p.KB)
	}
	if out := os.Getenv("DOZE_DESKBENCH_OUT"); out != "" {
		b, _ := json.MarshalIndent(res, "", "  ")
		if err := os.WriteFile(out, b, 0o644); err != nil {
			t.Error(err)
		}
	}
	_ = fmt.Sprint
}
