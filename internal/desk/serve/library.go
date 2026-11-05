package serve

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	names "github.com/doze-dev/doze-names"

	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/prefs"
	"github.com/doze-dev/doze/internal/desk/view"
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
	lib.Doctor = s.doctor(stacks)
	lib.Machine = machine(lib.Doctor, stacks)
	return lib
}

// SetView remembers a stack's overview view (tiles, list, map), by key.
func (s *Service) SetView(key, name string) {
	switch name {
	case "tiles", "list", "map":
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
	if len(domains) > 0 && !names.Disabled() {
		for _, step := range s.trust(domains) {
			c := view.Check{Section: "domains", Name: step.Name, OK: step.Done, Text: step.Detail}
			if !step.Done {
				c.Fix = "doze trust"
			}
			out = append(out, c)
		}
	}
	app := view.Check{Section: "app", Name: "doze on PATH", OK: true}
	if p, err := exec.LookPath("doze"); err == nil {
		app.Text = p + " · " + s.version
	} else {
		app.OK, app.Text, app.Fix = false, "not on your PATH: a terminal and scripts cannot find doze", "ln -s "+os.Args[0]+" /usr/local/bin/doze"
	}
	out = append(out, app)
	return out
}

// trust is the domains' setup, kept a minute: the checks read files and
// the keychain.
func (s *Service) trust(domains []string) []names.Step {
	key := "machine|" + strings.Join(domains, ",")
	s.mu.Lock()
	steps, fresh := s.trustSteps, s.trustKey == key && s.now().Sub(s.trustAt) < time.Minute
	s.mu.Unlock()
	if fresh {
		return steps
	}
	req := names.TrustRequest{Domains: domains, Home: s.hub.Home()}
	if ca, ok := names.OpenCA(names.CADir(s.hub.Home())); ok {
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
	for _, c := range doctor {
		switch c.Section {
		case "names":
			out = append(out, c)
		case "domains":
			anyDomain = true
			domainsOK = domainsOK && c.OK
		}
	}
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
	case "forget":
		if st.Running {
			return errors.New("stop it first: Forget is for a stack that is not running")
		}
		if err := os.Remove(filepath.Join(key, projects.FileName)); err != nil && !os.IsNotExist(err) {
			return err
		}
		s.hub.Refresh()
		s.changed()
		s.Say("forgot "+st.Name+"; everything is still on disk", false)
		return nil
	case "up-last", "up-all", "up", "down":
	default:
		return fmt.Errorf("no such action %q", action)
	}
	self, err := os.Executable()
	if err != nil {
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
			return errors.New("choose at least one")
		}
		args, what = append(append(args, "up"), chosen...), "bringing up "+strings.Join(chosen, ", ")+"…"
	case "down":
		args, what = append(args, "down"), "stopping "+st.Name+"…"
	}
	s.Say(what, false)
	cmd := exec.Command(self, args...)
	cmd.Dir = st.Dir
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	go func() {
		out, err := cmd.CombinedOutput()
		if err != nil {
			last := strings.TrimSpace(string(out))
			if i := strings.LastIndex(last, "\n"); i >= 0 {
				last = strings.TrimSpace(last[i+1:])
			}
			if last == "" {
				last = err.Error()
			}
			s.Say(action+" "+st.Name+" failed: "+last, true)
		}
		s.hub.Refresh()
		s.changed()
	}()
	return nil
}
