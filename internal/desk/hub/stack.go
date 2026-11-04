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
var ErrNotResponding = errors.New("the daemon is not responding: nothing was sent")

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
	name, dir string
	instances []control.InstanceView
	events    []journal.Event
	seq       uint64 // the last journal sequence seen, to resume after
}

func newStack(key, socket string, d control.Daemon, now func() time.Time, changed func()) *stack {
	return &stack{key: key, socket: socket, d: d, now: now, changed: changed, cancel: func() {}}
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
		first := true
		err := s.d.Subscribe(ctx, s.subscription(), func(f control.Frame) error {
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
		if ctx.Err() != nil {
			return
		}
		_ = err
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
	s.name, s.dir = snap.Stack, snap.Dir
	s.instances = sorted(snap.Instances)
	s.mu.Unlock()
}

func (s *stack) subscription() control.Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	since := control.JournalFromStart
	if s.seq > 0 {
		since = s.seq
	}
	return control.Subscription{Topics: []string{control.TopicInstances, control.TopicJournal}, Since: since}
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
	case control.TopicJournal:
		if f.Event != nil {
			s.events = append(s.events, *f.Event)
			if len(s.events) > eventsKept {
				s.events = s.events[len(s.events)-eventsKept:]
			}
			if f.Event.Seq > s.seq {
				s.seq = f.Event.Seq
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
	return StackView{
		Key: s.key, Name: s.name, Dir: s.dir, Running: true,
		Conn: s.conn, RetryAt: s.retryAt, LastFrame: s.lastFrame,
		Instances: append([]control.InstanceView(nil), s.instances...),
		Events:    append([]journal.Event(nil), s.events...),
	}
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
