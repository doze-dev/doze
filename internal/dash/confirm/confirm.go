// Package confirm is the page that stops a destructive or disruptive verb for
// a moment: it says who is affected and what happens next, and for a verb that
// deletes data it asks to be given the name of what will be deleted.
package confirm

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/core"
)

// Options say what is being confirmed.
type Options struct {
	Title string   // "Sleep db?"
	Body  []string // who is affected, what happens next
	Verb  control.Verb
	// Accept is the label of the action: "sleep db and api".
	Accept string
	// TypeName, when set, must be typed in full before the verb runs.
	TypeName string
	// Danger paints the page as a loss, not a pause.
	Danger bool
}

// Model is the confirmation page.
type Model struct {
	o     Options
	typed string
}

// New returns a confirmation.
func New(o Options) *Model { return &Model{o: o} }

// Keys implements core.Page.
func (m *Model) Keys() []core.Key {
	if m.o.TypeName != "" {
		return []core.Key{{Keys: "enter", Label: "confirm when the name matches"}, {Keys: "esc", Label: "cancel"}}
	}
	return []core.Key{{Keys: "y", Label: m.o.Accept}, {Keys: "n / esc", Label: "cancel"}}
}

// ready reports whether the confirmation can be accepted now.
func (m *Model) ready() bool { return m.o.TypeName == "" || m.typed == m.o.TypeName }

// Update implements core.Page.
func (m *Model) Update(msg tea.Msg, c *core.Context) (core.Page, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	back := func() tea.Msg { return core.Back{} }
	accept := func() tea.Cmd {
		d, v := c.Daemon, m.o.Verb
		return tea.Batch(back, func() tea.Msg {
			err := d.Do(context.Background(), v)
			return core.Done{Verb: v.Name, Target: v.Target, Err: err}
		})
	}
	switch s := key.String(); s {
	case "esc", "ctrl+c":
		return m, back
	case "enter":
		if m.o.TypeName != "" && m.ready() {
			return m, accept()
		}
	case "backspace":
		if r := []rune(m.typed); len(r) > 0 {
			m.typed = string(r[:len(r)-1])
		}
	case "y", "Y":
		if m.o.TypeName == "" {
			return m, accept()
		}
		fallthrough
	default:
		if m.o.TypeName != "" && key.Text != "" {
			m.typed += key.Text
		} else if s == "n" || s == "N" {
			return m, back
		}
	}
	return m, nil
}

// View implements core.Page.
func (m *Model) View(c *core.Context, w, h int) string {
	st := c.Styles
	title := st.Warn
	if m.o.Danger {
		title = st.Bad
	}
	var out []string
	out = append(out, title.Bold(true).Render(m.o.Title), "")
	for _, l := range m.o.Body {
		out = append(out, st.Text.Render(l))
	}
	out = append(out, "")
	if m.o.TypeName != "" {
		prompt := st.Dim.Render("type ") + st.Text.Render(m.o.TypeName) + st.Dim.Render(" to confirm: ")
		field := st.Text.Render(m.typed) + st.Accent.Render("▏")
		out = append(out, prompt+field)
		if m.typed != "" && !strings.HasPrefix(m.o.TypeName, m.typed) {
			out = append(out, st.Bad.Render("that is not the name"))
		}
	} else {
		out = append(out, st.Key.Render("y")+" "+st.KeyLabel.Render(m.o.Accept)+st.Faint.Render("   ·   ")+
			st.Key.Render("n")+" "+st.KeyLabel.Render("cancel"))
	}
	for i := range out {
		out[i] = "  " + ansi.Truncate(out[i], w-4, "…")
	}
	for len(out) < h {
		out = append(out, "")
	}
	return fmt.Sprint(strings.Join(out[:h], "\n"))
}
