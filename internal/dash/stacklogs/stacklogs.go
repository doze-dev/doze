// Package stacklogs is the page that merges every instance's output into one
// time-ordered stream, with search, per-source toggles and a level filter.
package stacklogs

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/core"
	"github.com/doze-dev/doze/internal/display"
)

const (
	id   = "stacklogs"
	keep = 5000
)

// levels the filter cycles through; each shows its own and everything worse.
var levels = []string{"all", "warn", "error"}

func rank(level string) int {
	switch level {
	case "error":
		return 3
	case "warn":
		return 2
	}
	return 1
}

// Model is the stack-wide logs page.
type Model struct {
	lines   []control.LogFrame
	muted   map[string]bool
	level   int // index into levels
	query   string
	editing bool
	follow  bool
	off     int // lines above the bottom when paused
	shown   bool
	w, h    int
	notice  string
}

// New returns the page.
func New() *Model { return &Model{muted: map[string]bool{}, follow: true} }

// Keys implements core.Page.
func (m *Model) Keys() []core.Key {
	if m.editing {
		return []core.Key{{Keys: "enter", Label: "search"}, {Keys: "esc", Label: "cancel"}}
	}
	return []core.Key{
		{Keys: "/", Label: "search"}, {Keys: "n N", Label: "next match"}, {Keys: "1-9", Label: "toggle source"},
		{Keys: "e", Label: "level"}, {Keys: "f", Label: "follow"}, {Keys: "esc", Label: "back"},
	}
}

// sources lists the instances the stack declares, in a stable order.
func sources(c *core.Context) []string {
	var out []string
	for _, v := range c.Snap.Instances {
		out = append(out, v.Name)
	}
	sort.Strings(out)
	return out
}

// Update implements core.Page.
func (m *Model) Update(msg tea.Msg, c *core.Context) (core.Page, tea.Cmd) {
	var cmds []tea.Cmd
	if !m.shown {
		m.shown = true
		cmds = append(cmds, func() tea.Msg { return core.Follow{ID: id, Names: nil, Backlog: 100} })
	}
	switch msg := msg.(type) {
	case core.Resized:
		m.w, m.h = msg.W, msg.H
	case core.LogLine:
		if msg.ID != id {
			break
		}
		m.insert(msg.Frame)
		if !m.follow {
			m.off++
		}
	case tea.KeyPressMsg:
		if m.editing {
			switch k := msg.String(); k {
			case "enter":
				m.editing = false
				m.jump(c, -1, true)
			case "esc":
				m.editing, m.query = false, ""
			case "backspace":
				if r := []rune(m.query); len(r) > 0 {
					m.query = string(r[:len(r)-1])
				}
			default:
				if msg.Text != "" {
					m.query += msg.Text
				}
			}
			return m, tea.Batch(cmds...)
		}
		switch k := msg.String(); k {
		case "esc", "backspace", "h", "left":
			cmds = append(cmds, func() tea.Msg { return core.Unfollow{ID: id} }, func() tea.Msg { return core.Back{} })
		case "/":
			m.editing, m.query = true, ""
		case "n":
			m.jump(c, 1, false)
		case "N":
			m.jump(c, -1, false)
		case "e":
			m.level = (m.level + 1) % len(levels)
		case "a":
			m.muted = map[string]bool{}
		case "f", "end", "G":
			m.follow, m.off = true, 0
		case "up", "k":
			m.follow = false
			m.off++
		case "down", "j":
			if m.off > 0 {
				m.off--
			}
			m.follow = m.off == 0
		case "pgup":
			m.follow = false
			m.off += 10
		case "pgdown":
			m.off -= 10
			if m.off <= 0 {
				m.off, m.follow = 0, true
			}
		default:
			if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
				if src := sources(c); int(k[0]-'1') < len(src) {
					name := src[k[0]-'1']
					m.muted[name] = !m.muted[name]
				}
			}
		}
	}
	return m, tea.Batch(cmds...)
}

// insert puts a line in time order: a followed stream is nearly sorted, so the
// scan from the end is short.
func (m *Model) insert(l control.LogFrame) {
	i := len(m.lines)
	for i > 0 && !l.Time.IsZero() && m.lines[i-1].Time.After(l.Time) {
		i--
	}
	m.lines = append(m.lines, control.LogFrame{})
	copy(m.lines[i+1:], m.lines[i:])
	m.lines[i] = l
	if len(m.lines) > keep {
		m.lines = m.lines[len(m.lines)-keep:]
	}
}

// visible is the lines that pass the source toggles and the level filter.
func (m *Model) visible() []control.LogFrame {
	var out []control.LogFrame
	min := rank(levels[m.level])
	for _, l := range m.lines {
		if m.muted[l.Instance] {
			continue
		}
		if min > 1 && rank(display.Level(l.Line)) < min {
			continue
		}
		out = append(out, l)
	}
	return out
}

func (m *Model) matches(l control.LogFrame) bool {
	return m.query != "" && strings.Contains(strings.ToLower(l.Line), strings.ToLower(m.query))
}

// jump scrolls to the next match in a direction: 1 toward newer lines, -1
// toward older. From the bottom it finds the newest match first. Past the last
// match it wraps, and says so, rather than stopping dead.
func (m *Model) jump(c *core.Context, by int, fromBottom bool) {
	vis := m.visible()
	if m.query == "" || len(vis) == 0 {
		return
	}
	cur := len(vis) - 1 - m.off // the newest line in view
	if fromBottom {
		cur = len(vis)
	}
	for i := cur + by; i >= 0 && i < len(vis); i += by {
		if m.matches(vis[i]) {
			m.follow, m.off, m.notice = false, len(vis)-1-i, ""
			return
		}
	}
	start := 0
	if by < 0 {
		start = len(vis) - 1
	}
	for i := start; i >= 0 && i < len(vis); i += by {
		if m.matches(vis[i]) {
			m.follow, m.off, m.notice = false, len(vis)-1-i, "wrapped"
			return
		}
	}
	m.notice = "no matches for " + m.query
}

// View implements core.Page.
func (m *Model) View(c *core.Context, w, h int) string {
	st := c.Styles
	m.w, m.h = w, h
	if w <= 0 || h <= 0 {
		return ""
	}
	vis := m.visible()
	matches := 0
	for _, l := range vis {
		if m.matches(l) {
			matches++
		}
	}

	// Top: the title, and the sources with their toggle numbers.
	title := st.Dim.Render("‹ ") + st.Title.Render("Stack logs")
	state := "live"
	if !m.follow {
		state = "paused"
		if m.off > 0 {
			state = fmt.Sprintf("paused, %d below", m.off)
		}
	}
	title += st.Dim.Render("  " + state + fmt.Sprintf(" · level %s · %d lines", levels[m.level], len(vis)))
	if m.query != "" {
		noun := "matches"
		if matches == 1 {
			noun = "match"
		}
		title += st.Dim.Render(fmt.Sprintf(" · %d %s for ", matches, noun)) + st.Selected.Render(m.query)
	}
	var src []string
	for i, name := range sources(c) {
		if i >= 9 {
			break
		}
		label := fmt.Sprintf("%d %s", i+1, name)
		if m.muted[name] {
			src = append(src, st.Faint.Render(label))
		} else {
			src = append(src, st.Pick(name).Render(label))
		}
	}

	bodyH := h - 5
	if bodyH < 1 {
		bodyH = 1
	}
	var out []string
	out = append(out, ansi.Truncate(title, w, "…"), ansi.Truncate(strings.Join(src, "  "), w, "…"), st.Faint.Render(strings.Repeat("─", w)))

	if len(vis) == 0 {
		out = append(out, st.Dim.Render("nothing to show"))
	} else {
		end := len(vis) - m.off
		if end < 0 {
			end = 0
		}
		start := end - bodyH
		if start < 0 {
			start = 0
		}
		for _, l := range vis[start:end] {
			out = append(out, m.line(c, l, w))
		}
	}
	for len(out) < h-2 {
		out = append(out, "")
	}
	out = out[:h-2]
	foot := m.footer(c, w)
	if m.editing {
		foot = st.Accent.Render("/") + st.Text.Render(m.query) + st.Accent.Render("▏")
	} else if m.notice != "" {
		foot = st.Bad.Render(m.notice)
	}
	out = append(out, st.Faint.Render(strings.Repeat("─", w)), ansi.Truncate(foot, w, "…"))
	return strings.Join(out, "\n")
}
func (m *Model) line(c *core.Context, l control.LogFrame, w int) string {
	st := c.Styles
	stamp := "        "
	if !l.Time.IsZero() {
		stamp = l.Time.In(c.Loc).Format("15:04:05")
	}
	name := l.Instance
	if len(name) > 10 {
		name = name[:10]
	}
	style := st.Text
	switch display.Level(l.Line) {
	case "error":
		style = st.Bad
	case "warn":
		style = st.Warn
	case "debug":
		style = st.Dim
	}
	text := ansi.Truncate(l.Line, w-22, "…")
	rendered := style.Render(text)
	if m.matches(l) {
		rendered = highlight(text, m.query, style, st.Selected)
	}
	return st.Dim.Render(stamp) + " " + st.Pick(l.Instance).Render(fmt.Sprintf("%-10s", name)) + " " + rendered
}

// highlight renders text with every occurrence of q in the highlight style.
func highlight(text, q string, base, hi lipgloss.Style) string {
	lower, lq := strings.ToLower(text), strings.ToLower(q)
	var b strings.Builder
	for {
		i := strings.Index(lower, lq)
		if i < 0 || lq == "" {
			b.WriteString(base.Render(text))
			return b.String()
		}
		b.WriteString(base.Render(text[:i]))
		b.WriteString(hi.Render(text[i : i+len(q)]))
		text, lower = text[i+len(q):], lower[i+len(q):]
	}
}

func (m *Model) footer(c *core.Context, w int) string {
	st := c.Styles
	var parts []string
	for _, k := range m.Keys() {
		parts = append(parts, st.Key.Render(k.Keys)+" "+st.KeyLabel.Render(k.Label))
	}
	return strings.Join(parts, st.Faint.Render("  ·  "))
}
