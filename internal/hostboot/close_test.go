package hostboot

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/doze-dev/doze-sdk/engine"

	"github.com/doze-dev/doze/internal/modules"
)

// modulesRunning is the pids of this process's children running exe.
func modulesRunning(t *testing.T, exe string) []int {
	t.Helper()
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,command=").Output()
	if err != nil {
		t.Fatal(err)
	}
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !strings.HasPrefix(f[2], exe) {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		if ppid == os.Getpid() {
			pids = append(pids, pid)
		}
	}
	return pids
}

// After Close the host launches no module. A lookup still running as the
// process ends (a wake, a status call in the daemon after its reap) used to
// start the module again, with nothing left to reap it: when the process
// exited, the module ran on under PID 1 (one way to F7's leftover).
func TestNoModuleIsLaunchedAfterClose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix process table")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("needs the go tool to build a module")
	}
	bin := filepath.Join(t.TempDir(), "echo-plugin")
	if out, err := exec.Command("go", "build", "-o", bin, "github.com/doze-dev/doze-sdk/plugin/echo").CombinedOutput(); err != nil {
		t.Fatalf("building the echo module: %v\n%s", err, out)
	}
	t.Cleanup(modules.UseLocalPlugin("echo", bin))
	t.Cleanup(func() {
		for _, pid := range modulesRunning(t, bin) { // exactly the pids this test started
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})

	t.Setenv("DOZE_MODULES", "off") // the local module only: no registry
	h, err := Init(Options{Home: t.TempDir(), LockPath: func() string { return "" }, PersistLock: func() bool { return false }})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := engine.Lookup("echo"); !ok {
		t.Fatal("the echo module did not load")
	}
	if n := len(modulesRunning(t, bin)); n != 1 {
		t.Fatalf("%d echo modules running, want 1", n)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if left := modulesRunning(t, bin); len(left) != 0 {
		t.Fatalf("Close left the module running: %v", left)
	}

	if _, ok := engine.Lookup("echo"); ok {
		t.Error("a lookup after Close found a module")
	}
	if left := modulesRunning(t, bin); len(left) != 0 {
		t.Errorf("a lookup after Close launched the module again (pid %v), and nothing will reap it", left)
	}
}
