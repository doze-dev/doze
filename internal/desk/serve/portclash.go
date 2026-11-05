package serve

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/doze-dev/doze/internal/desk/view"
)

// reInUse finds the port in a bind failure, in the words Go, Node, Python
// and Ruby use: "listen tcp 127.0.0.1:3000: bind: address already in use",
// "EADDRINUSE: address already in use :::3000", "[Errno 48] Address already
// in use", with the port nearest the words.
var (
	reInUse = regexp.MustCompile(`(?i)address already in use|EADDRINUSE|Errno 48|Errno 98`)
	rePort  = regexp.MustCompile(`:(\d{2,5})\b`)
)

// portInUse is the port a crash says was taken, or 0.
func portInUse(texts ...string) int {
	for _, t := range texts {
		if !reInUse.MatchString(t) {
			continue
		}
		if m := rePort.FindAllStringSubmatch(t, -1); m != nil {
			if p, err := strconv.Atoi(m[len(m)-1][1]); err == nil && p > 0 && p < 65536 {
				return p
			}
		}
	}
	return 0
}

// portHolder is the process listening on port, from lsof, or ss where
// there is no lsof (a Linux box). Nil when nothing is found.
func portHolder(port int) *view.PortHolder {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc").Output(); err == nil {
		h := &view.PortHolder{Port: port}
		sc := bufio.NewScanner(strings.NewReader(string(out)))
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "p") && h.PID == 0:
				h.PID, _ = strconv.Atoi(line[1:])
			case strings.HasPrefix(line, "c") && h.Command == "":
				h.Command = line[1:]
			}
		}
		if h.PID > 0 {
			h.Doze = isDoze(h.Command)
			return h
		}
	}
	// ss -ltnpH 'sport = :3000' → … users:(("node",pid=7731,fd=20))
	out, err := exec.CommandContext(ctx, "ss", "-ltnpH", "sport = :"+strconv.Itoa(port)).Output()
	if err != nil {
		return nil
	}
	m := regexp.MustCompile(`\("([^"]+)",pid=(\d+)`).FindStringSubmatch(string(out))
	if m == nil {
		return nil
	}
	pid, _ := strconv.Atoi(m[2])
	return &view.PortHolder{Port: port, PID: pid, Command: m[1], Doze: isDoze(m[1])}
}

func isDoze(cmd string) bool {
	return cmd == "doze" || strings.HasPrefix(cmd, "doze-")
}

// StopHolder sends TERM to the process holding name's port, after looking
// again that it still does, and then restarts name. doze never signals a
// process it did not start without being asked, and only the one the page
// named.
func (s *Service) StopHolder(key, name string, pid int) error {
	inst := s.Instance(key, name)
	if inst.Crash == nil || inst.Crash.Held == nil || inst.Crash.Held.PID != pid {
		return errors.New("that process no longer holds the port; nothing was stopped")
	}
	if inst.Crash.Held.Doze {
		return errors.New("that is doze's own; bring its stack down from the Library instead")
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		s.Say(fmt.Sprintf("could not stop pid %d: %v", pid, err), true)
		return err
	}
	s.Say(fmt.Sprintf("sent TERM to %s (pid %d); starting %s", inst.Crash.Held.Command, pid, name), false)
	go func() {
		// A polite TERM takes a moment to be heard.
		for i := 0; i < 20; i++ {
			if syscall.Kill(pid, 0) != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = s.Do(key, "restart", name)
	}()
	return nil
}

func lineTexts(ls []view.Line) []string {
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		out = append(out, l.Line)
	}
	return out
}

// ownedBy marks a holder that is one of doze's own: a service of some
// stack the hub knows (its pid, or its parent's, is that service's), so
// the page says which and does not offer to stop it.
func (s *Service) ownedBy(h *view.PortHolder) {
	if h == nil {
		return
	}
	pids := []int{h.PID}
	if out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(h.PID)).Output(); err == nil {
		if pp, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && pp > 1 {
			pids = append(pids, pp)
		}
	}
	for _, st := range s.hub.Stacks() {
		for _, v := range st.Instances {
			for _, p := range pids {
				if v.PID > 0 && v.PID == p {
					h.Doze, h.Stack, h.Service = true, st.Name, v.Name
					return
				}
			}
		}
	}
}
