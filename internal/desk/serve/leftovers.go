package serve

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	names "github.com/doze-dev/doze-names"

	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/view"
	"github.com/doze-dev/doze/internal/projects"
)

// Leftovers are what stacks leave behind when they go out of step with
// doze: engine processes whose daemon died, daemons running for stacks the
// Library does not list (another home, a deleted folder), state folders
// with no stack, and domain routes for folders that are gone. Each has a
// way to clear it; nothing is cleared unasked.
func (s *Service) Leftovers() []view.Leftover {
	if s.FakeStack != nil {
		// A fixture's stacks are made up; this machine's processes are not
		// theirs to judge, let alone stop.
		return nil
	}
	home := s.hub.Home()
	var out []view.Leftover
	known := map[string]bool{}
	for _, st := range s.hub.Stacks() {
		if st.Dir != "" {
			known[canon(filepath.Join(st.Dir, "doze.hcl"))] = true
		}
	}
	modules := canon(filepath.Join(home, "modules")) + string(filepath.Separator)
	for _, p := range processes() {
		switch {
		case p.daemon != "" && !known[canon(p.daemon)]:
			why := "for a stack this Library does not list"
			if _, err := os.Stat(p.daemon); err != nil {
				why = "for a doze.hcl that is gone"
			}
			out = append(out, view.Leftover{
				Kind: "daemon", ID: "pid:" + strconv.Itoa(p.pid), What: "a stack's daemon, " + why,
				Detail: fmt.Sprintf("pid %d · %s · since %s", p.pid, tilde(p.daemon), p.since), Action: "Stop",
			})
		case p.ppid == 1 && (strings.HasPrefix(canon(p.exe), modules) || isPlugin(p.exe)):
			out = append(out, view.Leftover{
				Kind: "engine", ID: "pid:" + strconv.Itoa(p.pid), What: filepath.Base(p.exe) + ", whose daemon is gone",
				Detail: fmt.Sprintf("pid %d · since %s", p.pid, p.since), Action: "Stop",
			})
		}
	}
	dirs, _ := filepath.Glob(filepath.Join(home, "projects", "*"))
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, projects.FileName)); err == nil {
			continue // a stack: the Library lists it, missing files and all
		}
		if _, alive, ok := hub.DaemonPID(dir); ok && alive {
			continue // running: listed above if it is not this Library's
		}
		size := du(dir)
		out = append(out, view.Leftover{
			Kind: "state", ID: "dir:" + dir, What: "data of a stack doze no longer lists",
			Detail: tilde(dir) + " · " + size, Action: "Delete…", Danger: true,
		})
	}
	for _, d := range names.StaleRoutes(home) {
		out = append(out, view.Leftover{
			Kind: "route", ID: "route:" + d, What: "a route for " + d,
			Detail: "no project with a doze.hcl declares it any more", Action: "Remove",
		})
	}
	return out
}

// ClearLeftover clears one leftover by its ID; "all" clears every one
// that is not a deletion of data.
func (s *Service) ClearLeftover(id string) error {
	if id == "all" {
		var failed []string
		for _, l := range s.Leftovers() {
			if l.Danger {
				continue
			}
			if err := s.clear(l.ID); err != nil {
				failed = append(failed, l.What+": "+err.Error())
			}
		}
		s.changed()
		if len(failed) > 0 {
			return fmt.Errorf("%s", strings.Join(failed, "; "))
		}
		s.Say("cleaned up what was left behind", false)
		return nil
	}
	err := s.clear(id)
	s.changed()
	return err
}

func (s *Service) clear(id string) error {
	if s.FakeStack != nil {
		return errors.New("a fixture clears nothing on this machine")
	}
	kind, arg, _ := strings.Cut(id, ":")
	switch kind {
	case "pid":
		pid, err := strconv.Atoi(arg)
		if err != nil {
			return err
		}
		// Only what the list named: look again that it is still a leftover.
		still := false
		for _, l := range s.Leftovers() {
			if l.ID == id {
				still = true
			}
		}
		if !still {
			return fmt.Errorf("pid %d is no longer a leftover; nothing was stopped", pid)
		}
		_ = syscall.Kill(pid, syscall.SIGTERM)
		for i := 0; i < 50 && hub.Alive(pid); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if hub.Alive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		s.Say(fmt.Sprintf("stopped pid %d", pid), false)
		return nil
	case "dir":
		home := canon(filepath.Join(s.hub.Home(), "projects")) + string(filepath.Separator)
		if !strings.HasPrefix(canon(arg), home) {
			return fmt.Errorf("%s is not a doze state folder", arg)
		}
		if _, err := os.Stat(filepath.Join(arg, projects.FileName)); err == nil {
			return fmt.Errorf("%s belongs to a listed stack; nothing was deleted", arg)
		}
		if err := os.RemoveAll(arg); err != nil {
			return err
		}
		s.Say("deleted "+tilde(arg), false)
		return nil
	case "route":
		if err := names.RouteRemoved(s.hub.Home(), arg); err != nil {
			return err
		}
		s.Say("removed the route for "+arg, false)
		return nil
	}
	return fmt.Errorf("no leftover called %q", id)
}

// proc is one process, as ps lists it.
type proc struct {
	pid, ppid int
	since     string
	exe       string
	// daemon is the doze.hcl a "doze __daemon --config" process serves.
	daemon string
}

// processes are this user's processes, from ps: enough to tell a doze
// daemon or an engine plugin and who started it.
func processes() []proc {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-U", strconv.Itoa(os.Getuid()), "-o", "pid=,ppid=,lstart=,command=").Output()
	if err != nil {
		return nil
	}
	var ps []proc
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		// pid ppid Day Mon DD HH:MM:SS YYYY command…
		if len(f) < 8 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		p := proc{pid: pid, ppid: ppid, since: f[3] + " " + f[4] + " " + f[5][:5], exe: f[7]}
		args := f[8:]
		for i, a := range args {
			if a == "__daemon" {
				for j := i + 1; j+1 < len(args); j++ {
					if args[j] == "--config" {
						p.daemon = args[j+1]
					}
				}
			}
		}
		ps = append(ps, p)
	}
	return ps
}

// canon is a path with its links followed (/var is /private/var on a Mac),
// so two spellings of one file compare equal.
func canon(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func tilde(p string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
}

// du is a folder's size on disk, in words.
func du(dir string) string {
	var n int64
	_ = filepath.Walk(dir, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			n += fi.Size()
		}
		return nil
	})
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}

// leftoverCount is how many leftovers there are, from a look at most
// leftoverEvery old, taken in the background: the Library shows the number
// on every push, and a look runs ps.
func (s *Service) leftoverCount() int {
	s.mu.Lock()
	n, stale := s.leftoversN, s.now().Sub(s.leftoversAt) > leftoverEvery
	start := stale && !s.leftoversBusy
	if start {
		s.leftoversBusy = true
	}
	s.mu.Unlock()
	if start {
		go func() {
			got := len(s.Leftovers())
			s.mu.Lock()
			changed := got != s.leftoversN
			s.leftoversN, s.leftoversAt, s.leftoversBusy = got, s.now(), false
			s.mu.Unlock()
			if changed {
				s.changed()
			}
		}()
	}
	return n
}

const leftoverEvery = 15 * time.Second

// isPlugin says a path is an engine plugin in some doze home's module
// cache (<home>/modules/<ns>/<name>/<version>/bin/<name>-plugin), so an
// orphan from a test's or another home's daemon is found too.
func isPlugin(exe string) bool {
	parts := strings.Split(filepath.ToSlash(exe), "/")
	n := len(parts)
	return n >= 6 && parts[n-2] == "bin" && parts[n-6] == "modules" && strings.HasSuffix(parts[n-1], "-plugin")
}
