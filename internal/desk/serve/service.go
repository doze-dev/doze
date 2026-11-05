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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/notify"
	"github.com/doze-dev/doze/internal/desk/view"
	"github.com/doze-dev/doze/internal/detect"
	"github.com/doze-dev/doze/internal/display"
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
}

// Emitter delivers a push to the page.
type Emitter func(name string, data any)

// Service is the API.
type Service struct {
	hub     *hub.Hub
	now     func() time.Time
	emit    Emitter
	version string

	mu         sync.Mutex
	focus      Focus
	pendingDir string
	releases   []func()
	notices    []Notice
	// tasks is each task's last run as last seen, to say when one finished.
	tasks map[string]string
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
	theme           string
	sent            map[string]uint64 // event name → hash of what was last sent
	dirty           bool
	wake            chan struct{}
}

// New makes a service over a hub. Run starts its pushes.
func New(h *hub.Hub, emit Emitter) *Service {
	s := &Service{hub: h, now: time.Now, emit: emit, sent: map[string]uint64{}, wake: make(chan struct{}, 1), rules: notify.New()}
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
	s.reportTasks()
	s.reportOutside()
	s.mu.Lock()
	f := s.focus
	dir := s.pendingDir
	emit := s.emit
	s.mu.Unlock()
	if dir != "" && emit != nil {
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

// Home is a stack's home page.
func (s *Service) Home(key string) view.Home {
	st, _ := s.stack(key)
	return view.HomeOf(st, s.now())
}

// Instance is an instance's page.
func (s *Service) Instance(key, name string) view.Instance {
	st, _ := s.stack(key)
	out := view.InstanceOf(st, name, s.now())
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
	s.mu.Lock()
	old := s.releases
	s.releases = nil
	s.focus = f
	delete(s.sent, EventHome)
	delete(s.sent, EventInstance)
	delete(s.sent, EventLogs)
	delete(s.sent, EventConsole)
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
		s.Say(what+" failed: "+display.Humanise(err), true)
		return errors.New(display.Humanise(err))
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

// Palette is the ⌘K list for a query.
func (s *Service) Palette(key, selected, query string) view.Palette {
	st, _ := s.stack(key)
	s.mu.Lock()
	theme := s.theme
	s.mu.Unlock()
	return view.PaletteOf(st, selected, theme, query)
}

// SetTheme remembers the theme the page uses, for the palette's note.
func (s *Service) SetTheme(theme string) {
	s.mu.Lock()
	s.theme = theme
	s.mu.Unlock()
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
	s.mu.Lock()
	if s.pendingDir != dir {
		s.pendingInstance = "" // an older Show's instance is not for this folder
	}
	s.pendingDir = dir
	s.mu.Unlock()
	s.hub.Refresh() // a socket that appeared since the last look
	s.changed()     // push resolves it when it can
}

// Pending is where the page should start: the last Open, once it resolves
// to a stack the hub knows; until then the page starts on the first tab
// and the Nav arrives with a push.
func (s *Service) Pending() Nav {
	s.mu.Lock()
	dir := s.pendingDir
	s.mu.Unlock()
	if dir == "" {
		return Nav{}
	}
	key := s.KeyFor(dir)
	if key == "" {
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
	if res, err := detect.Scan(dir); err == nil {
		for _, c := range []struct {
			n    int
			what string
		}{{len(res.Apps), "app"}, {len(res.Modules), "module"}, {len(res.Services), "service"}, {len(res.Tasks), "task"}} {
			if c.n > 0 {
				found = append(found, fmt.Sprintf("%d %s%s", c.n, c.what, display.Plural(c.n)))
			}
		}
	}
	return view.EmptyOf(dir, found)
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
