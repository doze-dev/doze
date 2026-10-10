// Package logs is the logs tab: the lines an instance printed, timestamped and
// levelled, following live, with earlier runs a key away.
package logs

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/core"
)

// keep is how many lines the tab holds. A chatty service would otherwise grow
// the dash without bound.
const keep = 2000

// Tab is the logs tab of one instance.
type Tab struct {
	name   string
	lines  []control.LogFrame
	follow bool // pinned to the newest line
	off    int  // lines above the bottom when not following
	run    int  // 0: the live run; -1, -2: earlier ones
	err    string
	id     string
}

// New returns the logs tab for an instance.
func New(name string) *Tab { return &Tab{name: name, follow: true, id: "logs:" + name} }

// Title implements core.Tab.
func (t *Tab) Title() string { return "Logs" }

// Show implements core.Tab: follow the instance.
func (t *Tab) Show(*core.Context, control.InstanceView) tea.Cmd {
	return func() tea.Msg { return core.Follow{ID: t.id, Names: []string{t.name}, Backlog: 200} }
}

// Hide implements core.Tab.
func (t *Tab) Hide() tea.Cmd {
	return func() tea.Msg { return core.Unfollow{ID: t.id} }
}

// Keys implements core.Tab.
func (t *Tab) Keys() []core.Key {
	keys := []core.Key{{Keys: "↑↓", Label: "scroll"}, {Keys: "f", Label: "follow"}}
	keys = append(keys, core.Key{Keys: "[ ]", Label: "runs"})
	return keys
}

// Update implements core.Tab.
func (t *Tab) Update(msg tea.Msg, c *core.Context, _ control.InstanceView) (core.Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case core.LogLine:
		if msg.ID != t.id || t.run != 0 {
			return t, nil
		}
		t.lines = append(t.lines, msg.Frame)
		if len(t.lines) > keep {
			t.lines = t.lines[len(t.lines)-keep:]
		}
		if !t.follow {
			t.off++ // keep what is on screen where it is
		}
	case core.Logs:
		if msg.Instance != t.name {
			return t, nil
		}
		if msg.Err != nil {
			t.err = msg.Err.Error()
			return t, nil
		}
		t.err, t.lines = "", msg.Lines
		t.follow, t.off = t.run == 0, 0
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k":
			t.follow = false
			t.off++
		case "down", "j":
			if t.off > 0 {
				t.off--
			}
			if t.off == 0 {
				t.follow = t.run == 0
			}
		case "pgup":
			t.follow = false
			t.off += 10
		case "pgdown":
			t.off -= 10
			if t.off <= 0 {
				t.off, t.follow = 0, t.run == 0
			}
		case "f", "end", "G":
			t.off, t.follow = 0, t.run == 0
		case "[":
			t.run--
			return t, t.fetch(c)
		case "]":
			if t.run < 0 {
				t.run++
			}
			return t, t.fetch(c)
		}
	}
	return t, nil
}

// fetch reads one earlier run, or goes back to the live one.
func (t *Tab) fetch(c *core.Context) tea.Cmd {
	d, name, run := c.Daemon, t.name, t.run
	return func() tea.Msg {
		lines, err := d.Logs(context.Background(), control.LogQuery{Instance: name, Run: run, Tail: keep})
		return core.Logs{Instance: name, Lines: lines, Err: err}
	}
}

// level reads a line's severity from what the program wrote: a level word near
// the front, or level=… as logfmt has it.
func level(line string) string {
	head := strings.ToLower(line)
	if len(head) > 60 {
		head = head[:60]
	}
	for _, w := range []struct{ word, level string }{
		{"fatal", "error"}, {"panic", "error"}, {"error", "error"}, {"err", "error"},
		{"warning", "warn"}, {"warn", "warn"}, {"info", "info"}, {"debug", "debug"}, {"trace", "debug"},
	} {
		for _, pat := range []string{"[" + w.word + "]", "level=" + w.word, " " + w.word + " ", "|" + w.word + "|"} {
			if strings.Contains(head, pat) {
				return w.level
			}
		}
		if strings.HasPrefix(head, w.word+" ") || strings.HasPrefix(head, w.word+":") {
			return w.level
		}
	}
	return ""
}

// Counts returns how many lines are errors and warnings.
func (t *Tab) Counts() (errors, warnings int) {
	for _, l := range t.lines {
		switch level(l.Line) {
		case "error":
			errors++
		case "warn":
			warnings++
		}
	}
	return
}

// View implements core.Tab.
func (t *Tab) View(c *core.Context, inst control.InstanceView, w, h int) string {
	st := c.Styles
	if h <= 0 {
		return ""
	}
	// The status line: which run, how many problems, whether it is live.
	errs, warns := t.Counts()
	status := "live"
	switch {
	case t.run != 0:
		status = fmt.Sprintf("%d run%s ago", -t.run, plural(-t.run))
	case !t.follow:
		status = fmt.Sprintf("paused, %d line%s below", t.off, plural(t.off))
	}
	bar := st.Dim.Render(status)
	if errs > 0 {
		bar += st.Dim.Render(" · ") + st.Bad.Render(fmt.Sprintf("%d error%s", errs, plural(errs)))
	}
	if warns > 0 {
		bar += st.Dim.Render(" · ") + st.Warn.Render(fmt.Sprintf("%d warning%s", warns, plural(warns)))
	}
	body := h - 1
	var out []string
	out = append(out, ansi.Truncate(bar, w, "…"))

	switch {
	case t.err != "":
		out = append(out, st.Bad.Render(t.err))
	case len(t.lines) == 0:
		msg := "nothing printed yet"
		if inst.State == "reaped" || inst.State == "" {
			msg = "asleep — it prints nothing while it sleeps; its last run is above `[`"
		}
		out = append(out, st.Dim.Render(msg))
	default:
		end := len(t.lines) - t.off
		if end < 0 {
			end = 0
		}
		start := end - body
		if start < 0 {
			start = 0
		}
		lastRun := -1
		for i := start; i < end; i++ {
			l := t.lines[i]
			if lastRun != -1 && l.Run != lastRun && l.Run != 0 {
				out = append(out, st.Faint.Render(fmt.Sprintf("── run %d ──", l.Run)))
			}
			lastRun = l.Run
			out = append(out, t.line(c, l, w))
		}
	}
	for len(out) < h {
		out = append(out, "")
	}
	return strings.Join(out[:h], "\n")
}

// line draws one log line: time, source mark, text coloured by level.
func (t *Tab) line(c *core.Context, l control.LogFrame, w int) string {
	st := c.Styles
	stamp := "        "
	if !l.Time.IsZero() {
		stamp = l.Time.In(c.Loc).Format("15:04:05")
	}
	mark := " "
	switch {
	case strings.HasPrefix(l.Stream, "s:"):
		mark = "▸"
	case strings.HasPrefix(l.Stream, "e"):
		mark = "!"
	}
	var style lipgloss.Style = st.Text
	switch level(l.Line) {
	case "error":
		style = st.Bad
	case "warn":
		style = st.Warn
	case "debug":
		style = st.Dim
	}
	text := l.Line
	if strings.HasPrefix(l.Stream, "s:") {
		text = strings.TrimPrefix(l.Stream, "s:") + ": " + text
		style = st.Cool
	}
	room := w - 11
	if room < 1 {
		room = 1
	}
	return st.Dim.Render(stamp) + " " + st.Faint.Render(mark) + " " + style.Render(ansi.Truncate(text, room, "…"))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

var _ = time.Second
