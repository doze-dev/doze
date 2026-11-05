package hub

import (
	"context"
	"sync"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/display"
)

// A module's console, as the hub holds it for one instance: the spec the
// module described, the rows its tables last read, the lines its stream has
// produced, as the dash's consoleState did. A console is subscribed to only
// while a page shows it (D14), one subscription per instance, remade when
// the stream it follows changes.

// streamKept is how many stream rows a panel keeps, as the dash kept.
const streamKept = 300

// ConsoleView is what a page draws a console from.
type ConsoleView struct {
	Spec    *control.ConsoleSpec
	Actions []control.ActionView
	// State is the instance's state as the module last said it; Err the
	// console's error, when the module could not answer.
	State, Err string
	// Tables are the rows last read by panel id, with rate cells worked out;
	// Streams the rows a stream panel has produced, oldest first.
	Tables  map[string][]control.ConsoleRow
	Streams map[string][]control.ConsoleRow
}

type consoleState struct {
	spec    *control.ConsoleSpec
	actions []control.ActionView
	state   string
	err     string
	tables  map[string][]control.ConsoleRow
	streams map[string][]control.ConsoleRow
	prev    map[string]display.RateSample
	// panel and sector are the stream being followed, and which row of its
	// table; they scope the subscription.
	panel, sector string
	gen           int
	cancel        context.CancelFunc
}

func newConsoleState() *consoleState {
	return &consoleState{
		tables: map[string][]control.ConsoleRow{}, streams: map[string][]control.ConsoleRow{},
		prev: map[string]display.RateSample{}, cancel: func() {},
	}
}

// setConsoleWanted opens an instance's console subscription, or closes it
// and forgets the rows. The hub decides what is wanted from the pages'
// interest, so a stack that comes back after its daemon restarted is
// asked again from the start.
func (s *stack) setConsoleWanted(inst string, wanted bool) {
	s.mu.Lock()
	c := s.consoles[inst]
	switch {
	case wanted && c == nil:
		s.consoles[inst] = newConsoleState()
		s.mu.Unlock()
		s.resubConsole(inst)
		return
	case !wanted && c != nil:
		c.cancel()
		delete(s.consoles, inst)
	}
	s.mu.Unlock()
}

// followConsole makes the subscription follow a stream panel and the row of
// its table it is for, when that changed, as the dash's followStream did.
func (s *stack) followConsole(inst, panel, sector string) {
	s.mu.Lock()
	c := s.consoles[inst]
	if c == nil || (c.panel == panel && c.sector == sector) {
		s.mu.Unlock()
		return
	}
	c.panel, c.sector = panel, sector
	delete(c.streams, panel)
	s.mu.Unlock()
	s.resubConsole(inst)
}

// resubConsole replaces the instance's console subscription with one for
// what is followed now. Caller does not hold mu.
func (s *stack) resubConsole(inst string) {
	s.mu.Lock()
	c := s.consoles[inst]
	if c == nil {
		s.mu.Unlock()
		return
	}
	c.cancel()
	c.gen++
	gen := c.gen
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	sub := control.Subscription{Topics: []string{control.TopicConsole}, Names: []string{inst}, Panel: c.panel, Selector: c.sector}
	s.mu.Unlock()
	go s.keepConsole(ctx, inst, gen, sub)
}

// keepConsole holds the console subscription open until ctx ends, asking
// again after a drop with a wait that doubles to five seconds.
func (s *stack) keepConsole(ctx context.Context, inst string, gen int, sub control.Subscription) {
	wait := 500 * time.Millisecond
	for ctx.Err() == nil {
		err := s.d.Subscribe(ctx, sub, func(f control.Frame) error {
			if f.Console != nil {
				s.applyConsole(inst, gen, *f.Console)
				s.changed()
			}
			return nil
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.mu.Lock()
			if c := s.consoles[inst]; c != nil && c.gen == gen {
				c.err = display.Humanise(err)
			}
			s.mu.Unlock()
			s.changed()
		}
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

// applyConsole folds a console frame in, as the dash's did: rows of a table
// replace what was there, with rates worked out; rows of a stream are
// appended.
func (s *stack) applyConsole(inst string, gen int, f control.ConsoleFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.consoles[inst]
	if c == nil || c.gen != gen {
		return
	}
	if f.Spec != nil {
		c.spec, c.actions, c.state, c.err = f.Spec, f.Actions, f.State, ""
	}
	if f.Error != "" && f.Panel == "" {
		c.err = f.Error
	}
	if f.Panel == "" {
		return
	}
	switch {
	case f.Rows != nil:
		c.tables[f.Panel] = display.Rates(c.spec, f.Panel, f.Rows, c.prev, s.now())
	case f.Row != nil:
		rows := append(c.streams[f.Panel], *f.Row)
		if len(rows) > streamKept {
			rows = rows[len(rows)-streamKept:]
		}
		c.streams[f.Panel] = rows
	}
}

// consoleViews is every console known, cloned. Caller holds mu.
func (s *stack) consoleViews() map[string]ConsoleView {
	if len(s.consoles) == 0 {
		return nil
	}
	out := map[string]ConsoleView{}
	for inst, c := range s.consoles {
		v := ConsoleView{Spec: c.spec, Actions: append([]control.ActionView(nil), c.actions...), State: c.state, Err: c.err,
			Tables: map[string][]control.ConsoleRow{}, Streams: map[string][]control.ConsoleRow{}}
		for k, rows := range c.tables {
			v.Tables[k] = append([]control.ConsoleRow(nil), rows...)
		}
		for k, rows := range c.streams {
			v.Streams[k] = append([]control.ConsoleRow(nil), rows...)
		}
		out[inst] = v
	}
	return out
}

// admin runs a module's action, refusing while the daemon is not answering.
func (s *stack) admin(ctx context.Context, inst, action, resource, input string) (string, error) {
	s.mu.Lock()
	live := s.conn == ConnLive
	s.mu.Unlock()
	if !live {
		return "", ErrNotResponding
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.d.Admin(c, inst, action, resource, input)
}

// WatchConsole says a page shows an instance's console, so the hub subscribes
// to it; the returned func says it stopped. The interest is kept by key,
// as Watch keeps it, so a stack whose daemon restarts streams the console
// again as soon as it is back.
func (h *Hub) WatchConsole(key, inst string) (release func()) {
	h.mu.Lock()
	if h.consoleWants[key] == nil {
		h.consoleWants[key] = map[string]int{}
	}
	h.consoleWants[key][inst]++
	s := h.stacks[key]
	h.mu.Unlock()
	if s != nil {
		s.setConsoleWanted(inst, true)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			still := 0
			if m := h.consoleWants[key]; m != nil && m[inst] > 0 {
				m[inst]--
				still = m[inst]
				if still == 0 {
					delete(m, inst)
				}
			}
			s := h.stacks[key]
			h.mu.Unlock()
			if still == 0 && s != nil {
				s.setConsoleWanted(inst, false)
			}
		})
	}
}

// FollowConsole makes a watched console's subscription follow a stream
// panel and the row of its table.
func (h *Hub) FollowConsole(key, inst, panel, sector string) {
	h.mu.Lock()
	s := h.stacks[key]
	h.mu.Unlock()
	if s != nil {
		s.followConsole(inst, panel, sector)
	}
}

// Admin runs a module's action on an instance: cancel a query, produce a
// message. What it answers is for the person.
func (h *Hub) Admin(ctx context.Context, key, inst, action, resource, input string) (string, error) {
	h.mu.Lock()
	s := h.stacks[key]
	h.mu.Unlock()
	if s == nil {
		return "", ErrNotRunning
	}
	return s.admin(ctx, inst, action, resource, input)
}

// stopConsoles ends every console subscription: the stack is gone.
func (s *stack) stopConsoles() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for inst, c := range s.consoles {
		c.cancel()
		delete(s.consoles, inst)
	}
}
