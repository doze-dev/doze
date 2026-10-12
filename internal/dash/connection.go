package dash

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/control"
)

// What the dash knows about its daemon, and says. A screen that goes on showing
// the last picture of a daemon that has gone is the worst thing a dashboard can
// do: it looks healthy and is not. So the connection is a state of the model,
// the subscriptions are kept up and re-made, and every page says when what it
// shows is old.

type connState int

const (
	connLive connState = iota // the zero value: a model that was never given a daemon to watch
	connConnecting
	connDown
)

// staleAfter is how long without a frame before the page says its picture is
// old. The daemon sends the meters every second, so three is already late.
const staleAfter = 3 * time.Second

type connMsg struct {
	up    bool
	err   error
	retry time.Duration
}

// keepSubscribed holds a subscription open for as long as ctx is: when it ends,
// for any reason, it says so, waits, and asks again, with a wait that doubles to
// five seconds. onFrame sees each frame; first reports that the stream is up.
// since, if set, is asked again of the caller on each attempt (the journal
// resumes where it was).
func keepSubscribed(ctx context.Context, d control.Daemon, sub func() control.Subscription, send func(tea.Msg), frame func(control.Frame), report bool) {
	wait := 500 * time.Millisecond
	for ctx.Err() == nil {
		first := true
		err := d.Subscribe(ctx, sub(), func(f control.Frame) error {
			if first {
				first, wait = false, 500*time.Millisecond
				if report {
					send(connMsg{up: true})
				}
			}
			frame(f)
			return nil
		})
		if ctx.Err() != nil {
			return
		}
		if report {
			send(connMsg{up: false, err: err, retry: wait})
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

// health says whether what the page shows can be believed, and if not, what to
// put at the top right of it in place of the resident total.
func (m *Model) health() (ok bool, banner string) {
	switch {
	case m.conn == connConnecting:
		return false, "connecting to the daemon…"
	case m.conn == connDown:
		out := "daemon not responding"
		if left := m.retryAt.Sub(m.now).Round(time.Second); left > 0 {
			out += fmt.Sprintf(" · retrying in %ds", int(left.Seconds()))
		} else {
			out += " · retrying"
		}
		return false, out
	case !m.lastFrame.IsZero() && m.now.Sub(m.lastFrame) > staleAfter:
		return false, fmt.Sprintf("last update %ds ago", int(m.now.Sub(m.lastFrame).Seconds()))
	}
	return true, ""
}

// refuse is what a verb does while the daemon cannot be reached: it says so,
// where dialling would hang for a minute and then say something worse.
func (m *Model) refuse() tea.Cmd {
	if m.conn == connLive {
		return nil
	}
	return func() tea.Msg { return failMsg("the daemon is not responding: nothing was sent") }
}

// humanise makes a connection error one short sentence: what happened first,
// the path it happened on last.
func humanise(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "connection refused"), strings.Contains(s, "no such file"), strings.Contains(s, "not reachable"):
		return "the daemon is not reachable"
	case strings.Contains(s, "EOF"), strings.Contains(s, "broken pipe"), strings.Contains(s, "connection reset"):
		return "the daemon closed the connection"
	}
	return s
}

// seenLine says whether an instance's kept lines already hold this one, which is
// what a reconnect's backlog is. Lines are in time order, so only those as new as
// it need be looked at.
func (m *Model) seenLine(name string, f control.LogFrame) bool {
	c := m.cache[name]
	for i := len(c) - 1; i >= 0 && !c[i].Time.Before(f.Time); i-- {
		if c[i].Time.Equal(f.Time) && c[i].Line == f.Line && c[i].Run == f.Run {
			return true
		}
	}
	return false
}

// noteNew asks for the logs again when an instance appears that the dash had
// not seen. The first set is the baseline: the subscription was opened for it.
func (m *Model) noteNew() {
	fresh := false
	baseline := m.known == nil
	if baseline {
		m.known = map[string]bool{}
	}
	for _, v := range m.snap.Instances {
		if !m.known[v.Name] {
			m.known[v.Name] = true
			fresh = fresh || !baseline
		}
	}
	if fresh && m.resubLogs != nil {
		m.resubLogs()
	}
}
