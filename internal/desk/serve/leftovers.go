package serve

import (
	"io"

	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/doze-dev/doze/internal/adminprompt"
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
	// Routes and the authority are the machine's: removing them takes the
	// administrator's password, asked once for all the routes.
	s.mu.Lock()
	admin := s.admin != nil
	s.mu.Unlock()
	action := "Remove (one password)"
	if !admin {
		action = "Remove"
	}
	for _, d := range names.StaleRoutes(home) {
		out = append(out, view.Leftover{
			Kind: "route", ID: "route:" + d, What: "a route sending " + d + " to doze",
			Detail: "/etc/resolver/" + d + " · no project with a doze.hcl declares it any more", Action: action, Admin: true,
		})
	}
	if ca, ok := names.OpenCA(names.CADir(home)); ok && !names.AnyDeclared(home) {
		trusted := false
		for _, st := range names.TrustStatus(names.TrustRequest{CA: ca}) {
			if st.Name == "certificate authority" && st.Done {
				trusted = true
			}
		}
		l := view.Leftover{Kind: "authority", ID: "ca:", What: "doze's certificate authority, which no stack uses now",
			Detail: tilde(names.CADir(home)) + " · not trusted; its files only", Action: "Remove"}
		if trusted {
			l.What = "doze's certificate authority, still trusted by this Mac"
			l.Detail = tilde(names.CADir(home)) + " · no stack declares a domain any more"
			l.Action, l.Admin = action, true
		}
		out = append(out, l)
	}
	return out
}

// errNoPrompt says a machine leftover needs the administrator's password
// and this face cannot ask for it.
var errNoPrompt = errors.New("this needs the administrator's password, which doze can only ask for in the app: in a terminal, doze doctor --fix removes stale routes")

// clearRoutes removes the routes named, in one prompt, the way doze trust
// prunes them: the files go first, and only a route that is gone is
// forgotten from the record.
func (s *Service) clearRoutes(domains []string) error {
	s.mu.Lock()
	admin := s.admin
	s.mu.Unlock()
	if admin == nil {
		return errNoPrompt
	}
	home := s.hub.Home()
	err := names.Trust(names.Options{Out: io.Discard, Run: admin}, names.TrustRequest{Home: home, Prune: domains})
	if errors.Is(err, adminprompt.ErrCancelled) {
		return adminprompt.ErrCancelled
	}
	if err != nil {
		return err
	}
	left := map[string]bool{}
	for _, d := range names.StaleRoutes(home) {
		left[d] = true
	}
	var gone, kept []string
	for _, d := range domains {
		if left[d] {
			kept = append(kept, d)
		} else {
			gone = append(gone, d)
		}
	}
	s.forgetMachine()
	if len(gone) > 0 {
		s.Say("removed the route for "+strings.Join(gone, ", "), false)
	}
	if len(kept) > 0 {
		return fmt.Errorf("the route for %s is still there", strings.Join(kept, ", "))
	}
	return nil
}

// clearAuthority stops trusting doze's certificate authority (every doze
// root this Mac trusts) and deletes its files, when no stack declares a
// domain any more.
func (s *Service) clearAuthority() error {
	home := s.hub.Home()
	if names.AnyDeclared(home) {
		return errors.New("a stack declares a domain again; the authority stays")
	}
	ca, ok := names.OpenCA(names.CADir(home))
	if !ok {
		return nil
	}
	trusted := false
	for _, st := range names.TrustStatus(names.TrustRequest{CA: ca}) {
		if st.Name == "certificate authority" && st.Done {
			trusted = true
		}
	}
	if trusted {
		s.mu.Lock()
		admin := s.admin
		s.mu.Unlock()
		if admin == nil {
			return errNoPrompt
		}
		err := names.Untrust(names.Options{Out: io.Discard, Run: admin}, names.TrustRequest{Home: home, CA: ca, UntrustAll: true})
		if errors.Is(err, adminprompt.ErrCancelled) {
			return errors.New("cancelled; nothing was removed")
		}
		if err != nil {
			return err
		}
	}
	if err := names.RemoveCA(home); err != nil {
		return err
	}
	s.forgetMachine()
	s.Say("removed doze's certificate authority", false)
	return nil
}

// ClearLeftover clears one leftover by its ID; "all" clears every one
// that is not a deletion of data.
func (s *Service) ClearLeftover(id string) error {
	if id == "all" {
		var failed, routes []string
		for _, l := range s.Leftovers() {
			if l.Danger {
				continue
			}
			if l.Kind == "route" {
				routes = append(routes, strings.TrimPrefix(l.ID, "route:"))
				continue
			}
			if err := s.clear(l.ID); err != nil {
				failed = append(failed, l.What+": "+err.Error())
			}
		}
		if len(routes) > 0 && s.FakeStack == nil {
			if err := s.clearRoutes(routes); err != nil {
				failed = append(failed, "routes: "+err.Error())
			}
		}
		s.recountLeftovers()
		s.changed()
		if len(failed) > 0 {
			return fmt.Errorf("%s", strings.Join(failed, "; "))
		}
		s.Say("cleaned up what was left behind", false)
		return nil
	}
	err := s.clear(id)
	s.recountLeftovers()
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
		return s.clearRoutes([]string{arg})
	case "ca":
		return s.clearAuthority()
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
	safe, data := s.leftoversWithin(leftoverEvery)
	return safe + data
}

// leftoversWithin counts the leftovers, at most age old: those Clean up
// clears, and the data folders, which are deleted one at a time.
func (s *Service) leftoversWithin(age time.Duration) (safe, data int) {
	s.mu.Lock()
	n, d, stale := s.leftoversN, s.leftoversData, s.now().Sub(s.leftoversAt) > age
	start := stale && !s.leftoversBusy
	if start {
		s.leftoversBusy = true
	}
	s.mu.Unlock()
	if start {
		go func() {
			got, gotData := 0, 0
			for _, l := range s.Leftovers() {
				if l.Danger {
					gotData++
				} else {
					got++
				}
			}
			s.mu.Lock()
			changed := got != s.leftoversN || gotData != s.leftoversData
			s.leftoversN, s.leftoversData, s.leftoversAt, s.leftoversBusy = got, gotData, s.now(), false
			s.mu.Unlock()
			if changed {
				s.changed()
			}
		}()
	}
	return n, d
}

// recountLeftovers makes the next look count again, after a clean up.
func (s *Service) recountLeftovers() {
	s.mu.Lock()
	s.leftoversAt = time.Time{}
	s.mu.Unlock()
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
