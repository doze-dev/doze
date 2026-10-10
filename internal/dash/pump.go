package dash

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/control"
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
