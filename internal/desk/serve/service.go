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
	"hash/fnv"
	"sync"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/view"
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
)

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
}

// Focus is what the page is showing: the stack, the page and, on an
// instance or logs page, the instance. The service pushes only that.
type Focus struct {
	Key      string `json:"key"`
	Page     string `json:"page"` // "home", "instance", "logs"
	Instance string `json:"instance,omitempty"`
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
	Key  string `json:"key"`
	Page string `json:"page"`
}

// Emitter delivers a push to the page.
type Emitter func(name string, data any)

// Service is the API.
type Service struct {
	hub  *hub.Hub
	now  func() time.Time
	emit Emitter

	mu         sync.Mutex
	focus      Focus
	pendingDir string
	releases   []func()
	sent       map[string]uint64 // event name → hash of what was last sent
	dirty      bool
	wake       chan struct{}
}

// New makes a service over a hub. Run starts its pushes.
func New(h *hub.Hub, emit Emitter) *Service {
	s := &Service{hub: h, now: time.Now, emit: emit, sent: map[string]uint64{}, wake: make(chan struct{}, 1)}
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
			emit(EventNav, Nav{Key: key, Page: "home"})
		}
	}
	if f.Key == "" {
		return
	}
	switch f.Page {
	case "home":
		s.send(EventHome, s.Home(f.Key))
	case "instance":
		s.send(EventInstance, s.Instance(f.Key, f.Instance))
		s.sendLogs(f) // the page's logs tab
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
	return view.InstanceOf(st, name, s.now())
}

// Logs is a window of an instance's lines, or of every instance's.
func (s *Service) Logs(key string, q view.LogsQuery) view.Logs {
	return view.LogsOf(q, s.hub.Logs(key, q.Instance), s.now())
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
	if f.Key != "" {
		switch f.Page {
		case "home":
			s.releases = append(s.releases, s.hub.Watch(f.Key, control.TopicStats))
		case "instance":
			s.releases = append(s.releases, s.hub.Watch(f.Key, control.TopicStats), s.hub.Watch(f.Key, control.TopicLogs))
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
// daemon is not answering.
func (s *Service) Do(key, verb, target string) error {
	err := s.hub.Do(context.Background(), key, control.Verb{Name: verb, Target: target})
	s.hub.Refresh()
	s.changed()
	if err != nil {
		return errors.New(display.Humanise(err))
	}
	return nil
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
		return Nav{}
	}
	s.mu.Lock()
	if s.pendingDir == dir {
		s.pendingDir = ""
	}
	s.mu.Unlock()
	return Nav{Key: key, Page: "home"}
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
