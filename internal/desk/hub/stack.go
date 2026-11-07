package hub

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/journal"
)

// ErrNotRunning is a verb sent to a stack with no daemon.
var ErrNotRunning = errors.New("the stack is not running")

// ErrNotResponding is a verb sent while the daemon is not answering.
var ErrNotResponding = errors.New("the daemon is not answering: nothing was sent")

// eventsKept is how many journal events a stack keeps, as the dash did.
const eventsKept = 300

// stack is one running daemon and what it has said.
type stack struct {
	key, socket string
	d           control.Daemon
	now         func() time.Time
	changed     func()
	cancel      context.CancelFunc

	mu        sync.Mutex
	conn      Conn
	retryAt   time.Time
	lastFrame time.Time
	loaded    time.Time // when the daemon read doze.hcl
	name, dir string
	idle      time.Duration
	instances []control.InstanceView
	events    []journal.Event
	seq       uint64 // the last journal sequence seen, to resume after
	series    map[string]*Series
	lines     map[string]lines
	all       lines
	// added counts lines ever added per instance ("" for the merged list):
	// a ring's length stops moving once it is full, this does not.
	added map[string]uint64
	// want counts the pages that need each optional topic (stats, logs);
	// the subscription is remade when a topic's count crosses zero.
	want map[string]int
	// resub ends the current subscription so keep opens one with the
	// topics wanted now. Nil between subscriptions.
	resub context.CancelFunc
	// consoles are the module consoles a page is showing, by instance.
	consoles map[string]*consoleState
	// since is when the hub first saw the daemon; lastSet is the project
	// record's remembered start set.
	since   time.Time
	lastSet []string
}

func newStack(key, socket string, d control.Daemon, now func() time.Time, changed func()) *stack {
	return &stack{
		key: key, socket: socket, d: d, now: now, changed: changed, cancel: func() {},
		series: map[string]*Series{}, lines: map[string]lines{}, want: map[string]int{}, added: map[string]uint64{},
		consoles: map[string]*consoleState{}, since: now(),
	}
}

// setRecord keeps what the project record says was started last time.
func (s *stack) setRecord(lastSet []string) {
	s.mu.Lock()
	s.lastSet = lastSet
	s.mu.Unlock()
}

// start opens the subscription and keeps it open until ctx ends, as the
// dash's keepSubscribed did: when it drops, wait, then ask again, with a
// wait that doubles to five seconds.
func (s *stack) start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	go s.keep(ctx)
}

func (s *stack) keep(ctx context.Context) {
	wait := 500 * time.Millisecond
	for ctx.Err() == nil {
		s.snapshot(ctx)
		subCtx, end := context.WithCancel(ctx)
		s.mu.Lock()
		s.resub = end
		sub := s.subscription()
		s.mu.Unlock()
		first := true
		err := s.d.Subscribe(subCtx, sub, func(f control.Frame) error {
			if first {
				first, wait = false, 500*time.Millisecond
				s.mu.Lock()
				s.conn = ConnLive
				s.mu.Unlock()
			}
			s.apply(f)
			s.changed()
			return nil
		})
		s.mu.Lock()
		s.resub = nil
		s.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		if subCtx.Err() != nil && err == nil {
			continue // ended on purpose, to change topics: straight back
		}
		s.mu.Lock()
		s.conn = ConnDown
		s.retryAt = s.now().Add(wait)
		s.mu.Unlock()
		s.changed()
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if wait *= 2; wait > 5*time.Second {
			wait = 5 * time.Second
		}
	}
}

// snapshot asks the daemon what the stack is, before the stream starts and
// again after each reconnect, so the name and folder are known at once.
func (s *stack) snapshot(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	snap, err := s.d.Snapshot(c)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.name, s.dir, s.idle, s.loaded = snap.Stack, snap.Dir, snap.IdleTimeout, snap.Loaded
	s.instances = sorted(snap.Instances)
	// The daemon answered: it is live from here, whether or not its stream
	// has said anything yet.
	s.conn, s.lastFrame = ConnLive, s.now()
	s.mu.Unlock()
	s.changed()
}

// subscription is what to ask for: instances and the journal always; stats
// and logs while a page wants them. Caller holds mu.
func (s *stack) subscription() control.Subscription {
	since := control.JournalFromStart
	if s.seq > 0 {
		since = s.seq
	}
	sub := control.Subscription{Topics: []string{control.TopicInstances, control.TopicJournal}, Since: since}
	if s.want[control.TopicStats] > 0 {
		sub.Topics = append(sub.Topics, control.TopicStats)
	}
	if s.want[control.TopicLogs] > 0 {
		sub.Topics = append(sub.Topics, control.TopicLogs)
		sub.Backlog = linesKept
	}
	return sub
}

// watch adds a page's interest in a topic and returns what ends it. The
// subscription is remade when the first page arrives or the last leaves.
func (s *stack) watch(topic string) (release func()) {
	s.mu.Lock()
	s.want[topic]++
	first := s.want[topic] == 1
	end := s.resub
	s.mu.Unlock()
	if first && end != nil {
		end()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.want[topic]--
			last := s.want[topic] == 0
			end := s.resub
			s.mu.Unlock()
			if last && end != nil {
				end()
			}
		})
	}
}

// apply folds a frame into what the stack knows, as the dash's apply did.
func (s *stack) apply(f control.Frame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFrame = s.now()
	switch f.Topic {
	case control.TopicInstances:
		if f.Snapshot {
			s.instances = sorted(f.Instances)
		} else {
			for _, v := range f.Instances {
				replaced := false
				for i := range s.instances {
					if s.instances[i].Name == v.Name {
						s.instances[i], replaced = v, true
					}
				}
				if !replaced {
					s.instances = sorted(append(s.instances, v))
				}
			}
		}
		for _, name := range f.Removed {
			out := s.instances[:0]
			for _, v := range s.instances {
				if v.Name != name {
					out = append(out, v)
				}
			}
			s.instances = out
		}
	case control.TopicStats:
		for _, st := range f.Stats {
			sr := s.series[st.Name]
			if sr == nil {
				sr = &Series{}
				s.series[st.Name] = sr
			}
			sr.push(st, f.At)
			for i := range s.instances {
				if s.instances[i].Name == st.Name {
					s.instances[i].RAM, s.instances[i].CPU, s.instances[i].Conns = st.RAM, st.CPU, st.Conns
				}
			}
		}
	case control.TopicJournal:
		if f.Event != nil && !s.seenEvent(*f.Event) {
			s.events = append(s.events, *f.Event)
			if len(s.events) > eventsKept {
				s.events = s.events[len(s.events)-eventsKept:]
			}
			if f.Event.Seq > s.seq {
				s.seq = f.Event.Seq
			}
		}
	case control.TopicLogs:
		if f.Log != nil {
			var added bool
			s.lines[f.Log.Instance], added = s.lines[f.Log.Instance].add(*f.Log, linesKept)
			if added {
				s.added[f.Log.Instance]++
				s.added[""]++
				s.all, _ = s.all.add(*f.Log, allLinesKept)
			}
		}
	}
}

func sorted(vs []control.InstanceView) []control.InstanceView {
	out := append([]control.InstanceView(nil), vs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *stack) view() StackView {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := StackView{
		Key: s.key, Name: s.name, Dir: s.dir, IdleTimeout: s.idle, Running: true,
		Conn: s.conn, RetryAt: s.retryAt, LastFrame: s.lastFrame, Metered: s.want[control.TopicStats] > 0, Loaded: s.loaded,
		Instances: append([]control.InstanceView(nil), s.instances...),
		Events:    append([]journal.Event(nil), s.events...),
		Series:    map[string]Series{},
		Consoles:  s.consoleViews(),
		LastSet:   append([]string(nil), s.lastSet...),
		Since:     s.since,
	}
	if !s.loaded.IsZero() && s.loaded.Before(v.Since) {
		// The daemon read doze.hcl when it started, which may be long
		// before this hub saw it: "up" counts from then.
		v.Since = s.loaded
	}
	for name, sr := range s.series {
		v.Series[name] = sr.clone()
	}
	return v
}

// logs is what an instance printed, oldest first; "" is every instance.
func (s *stack) logs(instance string) []control.LogFrame {
	s.mu.Lock()
	defer s.mu.Unlock()
	if instance == "" {
		return append([]control.LogFrame(nil), s.all...)
	}
	return append([]control.LogFrame(nil), s.lines[instance]...)
}

func (s *stack) logsVersion(instance string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.added[instance]
}

func (s *stack) do(ctx context.Context, verb control.Verb) error {
	s.mu.Lock()
	live := s.conn == ConnLive
	s.mu.Unlock()
	if !live {
		return ErrNotResponding
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.d.Do(c, verb)
}

// seenEvent says the journal already holds e: a daemon replays its journal
// on a resubscribe from the start (or a scripted one with no sequence
// numbers does), and the page must not read an event twice.
func (s *stack) seenEvent(e journal.Event) bool {
	for i := len(s.events) - 1; i >= 0; i-- {
		h := s.events[i]
		if e.Seq > 0 && h.Seq > 0 {
			if h.Seq == e.Seq {
				return true
			}
			continue
		}
		if h.Time.Equal(e.Time) && h.Kind == e.Kind && h.Instance == e.Instance && h.Detail == e.Detail {
			return true
		}
	}
	return false
}
