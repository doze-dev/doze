package serve

import (
	"errors"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/prefs"
	"github.com/doze-dev/doze/internal/desk/view"
	"github.com/doze-dev/doze/internal/display"
)

const (
	// graceFor is how long an item may be missing before it counts as gone:
	// a crash loop passes through "starting" between tries, and that is not
	// a fix.
	graceFor = 5 * time.Second
	// fixedFor is how long an item whose cause went away stays, grey,
	// saying "fixed", before it leaves the list.
	fixedFor = 8 * time.Second
)

type goneItem struct {
	it view.Item
	at time.Time
}

// Attention is Needs you (D19): every stack's trouble, its doze.hcl's
// state, a failed start, the machine's missing parts and what was left
// behind, red first. Every source is already kept for a page or cached, so
// building it is a walk.
//
// An item that leaves is shown as fixed only when doze can tell it was:
// it has been gone a few seconds, its stack is steady (not mid-action,
// not out of touch, still there), and the person did not put it aside,
// dismiss it or remove its stack. Otherwise it is carried as it was, or
// leaves quietly.
func (s *Service) Attention() view.Attention {
	now := s.now()
	stacks := s.hub.Stacks()
	var items []view.Item
	// steady says what an item of each stack may conclude when it is
	// missing: "live" (it was fixed), "stopped" (instance items leave
	// quietly), "carry" (mid-action or out of touch: keep it as it was).
	steady := map[string]string{"": "live"}
	// insts is each stack's instances now, for a failed task: it stays red
	// until a run of it succeeds (D19), whatever happened in between.
	insts := map[string]map[string]control.InstanceView{}
	names := map[string]string{}
	for _, st := range stacks {
		insts[st.Key] = map[string]control.InstanceView{}
		for _, v := range st.Instances {
			insts[st.Key][v.Name] = v
		}
		names[st.Key] = st.Name
		f := view.StackFacts{Held: heldBy, Port: func(v control.InstanceView) int { return portInUse(v.LastError) }}
		if st.Dir != "" {
			f.Missing = view.MissingFiles(st.Dir)
			if f.Missing == "" {
				f.Config = s.configError(st.Dir)
				if f.Config == nil && st.Running {
					if f.Changed = s.configChanged(st); f.Changed != nil {
						if fi, ok := configStamp(st.Dir); ok {
							f.ChangedAt = fi.mtime
						}
					}
				}
			}
		}
		s.mu.Lock()
		f.Failed = s.failed[st.Key]
		busy := s.busy[st.Key]
		s.mu.Unlock()
		switch {
		case busy != "", st.Running && st.Conn != hub.ConnLive:
			steady[st.Key] = "carry"
		case !st.Running:
			steady[st.Key] = "stopped"
		default:
			steady[st.Key] = "live"
		}
		if busy != "" {
			continue // an action is under way; what it ends in is said then
		}
		items = append(items, view.StackItems(st, f, now)...)
	}
	checks := s.machineChecks(stacks)
	s.mu.Lock()
	repairable := s.admin != nil && fixable(checks) && !s.repairing
	s.mu.Unlock()
	safe, data := s.leftoversNow()
	items = append(items, view.MachineItems(checks, repairable, safe, data)...)

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.notNowSet {
		s.notNow, s.notNowSet = prefs.Load(s.hub.Home()).NotNow, true
	}
	if s.attnGone == nil {
		s.attnGone, s.attnAbsent, s.attnQuiet = map[string]goneItem{}, map[string]goneItem{}, map[string]bool{}
	}
	present := map[string]bool{}
	for _, it := range items {
		present[it.ID] = true
		delete(s.attnAbsent, it.ID)
		delete(s.attnQuiet, it.ID)
	}
	for id, prev := range s.attnLast {
		if present[id] {
			continue
		}
		state, known := steady[prev.Key]
		switch {
		case !known:
			continue // its stack was removed: nothing to say
		case state == "carry":
			items, present[id] = append(items, prev), true
			continue
		case prev.Kind == "task" && state == "stopped":
			// It cannot run again until its stack is up: say so, keep it red.
			prev.Detail = trimStopped(prev.Detail) + " · its stack is stopped"
			prev.Fixes = []view.Fix{{Label: "Up " + names[prev.Key], Kind: "stack", Verb: "up-last", Primary: true}}
			items, present[id] = append(items, prev), true
			continue
		case prev.Kind == "task" && state == "live":
			v, declared := insts[prev.Key][prev.Instance]
			if !declared {
				continue // no longer in doze.hcl: nothing to run
			}
			if v.Task == nil {
				// Up again with no run since: still the last word on it.
				prev.Detail = trimStopped(prev.Detail)
				prev.Fixes = []view.Fix{{Label: "Run again", Kind: "do", Verb: "task", Primary: true}, {Label: "Output", Kind: "show"}}
				items, present[id] = append(items, prev), true
				continue
			}
		case state == "stopped" && prev.Instance != "":
			continue // the stack stopped; its services' trouble went with it
		case s.attnQuiet[id]:
			delete(s.attnQuiet, id) // dismissed or removed: gone at once
			continue
		}
		a, waiting := s.attnAbsent[id]
		if !waiting {
			a = goneItem{it: prev, at: now}
			s.attnAbsent[id] = a
		}
		if now.Sub(a.at) < graceFor {
			items, present[id] = append(items, prev), true
			continue
		}
		delete(s.attnAbsent, id)
		if s.attnQuiet[id] || (s.notNow[id] != "" && s.notNow[id] == prev.Print) {
			delete(s.attnQuiet, id)
			continue
		}
		prev.Fixed, prev.Fixes = true, nil
		s.attnGone[id] = goneItem{it: prev, at: now}
	}
	for id, g := range s.attnGone {
		if present[id] || now.Sub(g.at) > fixedFor {
			delete(s.attnGone, id)
			continue
		}
		items = append(items, g.it)
	}
	s.attnLast = map[string]view.Item{}
	for _, it := range items {
		if !it.Fixed {
			s.attnLast[it.ID] = it
		}
	}
	if (len(s.attnGone) > 0 || len(s.attnAbsent) > 0) && !s.attnTimer {
		// Nothing else may change while an item waits out its grace or its
		// "fixed": look again in a moment.
		s.attnTimer = true
		time.AfterFunc(3*time.Second, func() {
			s.mu.Lock()
			s.attnTimer = false
			s.mu.Unlock()
			s.changed()
		})
	}
	a := view.AttentionOf(items, s.notNow)
	s.lastAttn = &a
	return a
}

// machineChecks is the machine's report, looked at every half minute
// while the window is open and every few minutes while only the menu bar
// asks: each look runs launchd and the keychain.
func (s *Service) machineChecks(stacks []hub.StackView) []view.Check {
	return s.doctorWithin(stacks, s.lookEvery(30*time.Second))
}

// leftoversNow is the leftover counts at the same pace.
func (s *Service) leftoversNow() (safe, data int) {
	return s.leftoversWithin(s.lookEvery(leftoverEvery))
}

// lookEvery is open, or five minutes when no window is showing a page.
func (s *Service) lookEvery(open time.Duration) time.Duration {
	s.mu.Lock()
	shown := s.focus.Page != ""
	s.mu.Unlock()
	if shown {
		return open
	}
	return 5 * time.Minute
}

// LastAttention is the list as last built for a push: the menu bar and
// notifications read it, so neither builds it again.
func (s *Service) LastAttention() view.Attention {
	s.mu.Lock()
	a := s.lastAttn
	s.mu.Unlock()
	if a == nil {
		return s.Attention()
	}
	return *a
}

// OnAttention says who else wants the list when it changes (the menu bar).
func (s *Service) OnAttention(fn func()) {
	s.mu.Lock()
	s.onAttn = fn
	s.mu.Unlock()
}

// quiet says items leave without "fixed": the person dismissed or removed
// them.
func (s *Service) quiet(match func(view.Item) bool) {
	s.mu.Lock()
	if s.attnQuiet == nil {
		s.attnQuiet = map[string]bool{}
	}
	for id, it := range s.attnLast {
		if match(it) {
			s.attnQuiet[id] = true
		}
	}
	s.mu.Unlock()
}

// NotNow puts an item aside until what it says changes; "" for print
// brings it back.
func (s *Service) NotNow(id, print string) {
	home := s.hub.Home()
	p := prefs.Load(home)
	if p.NotNow == nil {
		p.NotNow = map[string]string{}
	}
	if print == "" {
		delete(p.NotNow, id)
	} else {
		p.NotNow[id] = print
	}
	// The file is kept small: the oldest choices go past a hundred.
	for k := range p.NotNow {
		if len(p.NotNow) <= 100 {
			break
		}
		if k != id {
			delete(p.NotNow, k)
		}
	}
	s.mu.Lock()
	s.notNow, s.notNowSet = p.NotNow, true
	s.mu.Unlock()
	_ = prefs.Save(home, p)
	s.changed()
}

// OpenBell opens Needs you: at once on a page that is up, or as the page
// boots when the window is only now opening (Pending says so, briefly).
func (s *Service) OpenBell() {
	s.mu.Lock()
	s.bellAt = s.now()
	s.mu.Unlock()
	s.Menu("bell")
}

// FixItem presses an item's fix from outside the page (the menu bar, a
// notification): the ones that ask nothing, since there is no dialog
// there. An Up still looks first, and one that would ask opens the window
// on the item's page instead.
func (s *Service) FixItem(it view.Item, f view.Fix) error {
	switch f.Kind {
	case "do":
		if f.Verb == "restart" || f.Verb == "task" {
			return s.Do(it.Key, f.Verb, it.Instance)
		}
	case "stack":
		if f.Verb == "up-last" && s.Preflight(it.Key, "up-last", nil).Blocked {
			return errNeedsPage
		}
		if f.Verb == "restart" || f.Verb == "up-last" {
			return s.Stack(it.Key, f.Verb, nil)
		}
	case "holder":
		return s.StopHolder(it.Key, it.Instance, f.PID)
	case "dismiss":
		s.Dismiss(it.Key)
		return nil
	case "clean":
		return s.ClearLeftover("all")
	}
	return errNeedsPage
}

// errNeedsPage says a fix asks first, so it is done on the page.
var errNeedsPage = errors.New("this one asks first: open it in doze")

// Away is what happened while the window was closed, once: asking for it
// forgets when the window closed, so a reload does not say it again. Time
// before the app started is not counted: nothing was watching then.
func (s *Service) Away() *view.Away {
	home := s.hub.Home()
	p := prefs.Load(home)
	since := p.WindowClosed
	if since.IsZero() {
		return nil
	}
	p.WindowClosed = time.Time{}
	_ = prefs.Save(home, p)
	if since.Before(s.started) {
		since = s.started
	}
	return view.AwayOf(s.hub.Stacks(), since, s.now())
}

// trimStopped takes " · its stack is stopped" off a task item's detail.
func trimStopped(d string) string {
	return strings.TrimSuffix(d, " · its stack is stopped")
}

// WebURL is the address to open a running stack at, for the menu bar: its
// own domain when an app declares one, else its first app's .doze address
// (port-less while the front door is up), else "".
func (s *Service) WebURL(key string) string {
	st, ok := s.stack(key)
	if !ok || !st.Running {
		return ""
	}
	for _, v := range st.Instances {
		if v.HTTPS != "" {
			return v.HTTPS
		}
	}
	for _, v := range st.Instances {
		if v.Kind != "app" || v.Domain == "" {
			continue
		}
		if url := s.urlFor(v.Domain); url != "" {
			return url
		}
		if addr, port := display.URL(v); strings.HasPrefix(addr, "http") {
			return addr + port
		}
	}
	return ""
}

// AskUp has the page start a stack itself, so the checks' sheet can ask:
// at once on a page that is up, or as the page boots (Pending, briefly).
func (s *Service) AskUp(key, action string) {
	s.mu.Lock()
	s.upKey, s.upAction, s.upAt = key, action, s.now()
	s.mu.Unlock()
	s.Menu(action + ":" + key)
}
