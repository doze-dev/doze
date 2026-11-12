package daemonctl

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/doze-dev/doze-sdk/plugin"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/daemon"
)

// The test binary doubles as a daemon that exits without reaping its module:
// with DOZE_TEST_FAKE_DAEMON set it writes the pidfile, launches a real
// go-plugin module (the SDK's echo plugin) the way the daemon does, and on
// SIGTERM exits at once, never closing it. That is any daemon exit that misses
// the reap (a crash, a kill, a module launched after it).
func TestMain(m *testing.M) {
	if pidPath := os.Getenv("DOZE_TEST_FAKE_DAEMON"); pidPath != "" {
		fakeDaemon(pidPath, os.Getenv("DOZE_TEST_MODULE"))
		return
	}
	os.Exit(m.Run())
}

func fakeDaemon(pidPath, module string) {
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	if _, err := plugin.Launch(module, nil); err != nil {
		os.Exit(3)
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		os.Exit(4)
	}
	<-term
	os.Exit(0) // the module is left running: no Close
}

// buildEchoModule builds the SDK's echo plugin, a real go-plugin module.
func buildEchoModule(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the daemon lifecycle is unix-only")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("needs the go tool to build a module")
	}
	bin := filepath.Join(t.TempDir(), "echo-plugin")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/doze-dev/doze-sdk/plugin/echo").CombinedOutput()
	if err != nil {
		t.Fatalf("building the echo module: %v\n%s", err, out)
	}
	return bin
}

// childrenRunning is the pids running exe whose parent is ppid (0: any parent).
func childrenRunning(t *testing.T, exe string, ppid int) []int {
	t.Helper()
	procs, err := psList()
	if err != nil {
		t.Fatal(err)
	}
	var out []int
	for _, p := range procs {
		if strings.HasPrefix(p.command, exe) && (ppid == 0 || p.ppid == ppid) {
			out = append(out, p.pid)
		}
	}
	return out
}

// down used to print "✓ doze stopped" when the daemon's pid was gone, with
// the valkey module still running under PID 1 (F7). A module is a go-plugin
// child: it does not die with the daemon. Stop now finds what the daemon left
// and stops it, and says so.
func TestStopLeavesNoModuleRunning(t *testing.T) {
	module := buildEchoModule(t)
	t.Setenv("DOZE_HOME", t.TempDir())
	dir := t.TempDir()
	cfg, err := config.ParseShape([]byte("stack = \"orphan\"\n"), filepath.Join(dir, "doze.hcl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.RunDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	pidPath := daemon.PidFilePath(cfg)

	self, _ := os.Executable()
	d := exec.Command(self)
	d.Env = append(os.Environ(), "DOZE_TEST_FAKE_DAEMON="+pidPath, "DOZE_TEST_MODULE="+module)
	d.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // as daemonctl.Start does
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = d.Wait(); close(exited) }() // reap it, or it lingers as a zombie
	t.Cleanup(func() {
		// Whatever the outcome, kill exactly the pids this test started.
		_ = d.Process.Kill()
		for _, pid := range childrenRunning(t, module, 0) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})

	deadline := time.Now().Add(15 * time.Second)
	for Pid(cfg) == 0 || len(childrenRunning(t, module, d.Process.Pid)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the fake daemon and its module did not come up")
		}
		time.Sleep(50 * time.Millisecond)
	}
	mods := childrenRunning(t, module, d.Process.Pid)

	old := leftoverGrace
	leftoverGrace = 500 * time.Millisecond
	t.Cleanup(func() { leftoverGrace = old })

	rep, err := Stop(cfg)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	<-exited
	if !rep.Stopped {
		t.Fatal("Stop says no daemon was running")
	}
	if left := childrenRunning(t, module, 0); len(left) > 0 {
		t.Fatalf("Stop reported the stack stopped, and the module (pid %v) is still running", left)
	}
	if len(rep.Leftovers) != 1 || rep.Leftovers[0].PID != mods[0] || !rep.Leftovers[0].Stopped || rep.Leftovers[0].Command != "echo-plugin" {
		t.Errorf("leftovers = %+v, want echo-plugin pid %d, stopped", rep.Leftovers, mods[0])
	}
}

// A daemon that reaps its modules leaves nothing, and Stop reports nothing.
func TestStopReportsNothingWhenTheDaemonReaped(t *testing.T) {
	tr := newTracker(1 << 30) // no such process: nothing below it
	tr.look()
	left, err := tr.reapLeftovers()
	if err != nil || len(left) != 0 {
		t.Errorf("leftovers = %+v, err = %v", left, err)
	}
}

// A pid the system has given to another program since is not a leftover.
func TestAReusedPidIsNotStopped(t *testing.T) {
	old := psList
	t.Cleanup(func() { psList = old })
	psList = func() ([]proc, error) {
		return []proc{{pid: 50, ppid: 1, command: "/usr/bin/something-else"}}, nil
	}
	tr := &tracker{root: 40, seen: map[int]string{50: "/home/.doze/modules/doze/valkey/bin/valkey-plugin"}}
	if s := tr.survivors(); len(s) != 0 {
		t.Errorf("survivors = %v, want none: pid 50 runs another program now", s)
	}
}

func TestDescendants(t *testing.T) {
	procs := []proc{
		{pid: 10, ppid: 1, command: "doze __daemon"},
		{pid: 11, ppid: 10, command: "valkey-plugin"},
		{pid: 12, ppid: 10, command: "valkey-server"},
		{pid: 13, ppid: 12, command: "helper"},
		{pid: 20, ppid: 1, command: "other"},
	}
	got := descendants(10, procs)
	if len(got) != 3 || got[11] != "valkey-plugin" || got[13] != "helper" || got[20] != "" {
		t.Errorf("descendants = %v", got)
	}
}
