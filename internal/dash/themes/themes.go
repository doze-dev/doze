// Package themes is the theme picker. Moving the cursor previews a theme on
// the whole dash at once; enter keeps it, esc puts the old one back.
package themes

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/dash/core"
	"github.com/doze-dev/doze/internal/dash/theme"
)

// Model is the picker.
type Model struct {
	all      []theme.Theme
	cursor   int
	dark     bool
	plain    bool
	original theme.Choice
}

// New returns the picker, starting on the theme in use.
func New(current theme.Choice) *Model {
	m := &Model{all: theme.All(), dark: current.Dark, plain: current.Plain, original: current}
	for i, t := range m.all {
		if t.Name == current.Name {
			m.cursor = i
		}
	}
	return m
}

// Keys implements core.Page.
func (m *Model) Keys() []core.Key {
	return []core.Key{
		{Keys: "↑↓", Label: "preview"}, {Keys: "d", Label: "dark / light"}, {Keys: "p", Label: "no colour"},
		{Keys: "enter", Label: "keep"}, {Keys: "esc", Label: "cancel"},
	}
}

func (m *Model) preview(save bool) tea.Cmd {
	msg := core.SetTheme{Name: m.all[m.cursor].Name, Dark: m.dark, Plain: m.plain, Save: save}
	return func() tea.Msg { return msg }
}

// Update implements core.Page.
func (m *Model) Update(msg tea.Msg, c *core.Context) (core.Page, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, m.preview(false)
	case "down", "j":
		if m.cursor < len(m.all)-1 {
			m.cursor++
		}
		return m, m.preview(false)
	case "d":
		m.dark = !m.dark
		return m, m.preview(false)
	case "p":
		m.plain = !m.plain
		return m, m.preview(false)
	case "enter":
		return m, tea.Batch(m.preview(true), func() tea.Msg { return core.Back{} })
	case "esc", "q":
		o := m.original
		return m, tea.Batch(func() tea.Msg {
			return core.SetTheme{Name: o.Name, Dark: o.Dark, Plain: o.Plain}
		}, func() tea.Msg { return core.Back{} })
	}
	return m, nil
}

// View implements core.Page. It draws with the styles of the theme under the
// cursor — which, because the host has applied it, are the context's own.
func (m *Model) View(c *core.Context, w, h int) string {
	st := c.Styles
	variant := "dark"
	if !m.dark {
		variant = "light"
	}
	colour := ""
	if m.plain {
		colour = " · no colour"
	}
	var out []string
	out = append(out, st.Title.Render("Theme"), st.Dim.Render(variant+colour), "")
	for i, t := range m.all {
		mark := "  "
		if i == m.cursor {
			mark = st.Accent.Render("› ")
		}
		swatch := t.Dark
		if !m.dark {
			swatch = t.Light
		}
		_ = swatch
		out = append(out, mark+st.Text.Render(t.Name))
	}
	out = append(out, "", st.Dim.Render("how it looks"), "")
	out = append(out,
		"  "+st.Good.Render("● awake")+"   "+st.Warn.Render("○ dozing")+"   "+st.Cool.Render("◌ waking")+"   "+st.Bad.Render("✕ failed")+"   "+st.Dim.Render("· asleep"),
		"  "+st.Selected.Render(" db        postgres   awake    96M   held by api, worker "),
		"  "+st.Text.Render("api")+"  "+st.Dim.Render("app")+"  "+st.Accent.Render("api.shop.doze:8080")+"  "+st.Faint.Render("doze wake api"),
	)
	for len(out) < h {
		out = append(out, "")
	}
	return strings.Join(out[:h], "\n")
}
