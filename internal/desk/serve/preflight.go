package serve

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/view"
	"github.com/doze-dev/doze/internal/display"
)

// Preflight looks before a stopped stack starts (the PreFlight board): its
// folder and doze.hcl, each port the starting set declares, its domains'
// setup. Nothing is started to find out. Blocked says the page should ask
// first; with nothing red, Up just goes.
func (s *Service) Preflight(key, action string, chosen []string) view.Preflight {
	st, ok := s.stack(key)
	if !ok || st.Running || st.Dir == "" {
		return view.Preflight{Checks: []view.PreCheck{}}
	}
	name := st.Name
	if name == "" {
		name = filepath.Base(st.Dir)
	}
	p := view.Preflight{Stack: name, Checks: []view.PreCheck{}}
	if m := view.MissingFiles(st.Dir); m != "" {
		p.Blocked = true
		p.Checks = append(p.Checks, view.PreCheck{Tone: "bad", Title: name + "'s " + strings.TrimPrefix(m, "the "), Detail: display.Tilde(st.Dir) + " was moved or deleted; nothing can start from it",
			Fixes: []view.Fix{{Label: "Remove…", Kind: "stack", Verb: "remove"}}})
		return p
	}
	cfg, err := config.LoadShape(filepath.Join(st.Dir, "doze.hcl"), nil)
	if err != nil {
		p.Blocked = true
		c := view.ConfigErrorOf(filepath.Join(st.Dir, "doze.hcl"), err, time.Time{}, time.Now())
		p.Checks = append(p.Checks, view.PreCheck{Tone: "bad", Title: "doze.hcl has a mistake", Detail: view.ConfigProblem(c), Note: c.Hint,
			Fixes: []view.Fix{{Label: "Edit " + filepath.Base(c.File), Kind: "edit", Path: c.File}}})
		return p
	}
	set := startingSet(cfg.Decls(), st.LastSet, action, chosen)
	p.Checks = append(p.Checks, view.PreCheck{Tone: "ok", Title: "The folder is there, and doze.hcl loads", Detail: display.Tilde(st.Dir) + " · starts " + andWords(set.names) + (map[bool]string{true: ", and what they need", false: ""}[len(set.deps) > 0])})

	held := listening()
	var free []string
	for _, d := range set.decls {
		port := declPort(d)
		if port == 0 {
			continue
		}
		h, taken := held[port]
		if !taken && !bindable(port) {
			// lsof sees this user's processes only: something of another
			// user's (or root's) has it, and doze cannot say what.
			p.Blocked = true
			p.Checks = append(p.Checks, view.PreCheck{Tone: "bad", Title: fmt.Sprintf("Port %d for %s is taken", port, d.Name), Detail: "by a process of another user, which doze cannot see or stop",
				Note: d.Name + " would crash on start.", Fixes: s.withoutFix(set, d.Name, st.Dir)})
			continue
		}
		if taken && !h.Doze {
			s.ownedBy(&h)
		}
		if !taken || h.Doze && h.Stack == "" {
			free = append(free, strconv.Itoa(port))
			continue
		}
		if h.Stack != "" {
			// Another stack's service: never stopped from here.
			p.Blocked = true
			p.Checks = append(p.Checks, view.PreCheck{Tone: "bad", Title: fmt.Sprintf("Port %d for %s is used by %s in %s", port, d.Name, h.Service, h.Stack),
				Note: "Two stacks cannot both have it. Stop " + h.Stack + " first, or start without " + d.Name + ".", Fixes: s.withoutFix(set, d.Name, st.Dir)})
			continue
		}
		p.Blocked = true
		c := view.PreCheck{Tone: "bad", Title: fmt.Sprintf("Port %d for %s is taken", port, d.Name), Detail: fmt.Sprintf("%s (pid %d)", h.Command, h.PID), Mono: true,
			Note: d.Name + " would crash on start. doze never stops a process it did not start without asking."}
		c.Fixes = append([]view.Fix{{Label: fmt.Sprintf("Stop pid %d, then Up", h.PID), Kind: "stop", PID: h.PID, Primary: true}}, s.withoutFix(set, d.Name, st.Dir)...)
		p.Checks = append(p.Checks, c)
	}
	var domains []string
	for _, d := range set.decls {
		domains = append(domains, d.Domains...)
	}
	if len(domains) > 0 {
		var missing []string
		for _, step := range s.trust(domains) {
			if !step.Done {
				missing = append(missing, step.Name)
			}
		}
		if len(missing) > 0 {
			c := view.PreCheck{Tone: "warn", Title: domains[0] + " is not set up on this Mac", Detail: name + " starts anyway; its .doze names answer until it is. This waits in Needs you."}
			s.mu.Lock()
			if s.admin != nil {
				c.Fixes = []view.Fix{{Label: "Set up (one password)", Kind: "repair"}}
			}
			s.mu.Unlock()
			p.Checks = append(p.Checks, c)
		}
	}
	if len(free) > 0 {
		sort.Strings(free)
		word := "Port " + free[0] + " is free"
		if len(free) > 1 {
			word = "Ports " + andWords(free) + " are free"
		}
		p.Checks = append(p.Checks, view.PreCheck{Tone: "ok", Title: word})
	}
	return p
}

// reSSUser is ss's "users:((\"node\",pid=7731,fd=20))".
var reSSUser = regexp.MustCompile(`\("([^"]+)",pid=(\d+)`)

// PreflightFix is a way out from the sheet: stop the process on a port the
// starting set declares and then start, start without one service, or
// start anyway.
func (s *Service) PreflightFix(key, action string, chosen []string, kind string, pid int, without string) error {
	st, ok := s.stack(key)
	if !ok {
		return errors.New("no such stack")
	}
	switch kind {
	case "stop":
		// Only a process the checks named: it must still hold a port the
		// starting set declares, and not be doze.
		cfg, err := config.LoadShape(filepath.Join(st.Dir, "doze.hcl"), nil)
		if err != nil {
			return err
		}
		set := startingSet(cfg.Decls(), st.LastSet, action, chosen)
		held := listening()
		named := false
		for _, d := range set.decls {
			if h, ok := held[declPort(d)]; ok && h.PID == pid && !h.Doze {
				if s.ownedBy(&h); h.Stack != "" {
					return fmt.Errorf("pid %d is %s in %s; doze does not stop another stack's service from here", pid, h.Service, h.Stack)
				}
				named = true
				forgetHeld(declPort(d))
			}
		}
		if !named {
			return fmt.Errorf("pid %d no longer holds a port %s needs; nothing was stopped", pid, st.Name)
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("could not stop pid %d: %w", pid, err)
		}
		for i := 0; i < 30 && hub.Alive(pid); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if hub.Alive(pid) {
			return fmt.Errorf("pid %d did not stop; nothing was started", pid)
		}
		s.Say(fmt.Sprintf("stopped pid %d", pid), false)
		return s.Stack(key, action, chosen)
	case "without":
		cfg, err := config.LoadShape(filepath.Join(st.Dir, "doze.hcl"), nil)
		if err != nil {
			return err
		}
		set := startingSet(cfg.Decls(), st.LastSet, action, chosen)
		var names []string
		for _, n := range set.names {
			if n != without {
				names = append(names, n)
			}
		}
		return s.Stack(key, "up", names)
	case "anyway":
		return s.Stack(key, action, chosen)
	}
	return fmt.Errorf("no such way %q", kind)
}

// withoutFix is "Up without name" (when something else would still start
// and nothing in the set needs it) and Edit doze.hcl.
func (s *Service) withoutFix(set starting, name, dir string) []view.Fix {
	var out []view.Fix
	if !set.needed[name] && len(set.names) > 1 {
		out = append(out, view.Fix{Label: "Up without " + name, Kind: "without", Verb: name})
	}
	return append(out, view.Fix{Label: "Edit doze.hcl", Kind: "edit", Path: filepath.Join(dir, "doze.hcl")})
}

// bindable says this user could listen on port on loopback now.
func bindable(port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

type starting struct {
	decls  []*config.InstanceDecl
	names  []string        // what was asked for, by name (apps, or all)
	deps   []string        // what comes because something needs it
	needed map[string]bool // needed by another in the set
}

// startingSet is what Up would start, by the CLI's rule (chooseUpTargets):
// the chosen ones, or last time's set still declared, or else every enabled
// instance, tasks included; each with what it needs.
func startingSet(decls []*config.InstanceDecl, last []string, action string, chosen []string) starting {
	byName := map[string]*config.InstanceDecl{}
	for _, d := range decls {
		byName[d.Name] = d
	}
	keep := func(names []string) []string {
		var out []string
		for _, n := range names {
			if d := byName[n]; d != nil && d.Enabled {
				out = append(out, n)
			}
		}
		return out
	}
	var roots []string
	switch action {
	case "up":
		roots = keep(chosen)
	case "up-last":
		roots = keep(last)
	}
	if len(roots) == 0 {
		for _, d := range decls {
			if d.Enabled {
				roots = append(roots, d.Name)
			}
		}
	}
	out := starting{needed: map[string]bool{}}
	on := map[string]bool{}
	isRoot := map[string]bool{}
	for _, r := range roots {
		isRoot[r] = true
	}
	var mark func(name string, root bool)
	mark = func(name string, root bool) {
		d := byName[name]
		if d == nil || !d.Enabled {
			return
		}
		if !root {
			out.needed[name] = true
		}
		if on[name] {
			return
		}
		on[name] = true
		out.decls = append(out.decls, d)
		if isRoot[name] {
			out.names = append(out.names, name)
		} else {
			out.deps = append(out.deps, name)
		}
		for _, dep := range d.Deps {
			mark(dep.Name, false)
		}
	}
	for _, r := range roots {
		mark(r, true)
	}
	return out
}

// declPort is the port an instance declares, or 0 when it leaves it to
// its module.
func declPort(d *config.InstanceDecl) int {
	if d.Listen != "" {
		if _, p, err := net.SplitHostPort(d.Listen); err == nil {
			n, _ := strconv.Atoi(p)
			return n
		}
	}
	return d.Port
}

// listening is every TCP port something listens on, with who: one lsof
// for them all (ss on Linux), so the checks cost one call.
func listening() map[int]view.PortHolder {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out := map[int]view.PortHolder{}
	if b, err := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpcn").Output(); err == nil || len(b) > 0 {
		var pid int
		var cmd string
		sc := bufio.NewScanner(strings.NewReader(string(b)))
		for sc.Scan() {
			l := sc.Text()
			if l == "" {
				continue
			}
			switch l[0] {
			case 'p':
				pid, _ = strconv.Atoi(l[1:])
			case 'c':
				cmd = l[1:]
			case 'n':
				if i := strings.LastIndex(l, ":"); i >= 0 {
					if port, err := strconv.Atoi(l[i+1:]); err == nil {
						if _, seen := out[port]; !seen {
							out[port] = view.PortHolder{Port: port, PID: pid, Command: cmd, Doze: isDoze(cmd)}
						}
					}
				}
			}
		}
		return out
	}
	if runtime.GOOS == "darwin" {
		return out // lsof found nothing listening; there is no ss here
	}
	b, err := exec.CommandContext(ctx, "ss", "-ltnpH").Output()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		i := strings.LastIndex(f[3], ":")
		port, err := strconv.Atoi(f[3][i+1:])
		if err != nil {
			continue
		}
		h := view.PortHolder{Port: port}
		if m := reSSUser.FindStringSubmatch(line); m != nil {
			h.Command = m[1]
			h.PID, _ = strconv.Atoi(m[2])
			h.Doze = isDoze(h.Command)
		}
		out[port] = h
	}
	return out
}

func andWords(xs []string) string {
	switch len(xs) {
	case 0:
		return "nothing"
	case 1:
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

// PreflightInstance looks before one service of a running stack starts
// (turned on, or woken): the port it declares, and who holds it. Nothing
// to say for one already awake.
func (s *Service) PreflightInstance(key, name string) view.Preflight {
	p := view.Preflight{Stack: name, Checks: []view.PreCheck{}}
	st, ok := s.stack(key)
	if !ok || !st.Running || st.Dir == "" {
		return p
	}
	if v, ok := display.Find(view.Snapshot(st), name); !ok || display.AwakeNow(v) || v.Kind == "task" {
		return p
	}
	cfg, err := config.LoadShape(filepath.Join(st.Dir, "doze.hcl"), nil)
	if err != nil {
		return p // the page already says doze.hcl does not load
	}
	d := cfg.Lookup(name)
	if d == nil {
		return p
	}
	port := declPort(d)
	if port == 0 {
		return p
	}
	h, taken := listening()[port]
	switch {
	case taken && !h.Doze:
		if s.ownedBy(&h); h.Stack != "" {
			p.Blocked = true
			p.Checks = append(p.Checks, view.PreCheck{Tone: "bad", Title: fmt.Sprintf("Port %d for %s is used by %s in %s", port, name, h.Service, h.Stack),
				Note: "Two services cannot both have it. Stop " + h.Service + " first.", Fixes: []view.Fix{{Label: "Edit doze.hcl", Kind: "edit", Path: filepath.Join(st.Dir, "doze.hcl")}}})
			return p
		}
		p.Blocked = true
		p.Checks = append(p.Checks, view.PreCheck{Tone: "bad", Title: fmt.Sprintf("Port %d for %s is taken", port, name), Detail: fmt.Sprintf("%s (pid %d)", h.Command, h.PID), Mono: true,
			Note:  name + " would crash on start. doze never stops a process it did not start without asking.",
			Fixes: []view.Fix{{Label: fmt.Sprintf("Stop pid %d, then start %s", h.PID, name), Kind: "stop", PID: h.PID, Primary: true}, {Label: "Edit doze.hcl", Kind: "edit", Path: filepath.Join(st.Dir, "doze.hcl")}}})
	case !taken && !bindable(port):
		p.Blocked = true
		p.Checks = append(p.Checks, view.PreCheck{Tone: "bad", Title: fmt.Sprintf("Port %d for %s is taken", port, name), Detail: "by a process of another user, which doze cannot see or stop",
			Note: name + " would crash on start.", Fixes: []view.Fix{{Label: "Edit doze.hcl", Kind: "edit", Path: filepath.Join(st.Dir, "doze.hcl")}}})
	}
	return p
}

// PreflightInstanceFix is a way out for one service: stop the process the
// checks named on its port and then run the verb, or run it anyway.
func (s *Service) PreflightInstanceFix(key, name, verb, kind string, pid int) error {
	if kind == "stop" {
		st, ok := s.stack(key)
		if !ok {
			return errors.New("no such stack")
		}
		cfg, err := config.LoadShape(filepath.Join(st.Dir, "doze.hcl"), nil)
		if err != nil {
			return err
		}
		d := cfg.Lookup(name)
		if d == nil {
			return fmt.Errorf("%s is not declared", name)
		}
		h, ok := listening()[declPort(d)]
		if !ok || h.PID != pid || h.Doze {
			return fmt.Errorf("pid %d no longer holds %s's port; nothing was stopped", pid, name)
		}
		if s.ownedBy(&h); h.Stack != "" {
			return fmt.Errorf("pid %d is %s in %s; doze does not stop another stack's service from here", pid, h.Service, h.Stack)
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("could not stop pid %d: %w", pid, err)
		}
		for i := 0; i < 30 && hub.Alive(pid); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if hub.Alive(pid) {
			return fmt.Errorf("pid %d did not stop; %s was not started", pid, name)
		}
		forgetHeld(declPort(d))
		s.Say(fmt.Sprintf("stopped pid %d", pid), false)
	} else if kind != "anyway" {
		return fmt.Errorf("no such way %q", kind)
	}
	if verb == "on" {
		// Turned on, then woken, as the switch does.
		if err := s.Do(key, "on", name); err != nil {
			return err
		}
		verb = "wake"
	}
	return s.Do(key, verb, name)
}
