package serve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	names "github.com/doze-dev/doze-names"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/desk/autostart"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/prefs"
	"github.com/doze-dev/doze/internal/desk/view"
	"github.com/doze-dev/doze/internal/journal"
	"github.com/doze-dev/doze/internal/projects"
)

// Library is the first tab: every stack the hub knows, the help strip,
// and the machine line.
func (s *Service) Library() view.Library {
	home := s.hub.Home()
	p := prefs.Load(home)
	stacks := s.hub.Stacks()
	hints := view.Hints{Dismissed: p.Hints.Dismissed, Connected: p.Hints.Connected}
	if !hints.Connected {
		// The third step is done the first time a connection is seen, and
		// stays done: a strip that un-ticks itself would be a lie.
		for _, st := range stacks {
			for _, v := range st.Instances {
				if v.Conns > 0 {
					hints.Connected = true
				}
			}
		}
		if hints.Connected {
			p.Hints.Connected = true
			_ = prefs.Save(home, p)
		}
	}
	lib := view.LibraryOf(stacks, hints, s.now())
	// An https domain that this machine cannot serve yet is not offered as a
	// link: the stack's page says what the setup is.
	for _, st := range stacks {
		if s.setupFor(st) == nil {
			continue
		}
		for j := range lib.Stacks {
			if lib.Stacks[j].Key != st.Key {
				continue
			}
			for k, a := range lib.Stacks[j].Addresses {
				if strings.HasPrefix(a.Text, "https://") {
					lib.Stacks[j].Addresses[k].Unready, lib.Stacks[j].Addresses[k].URL, lib.Stacks[j].Addresses[k].Up = true, "", false
				}
			}
		}
	}
	lib.Doctor = s.doctorCached(stacks)
	lib.Machine = machine(lib.Doctor, stacks)
	s.mu.Lock()
	lib.Repairing = s.repairing
	for i := range lib.Stacks {
		lib.Stacks[i].Busy = s.busy[lib.Stacks[i].Key]
	}
	lib.Repairable = s.admin != nil && fixable(lib.Doctor)
	lib.OpenAtLogin = autostart.Enabled()
	lib.Others = others(home)
	s.mu.Unlock()
	lib.Leftovers = s.leftoverCount()
	for i := range lib.Stacks {
		for j, a := range lib.Stacks[i].Addresses {
			if a.Host == "" || a.Unready {
				continue
			}
			if url := s.urlFor(a.Host); url != "" {
				lib.Stacks[i].Addresses[j].Text, lib.Stacks[i].Addresses[j].URL = url, url
			}
		}
	}
	// A doze.hcl that does not load is said on its row (configError takes
	// the lock itself, so this is after it).
	for _, st := range stacks {
		if st.Dir == "" {
			continue
		}
		ce := s.configError(st.Dir)
		if ce == nil {
			continue
		}
		for j := range lib.Stacks {
			if lib.Stacks[j].Key == st.Key {
				lib.Stacks[j].Broken = "doze.hcl does not load"
				if ce.Line > 0 {
					lib.Stacks[j].Broken += fmt.Sprintf(" · line %d", ce.Line)
				}
			}
		}
	}
	return lib
}

// doctorCached is the last machine report, refreshed in the background
// when older than half a minute: the first paint never waits on launchd or
// the keychain, and the line fills in on the next push.
func (s *Service) doctorCached(stacks []hub.StackView) []view.Check {
	return s.doctorWithin(stacks, 30*time.Second)
}

// doctorWithin is doctorCached with how old the report may be.
func (s *Service) doctorWithin(stacks []hub.StackView, age time.Duration) []view.Check {
	s.mu.Lock()
	checks := append([]view.Check{}, s.doctorChecks...)
	stale := s.now().Sub(s.doctorAt) > age
	start := stale && !s.doctorBusy
	if start {
		s.doctorBusy = true
	}
	s.mu.Unlock()
	if start {
		go func() {
			d := s.doctor(stacks)
			s.mu.Lock()
			s.doctorChecks, s.doctorAt, s.doctorBusy = d, s.now(), false
			s.mu.Unlock()
			s.changed()
		}()
	}
	return checks
}

// SetView remembers a stack's overview view (tiles, list, map), by key.
func (s *Service) SetView(key, name string) {
	switch name {
	case "tiles", "list", "map", "grid":
	default:
		return
	}
	home := s.hub.Home()
	p := prefs.Load(home)
	if p.Views == nil {
		p.Views = map[string]string{}
	}
	p.Views[key] = name
	_ = prefs.Save(home, p)
	s.changed()
}

// viewFor is the overview view a stack was last shown in; tiles to begin with.
func (s *Service) viewFor(key string) string {
	if v := prefs.Load(s.hub.Home()).Views[key]; v != "" {
		return v
	}
	return "tiles"
}

// DismissHints hides the help strip for good.
func (s *Service) DismissHints() {
	home := s.hub.Home()
	p := prefs.Load(home)
	p.Hints.Dismissed = true
	_ = prefs.Save(home, p)
	s.changed()
}

// machine is the Library's bottom line: the names setup, part by part,
// and the domains declared across every stack. The trust check runs the
// keychain tool, so it is cached a minute, like the banner's.
// doctor is the machine's setup part by part, as Settings lists it: the
// names (the resolver route, the network daemon), each declared domain's
// route and the certificate authority, and the app itself. A part that
// is not in place carries the command that puts it there.
func (s *Service) doctor(stacks []hub.StackView) []view.Check {
	out := []view.Check{}
	if names.Disabled() {
		out = append(out, view.Check{Section: "names", Name: "names", OK: false, Text: "switched off (DOZE_ZONE)"})
	} else {
		for _, step := range names.Check().Steps {
			c := view.Check{Section: "names", Name: step.Name, OK: step.Done, Text: step.Detail}
			if !step.Done {
				c.Fix = "doze dns-setup"
			}
			out = append(out, c)
		}
	}
	var domains []string
	for _, st := range stacks {
		domains = append(domains, view.Domains(st)...)
	}
	// Stale routes are reported with the domains, so Repair removes them as
	// doze doctor --fix does, even when no stack declares a domain now.
	if (len(domains) > 0 || len(names.StaleRoutes(s.hub.Home())) > 0) && !names.Disabled() {
		for _, step := range s.trust(domains) {
			c := view.Check{Section: "domains", Name: step.Name, OK: step.Done, Text: step.Detail}
			if !step.Done {
				c.Fix = "doze trust"
			}
			out = append(out, c)
		}
	}
	app := view.Check{Section: "app", Name: "doze on PATH", OK: true}
	if p, ok := dozeOnPath(); ok {
		app.Text = p + " · " + s.version
	} else {
		app.OK, app.Text, app.Fix = false, "not on your PATH: a terminal and scripts cannot find doze", "ln -s "+os.Args[0]+" /usr/local/bin/doze"
	}
	out = append(out, app)
	return out
}

// dozeOnPath finds the doze a terminal would run. The app started from
// Finder has launchd's short PATH, so the places a terminal's PATH has
// (where Repair links doze, and Homebrew's) are looked at too.
func dozeOnPath() (string, bool) {
	if p, err := exec.LookPath("doze"); err == nil {
		return p, true
	}
	for _, p := range []string{pathLink, "/opt/homebrew/bin/doze"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// trust is the domains' setup, kept a minute: the checks read files and
// the keychain.
func (s *Service) trust(domains []string) []names.Step {
	prune := names.StaleRoutes(s.hub.Home())
	key := "machine|" + strings.Join(domains, ",") + "|" + strings.Join(prune, ",")
	s.mu.Lock()
	steps, fresh := s.trustSteps, s.trustKey == key && s.now().Sub(s.trustAt) < time.Minute
	s.mu.Unlock()
	if fresh {
		return steps
	}
	req := names.TrustRequest{Domains: domains, Home: s.hub.Home(), Prune: prune}
	if ca, ok := names.OpenCA(names.CADir(s.hub.Home())); ok && len(domains) > 0 {
		req.CA = ca
	}
	steps = names.TrustStatus(req)
	s.mu.Lock()
	s.trustSteps, s.trustKey, s.trustAt = steps, key, s.now()
	s.mu.Unlock()
	return steps
}

// machine is the Library's line: the names' parts, and the domains as one.
func machine(doctor []view.Check, stacks []hub.StackView) []view.Check {
	out := []view.Check{}
	domainsOK, anyDomain := true, false
	var stale []string
	for _, c := range doctor {
		switch {
		case c.Section == "names":
			out = append(out, c)
		case c.Section == "domains" && strings.HasPrefix(c.Name, "stale route "):
			stale = append(stale, strings.TrimPrefix(c.Name, "stale route "))
		case c.Section == "domains":
			anyDomain = true
			domainsOK = domainsOK && c.OK
		}
	}
	defer func() {
		if len(stale) > 0 {
			out = append(out, view.Check{Name: "stale routes", Text: strings.Join(stale, ", ") + " · no stack declares them"})
		}
	}()
	if anyDomain {
		var domains []string
		for _, st := range stacks {
			domains = append(domains, view.Domains(st)...)
		}
		c := view.Check{Name: "domains", OK: domainsOK, Text: strings.Join(names.RouteDomains(domains), ", ")}
		if !domainsOK {
			c.Text += " · not set up yet"
		}
		out = append(out, c)
	}
	return out
}

// Stack acts on a whole stack from the Library's switch: up as last time,
// up everything, up a chosen set, down, or forget. A stopped stack has no
// daemon to ask, so the CLI does it, in the project's folder: the app and
// the CLI are one binary, and `doze up` is the only way a daemon starts.
func (s *Service) Stack(key, action string, chosen []string) error {
	st, ok := s.stack(key)
	if !ok {
		return errors.New("no such stack")
	}
	switch action {
	case "forget", "remove":
		return s.remove(key, st)
	case "up-last", "up-all", "up", "down", "restart":
	default:
		return fmt.Errorf("no such action %q", action)
	}
	// One action at a time per stack: a second click while the first runs
	// would start a second up or down.
	busyWord := map[string]string{"up-last": "starting", "up-all": "starting", "up": "starting", "down": "stopping", "restart": "restarting"}[action]
	s.mu.Lock()
	if w := s.busy[key]; w != "" {
		s.mu.Unlock()
		return fmt.Errorf("%s is already %s", st.Name, w)
	}
	if s.busy == nil {
		s.busy, s.failed = map[string]string{}, map[string]*view.StackFailure{}
	}
	s.busy[key] = busyWord
	delete(s.failed, key)
	s.mu.Unlock()
	done := func(failure *view.StackFailure) {
		s.mu.Lock()
		delete(s.busy, key)
		if failure != nil {
			s.failed[key] = failure
		}
		s.mu.Unlock()
		s.hub.Refresh()
		s.changed()
	}
	s.changed()
	self, err := os.Executable()
	if err != nil {
		done(nil)
		return err
	}
	args := []string{"-y"}
	what := ""
	switch action {
	case "up-last":
		args, what = append(args, "up", "--last"), "bringing "+st.Name+" up as last time…"
	case "up-all":
		args, what = append(args, "up", "--all"), "bringing all of "+st.Name+" up…"
	case "up":
		if len(chosen) == 0 {
			done(nil)
			return errors.New("choose at least one")
		}
		args, what = append(append(args, "up"), chosen...), "bringing up "+strings.Join(chosen, ", ")+"…"
	case "down":
		args, what = append(args, "down"), "stopping "+st.Name+"…"
	case "restart":
		// Down, then up as last time, one after the other: two separate
		// actions would race, and up would find the old daemon still there.
		args, what = append(args, "down"), "restarting "+st.Name+" on the new doze.hcl…"
	}
	s.Say(what, false)
	cmd := exec.Command(self, args...)
	cmd.Dir = st.Dir
	// The CLI works in the doze home this app shows: a fixture's own home
	// under --serve --fixture, never the person's real one by accident.
	cmd.Env = append(childEnv(), "NO_COLOR=1", "DOZE_HOME="+s.hub.Home())
	go func() {
		var out []byte
		var err error
		if s.FakeStack != nil {
			out, err = s.FakeStack(st, action)
		} else {
			out, err = cmd.CombinedOutput()
			if err == nil && action == "restart" {
				up := exec.Command(self, "-y", "up", "--last")
				up.Dir, up.Env = st.Dir, cmd.Env
				out, err = up.CombinedOutput()
			}
		}
		if err == nil {
			done(nil)
			return
		}
		// The failure stays on the stack's page, read into what went wrong
		// and how to fix it; the toast says the same in a line.
		title := "Couldn't " + map[string]string{"starting": "start", "stopping": "stop", "restarting": "restart"}[busyWord] + " " + st.Name
		f := view.FailureOf(title, st.Dir, string(out), err)
		if len(f.Lines) > 40 {
			f.Lines = f.Lines[len(f.Lines)-40:]
		}
		// Needs you raises it as an alert with Try again, so no second
		// toast; recent messages keep the line.
		said := title + ": " + f.Problem
		if f.Config != nil {
			said = title + ": doze.hcl line " + strconv.Itoa(f.Config.Line) + ", " + f.Problem
		}
		s.remember(said)
		done(f)
	}()
	return nil
}

// SetOpenAtLogin puts the login item in place or removes it: the app at
// login, in the menu bar only.
func (s *Service) SetOpenAtLogin(on bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := autostart.Set(on, autostart.Stable(exe)); err != nil {
		s.Say("could not change the login item: "+err.Error(), true)
		return err
	}
	if on {
		s.Say("doze will start at login, in the menu bar", false)
	} else {
		s.Say("doze will not start at login", false)
	}
	s.changed()
	return nil
}

// configSeen is the last look at a running stack's doze.hcl: the file's
// stamp, and what loading it said.
type configSeen struct {
	mtime time.Time
	size  int64
	err   *view.ConfigError
	// at is when the files were last stamped: a push every second must not
	// stat and glob every stack's folder each time.
	at time.Time
}

// configLook is how often a project's files are stamped again.
const configLook = 2 * time.Second

// configError is the running stack's doze.hcl when it does not load, or
// nil. The file is stat'ed on every look and loaded only when its stamp
// changed, with the offline pass (no module is fetched), so a page that
// is pushed every second costs one stat.
func (s *Service) configError(dir string) *view.ConfigError {
	path := filepath.Join(dir, "doze.hcl")
	s.mu.Lock()
	recent, seenAt := s.configs[path]
	s.mu.Unlock()
	if seenAt && s.now().Sub(recent.at) < configLook {
		if recent.err == nil {
			return nil
		}
		fresh := *recent.err
		fresh.Text = view.ConfigErrorText(recent.mtime, s.now())
		return &fresh
	}
	fi, ok := configStamp(dir)
	if !ok {
		return nil
	}
	s.mu.Lock()
	seen, ok := s.configs[path]
	s.mu.Unlock()
	if ok && seen.mtime.Equal(fi.mtime) && seen.size == fi.size {
		s.mu.Lock()
		seen.at = s.now()
		s.configs[path] = seen
		s.mu.Unlock()
		if seen.err != nil {
			// The words keep their "changed 20 s ago" fresh.
			fresh := *seen.err
			fresh.Text = view.ConfigErrorText(fi.mtime, s.now())
			return &fresh
		}
		return nil
	}
	_, loadErr := config.LoadShape(path, nil)
	ce := view.ConfigErrorOf(path, loadErr, fi.mtime, s.now())
	s.mu.Lock()
	if s.configs == nil {
		s.configs = map[string]configSeen{}
	}
	s.configs[path] = configSeen{mtime: fi.mtime, size: fi.size, err: ce, at: s.now()}
	s.mu.Unlock()
	return ce
}

// configChanged is the running stack's doze.hcl when it loads and is newer
// than what the daemon read, or nil.
func (s *Service) configChanged(st hub.StackView) *view.ConfigChanged {
	// The stamp configError just took, when it is fresh; else a new one.
	path := filepath.Join(st.Dir, "doze.hcl")
	s.mu.Lock()
	seen, ok := s.configs[path]
	s.mu.Unlock()
	if ok && s.now().Sub(seen.at) < configLook {
		return view.ConfigChangedOf(seen.mtime, st.Loaded, s.now())
	}
	fi, ok := configStamp(st.Dir)
	if !ok {
		return nil
	}
	return view.ConfigChangedOf(fi.mtime, st.Loaded, s.now())
}

// stamp is a project's config files taken together: the newest change
// and the sizes summed, so an edit to any of them is seen.
type stamp struct {
	mtime time.Time
	size  int64
}

// configStamp stamps doze.hcl and what the loader merges with it: every
// sibling *.doze.hcl and the *.auto.doze.vars files. False when there is
// no doze.hcl.
func configStamp(dir string) (stamp, bool) {
	fi, err := os.Stat(filepath.Join(dir, "doze.hcl"))
	if err != nil {
		return stamp{}, false
	}
	st := stamp{mtime: fi.ModTime(), size: fi.Size()}
	more, _ := filepath.Glob(filepath.Join(dir, "*.doze.hcl"))
	vars, _ := filepath.Glob(filepath.Join(dir, "*.auto.doze.vars"))
	for _, f := range append(more, vars...) {
		if fi, err := os.Stat(f); err == nil {
			if fi.ModTime().After(st.mtime) {
				st.mtime = fi.ModTime()
			}
			st.size += fi.Size() + 1
		}
	}
	return st, true
}

// Activity is a stack's span ("1h", "24h", "7d") from its journal on disk,
// for one service or all; the memory figures are each service's last seen
// while awake (rememberRAM).
func (s *Service) Activity(key, span, inst string) view.Activity {
	st, _ := s.stack(key)
	events, _ := journal.Read(filepath.Join(key, "run", "journal.jsonl"))
	if len(events) == 0 {
		events = append(events, st.Events...)
	}
	s.rememberRAM()
	s.mu.Lock()
	ram := map[string]int64{}
	for k, v := range s.ram {
		if n, ok := strings.CutPrefix(k, key+"#"); ok {
			ram[n] = v
		}
	}
	s.mu.Unlock()
	return view.ActivityOf(st, events, span, inst, ram, s.now())
}

// rememberRAM keeps each awake service's memory, so Activity can price an
// hour of sleep after the service has gone to sleep.
func (s *Service) rememberRAM() {
	stacks := s.hub.Stacks()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ram == nil {
		s.ram = map[string]int64{}
	}
	for _, st := range stacks {
		for _, v := range st.Instances {
			if v.RAM > 0 {
				s.ram[st.Key+"#"+v.Name] = v.RAM
			}
		}
	}
}

// others is what other doze programs serve on this machine, from the names
// registry: names in the zone owned by a program that is not doze, and
// routed (a console, an endpoint), each with the URL the registry says
// reaches it. A stack's own names are its stack's business, not listed.
func others(home string) []view.Other {
	reg := names.Open(home, "doze")
	var out []view.Other
	for host, e := range reg.Snapshot() {
		if e.Owner == "" || e.Owner == "doze" || !names.InZone(host) || e.Target == "" {
			continue
		}
		out = append(out, view.Other{Name: host, Owner: e.Owner, PID: e.PID, URL: reg.URLFor(host)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// childEnv is this process's environment for a doze command it runs,
// without the marker that says "you are the detached app".
func childEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "DOZE_APP_CHILD=") && !strings.HasPrefix(kv, "DOZE_HOME=") {
			out = append(out, kv)
		}
	}
	return out
}

// Dismiss puts away a stack action's failure once it has been read.
func (s *Service) Dismiss(key string) {
	s.quiet(func(it view.Item) bool { return it.ID == "failed:"+key })
	s.mu.Lock()
	delete(s.failed, key)
	s.mu.Unlock()
	s.changed()
}

// watchConfigs stamps every stack's config files each configLook and
// pushes when one changed since it was last checked, so a broken, fixed
// or edited doze.hcl reaches every page without waiting for anything else
// to change.
func (s *Service) watchConfigs(ctx context.Context) {
	t := time.NewTicker(configLook)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		// Only the files the pages have read (the looks kept), and only those
		// not looked at since the last tick: no views are built for this.
		s.mu.Lock()
		paths := make([]string, 0, len(s.configs))
		for p, seen := range s.configs {
			if s.now().Sub(seen.at) >= configLook {
				paths = append(paths, p)
			}
		}
		s.mu.Unlock()
		changed := false
		for _, path := range paths {
			fi, ok := configStamp(filepath.Dir(path))
			s.mu.Lock()
			seen, known := s.configs[path]
			s.mu.Unlock()
			if !ok || !known {
				continue
			}
			if !seen.mtime.Equal(fi.mtime) || seen.size != fi.size {
				// Forget the old look, so the next one loads the file.
				s.mu.Lock()
				delete(s.configs, path)
				s.mu.Unlock()
				changed = true
			} else {
				// Unchanged: the look is fresh, so a push does not stamp again.
				s.mu.Lock()
				seen.at = s.now()
				s.configs[path] = seen
				s.mu.Unlock()
			}
		}
		if changed {
			s.changed()
		}
	}
}

// remove takes a stack out of doze, running or not, whatever is left of its
// folder: its daemon is stopped (by its pid, so a deleted folder is no
// obstacle), its record and stale control files go, and so do the domain
// routes it claimed. Its folder, doze.hcl and data stay where they are;
// opening the folder again brings it back, with its data.
func (s *Service) remove(key string, st hub.StackView) error {
	s.mu.Lock()
	if w := s.busy[key]; w != "" {
		s.mu.Unlock()
		return fmt.Errorf("%s is already %s", st.Name, w)
	}
	if s.busy == nil {
		s.busy, s.failed = map[string]string{}, map[string]*view.StackFailure{}
	}
	s.busy[key] = "removing"
	s.mu.Unlock()
	s.quiet(func(it view.Item) bool { return it.Key == key })
	s.changed()
	go func() {
		var failure *view.StackFailure
		if pid, alive, ok := hub.DaemonPID(key); ok && alive {
			// The daemon stops its services as it exits; a slow one gets the
			// time doze down gives it.
			_ = syscall.Kill(pid, syscall.SIGTERM)
			for i := 0; i < 200 && hub.Alive(pid); i++ {
				time.Sleep(100 * time.Millisecond)
			}
			if hub.Alive(pid) {
				failure = &view.StackFailure{What: "Removing " + st.Name + " failed", Lines: []string{fmt.Sprintf("its daemon (pid %d) did not stop; try doze down in its folder", pid)}}
			}
		}
		if failure == nil {
			for _, f := range []string{projects.FileName, filepath.Join("run", "doze.admin.sock"), filepath.Join("run", "doze.pid")} {
				_ = os.Remove(filepath.Join(key, f))
			}
			if st.Dir != "" {
				_ = names.ForgetProject(s.hub.Home(), st.Dir)
			}
			s.Say("removed "+st.Name+" from doze; its folder and data are still on disk, and opening the folder brings it back", false)
		}
		s.mu.Lock()
		delete(s.busy, key)
		if failure != nil {
			s.failed[key] = failure
		}
		s.mu.Unlock()
		s.hub.Refresh()
		s.changed()
	}()
	return nil
}

// refsSeen is a stack's references, and the stamp of the files they were
// read from.
type refsSeen struct {
	stamp stamp
	refs  view.Refs
}

// refsFor is what the Grid's tooltips quote: each reference as doze.hcl
// writes it. The file is parsed again only when it changes.
func (s *Service) refsFor(dir string) view.Refs {
	fi, ok := configStamp(dir)
	if !ok {
		return nil
	}
	s.mu.Lock()
	seen, hit := s.refs[dir]
	s.mu.Unlock()
	if hit && seen.stamp == fi {
		return seen.refs
	}
	refs := view.RefsIn(dir)
	s.mu.Lock()
	if s.refs == nil {
		s.refs = map[string]refsSeen{}
	}
	s.refs[dir] = refsSeen{stamp: fi, refs: refs}
	s.mu.Unlock()
	return refs
}
