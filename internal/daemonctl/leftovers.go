package daemonctl

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// A module is a go-plugin child of the daemon. Nothing ties its life to the
// daemon's: macOS has no parent-death signal, and a go-plugin server does not
// watch its parent. The daemon reaps its modules (and the engines it
// supervises) on a clean shutdown; if it exits any other way (a crash, a
// kill, a module launched after the reap), they are reparented to PID 1 and
// keep running. So Stop does not take "the daemon's pid is gone" for "the
// stack is stopped": it remembers what the daemon had started, and after the
// daemon exits it checks each one, and stops what is left.

// Leftover is a process the daemon started that was still running after the
// daemon exited.
type Leftover struct {
	PID     int    `json:"pid"`
	Command string `json:"command"` // the executable's base name, e.g. valkey-plugin
	// Stopped is true when Stop ended it; false when it is still running
	// (Stop returns an error naming it).
	Stopped bool `json:"stopped"`
}

// proc is one process as ps lists it.
type proc struct {
	pid, ppid int
	command   string // the full command line
}

// psList is how the process table is read: `ps -A -o pid=,ppid=,command=`.
// A test may replace it.
var psList = func() ([]proc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil, err
	}
	var procs []proc
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		procs = append(procs, proc{pid: pid, ppid: ppid, command: strings.Join(f[2:], " ")})
	}
	return procs, nil
}

// descendants is every process below root in procs, by pid, with its command.
func descendants(root int, procs []proc) map[int]string {
	children := map[int][]proc{}
	for _, p := range procs {
		if p.pid != p.ppid {
			children[p.ppid] = append(children[p.ppid], p)
		}
	}
	out := map[int]string{}
	queue := []int{root}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, c := range children[next] {
			if _, seen := out[c.pid]; !seen && c.pid != root {
				out[c.pid] = c.command
				queue = append(queue, c.pid)
			}
		}
	}
	return out
}

// tracker remembers every process seen below the daemon while it runs.
type tracker struct {
	root int
	seen map[int]string
}

func newTracker(root int) *tracker { return &tracker{root: root, seen: map[int]string{}} }

// look adds what is below the daemon now. A ps that fails adds nothing.
func (t *tracker) look() {
	procs, err := psList()
	if err != nil {
		return
	}
	for pid, cmd := range descendants(t.root, procs) {
		t.seen[pid] = cmd
	}
}

// survivors is each remembered process still running as the same command:
// a pid the system has since given to another program is not ours.
func (t *tracker) survivors() map[int]string {
	if len(t.seen) == 0 {
		return nil
	}
	procs, err := psList()
	if err != nil {
		// Without the table, fall back to "is the pid alive"; a reused pid
		// would be stopped wrongly, so say nothing rather than guess.
		return nil
	}
	now := map[int]string{}
	for _, p := range procs {
		now[p.pid] = p.command
	}
	out := map[int]string{}
	for pid, cmd := range t.seen {
		if cur, ok := now[pid]; ok && cur == cmd {
			out[pid] = cmd
		}
	}
	return out
}

// leftoverGrace is how long a leftover is given to exit on its own, and then
// again after SIGTERM, before it is killed.
var leftoverGrace = 3 * time.Second

// reapLeftovers stops what the daemon left running: a grace period to exit
// on its own (a module mid-exit), then SIGTERM, then SIGKILL. Only the exact
// pids remembered, and only while they still run the same command.
func (t *tracker) reapLeftovers() ([]Leftover, error) {
	left := waitGone(t, leftoverGrace)
	if len(left) == 0 {
		return nil, nil
	}
	report := map[int]*Leftover{}
	for pid, cmd := range left {
		report[pid] = &Leftover{PID: pid, Command: baseCommand(cmd)}
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	left = waitGone(t, leftoverGrace)
	for pid := range left {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	left = waitGone(t, time.Second)
	var out []Leftover
	var still []string
	for pid, l := range report {
		if _, alive := left[pid]; alive {
			still = append(still, fmt.Sprintf("%s (pid %d)", l.Command, pid))
		} else {
			l.Stopped = true
		}
		out = append(out, *l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	if len(still) > 0 {
		sort.Strings(still)
		return out, fmt.Errorf("the daemon stopped, but %s is still running and could not be stopped", strings.Join(still, ", "))
	}
	return out, nil
}

// waitGone polls until none of the tracked processes is left, or d passes,
// and returns those still running.
func waitGone(t *tracker, d time.Duration) map[int]string {
	deadline := time.Now().Add(d)
	for {
		left := t.survivors()
		if len(left) == 0 || time.Now().After(deadline) {
			return left
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// baseCommand is the executable's base name from a ps command line.
func baseCommand(cmd string) string {
	exe, _, _ := strings.Cut(cmd, " ")
	return filepath.Base(exe)
}
