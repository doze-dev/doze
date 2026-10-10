// Package palette is the command palette: a filterable list of what the dash
// can do to the selected instance, each row showing the CLI line that does the
// same thing, so the dash teaches the command line as a side effect.
package palette

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/actions"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/confirm"
	"github.com/doze-dev/doze/internal/dash/core"
)

// entry is one row: an action aimed at a target.
type entry struct {
	action actions.Action
	target string
}

// cli is the command line that does what the row does.
func (e entry) cli() string {
	s := "doze " + e.action.Name
	if e.target != "" {
		s += " " + e.target
	}
	return s
}

// Model is the palette page.
type Model struct {
	target  string // the instance the dash was on; "" for none
	entries []entry
	query   string
	cursor  int
}

// New returns a palette for an instance (or "" when none is selected).
func New(target string) *Model {
	m := &Model{target: target}
	for _, a := range actions.Dash() {
		if a.Kind != actions.KindOp {
			continue
		}
		e := entry{action: a}
		switch a.Arg {
		case actions.ArgInstanceRequired:
			if target == "" {
				continue
			}
			e.target = target
		case actions.ArgInstanceOptional:
			e.target = target
		}
		m.entries = append(m.entries, e)
	}
	return m
}

// score is how well query matches s as a subsequence; 0 is no match, higher is
// better: consecutive runs and an early start count for more.
func score(query, s string) int {
	if query == "" {
		return 1
	}
	q, t := []rune(strings.ToLower(query)), []rune(strings.ToLower(s))
	qi, sc, run := 0, 0, 0
	for i, r := range t {
		if qi < len(q) && r == q[qi] {
			qi++
			run++
			sc += 1 + run*2
			if i == 0 {
				sc += 5
			}
		} else {
			run = 0
		}
	}
	if qi < len(q) {
		return 0
	}
	return sc
}

// matches returns the entries that match the query, best first, ties in
// registry order (which puts the most used first).
func (m *Model) matches() []entry {
	type scored struct {
		e  entry
		sc int
		i  int
	}
	var out []scored
	for i, e := range m.entries {
		sc := score(m.query, e.action.Name+" "+e.action.Summary)
		if sc > 0 {
			out = append(out, scored{e, sc, i})
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && (out[j].sc > out[j-1].sc || (out[j].sc == out[j-1].sc && out[j].i < out[j-1].i)); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	res := make([]entry, len(out))
	for i, s := range out {
		res[i] = s.e
	}
	return res
}

// Keys implements core.Page.
func (m *Model) Keys() []core.Key {
	return []core.Key{{Keys: "↑↓", Label: "choose"}, {Keys: "enter", Label: "run"}, {Keys: "esc", Label: "close"}}
}

// Update implements core.Page.
func (m *Model) Update(msg tea.Msg, c *core.Context) (core.Page, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	ms := m.matches()
	switch s := key.String(); s {
	case "esc", "ctrl+c":
		return m, func() tea.Msg { return core.Back{} }
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "ctrl+n":
		if m.cursor < len(ms)-1 {
			m.cursor++
		}
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.cursor = 0
		}
	case "enter":
		if m.cursor < len(ms) {
			return m, m.run(c, ms[m.cursor])
		}
	default:
		if key.Text != "" {
			m.query += key.Text
			m.cursor = 0
		}
	}
	return m, nil
}

// run does an entry: through the same confirmation the keys use.
func (m *Model) run(c *core.Context, e entry) tea.Cmd {
	back := func() tea.Msg { return core.Back{} }
	verb := control.Verb{Name: e.action.Op, Target: e.target}
	if e.target != "" {
		for _, v := range c.Snap.Instances {
			if v.Name != e.target {
				continue
			}
			name := e.action.Name
			if e.action.Op == "restart" {
				name = control.VerbRestart
			}
			if spec := core.Confirmation(v, name); spec != nil {
				return tea.Batch(back, func() tea.Msg { return core.Navigate{To: confirm.New(confirm.Options(*spec))} })
			}
		}
	}
	d := c.Daemon
	return tea.Batch(back, func() tea.Msg {
		err := d.Do(context.Background(), verb)
		return core.Done{Verb: verb.Name, Target: verb.Target, Err: err}
	})
}

// View implements core.Page.
func (m *Model) View(c *core.Context, w, h int) string {
	st := c.Styles
	var out []string
	title := st.Title.Render("Palette")
	if m.target != "" {
		title += st.Dim.Render("  on ") + st.Text.Render(m.target)
	}
	out = append(out, title, st.Accent.Render("› ")+st.Text.Render(m.query)+st.Accent.Render("▏"), st.Faint.Render(strings.Repeat("─", w)))
	ms := m.matches()
	if len(ms) == 0 {
		out = append(out, st.Dim.Render("nothing matches"))
	}
	for i, e := range ms {
		name := e.action.Name
		if e.target != "" {
			name += " " + e.target
		}
		line := st.Text.Render(pad(name, 22)) + st.Dim.Render(pad(e.action.Summary, w-22-26)) + "  " + st.Faint.Render(e.cli())
		line = ansi.Truncate(line, w, "…")
		if i == m.cursor {
			line = st.Selected.Render(pad(ansi.Strip(line), w))
		}
		out = append(out, line)
	}
	for len(out) < h {
		out = append(out, "")
	}
	return strings.Join(out[:h], "\n")
}

func pad(s string, w int) string {
	if w < 0 {
		w = 0
	}
	if n := w - ansi.StringWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return ansi.Truncate(s, w, "…")
}
