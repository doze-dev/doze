// Package dash is the dash: the terminal screen doze opens on. The root model
// owns what no page does — the stack as the daemon last said it, the clock,
// the theme, the size — and hosts one page at a time.
//
// It depends on control.Daemon and nothing under it: the same screens run
// against a live daemon and against fakedaemon, which is how they are tested.
package dash

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/core"
	"github.com/doze-dev/doze/internal/dash/home"
	"github.com/doze-dev/doze/internal/dash/theme"
)

// Options configure a dash.
type Options struct {
	// Theme is the theme name; empty is the default.
	Theme string
	// Dark says the terminal background is dark. Tests fix it.
	Dark bool
	// Plain turns colour off.
	Plain bool
	// Home is the doze home, where the chosen theme is remembered. Empty
	// remembers nothing, which is what tests want.
	Home string
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Loc is the zone to show times in; nil is the local zone.
	Loc *time.Location
}

// Model is the root model.
type Model struct {
	ctx     core.Context
	page    core.Page
	stack   []core.Page // the pages Navigate came from
	send    func(tea.Msg)
	choice  theme.Choice
	home    string // where the theme is remembered; "" does not
	follows map[string]context.CancelFunc
	w, h    int
	now     func() time.Time
	src     *source
	quit    bool
}

// New builds the dash around a daemon. It does not start listening: Init does.
func New(d control.Daemon, o Options) *Model {
	choice := theme.Choice{Name: o.Theme, Dark: o.Dark, Plain: o.Plain}
	if o.Home != "" {
		if saved, ok := theme.Load(o.Home); ok && o.Theme == "" {
			choice = saved
			choice.Plain = choice.Plain || o.Plain
		}
	}
	th, ok := theme.ByName(choice.Name)
	if !ok {
		th = theme.Default()
		choice.Name = th.Name
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	m := &Model{now: now, page: home.New()}
	loc := o.Loc
	if loc == nil {
		loc = time.Local
	}
	m.ctx = core.Context{Daemon: d, Now: now(), Styles: th.Styles(choice.Dark, choice.Plain), Loc: loc}
	m.ctx.Dark = choice.Dark
	m.choice, m.home = choice, o.Home
	return m
}

// Init implements tea.Model: take a snapshot, then follow the daemon.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.snapshot(), m.follow(), tick())
}

type (
	snapshotMsg struct {
		snap control.Snapshot
		err  error
	}
	frameMsg control.Frame
	tickMsg  time.Time
)

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) snapshot() tea.Cmd {
	d := m.ctx.Daemon
	return func() tea.Msg {
		s, err := d.Snapshot(context.Background())
		return snapshotMsg{snap: s, err: err}
	}
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m.toPage(core.Resized{W: m.w, H: m.h})
	case snapshotMsg:
		if msg.err == nil {
			m.ctx.Snap = msg.snap
		}
		m.ctx.Now = m.now()
		return m.toPage(core.Refreshed{})
	case frameMsg:
		m.apply(control.Frame(msg))
		m.ctx.Now = m.now()
		next, cmd := m.toPage(core.Refreshed{})
		return next, tea.Batch(cmd, m.src.wait())
	case tickMsg:
		m.ctx.Now = m.now()
		next, cmd := m.toPage(core.Refreshed{})
		return next, tea.Batch(cmd, tick())
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quit = true
			return m, tea.Quit
		}
	case core.Done:
		cmds = append(cmds, m.snapshot())
	case core.Navigate:
		m.stack = append(m.stack, m.page)
		m.page = msg.To
		return m.toPage(core.Resized{W: m.w, H: m.h})
	case core.Back:
		if n := len(m.stack); n > 0 {
			m.page, m.stack = m.stack[n-1], m.stack[:n-1]
		}
		return m.toPage(core.Resized{W: m.w, H: m.h})
	case core.Follow:
		m.startFollow(msg)
		return m, nil
	case core.Unfollow:
		m.unfollow(msg.ID)
		return m, nil
	case core.SetTheme:
		m.setTheme(theme.Choice{Name: msg.Name, Dark: msg.Dark, Plain: msg.Plain}, msg.Save)
		return m, nil
	case logMsg:
		return m.toPage(core.LogLine(msg))
	case eventMsg:
		return m.toPage(core.EventLine(msg))
	}
	next, cmd := m.toPage(msg)
	return next, tea.Batch(append(cmds, cmd)...)
}

func (m *Model) toPage(msg tea.Msg) (tea.Model, tea.Cmd) {
	page, cmd := m.page.Update(msg, &m.ctx)
	m.page = page
	return m, cmd
}

// setTheme rebuilds the styles; the choice is kept when save is set.
func (m *Model) setTheme(c theme.Choice, save bool) {
	th, ok := theme.ByName(c.Name)
	if !ok {
		return
	}
	m.choice = c
	m.ctx.Dark = c.Dark
	m.ctx.Styles = th.Styles(c.Dark, c.Plain)
	if save && m.home != "" {
		theme.Save(m.home, c)
	}
}

// apply folds an instances frame into the snapshot.
func (m *Model) apply(f control.Frame) {
	if f.Topic != "instances" && f.Topic != "stats" {
		return
	}
	if f.Topic == "instances" {
		if f.Snapshot {
			m.ctx.Snap.Instances = append([]control.InstanceView(nil), f.Instances...)
		} else {
			for _, v := range f.Instances {
				replaced := false
				for i := range m.ctx.Snap.Instances {
					if m.ctx.Snap.Instances[i].Name == v.Name {
						m.ctx.Snap.Instances[i] = v
						replaced = true
					}
				}
				if !replaced {
					m.ctx.Snap.Instances = append(m.ctx.Snap.Instances, v)
				}
			}
		}
		for _, name := range f.Removed {
			out := m.ctx.Snap.Instances[:0]
			for _, v := range m.ctx.Snap.Instances {
				if v.Name != name {
					out = append(out, v)
				}
			}
			m.ctx.Snap.Instances = out
		}
		return
	}
	for _, s := range f.Stats {
		for i := range m.ctx.Snap.Instances {
			if m.ctx.Snap.Instances[i].Name == s.Name {
				m.ctx.Snap.Instances[i].RAM, m.ctx.Snap.Instances[i].CPU = s.RAM, s.CPU
				m.ctx.Snap.Instances[i].Conns = s.Conns
			}
		}
	}
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	v := tea.NewView(m.Render(m.w, m.h))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// Render draws the current page at a size. It is what View draws and what the
// golden tests compare.
func (m *Model) Render(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	return m.page.View(&m.ctx, w, h)
}

// Run opens the dash on a terminal and returns when it is closed.
func Run(ctx context.Context, d control.Daemon, o Options) error {
	m := New(d, o)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	m.src = newSource(ctx, d, p)
	m.send = p.Send
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("the dash: %w", err)
	}
	return nil
}

// Dark guesses whether the terminal has a dark background. It is only a
// default: the theme picker can say otherwise.
func Dark() bool { return os.Getenv("COLORFGBG") == "" || os.Getenv("COLORFGBG") != "0;15" }
