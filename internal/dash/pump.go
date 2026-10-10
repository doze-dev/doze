package dash

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/core"
)

// source follows the daemon's frames and hands them to the program one at a
// time. It reconnects when the stream ends, so a daemon that restarts does not
// leave the dash showing a stack that is no longer there.
type source struct {
	frames chan control.Frame
}

func newSource(ctx context.Context, d control.Daemon, _ *tea.Program) *source {
	s := &source{frames: make(chan control.Frame, 64)}
	go func() {
		for ctx.Err() == nil {
			_ = d.Subscribe(ctx, control.Subscription{Topics: []string{"instances", "stats"}, Interval: time.Second},
				func(f control.Frame) error {
					select {
					case s.frames <- f:
					case <-ctx.Done():
						return ctx.Err()
					}
					return nil
				})
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}()
	return s
}

// wait is the command that delivers the next frame.
func (s *source) wait() tea.Cmd {
	if s == nil {
		return nil
	}
	return func() tea.Msg { return frameMsg(<-s.frames) }
}

// follow starts the loop: the first wait.
func (m *Model) follow() tea.Cmd { return m.src.wait() }

// logMsg is a followed log line on its way to the page.
type logMsg core.LogLine

// eventMsg is a followed journal event on its way to the page.
type eventMsg core.EventLine

// consoleMsg is a followed console frame on its way to the page.
type consoleMsg core.ConsoleLine

// follow starts (or restarts) one log stream for a page. The lines arrive as
// messages, so the page that asked sees them on its own goroutine's terms.
func (m *Model) startFollow(f core.Follow) {
	m.unfollow(f.ID)
	if m.send == nil {
		return // no program to deliver to: a test feeds lines itself
	}
	ctx, cancel := context.WithCancel(context.Background())
	if m.follows == nil {
		m.follows = map[string]context.CancelFunc{}
	}
	m.follows[f.ID] = cancel
	d, send := m.ctx.Daemon, m.send
	go func() {
		topic := f.Topic
		if topic == "" {
			topic = "logs"
		}
		sub := control.Subscription{Topics: []string{topic}, Names: f.Names, Backlog: f.Backlog, Panel: f.Panel, Selector: f.Selector}
		if topic == "journal" {
			sub.Since = control.JournalFromStart
		}
		_ = d.Subscribe(ctx, sub, func(fr control.Frame) error {
			switch {
			case fr.Log != nil:
				send(logMsg{ID: f.ID, Frame: *fr.Log})
			case fr.Event != nil:
				send(eventMsg{ID: f.ID, Event: *fr.Event})
			case fr.Console != nil:
				send(consoleMsg{ID: f.ID, Frame: *fr.Console})
			}
			return nil
		})
	}()
}

func (m *Model) unfollow(id string) {
	if cancel := m.follows[id]; cancel != nil {
		cancel()
		delete(m.follows, id)
	}
}
