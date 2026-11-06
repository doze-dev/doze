package portholder

import (
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ListeningPorts returns the TCP ports that root and its descendants listen
// on, sorted and without repeats. It asks the system (ps for the tree, then
// lsof, or ss where lsof is missing), so it costs a few process starts: call
// it on a failure path, not in a loop. An empty result means nothing listens,
// or the system could not say.
func ListeningPorts(root int) []int {
	if root <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=", "-o", "ppid=").Output()
	pids := []int{root}
	if err == nil {
		pids = descendants(root, out)
	}
	if _, err := exec.LookPath("lsof"); err == nil {
		list := make([]string, len(pids))
		for i, p := range pids {
			list[i] = strconv.Itoa(p)
		}
		out, _ := exec.CommandContext(ctx, "lsof", "-nP", "-a", "-p", strings.Join(list, ","), "-iTCP", "-sTCP:LISTEN", "-Fn").Output()
		return lsofPorts(out)
	}
	out, err = exec.CommandContext(ctx, "ss", "-ltnpH").Output()
	if err != nil {
		return nil
	}
	return ssPorts(out, pids)
}

// descendants reads `ps -o pid= -o ppid=` output and returns root and every
// process below it.
func descendants(root int, ps []byte) []int {
	children := map[int][]int{}
	for _, line := range strings.Split(string(ps), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil || pid == ppid {
			continue
		}
		children[ppid] = append(children[ppid], pid)
	}
	seen := map[int]bool{root: true}
	out := []int{root}
	for i := 0; i < len(out); i++ {
		for _, c := range children[out[i]] {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// lsofPorts reads the n<address> lines of lsof -Fn output.
func lsofPorts(out []byte) []int {
	var ports []int
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) > 1 && line[0] == 'n' {
			ports = append(ports, portOf(string(line[1:])))
		}
	}
	return uniq(ports)
}

var ssPID = regexp.MustCompile(`pid=(\d+)`)

// ssPorts reads `ss -ltnpH` lines — state, recv-q, send-q, local address,
// peer, then users:(("node",pid=12,fd=3)) — keeping those held by pids.
func ssPorts(out []byte, pids []int) []int {
	want := map[int]bool{}
	for _, p := range pids {
		want[p] = true
	}
	var ports []int
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		for _, m := range ssPID.FindAllStringSubmatch(line, -1) {
			if pid, _ := strconv.Atoi(m[1]); want[pid] {
				ports = append(ports, portOf(f[3]))
				break
			}
		}
	}
	return uniq(ports)
}

// portOf is the port at the end of an address such as "127.0.0.1:5173",
// "*:5173" or "[::1]:5173"; 0 when there is none.
func portOf(addr string) int {
	i := strings.LastIndexByte(addr, ':')
	if i < 0 {
		return 0
	}
	p, _ := strconv.Atoi(addr[i+1:])
	return p
}

func uniq(ports []int) []int {
	sort.Ints(ports)
	out := ports[:0]
	for i, p := range ports {
		if p > 0 && (i == 0 || p != ports[i-1]) {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
