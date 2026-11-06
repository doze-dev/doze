package hub

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// DaemonPID is the pid a stack's state folder says its daemon has, and
// whether that process is alive. ok is false when there is no pid file
// (a fixture's stack, or one never started), so nothing can be said.
func DaemonPID(key string) (pid int, alive, ok bool) {
	raw, err := os.ReadFile(filepath.Join(key, "run", "doze.pid"))
	if err != nil {
		return 0, false, false
	}
	pid, err = strconv.Atoi(strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0]))
	if err != nil || pid <= 0 {
		return 0, false, false
	}
	return pid, Alive(pid), true
}

// Alive reports whether a process with the pid exists.
func Alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
