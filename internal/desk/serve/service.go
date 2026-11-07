// Package serve is the app's API: the calls a page makes and the pushes it
// receives, over the hub and the views. It knows no transport: internal/
// desk/app binds it as a Wails service and forwards Emit to Wails events;
// Handler (http.go) serves the same calls over HTTP and the pushes over SSE
// for the browser, Playwright and the dev server.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	names "github.com/doze-dev/doze-names"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/notify"
	"github.com/doze-dev/doze/internal/desk/prefs"
	"github.com/doze-dev/doze/internal/desk/view"
	"github.com/doze-dev/doze/internal/detect"
	"github.com/doze-dev/doze/internal/display"
	"github.com/doze-dev/doze/internal/projects"
)

// Push names. The page subscribes by name; data is the view as JSON.
const (
	// EventStacks carries []StackSummary whenever a stack appears, goes away
	// or changes state.
	EventStacks = "stacks"
	// EventHome carries a view.Home for the focused stack.
	EventHome = "home"
	// EventInstance carries a view.Instance for the focused instance.
	EventInstance = "instance"
	// EventLogs says the focused logs changed: data is LogsChanged.
	EventLogs = "logs"
	// EventNav asks the page to go somewhere: data is Nav.
	EventNav = "nav"
	// EventConsole carries a view.Console for the focused console tab.
	EventConsole = "console"
	// EventNotice is something to say to the person: data is Notice. A
	// success fades after 4 s; a failure stays 12 s, as the TUI's did.
	EventNotice = "notice"
	// EventLibrary carries the first tab, while it is in focus.
	EventLibrary = "library"
	// EventMenu is a native menu item the page acts on, as its own key
	// would: data is its name (back, forward, logs, palette, help).
	EventMenu = "menu"
	// EventAttention is Needs you: what is wrong now, everywhere (D19).
	EventAttention = "attention"
)

// Notice is one thing said to the person, kept among the last twenty.
type Notice struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
	Bad  bool      `json:"bad"`
}

// noticesKept is how many recent messages are kept, as the dash kept them.
const noticesKept = 20

// Pace is the least time between two pushes of the same view (D14: live
// data only for what is on screen, and not faster than a person reads).
const Pace = 250 * time.Millisecond

// StackSummary is one stack in the tab strip.
type StackSummary struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Running bool   `json:"running"`
	Live    bool   `json:"live"`
	Awake   int    `json:"awake"`
	Trouble bool   `json:"trouble"`
	// Troubled counts the instances in trouble: the tab's red badge.
	Troubled int `json:"troubled,omitempty"`
	// Broken says its doze.hcl does not load: the tab's dot says so on
	// every page, not only the stack's overview.
	Broken bool `json:"broken,omitempty"`
}

// Focus is what the page is showing: the stack, the page and, on an
// instance or logs page, the instance. The service pushes only that.
type Focus struct {
	Key      string `json:"key"`
	Page     string `json:"page"` // "home", "instance", "logs"
	Instance string `json:"instance,omitempty"`
	// Tab is the instance page's tab; Sel the selected row of each console
	// table, by block id, which a detail and a stream follow.
	Tab string         `json:"tab,omitempty"`
	Sel map[string]int `json:"sel,omitempty"`
}

// LogsChanged says the focused logs have more or different lines. Version
// counts lines ever added, so it moves even when the kept ring is full.
type LogsChanged struct {
	Instance string `json:"instance"`
	Total    int    `json:"total"`
	Version  uint64 `json:"version"`
}

// Nav is where the app asks the page to go: a stack's home, from the
// menu bar or a second launch.
type Nav struct {
	Key      string `json:"key"`
	Page     string `json:"page"`
	Instance string `json:"instance,omitempty"`
	// Dir is the folder an "empty" page is for: one with no doze.hcl yet.
	Dir string `json:"dir,omitempty"`
	// Bell says Needs you was asked for as the window opened.
	Bell bool `json:"bell,omitempty"`
	// Act is a stack action the page makes on arrival: "up-last" or
	// "up-all" from the menu bar, on Key.
	Act string `json:"act,omitempty"`
}

// Emitter delivers a push to the page.
type Emitter func(name string, data any)

// Service is the API.
type Service struct {
	hub *hub.Hub
	// FakeStack, when set, stands in for the CLI behind a stack action (up,
	// down, restart): the fixture server sets it so a test never starts a
	// real daemon. It returns what the CLI would print, and its error.
	FakeStack func(st hub.StackView, action string) ([]byte, error)
	now       func() time.Time
	emit      Emitter
	version   string

	mu         sync.Mutex
	focus      Focus
	pendingDir string
	releases   []func()
	notices    []Notice
	// tasks is each task's last run as last seen, to say when one finished.
	tasks map[string]string
	// trustAt and trustSteps cache the domains' setup check, which runs the
	// keychain tool: once a minute is plenty for a banner.
	trustAt    time.Time
	trustKey   string
	trustSteps []names.Step
	// doctorAt and doctorChecks cache the machine report, which shells out
	// to launchd and the keychain: the Library paints without it and gets
	// it on the next push. doctorBusy says a check is under way.
	doctorAt     time.Time
	doctorChecks []view.Check
	doctorBusy   bool
	// configs is the last look at each running stack's doze.hcl.
	configs map[string]configSeen
	// refs is each stack's references as doze.hcl writes them, by folder,
	// read again when the file changes.
	refs map[string]refsSeen
	// admin runs a script as root after the system's administrator
	// prompt; nil says this face cannot ask (SetRepair). run runs a doze
	// command as the person, in a folder; a field so tests can stand in.
	// repairing says Repair is under way.
	admin     func(what, script string) error
	run       func(dir string, args []string) (string, error)
	repairing bool
	// labels are the module actions last seen, by stack, instance and id,
	// so what an action answered is said with its label even after its
	// console is no longer watched.
	labels map[string]string
	// setup sets a folder up (init, add, empty) and starts its daemon; the
	// CLI provides it. Nil says the face cannot.
	setup func(dir, action, arg string) error
	// rules decide what is said outside the window (D8); notify says it.
	rules  *notify.Rules
	notify func(notify.Note)
	// pendingInstance is the instance a notification's Show asked for.
	pendingInstance string
	// pendingPage is a page the menu bar asked for with no stack: "settings".
	pendingPage string
	// leftoversN is the last count of leftovers, taken at leftoversAt.
	leftoversN    int
	leftoversData int
	leftoversAt   time.Time
	leftoversBusy bool
	// urls are the registry's URLs for names, kept a moment (urlFor).
	urls map[string]urlSeen
	// emptyShown is the folder with no doze.hcl the page was last sent to,
	// so each push does not send it there again.
	emptyShown string
	// busy is each stack's action in flight (starting, stopping,
	// restarting), failed the last one that did not work, with its output,
	// kept until it is dismissed or tried again.
	busy   map[string]string
	failed map[string]*view.StackFailure
	theme  string
	// attn is Needs you's memory: what each item said last push, the
	// items whose cause went away a moment ago (shown as fixed), and Not
	// now's choices (loaded from prefs once).
	attnLast   map[string]view.Item
	attnGone   map[string]goneItem
	attnAbsent map[string]goneItem
	attnQuiet  map[string]bool
	lastAttn   *view.Attention
	onAttn     func()
	// started is when this service began: "while the window was closed"
	// counts from here at the earliest.
	started   time.Time
	notNow    map[string]string
	notNowSet bool
	attnTimer bool
	// ram is each service's memory when last seen awake, by key#name.
	ram map[string]int64
	// update is the last look at doze's newest release, at updateAt.
	update   *view.Update
	updateAt time.Time
	bellAt   time.Time
	// upKey and upAction are an Up the menu bar asked the page to make,
	// at upAt, so the checks' sheet can ask (AskUp).
	upKey, upAction string
	upAt            time.Time
	sent            map[string]uint64 // event name → hash of what was last sent
	dirty           bool
	wake            chan struct{}
}

// New makes a service over a hub. Run starts its pushes.
func New(h *hub.Hub, emit Emitter) *Service {
	s := &Service{hub: h, now: time.Now, started: time.Now(), emit: emit, sent: map[string]uint64{}, wake: make(chan struct{}, 1), rules: notify.New(), run: runSelf}
	h.OnChange(s.changed)
	return s
}

// SetEmitter replaces where pushes go (the app sets it once Wails is up).
func (s *Service) SetEmitter(e Emitter) {
	s.mu.Lock()
	s.emit = e
	s.mu.Unlock()
}

// SetClock is for tests.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

func (s *Service) changed() {
	s.mu.Lock()
	s.dirty = true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run pushes until ctx ends: when the hub changed, at most once per Pace,
// the stacks and the focused view, each only when it differs from what was
// last sent.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(Pace)
	defer t.Stop()
	// A doze.hcl saved while the window is open is noticed within a look,
	// whatever page is showing: each stack's files are stamped every
	// configLook, and a change is pushed.
	go s.watchConfigs(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}
		s.mu.Lock()
		s.dirty = false
		s.mu.Unlock()
		s.push()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Push sends the stacks and the focused view now, when they changed, and
// the navigation an Open is still waiting for once its stack is known.
func (s *Service) push() {
	s.send(EventStacks, s.Stacks())
	s.mu.Lock()
	before := s.sent[EventAttention]
	s.mu.Unlock()
	s.rememberRAM()
	s.send(EventAttention, s.Attention())
	s.mu.Lock()
	after, fn := s.sent[EventAttention], s.onAttn
	s.mu.Unlock()
	if after != before && fn != nil {
		fn() // the menu bar follows, at the push's pace
	}
	s.reportTasks()
	s.reportOutside()
	s.mu.Lock()
	f := s.focus
	dir := s.pendingDir
	emit := s.emit
	s.mu.Unlock()
	if dir != "" && emit != nil && noConfig(dir) {
		// A folder with nothing to run yet: its page, at once, whether or not
		// the window just opened. It stays pending until a stack appears there.
		s.mu.Lock()
		shown := s.emptyShown == dir
		s.emptyShown = dir
		s.mu.Unlock()
		if !shown {
			emit(EventNav, Nav{Page: "empty", Dir: dir})
		}
	}
	if dir != "" && emit != nil && !noConfig(dir) {
		if key := s.KeyFor(dir); key != "" {
			s.mu.Lock()
			if s.pendingDir == dir {
				s.pendingDir = ""
			}
			s.mu.Unlock()
			nav := Nav{Key: key, Page: "home"}
			s.mu.Lock()
			if s.pendingInstance != "" {
				nav.Page, nav.Instance = "instance", s.pendingInstance
				s.pendingInstance = ""
			}
			s.mu.Unlock()
			emit(EventNav, nav)
		}
	}
	if f.Page == "library" || f.Page == "settings" {
		// Settings reads the Library's machine checks too.
		s.send(EventLibrary, s.Library())
		return
	}
	if f.Key == "" {
		return
	}
	switch f.Page {
	case "home":
		s.send(EventHome, s.Home(f.Key))
		if f.Instance != "" {
			s.sendLogs(f) // the dock under the table follows the selected row
		}
	case "instance":
		s.send(EventInstance, s.Instance(f.Key, f.Instance))
		s.sendLogs(f) // the page's logs tab
		if strings.HasPrefix(f.Tab, "console:") {
			if st, ok := s.stack(f.Key); ok {
				panel, sector := view.Follow(st, f.Instance, f.Tab, f.Sel)
				s.hub.FollowConsole(f.Key, f.Instance, panel, sector)
			}
			s.send(EventConsole, s.Console(f.Key, f.Instance, f.Tab, f.Sel))
		}
	case "logs":
		s.sendLogs(f)
	}
}

func (s *Service) sendLogs(f Focus) {
	s.send(EventLogs, LogsChanged{
		Instance: f.Instance,
		Total:    len(s.hub.Logs(f.Key, f.Instance)),
		Version:  s.hub.LogsVersion(f.Key, f.Instance),
	})
}

func (s *Service) send(name string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	h := fnv.New64a()
	_, _ = h.Write(b)
	sum := h.Sum64()
	s.mu.Lock()
	same := s.sent[name] == sum
	s.sent[name] = sum
	emit := s.emit
	s.mu.Unlock()
	if same || emit == nil {
		return
	}
	emit(name, json.RawMessage(b))
}

// Stacks is every stack, for the tab strip.
func (s *Service) Stacks() []StackSummary {
	var out []StackSummary
	for _, st := range s.hub.Stacks() {
		sum := StackSummary{Key: st.Key, Name: st.Name, Dir: display.Tilde(st.Dir), Running: st.Running, Live: st.Conn == hub.ConnLive}
		if sum.Name == "" && st.Dir != "" {
			sum.Name = filepath.Base(st.Dir) // the tab says what the Library says
		}
		if st.Dir != "" {
			sum.Broken = s.configError(st.Dir) != nil
		}
		for _, v := range st.Instances {
			if v.Kind == "task" {
				continue
			}
			if display.AwakeNow(v) {
				sum.Awake++
			}
			if display.Troubled(v) {
				sum.Trouble = true
				sum.Troubled++
			}
		}
		out = append(out, sum)
	}
	if out == nil {
		out = []StackSummary{}
	}
	return out
}

func (s *Service) stack(key string) (hub.StackView, bool) {
	for _, st := range s.hub.Stacks() {
		if st.Key == key {
			return st, true
		}
	}
	return hub.StackView{}, false
}

// Home is a stack's home page, with the domains' setup banner when they
// are not set up on this machine.
func (s *Service) Home(key string) view.Home {
	st, _ := s.stack(key)
	h := view.HomeOf(st, s.now())
	if st.Running {
		s.fronted(&h, st)
	}
	h.Setup = s.setupFor(st)
	if h.Setup != nil {
		s.mu.Lock()
		h.Setup.Prompt = s.admin != nil
		s.mu.Unlock()
	}
	h.View = s.viewFor(key)
	if st.Running && st.Dir != "" {
		h.Config = s.configError(st.Dir)
		if h.Config == nil {
			h.Changed = s.configChanged(st)
		}
	}
	if !st.Running && st.Dir != "" {
		view.StoppedHome(&h, st, s.now())
		s.frontedDown(&h)
	}
	if st.Dir != "" {
		h.Grid.Refer(s.refsFor(st.Dir))
	}
	s.mu.Lock()
	h.Busy, h.Failed = s.busy[key], s.failed[key]
	s.mu.Unlock()
	return h
}

// setupFor says what an app's own domains still need here: a route, or the
// authority's trust. Nil when nothing is declared or all is in place.
func (s *Service) setupFor(st hub.StackView) *view.Setup {
	domains := view.Domains(st)
	if len(domains) == 0 {
		return nil
	}
	key := st.Key + "|" + strings.Join(domains, ",")
	s.mu.Lock()
	steps, fresh := s.trustSteps, s.trustKey == key && s.now().Sub(s.trustAt) < time.Minute
	s.mu.Unlock()
	if !fresh {
		req := names.TrustRequest{Domains: domains, Home: s.hub.Home()}
		if ca, ok := names.OpenCA(names.CADir(s.hub.Home())); ok {
			req.CA = ca
		}
		steps = names.TrustStatus(req)
		s.mu.Lock()
		s.trustSteps, s.trustKey, s.trustAt = steps, key, s.now()
		s.mu.Unlock()
	}
	var missing []string
	for _, step := range steps {
		if !step.Done {
			missing = append(missing, step.Name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	routes := names.RouteDomains(domains)
	text := routes[0] + " is not reachable here yet"
	if len(routes) > 1 {
		text = strings.Join(routes, ", ") + " are not reachable here yet"
	}
	return &view.Setup{Text: text, Detail: "missing: " + strings.Join(missing, ", ") + " · one admin prompt", Command: "doze trust"}
}

// Instance is an instance's page.
func (s *Service) Instance(key, name string) view.Instance {
	st, _ := s.stack(key)
	out := view.InstanceOf(st, name, s.now())
	if v, ok := display.Find(view.Snapshot(st), name); ok {
		out.Connect = s.reach(v, out.Connect)
	}
	if n := len(out.Runs); n > 0 {
		// Each run kept, matched to its run in the logs from the newest: the
		// two count differently, and older lines may be gone.
		logs := view.LogsOf(view.LogsQuery{Instance: name}, s.hub.Logs(key, name), st.Events, s.now())
		for i := range out.Runs {
			back := n - out.Runs[i].Seq // 0 for the newest
			if j := len(logs.Runs) - 1 - back; j >= 0 && j < len(logs.Runs) {
				out.Runs[i].Log = logs.Runs[j]
			}
		}
	}
	if st.Dir != "" {
		if ce := s.configError(st.Dir); ce != nil {
			out.Broken = "doze.hcl does not load"
			if ce.Line > 0 {
				out.Broken += fmt.Sprintf(" (line %d)", ce.Line)
			}
		}
	}
	if out.Crash != nil {
		// Its last run: the tail of what it said before it exited, from that
		// run only.
		kept := s.hub.Logs(key, name)
		logs := view.LogsOf(view.LogsQuery{Instance: name}, kept, st.Events, s.now())
		if n := len(logs.Runs); n > 0 {
			logs = view.LogsOf(view.LogsQuery{Instance: name, Run: logs.Runs[n-1]}, kept, st.Events, s.now())
		}
		if n := len(logs.Lines); n > 8 {
			logs.Lines = logs.Lines[n-8:]
		}
		out.Crash.LastRun = logs.Lines
	}
	if out.Crash != nil {
		// An exit because its port was taken names what has it (PortClash).
		if port := portInUse(append([]string{out.Crash.Error, out.Crash.Where}, lineTexts(out.Crash.LastRun)...)...); port > 0 {
			out.Crash.Held = heldBy(port)
			s.ownedBy(out.Crash.Held)
		}
	}
	return out
}

// Logs is a window of an instance's lines, or of every instance's.
func (s *Service) Logs(key string, q view.LogsQuery) view.Logs {
	st, _ := s.stack(key)
	return view.LogsOf(q, s.hub.Logs(key, q.Instance), st.Events, s.now())
}

// Version is the doze this app is, for the sidebar's foot.
func (s *Service) Version() string { return s.version }

// SetVersion says which doze this is. A dev build's long pseudo-version is
// cut to its release part for the sidebar.
func (s *Service) SetVersion(v string) {
	if i := strings.IndexAny(v, "-+"); i > 0 {
		v = v[:i]
	}
	s.version = v
}

// SetFocus says what the page shows. The hub streams stats for a home or
// instance page and logs for an instance or logs page, and nothing for a
// page nobody is looking at; the pushes follow the focus at once.
func (s *Service) SetFocus(f Focus) {
	if f.Page == "instance" && f.Key != "" && f.Instance != "" {
		// Remembered for ⌘K's Recent section.
		home := s.hub.Home()
		p := prefs.Load(home)
		if len(p.Recent) == 0 || p.Recent[0] != (prefs.Recent{Key: f.Key, Name: f.Instance}) {
			p.Opened(f.Key, f.Instance)
			_ = prefs.Save(home, p)
		}
	}
	s.mu.Lock()
	// Leaving a folder's three-ways page lets the folder go: a window
	// opened later starts where it should, not back on that folder.
	if f.Page != "empty" && f.Page != "" && s.emptyShown != "" && s.pendingDir == s.emptyShown && noConfig(s.pendingDir) {
		s.pendingDir, s.emptyShown = "", ""
	}
	old := s.releases
	s.releases = nil
	s.focus = f
	delete(s.sent, EventHome)
	delete(s.sent, EventInstance)
	delete(s.sent, EventLogs)
	delete(s.sent, EventConsole)
	// The Library too: what was last sent may be older than what the page
	// fetched on the way back, and a change that returns the list to that
	// older state would otherwise not be sent at all.
	delete(s.sent, EventLibrary)
	if f.Key != "" {
		switch f.Page {
		case "home":
			s.releases = append(s.releases, s.hub.Watch(f.Key, control.TopicStats))
			if f.Instance != "" {
				s.releases = append(s.releases, s.hub.Watch(f.Key, control.TopicLogs))
			}
		case "instance":
			s.releases = append(s.releases, s.hub.Watch(f.Key, control.TopicStats), s.hub.Watch(f.Key, control.TopicLogs))
			if st, ok := s.stack(f.Key); ok {
				if v, ok := display.Find(view.Snapshot(st), f.Instance); ok && v.HasConsole {
					s.releases = append(s.releases, s.hub.WatchConsole(f.Key, f.Instance))
				}
			}
		case "logs":
			s.releases = append(s.releases, s.hub.Watch(f.Key, control.TopicLogs))
		}
	}
	s.mu.Unlock()
	for _, r := range old {
		r()
	}
	s.changed()
}

// Blur says the page is hidden: nothing streams, nothing is pushed.
func (s *Service) Blur() { s.SetFocus(Focus{}) }

// Do runs a verb on a stack. The error is the dash's sentence while the
// daemon is not answering, and a failure is also said as a notice, as the
// dash's doneMsg was.
func (s *Service) Do(key, verb, target string) error {
	if verb == control.VerbTask && target != "" {
		s.Say(display.TaskStarted(target), false)
	}
	err := s.hub.Do(context.Background(), key, control.Verb{Name: verb, Target: target})
	s.hub.Refresh()
	s.changed()
	if err != nil {
		what := verb
		if target != "" {
			what += " " + target
		}
		said := display.Humanise(err)
		if strings.Contains(err.Error(), "unknown op") {
			// A daemon from before the verb existed: the app is newer than the
			// stack it is talking to.
			said = "the stack's daemon is from an older doze and does not know " + verb + "; bring the stack down and up to use it"
		}
		s.Say(what+" failed: "+said, true)
		return errors.New(said)
	}
	return nil
}

// DoAll runs a verb on several targets in order, stopping at the first
// failure: "Sleep db and api" sleeps api first, then db.
func (s *Service) DoAll(key, verb string, targets []string) error {
	for _, t := range targets {
		if err := s.Do(key, verb, t); err != nil {
			return err
		}
	}
	return nil
}

// Dialog is the confirmation for a verb on an instance, in the dash's
// words; Needed is false when it should just be done.
func (s *Service) Dialog(key, name, verb string) view.Dialog {
	st, _ := s.stack(key)
	return view.DialogOf(st, name, verb, s.now())
}

// Palette is the ⌘K list for a query, across every stack; key is the
// stack on screen, whose rows come first.
func (s *Service) Palette(key, instance, scope, query string) view.Palette {
	pr := prefs.Load(s.hub.Home())
	stacks := s.hub.Stacks()
	p := view.PaletteOf(stacks, key, instance, scope, pr.Recent, pr.Commands, query, s.now())
	// A stack whose doze.hcl does not load cannot start: its Up row says
	// so and opens its page, where the error is.
	broken := map[string]bool{}
	for _, st := range stacks {
		if !st.Running && st.Dir != "" && s.configError(st.Dir) != nil {
			broken[st.Key] = true
		}
	}
	for i := range p.Sections {
		for j, r := range p.Sections[i].Rows {
			if r.Do.Kind == "stack" && strings.HasPrefix(r.Do.Verb, "up") && broken[r.Do.StackKey] {
				p.Sections[i].Rows[j].Note = "doze.hcl does not load; open it to see why"
				p.Sections[i].Rows[j].Do = view.Doing{Kind: "page", StackKey: r.Do.StackKey, Page: "home"}
			}
		}
	}
	if strings.TrimSpace(query) == "" && scope == "" {
		s.paletteNeeds(&p)
	}
	return p
}

// RememberCommand keeps a command done from ⌘K for its Recent section:
// the newest first, the same one once, six at most.
func (s *Service) RememberCommand(c view.RecentCommand) {
	b, err := json.Marshal(c)
	if err != nil || c.Label == "" {
		return
	}
	home := s.hub.Home()
	p := prefs.Load(home)
	out := []json.RawMessage{b}
	for _, old := range p.Commands {
		var o view.RecentCommand
		if json.Unmarshal(old, &o) == nil && o.Label == c.Label {
			continue
		}
		if out = append(out, old); len(out) == 6 {
			break
		}
	}
	p.Commands = out
	_ = prefs.Save(home, p)
}

// OpenTerminal opens a terminal in a stack's folder (⌘K's "Open a
// terminal in"): Terminal on macOS, the desktop's own elsewhere.
func (s *Service) OpenTerminal(dir string) error {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return fmt.Errorf("%s is not a folder", dir)
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-a", "Terminal", dir).Start()
	}
	cmd := exec.Command("x-terminal-emulator")
	cmd.Dir = dir
	return cmd.Start()
}

// paletteNeeds puts the rest of what is broken (D19) in ⌘K's first
// section: the troubled services are already there; a stack's red items
// open the bell, where their fixes are. Yellow ones wait in the bell, so
// the palette stays short.
func (s *Service) paletteNeeds(p *view.Palette) {
	var rows []view.PaletteRow
	for _, it := range s.Attention().Items {
		if it.Instance != "" || it.Fixed || !it.Red {
			continue
		}
		tone := "r"
		note := it.Detail
		if it.Stack != "" {
			note = it.Stack + " · " + note
		}
		rows = append(rows, view.PaletteRow{Glyph: "dot", Tone: tone, Label: it.Title, Note: note, Key: "⏎ fix", Bad: it.Red, Do: view.Doing{Kind: "bell"}})
	}
	if len(rows) == 0 {
		return
	}
	for i := range p.Sections {
		if p.Sections[i].Title == "Needs you" {
			p.Sections[i].Rows = append(p.Sections[i].Rows, rows...)
			return
		}
	}
	p.Sections = append([]view.PaletteSection{{Title: "Needs you", Rows: rows}}, p.Sections...)
}

// SetTheme remembers the colour theme chosen in Settings, in prefs, so the
// next launch opens in it.
func (s *Service) SetTheme(theme string) {
	s.mu.Lock()
	s.theme = theme
	s.mu.Unlock()
	home := s.hub.Home()
	p := prefs.Load(home)
	if theme == "violet" {
		theme = ""
	}
	if p.Theme != theme {
		p.Theme = theme
		_ = prefs.Save(home, p)
	}
}

// SetMode remembers the mode chosen in Settings: dark, light, or system.
func (s *Service) SetMode(mode string) {
	home := s.hub.Home()
	p := prefs.Load(home)
	if mode == "system" {
		mode = ""
	}
	if p.Mode != mode {
		p.Mode = mode
		_ = prefs.Save(home, p)
	}
}

// Say tells the person something: a toast now, and a line in recent
// messages. The page says its own ("copied …", "opened …") through it too.
func (s *Service) Say(text string, bad bool) {
	n := Notice{At: s.now(), Text: text, Bad: bad}
	s.mu.Lock()
	s.notices = append(s.notices, n)
	if len(s.notices) > noticesKept {
		s.notices = s.notices[len(s.notices)-noticesKept:]
	}
	emit := s.emit
	s.mu.Unlock()
	if emit != nil {
		emit(EventNotice, n)
	}
}

// remember keeps a bad line in recent messages without a toast: for what
// Needs you already raises.
func (s *Service) remember(text string) {
	s.mu.Lock()
	s.notices = append(s.notices, Notice{At: s.now(), Text: text, Bad: true})
	if len(s.notices) > noticesKept {
		s.notices = s.notices[len(s.notices)-noticesKept:]
	}
	s.mu.Unlock()
}

// Recent is the last twenty things said, newest first.
func (s *Service) Recent() []Notice {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Notice, 0, len(s.notices))
	for i := len(s.notices) - 1; i >= 0; i-- {
		out = append(out, s.notices[i])
	}
	return out
}

// reportTasks says how a task went when its record changes, as the dash's
// reportTaskResult did.
func (s *Service) reportTasks() {
	for _, st := range s.hub.Stacks() {
		for _, v := range st.Instances {
			if v.Kind != "task" || v.Task == nil {
				continue
			}
			key := st.Key + "/" + v.Name
			sig := v.Task.RanAt.String() + v.Task.Result
			s.mu.Lock()
			if s.tasks == nil {
				s.tasks = map[string]string{}
			}
			prev, seen := s.tasks[key]
			s.tasks[key] = sig
			s.mu.Unlock()
			if seen && prev != sig {
				if text, bad := display.TaskResult(v.Name, v.Task); text != "" {
					s.Say(text, bad)
				}
			}
		}
	}
}

// Open asks the page to show the stack at a project folder. The folder is
// kept until the hub knows a stack there: a page that is loaded is told
// then (push), one that is not asks when it boots (Pending). At launch, or
// right after doze up, the daemon's socket may not have been seen yet.
func (s *Service) Open(dir string) {
	if dir == "" {
		return
	}
	// A doze.hcl that never ran is a stack all the same: it is recorded as
	// doze up would record it, so it shows, stopped, and the page goes to it.
	if !noConfig(dir) && s.KeyFor(dir) == "" {
		if cfg, err := config.LoadShape(filepath.Join(dir, "doze.hcl"), nil); err == nil {
			path := filepath.Join(dir, "doze.hcl")
			_ = projects.Update(config.ProjectDirIn(s.hub.Home(), path), func(p *projects.Project) {
				p.Config, p.Name = path, cfg.Stack()
			})
		}
	}
	s.mu.Lock()
	if s.pendingDir != dir {
		s.pendingInstance = "" // an older Show's instance is not for this folder
	}
	s.pendingDir = dir
	s.emptyShown = "" // asked again: go there again
	s.mu.Unlock()
	s.hub.Refresh() // a socket that appeared since the last look
	s.changed()     // push resolves it when it can
}

// OpenSettings asks the window for Settings: the menu bar's Settings… item.
func (s *Service) OpenSettings() { s.OpenPage("settings") }

// CheckForUpdates asks the window for Settings › About, checking GitHub
// afresh: the menus' "Check for Updates…".
func (s *Service) CheckForUpdates() { s.OpenPage("updates") }

// OpenLibrary asks the window for the Library: a doze://library link.
func (s *Service) OpenLibrary() { s.OpenPage("library") }

// OpenPage asks the window for a page that needs no stack.
func (s *Service) OpenPage(page string) {
	s.mu.Lock()
	emit := s.emit
	if emit == nil {
		// No page yet: it starts there when the window opens.
		s.pendingPage = page
	}
	s.mu.Unlock()
	if emit != nil {
		emit(EventNav, Nav{Page: page})
		return
	}
	s.changed()
}

// Pending is where the page should start: the last Open, once it resolves
// to a stack the hub knows; until then the page starts on the first tab
// and the Nav arrives with a push.
func (s *Service) Pending() Nav {
	s.mu.Lock()
	bell := !s.bellAt.IsZero() && s.now().Sub(s.bellAt) < 5*time.Second
	s.bellAt = time.Time{}
	upKey, upAct := "", ""
	if !s.upAt.IsZero() && s.now().Sub(s.upAt) < 5*time.Second {
		upKey, upAct = s.upKey, s.upAction
	}
	s.upAt = time.Time{}
	s.mu.Unlock()
	nav := s.pending()
	nav.Bell = bell
	if upKey != "" {
		nav.Key, nav.Page, nav.Act = upKey, "home", upAct
	}
	return nav
}

func (s *Service) pending() Nav {
	s.mu.Lock()
	dir := s.pendingDir
	page := s.pendingPage
	s.pendingPage = ""
	s.mu.Unlock()
	if page != "" {
		return Nav{Page: page}
	}
	if dir == "" {
		return Nav{}
	}
	key := s.KeyFor(dir)
	if key == "" || noConfig(dir) {
		if noConfig(dir) {
			// Nothing to run yet: the page offers the three ways to start,
			// and the folder stays pending until a daemon appears there.
			return Nav{Page: "empty", Dir: dir}
		}
		return Nav{}
	}
	s.mu.Lock()
	nav := Nav{Key: key, Page: "home"}
	if s.pendingDir == dir {
		s.pendingDir = ""
		if s.pendingInstance != "" {
			nav.Page, nav.Instance = "instance", s.pendingInstance
			s.pendingInstance = ""
		}
	}
	s.mu.Unlock()
	return nav
}

// KeyFor is the hub key of the stack at a project folder, or "" when no
// stack there is known.
func (s *Service) KeyFor(dir string) string {
	if dir == "" {
		return ""
	}
	for _, st := range s.hub.Stacks() {
		if st.Dir == dir {
			return st.Key
		}
	}
	return ""
}

// Console is an instance's console tab, resolved for the selection.
func (s *Service) Console(key, name, tab string, sel map[string]int) view.Console {
	st, _ := s.stack(key)
	s.rememberLabels(key, name, st)
	return view.ConsoleOf(st, name, tab, sel)
}

// rememberLabels keeps the labels of an instance's actions, for Admin.
func (s *Service) rememberLabels(key, name string, st hub.StackView) {
	c, ok := st.Consoles[name]
	if !ok {
		return
	}
	s.mu.Lock()
	if s.labels == nil {
		s.labels = map[string]string{}
	}
	for _, a := range c.Actions {
		s.labels[key+"/"+name+"/"+a.ID] = a.Label
	}
	s.mu.Unlock()
}

// Action is the confirmation for a module's action: input when it takes
// some, a question when it deletes something. Needed is false when it
// should just run.
func (s *Service) Action(key, name string, a view.Action) view.Dialog {
	st, _ := s.stack(key)
	s.rememberLabels(key, name, st)
	// The row it acts on is the one selected now, not the one the page
	// last drew: a key pressed right after ↓ means the new row.
	s.mu.Lock()
	f := s.focus
	s.mu.Unlock()
	if f.Key == key && f.Instance == name && f.Tab != "" {
		for _, blk := range collectActions(view.ConsoleOf(st, name, f.Tab, f.Sel).Body) {
			if blk.ID == a.ID {
				a.Resource = blk.Resource
			}
		}
	}
	return view.ActionDialog(st, name, a)
}

// Admin runs a module's action on an instance and says what it answered,
// as the dash's runAction did.
func (s *Service) Admin(key, name, action, resource, input string) error {
	out, err := s.hub.Admin(context.Background(), key, name, action, resource, input)
	s.changed()
	label := action
	s.mu.Lock()
	if l, ok := s.labels[key+"/"+name+"/"+action]; ok {
		label = l
	}
	s.mu.Unlock()
	if err != nil {
		s.Say(label+": "+display.Humanise(err), true)
		return errors.New(display.Humanise(err))
	}
	if out == "" {
		out = label + " done"
	}
	s.Say(out, false)
	return nil
}

// noConfig says a folder exists but has no doze.hcl.
func noConfig(dir string) bool {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "doze.hcl"))
	return os.IsNotExist(err)
}

// SetSetup says how a folder is set up: the CLI's init, add and starter
// file, then its daemon started.
func (s *Service) SetSetup(fn func(dir, action, arg string) error) {
	s.mu.Lock()
	s.setup = fn
	s.mu.Unlock()
}

// Empty is the page for a folder with no doze.hcl: what doze finds there
// and the three ways to start.
func (s *Service) Empty(dir string) view.Empty {
	var found []string
	var items []view.Found
	if res, err := detect.Scan(dir); err == nil {
		for _, c := range []struct {
			n    int
			what string
		}{{len(res.Apps), "app"}, {len(res.Modules), "module"}, {len(res.Services), "service"}, {len(res.Tasks), "task"}} {
			if c.n > 0 {
				found = append(found, fmt.Sprintf("%d %s%s", c.n, c.what, display.Plural(c.n)))
			}
		}
		for _, a := range res.Apps {
			where := a.Run
			if a.Dir != "" {
				where += " · in " + a.Dir
			}
			if a.Port > 0 {
				where += fmt.Sprintf(" · port %d", a.Port)
			}
			items = append(items, view.Found{Name: a.Name, Where: where, Becomes: fmt.Sprintf("app %q", a.Name)})
		}
		for _, m := range res.Modules {
			items = append(items, view.Found{Name: m.Name, Where: m.Why, Becomes: fmt.Sprintf("module %s %q", m.Type, m.Name)})
		}
		for _, sv := range res.Services {
			where := sv.Why
			if sv.Image != "" {
				where = "image " + sv.Image + " · " + where
			}
			items = append(items, view.Found{Name: sv.Name, Where: where, Becomes: fmt.Sprintf("service %q", sv.Name)})
		}
		for _, t := range res.Tasks {
			items = append(items, view.Found{Name: t.Name, Where: t.Run, Becomes: fmt.Sprintf("task %q", t.Name)})
		}
	}
	e := view.EmptyOf(dir, found)
	e.Items = items
	return e
}

// Setup sets a folder up the way chosen and says so; the stack is shown
// once its daemon is up (Open).
func (s *Service) Setup(dir, action, arg string) error {
	s.mu.Lock()
	fn := s.setup
	s.mu.Unlock()
	if fn == nil {
		err := errors.New("this doze cannot set a folder up from here")
		s.Say(err.Error(), true)
		return err
	}
	if err := fn(dir, action, arg); err != nil {
		s.Say("setting up "+display.Tilde(dir)+" failed: "+display.Humanise(err), true)
		return errors.New(display.Humanise(err))
	}
	switch action {
	case "init":
		s.Say("wrote doze.hcl from what "+display.Tilde(dir)+" says", false)
	case "add":
		s.Say("wrote doze.hcl with "+arg+" in "+display.Tilde(dir), false)
	default:
		s.Say("wrote a starter doze.hcl in "+display.Tilde(dir)+" — fill it in, then: doze up", false)
	}
	s.Open(dir)
	return nil
}

// SetNotifier says how to notify outside the window; nil says nothing is.
func (s *Service) SetNotifier(fn func(notify.Note)) {
	s.mu.Lock()
	s.notify = fn
	s.mu.Unlock()
}

// reportOutside says what the rules say is worth a notification: a crash
// or a failed task, once, and not while it is on screen. On screen is the
// page being shown (focus is empty while it is hidden) with that stack in
// front and the instance in view: its own page, or its stack's home.
func (s *Service) reportOutside() {
	s.mu.Lock()
	f := s.focus
	fn := s.notify
	s.mu.Unlock()
	visible := func(key, instance string) bool {
		if f.Key != key {
			return false
		}
		return f.Page == "home" || (f.Page == "instance" && f.Instance == instance)
	}
	notes := s.rules.Next(s.hub.Stacks(), visible, s.now())
	if fn == nil {
		return
	}
	for _, n := range notes {
		fn(n)
	}
}

// OpenAt is Open, then that instance's page: where a notification's Show
// goes. The nav waits with the folder until the hub knows its stack.
func (s *Service) OpenAt(dir, instance string) {
	s.Open(dir)
	s.mu.Lock()
	s.pendingInstance = instance
	s.mu.Unlock()
	s.changed()
}

// collectActions is every action in a console tree.
func collectActions(n view.Node) []view.Action {
	out := append([]view.Action(nil), n.Actions...)
	for _, ch := range n.Children {
		out = append(out, collectActions(ch)...)
	}
	return out
}

// Menu passes a native menu item to the page: the accelerators take their
// keys before the page sees them, so the page is told which one was chosen.
func (s *Service) Menu(item string) {
	s.mu.Lock()
	emit := s.emit
	s.mu.Unlock()
	if emit != nil {
		emit(EventMenu, item)
	}
}

// reach is how to reach an instance, as people should: for an app or a
// service with a name, the URL the front door serves (port-less when a doze
// process holds port 80, which only the registry can tell; with the port
// otherwise), then the name and port, then the IP and port the process
// itself listens on.
func (s *Service) reach(v control.InstanceView, rows []view.KV) []view.KV {
	if v.Endpoint == "" && v.URL != "" {
		// The live feed leaves a module's endpoint and name out; its
		// connection string has both: the name, whose address the registry
		// knows, and the port.
		if u, err := url.Parse(v.URL); err == nil && u.Port() != "" && strings.HasSuffix(u.Hostname(), ".doze") {
			if v.Domain == "" {
				v.Domain = u.Hostname()
			}
			if ip := names.Open(s.hub.Home(), "doze").Resolve(u.Hostname()); ip != nil {
				v.Endpoint = net.JoinHostPort(ip.String(), u.Port())
			}
			if len(rows) > 0 && !hasRow(rows, "Address") {
				addr := u.Hostname() + ":" + u.Port()
				rows = append([]view.KV{rows[0]}, append([]view.KV{{K: "Address", V: addr, Copy: addr, Mono: true}}, rows[1:]...)...)
			}
		}
	}
	if v.Endpoint == "" || strings.HasPrefix(v.Endpoint, "unix:") {
		return rows
	}
	_, port, _ := net.SplitHostPort(v.Endpoint)
	direct := v.Endpoint
	if h, _, err := net.SplitHostPort(v.Bind); err == nil && net.ParseIP(h) != nil && net.ParseIP(strings.Split(v.Endpoint, ":")[0]) == nil {
		direct = v.Bind
	}
	var out []view.KV
	if v.Kind != "module" && v.Domain != "" && v.HTTPS == "" {
		if url := s.urlFor(v.Domain); url != "" {
			kv := view.KV{K: "URL", V: url, Copy: url, Mono: true}
			if strings.HasSuffix(url, ":"+port) {
				kv.Note = "With the front door up, " + v.Domain + " works without the port: Settings › Machine."
			} else {
				kv.Note = "The front door serves it on port 80; no port needed."
			}
			out = append(out, kv)
		}
	}
	// The way to use it first, the bare address under it, then the rest
	// (the connection string of a module, its variable, the data folder).
	fronted := len(out) > 0 // a front-door URL leads; it replaces the name and port
	if direct != "" && fronted {
		out = append(out, view.KV{K: "IP and port", V: direct, Copy: direct, Mono: true, Dim: true, Note: "the process itself, without a name"})
	}
	for _, r := range rows {
		if r.K == "Address" && fronted {
			continue // the URL above says it, the way to use it
		}
		out = append(out, r)
		if r.K == "Address" && direct != "" {
			// A module: its name and port, then the IP and port under it.
			out = append(out, view.KV{K: "IP and port", V: direct, Copy: direct, Mono: true, Dim: true, Note: "the process itself, without a name"})
		}
	}
	return out
}

// urlFor is the registry's URL for a name (port-less while a doze process
// holds the front door), kept for configLook: pages ask for every row on
// every push, and the answer is a file read.
func (s *Service) urlFor(host string) string {
	s.mu.Lock()
	if c, ok := s.urls[host]; ok && s.now().Sub(c.at) < configLook {
		s.mu.Unlock()
		return c.url
	}
	s.mu.Unlock()
	url := names.Open(s.hub.Home(), "doze").URLFor(host)
	s.mu.Lock()
	if s.urls == nil {
		s.urls = map[string]urlSeen{}
	}
	s.urls[host] = urlSeen{url: url, at: s.now()}
	s.mu.Unlock()
	return url
}

type urlSeen struct {
	url string
	at  time.Time
}

// fronted gives every app and service row of a home the address that
// works, as the registry says: port-less while the front door is up.
func (s *Service) fronted(h *view.Home, st hub.StackView) {
	byName := map[string]control.InstanceView{}
	for _, v := range st.Instances {
		byName[v.Name] = v
	}
	for gi := range h.Groups {
		for ri := range h.Groups[gi].Rows {
			r := &h.Groups[gi].Rows[ri]
			v, ok := byName[r.Name]
			if !ok || v.Kind == "module" || v.Kind == "task" || v.Domain == "" || v.HTTPS != "" {
				continue
			}
			if url := s.urlFor(v.Domain); url != "" {
				r.Address, r.URL = url, url
			}
		}
	}
}

// frontedDown gives a stopped stack's web rows the address they will have
// once it is up: port-less while a doze process holds the front door, as
// fronted does for a running stack.
func (s *Service) frontedDown(h *view.Home) {
	port := s.frontDoorPort()
	if port == "" {
		return
	}
	for gi := range h.Groups {
		for ri := range h.Groups[gi].Rows {
			r := &h.Groups[gi].Rows[ri]
			rest, ok := strings.CutPrefix(r.Address, "http://")
			if !ok {
				continue
			}
			host, _, _ := strings.Cut(rest, ":")
			if !strings.HasSuffix(host, "."+config.DomainSuffix) {
				continue
			}
			if port == "80" {
				r.Address = "http://" + host
			} else {
				r.Address = "http://" + host + ":" + port
			}
		}
	}
}

// frontDoorPort is the port a doze process holds the front door on ("80"),
// or "" when none does; kept a moment, like urlFor.
func (s *Service) frontDoorPort() string {
	const key = "\x00front"
	s.mu.Lock()
	if c, ok := s.urls[key]; ok && s.now().Sub(c.at) < configLook {
		s.mu.Unlock()
		return c.url
	}
	s.mu.Unlock()
	port := ""
	if h, ok := names.Open(s.hub.Home(), "doze").IngressHolder(); ok {
		port = "80"
		if _, p, err := net.SplitHostPort(h.Target); err == nil {
			port = p
		}
	}
	s.mu.Lock()
	if s.urls == nil {
		s.urls = map[string]urlSeen{}
	}
	s.urls[key] = urlSeen{url: port, at: s.now()}
	s.mu.Unlock()
	return port
}

func hasRow(rows []view.KV, k string) bool {
	for _, r := range rows {
		if r.K == k {
			return true
		}
	}
	return false
}
