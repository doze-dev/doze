// Package frame is the page around one instance: its name and state on top, a
// tab bar, the tab, and the keys every instance shares.
package frame

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/confirm"
	"github.com/doze-dev/doze/internal/dash/core"
	"github.com/doze-dev/doze/internal/dash/layout"
	"github.com/doze-dev/doze/internal/dash/tab/console"
	"github.com/doze-dev/doze/internal/dash/tab/lastrun"
	"github.com/doze-dev/doze/internal/dash/tab/logs"
	"github.com/doze-dev/doze/internal/dash/tab/overview"
	"github.com/doze-dev/doze/internal/display"
)

// Model is the frame of one instance.
type Model struct {
	name      string
	tabs      []core.Tab
	active    int
	shown     bool
	lay       layout.Map
	notice    string
	noticeBad bool
}

// New returns the frame for an instance. The tabs are the ones its kind has.
// The tab a kind opens on is the one that answers what it is for: an app
// is for its output, a module for its address and why it is awake, a task
// for its last run.
func New(name string, inst control.InstanceView) *Model {
	var tabs []core.Tab
	switch inst.Kind {
	case "app", "service":
		tabs = []core.Tab{logs.New(name), overview.New(name)}
	case "task":
		tabs = []core.Tab{lastrun.New(name), logs.New(name)}
	default:
		// A module that describes a console opens on it; one that does not
		// opens on the overview the host draws for everything.
		if inst.HasConsole {
			tabs = []core.Tab{console.New(name), overview.New(name), logs.New(name)}
		} else {
			tabs = []core.Tab{overview.New(name), logs.New(name)}
		}
	}
	return &Model{name: name, tabs: tabs}
}

// inst is the instance as the stack last reported it.
func (m *Model) inst(c *core.Context) (control.InstanceView, bool) {
	for _, v := range c.Snap.Instances {
		if v.Name == m.name {
			return v, true
		}
	}
	return control.InstanceView{}, false
}

// Keys implements core.Page.
func (m *Model) Keys() []core.Key {
	keys := []core.Key{{Keys: "esc", Label: "back"}}
	if len(m.tabs) > 1 {
		keys = append(keys, core.Key{Keys: "tab", Label: "next tab"})
	}
	keys = append(keys, m.tabs[m.active].Keys()...)
	keys = append(keys,
		core.Key{Keys: "w", Label: "wake"}, core.Key{Keys: "s", Label: "sleep"}, core.Key{Keys: "r", Label: "restart"}, core.Key{Keys: "R", Label: "reset"})
	return keys
}

// Update implements core.Page.
func (m *Model) Update(msg tea.Msg, c *core.Context) (core.Page, tea.Cmd) {
	inst, _ := m.inst(c)
	var cmds []tea.Cmd
	if !m.shown {
		m.shown = true
		cmds = append(cmds, m.tabs[m.active].Show(c, inst))
	}
	switch msg := msg.(type) {
	case core.Notice:
		m.notice = msg.Text
		m.noticeBad = msg.Bad
	case core.Done:
		if msg.Err != nil {
			m.notice = fmt.Sprintf("%s %s: %v", msg.Verb, msg.Target, msg.Err)
		}
	case tea.MouseClickMsg:
		if id, ok := m.lay.At(msg.X, msg.Y); ok {
			if i := tabIndex(id); i >= 0 && i < len(m.tabs) && i != m.active {
				cmds = append(cmds, m.switchTo(c, inst, i))
				return m, tea.Batch(cmds...)
			}
		}
	case tea.KeyPressMsg:
		m.notice, m.noticeBad = "", false
		if cp, ok := m.tabs[m.active].(core.Capturer); ok && cp.Capturing() {
			t, cmd := m.tabs[m.active].Update(msg, c, inst)
			m.tabs[m.active] = t
			return m, tea.Batch(append(cmds, cmd)...)
		}
		switch msg.String() {
		case "esc", "backspace", "h", "left":
			cmds = append(cmds, m.tabs[m.active].Hide(), func() tea.Msg { return core.Back{} })
			return m, tea.Batch(cmds...)
		case "tab":
			if len(m.tabs) > 1 {
				cmds = append(cmds, m.switchTo(c, inst, (m.active+1)%len(m.tabs)))
				return m, tea.Batch(cmds...)
			}
		case "w":
			cmds = append(cmds, m.verb(c, control.VerbWake))
		case "s":
			cmds = append(cmds, m.confirmed(c, inst, control.VerbSleep))
		case "R":
			cmds = append(cmds, m.confirmed(c, inst, control.VerbReset))
		case "r":
			cmds = append(cmds, m.verb(c, control.VerbRestart))
		default:
			t, cmd := m.tabs[m.active].Update(msg, c, inst)
			m.tabs[m.active] = t
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	t, cmd := m.tabs[m.active].Update(msg, c, inst)
	m.tabs[m.active] = t
	return m, tea.Batch(append(cmds, cmd)...)
}

func tabIndex(id string) int {
	var i int
	if _, err := fmt.Sscanf(id, "tab:%d", &i); err != nil {
		return -1
	}
	return i
}

func (m *Model) switchTo(c *core.Context, inst control.InstanceView, i int) tea.Cmd {
	hide := m.tabs[m.active].Hide()
	m.active = i
	return tea.Batch(hide, m.tabs[i].Show(c, inst))
}

func (m *Model) confirmed(c *core.Context, inst control.InstanceView, name string) tea.Cmd {
	if spec := core.Confirmation(inst, name); spec != nil {
		return func() tea.Msg { return core.Navigate{To: confirm.New(confirm.Options(*spec))} }
	}
	return m.verb(c, name)
}

func (m *Model) verb(c *core.Context, name string) tea.Cmd {
	d, target := c.Daemon, m.name
	return func() tea.Msg {
		err := d.Do(context.Background(), control.Verb{Name: name, Target: target})
		return core.Done{Verb: name, Target: target, Err: err}
	}
}

// View implements core.Page.
func (m *Model) View(c *core.Context, w, h int) string {
	st := c.Styles
	m.lay = layout.Map{}
	if w <= 0 || h <= 0 {
		return ""
	}
	inst, ok := m.inst(c)
	word, gl, style := core.State(inst, st)
	title := st.Dim.Render("‹ ") + st.Title.Render(m.name)
	if ok {
		kind := inst.Engine
		if inst.Kind != "" && inst.Kind != "module" {
			kind = inst.Kind
		}
		title += st.Dim.Render("  "+kind+"  ") + style.Render(gl+" "+word)
		if inst.Endpoint != "" {
			title += st.Dim.Render("  " + inst.Endpoint)
		}
		if inst.Domain != "" {
			title += st.Dim.Render(" · ") + st.Text.Render(inst.Domain)
		}
	} else {
		title += st.Bad.Render("  no longer declared")
	}

	var tabs []string
	x := 0
	for i, t := range m.tabs {
		label := " " + t.Title() + " "
		if i == m.active {
			tabs = append(tabs, st.Selected.Render(label))
		} else {
			tabs = append(tabs, st.Dim.Render(label))
		}
		m.lay.Add(fmt.Sprintf("tab:%d", i), layout.Rect{X: x, Y: 1, W: ansi.StringWidth(label), H: 1})
		x += ansi.StringWidth(label) + 1
	}
	note := ""
	if ok {
		text, bad := display.Note(inst, c.Snap.IdleTimeout, c.Now)
		switch {
		case m.notice != "":
			if m.noticeBad {
				note = st.Bad.Render(m.notice)
			} else {
				note = st.Good.Render(m.notice)
			}
		case bad:
			note = st.Bad.Render(text)
		case text != "":
			note = st.Dim.Render(text)
		}
	}

	bodyH := h - 5
	if bodyH < 1 {
		bodyH = 1
	}
	var out []string
	out = append(out, ansi.Truncate(title, w, "…"))
	out = append(out, ansi.Truncate(strings.Join(tabs, " ")+"   "+note, w, "…"))
	out = append(out, st.Faint.Render(strings.Repeat("─", w)))
	out = append(out, strings.Split(m.tabs[m.active].View(c, inst, w, bodyH), "\n")...)
	for len(out) < h-2 {
		out = append(out, "")
	}
	out = out[:h-2]
	out = append(out, st.Faint.Render(strings.Repeat("─", w)), m.footer(c, w))
	return strings.Join(out, "\n")
}

func (m *Model) footer(c *core.Context, w int) string {
	st := c.Styles
	var parts []string
	for _, k := range m.Keys() {
		parts = append(parts, st.Key.Render(k.Keys)+" "+st.KeyLabel.Render(k.Label))
	}
	return ansi.Truncate(strings.Join(parts, st.Faint.Render("  ·  ")), w, "…")
}
