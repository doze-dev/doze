// Package fakedaemon is a control.Daemon that plays a script. A test sets the
// snapshot, the frames a subscriber will be sent and the lines each instance
// has printed, runs a command or a screen against it, and compares what came
// out with a golden file. Nothing in it sleeps or reads a clock, so the output
// is the same on every run.
package fakedaemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/doze-dev/doze/internal/control"
)

// Fake is a scripted daemon. The zero value is an empty, idle stack.
type Fake struct {
	// Snap is what Snapshot returns.
	Snap control.Snapshot
	// Frames is the script: every frame a subscriber is sent, in order. A
	// subscriber gets the ones whose topic it asked for.
	Frames []control.Frame
	// Hold keeps a subscription open after the script has played, until its
	// context ends — a daemon with nothing more to say. Without it the stream
	// ends, which is what lets a test of a "follow" command finish.
	Hold bool
	// Lines is what each instance has printed.
	Lines map[string][]control.LogFrame
	// Fail makes Do return an error for a verb, keyed "name" or "name target".
	Fail map[string]error
	// OnDo, if set, runs after a verb is recorded: a test uses it to change
	// Snap the way the verb would.
	OnDo func(control.Verb)

	// AdminResult is what Admin answers with.
	AdminResult string

	// ReplayOnce plays the script to the first subscriber of each topic only, as a
	// daemon that already told it does not say it again to one that came back.
	ReplayOnce bool

	mu      sync.Mutex
	done    []control.Verb
	actions []Action
	live    []*liveSub
	refuse  bool
	played  map[string]bool
}

// ErrGone is what a refused or dropped call answers with.
var ErrGone = errors.New("daemon not reachable: connection refused")

// Refuse makes every call fail, as a daemon that has stopped does; Refuse(false)
// brings it back. A held subscription is not ended by it: Drop does that.
func (f *Fake) Refuse(on bool) {
	f.mu.Lock()
	f.refuse = on
	f.mu.Unlock()
}

// Drop ends every held subscription with an error, as a daemon that went away
// does, and refuses calls until Refuse(false).
func (f *Fake) Drop() {
	f.mu.Lock()
	f.refuse = true
	subs := append([]*liveSub(nil), f.live...)
	f.mu.Unlock()
	for _, s := range subs {
		select {
		case s.drop <- struct{}{}:
		default:
		}
	}
}

func (f *Fake) refused() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refuse
}

// liveSub is a subscriber that is held open and is sent what Push is given.
type liveSub struct {
	ch   chan control.Frame
	drop chan struct{}
	want map[string]bool
	sub  control.Subscription
}

// Push sends a frame to every subscriber that is held open and asked for its
// topic, as a daemon does when something happens after the script is over. A
// subscriber that cannot keep up misses it, which the dash must stand anyway.
func (f *Fake) Push(fr control.Frame) {
	f.mu.Lock()
	subs := append([]*liveSub(nil), f.live...)
	f.mu.Unlock()
	for _, s := range subs {
		if !s.want[fr.Topic] {
			continue
		}
		if fr.Log != nil && len(s.sub.Names) > 0 && !contains(s.sub.Names, fr.Log.Instance) {
			continue
		}
		select {
		case s.ch <- fr:
		default:
		}
	}
}

// PushLog records a line an instance printed, for those that ask for its logs
// later, and sends it to those following now.
func (f *Fake) PushLog(l control.LogFrame) {
	f.mu.Lock()
	if f.Lines == nil {
		f.Lines = map[string][]control.LogFrame{}
	}
	f.Lines[l.Instance] = append(f.Lines[l.Instance], l)
	f.mu.Unlock()
	f.Push(control.Frame{Topic: control.TopicLogs, Log: &l})
}

var _ control.Daemon = (*Fake)(nil)

// Snapshot returns Snap.
func (f *Fake) Snapshot(context.Context) (control.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse {
		return control.Snapshot{}, ErrGone
	}
	return f.Snap, nil
}

// Subscribe plays the script to on.
func (f *Fake) Subscribe(ctx context.Context, sub control.Subscription, on func(control.Frame) error) error {
	if f.refused() {
		return ErrGone
	}
	want := map[string]bool{}
	for _, t := range sub.Topics {
		want[t] = true
	}
	skip := map[string]bool{}
	if f.ReplayOnce {
		f.mu.Lock()
		if f.played == nil {
			f.played = map[string]bool{}
		}
		for t := range want {
			key := t + "|" + strings.Join(sub.Names, ",")
			skip[t] = f.played[key]
			f.played[key] = true
		}
		f.mu.Unlock()
	}
	for _, fr := range f.Frames {
		if !want[fr.Topic] || skip[fr.Topic] {
			continue
		}
		// A subscriber that named instances is sent the lines of those, as the
		// daemon does.
		if fr.Log != nil && len(sub.Names) > 0 && !contains(sub.Names, fr.Log.Instance) {
			continue
		}
		// A console follows the one instance named, and a stream row is sent only
		// to a subscriber that asked for that panel and, where the row says which
		// ("_for"), that row of its table.
		if c := fr.Console; c != nil {
			if len(sub.Names) > 0 && c.Instance != "" && !contains(sub.Names, c.Instance) {
				continue
			}
			if c.Row != nil {
				if sub.Panel != c.Panel {
					continue
				}
				if for_ := c.Row.Cells["_for"]; for_ != "" && sub.Selector != for_ {
					continue
				}
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		if err := on(fr); err != nil {
			return err
		}
	}
	if f.Hold {
		ls := &liveSub{ch: make(chan control.Frame, 256), drop: make(chan struct{}, 1), want: want, sub: sub}
		f.mu.Lock()
		f.live = append(f.live, ls)
		f.mu.Unlock()
		defer func() {
			f.mu.Lock()
			for i, x := range f.live {
				if x == ls {
					f.live = append(f.live[:i], f.live[i+1:]...)
					break
				}
			}
			f.mu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ls.drop:
				return ErrGone
			case fr := <-ls.ch:
				if err := on(fr); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Do records the verb and returns the error scripted for it, if any.
func (f *Fake) Do(_ context.Context, verb control.Verb) error {
	if f.refused() {
		return ErrGone
	}
	f.mu.Lock()
	f.done = append(f.done, verb)
	err := f.Fail[verb.Name+" "+verb.Target]
	if err == nil {
		err = f.Fail[verb.Name]
	}
	hook := f.OnDo
	f.mu.Unlock()
	if err == nil && hook != nil {
		hook(verb)
	}
	return err
}

// Logs returns the scripted lines of an instance, honouring Tail and Since.
// Run 0 is every line; any other run keeps the lines scripted with that run.
func (f *Fake) Logs(_ context.Context, q control.LogQuery) ([]control.LogFrame, error) {
	if f.refused() {
		return nil, ErrGone
	}
	all, ok := f.Lines[q.Instance]
	if !ok {
		return nil, fmt.Errorf("instance %q is not declared", q.Instance)
	}
	var out []control.LogFrame
	for _, l := range all {
		if q.Run > 0 && l.Run != q.Run {
			continue
		}
		if !q.Since.IsZero() && l.Time.Before(q.Since) {
			continue
		}
		out = append(out, l)
	}
	if q.Tail > 0 && len(out) > q.Tail {
		out = out[len(out)-q.Tail:]
	}
	return out, nil
}

// Admin records a data action and answers with what was scripted.
func (f *Fake) Admin(_ context.Context, instance, action, resource, input string) (string, error) {
	if f.refused() {
		return "", ErrGone
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, Action{Instance: instance, Action: action, Resource: resource, Input: input})
	if err := f.Fail["admin "+action]; err != nil {
		return "", err
	}
	return f.AdminResult, nil
}

// Action is one data action a test ran.
type Action struct{ Instance, Action, Resource, Input string }

// Actions returns the data actions run so far, in order.
func (f *Fake) Actions() []Action {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Action{}, f.actions...)
}

// Done returns the verbs run so far, in order.
func (f *Fake) Done() []control.Verb {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]control.Verb{}, f.done...)
}

// Mutate changes the snapshot the way a verb would. The instances are copied
// first, so a snapshot already handed out is not altered under its reader.
func (f *Fake) Mutate(fn func(*control.Snapshot)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Snap.Instances = append([]control.InstanceView(nil), f.Snap.Instances...)
	fn(&f.Snap)
}
