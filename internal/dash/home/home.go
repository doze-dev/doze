// Package home is the dash's first screen: every instance of the stack grouped
// by kind, with a pane on the right that previews the one under the cursor.
package home

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/confirm"
	"github.com/doze-dev/doze/internal/dash/copymode"
	"github.com/doze-dev/doze/internal/dash/core"
	"github.com/doze-dev/doze/internal/dash/frame"
	"github.com/doze-dev/doze/internal/dash/layout"
	"github.com/doze-dev/doze/internal/dash/palette"
	"github.com/doze-dev/doze/internal/dash/stacklogs"
	"github.com/doze-dev/doze/internal/dash/theme"
	"github.com/doze-dev/doze/internal/dash/themes"
	"github.com/doze-dev/doze/internal/journal"
)

// eventsID names the home page's follow of the journal, and keptEvents is how
// many events it keeps.
const (
	eventsID   = "home:events"
	keptEvents = 60
)

// previewMin is the narrowest screen that shows the preview pane. Narrower
// screens get the list alone (the single-column home is unit 5.10).
const previewMin = 100

// group is a heading on the list and what belongs under it.
type group struct {
	title string
	kinds []string
}

var groups = []group{
	{"Apps", []string{"app"}},
	{"Modules", []string{"module", ""}},
	{"Services", []string{"service"}},
	{"Tasks", []string{"task"}},
}

// Model is the home page.
type Model struct {
	w, h      int
	sel       string
	lay       layout.Map
	logs      map[string][]control.LogFrame
	logWant   string          // the instance whose lines are being fetched
	notice    string          // the last thing a verb said, until the next key
	menu      bool            // the copy menu is open
	noticeBad bool            // the notice is a failure, not a confirmation
	events    []journal.Event // what happened lately, newest last
	following bool            // the journal is being followed for events
}

// New returns the home page.
func New() *Model { return &Model{logs: map[string][]control.LogFrame{}} }

// row is one instance as home lists it.
type row struct {
	v     control.InstanceView
	group int
}

// rows lists the stack in display order: by group, then by name.
func rows(snap control.Snapshot) []row {
	var out []row
	for gi, g := range groups {
		var in []control.InstanceView
		for _, v := range snap.Instances {
			if v.Disabled && false {
				continue
			}
			for _, k := range g.kinds {
				if v.Kind == k {
					in = append(in, v)
					break
				}
			}
		}
		sort.Slice(in, func(i, j int) bool { return in[i].Name < in[j].Name })
		for _, v := range in {
			out = append(out, row{v: v, group: gi})
		}
	}
	return out
}

func (m *Model) selected(c *core.Context) (control.InstanceView, bool) {
	for _, r := range rows(c.Snap) {
		if r.v.Name == m.sel {
			return r.v, true
		}
	}
	return control.InstanceView{}, false
}

// Keys implements core.Page.
func (m *Model) Keys() []core.Key {
	return []core.Key{
		{Keys: "↑↓", Label: "select"}, {Keys: "enter", Label: "open"}, {Keys: "L", Label: "all logs"}, {Keys: "w", Label: "wake"}, {Keys: "s", Label: "sleep"},
		{Keys: "r", Label: "restart"}, {Keys: "p", Label: "pin"}, {Keys: "c", Label: "copy"}, {Keys: "R", Label: "reset"}, {Keys: ":", Label: "palette"}, {Keys: "T", Label: "theme"}, {Keys: "q", Label: "quit"},
	}
}

// Update implements core.Page.
func (m *Model) Update(msg tea.Msg, c *core.Context) (core.Page, tea.Cmd) {
	rs := rows(c.Snap)
	if _, ok := m.selected(c); !ok && len(rs) > 0 {
		m.sel = rs[0].v.Name
	}
	switch msg := msg.(type) {
	case core.Resized:
		m.w, m.h = msg.W, msg.H
		if !m.following && c.Daemon != nil {
			m.following = true
			m.events = nil
			return m, tea.Batch(m.wantLogs(c), func() tea.Msg { return core.Follow{ID: eventsID, Topic: "journal"} })
		}
	case core.EventLine:
		if msg.ID == eventsID {
			m.events = append(m.events, msg.Event)
			if len(m.events) > keptEvents {
				m.events = m.events[len(m.events)-keptEvents:]
			}
		}
	case core.Logs:
		if msg.Err == nil {
			m.logs[msg.Instance] = msg.Lines
		}
	case core.Notice:
		m.notice, m.noticeBad = msg.Text, msg.Bad
	case core.Done:
		if msg.Err != nil {
			m.notice, m.noticeBad = fmt.Sprintf("%s %s: %v", msg.Verb, msg.Target, msg.Err), true
		} else {
			m.notice = ""
		}
	case tea.KeyPressMsg:
		m.notice = ""
		if m.menu {
			return m, m.menuKey(c, msg.String())
		}
		switch msg.String() {
		case "up", "k":
			m.move(rs, -1)
		case "down", "j":
			m.move(rs, 1)
		case "home", "g":
			if len(rs) > 0 {
				m.sel = rs[0].v.Name
			}
		case "end", "G":
			if len(rs) > 0 {
				m.sel = rs[len(rs)-1].v.Name
			}
		case "L":
			m.following = false
			return m, func() tea.Msg { return core.Navigate{To: stacklogs.New()} }
		case "enter", "l", "right":
			if v, ok := m.selected(c); ok {
				m.following = false
				return m, func() tea.Msg { return core.Navigate{To: frame.New(v.Name, v)} }
			}
		case "T":
			m.following = false
			ch := theme.Choice{Name: c.Styles.Name, Dark: c.Dark, Plain: c.Styles.Plain}
			return m, func() tea.Msg { return core.Navigate{To: themes.New(ch)} }
		case ":", "ctrl+k":
			m.following = false
			name := m.sel
			return m, func() tea.Msg { return core.Navigate{To: palette.New(name)} }
		case "c":
			if _, ok := m.selected(c); ok {
				m.menu = true
			}
		case "w":
			return m, m.verb(c, control.VerbWake)
		case "s":
			return m, m.confirmed(c, control.VerbSleep)
		case "R":
			return m, m.confirmed(c, control.VerbReset)
		case "r":
			return m, m.verb(c, control.VerbRestart)
		case "p":
			return m, m.verb(c, control.VerbPin)
		}
	case tea.MouseClickMsg:
		if id, ok := m.lay.At(msg.X, msg.Y); ok {
			m.sel = strings.TrimPrefix(id, "row:")
		}
	}
	return m, m.wantLogs(c)
}

func (m *Model) move(rs []row, by int) {
	for i, r := range rs {
		if r.v.Name == m.sel {
			j := i + by
			if j >= 0 && j < len(rs) {
				m.sel = rs[j].v.Name
			}
			return
		}
	}
}

// confirmed runs a verb, after the confirmation it needs, if it needs one.
func (m *Model) confirmed(c *core.Context, name string) tea.Cmd {
	v, ok := m.selected(c)
	if !ok {
		return nil
	}
	if spec := core.Confirmation(v, name); spec != nil {
		return func() tea.Msg { return core.Navigate{To: confirm.New(confirm.Options(*spec))} }
	}
	return m.verb(c, name)
}

// verb runs a verb on the selected instance.
func (m *Model) verb(c *core.Context, name string) tea.Cmd {
	v, ok := m.selected(c)
	if !ok {
		return nil
	}
	d, target := c.Daemon, v.Name
	return func() tea.Msg {
		err := d.Do(context.Background(), control.Verb{Name: name, Target: target})
		return core.Done{Verb: name, Target: target, Err: err}
	}
}

// wantLogs asks for the selected instance's last lines, once per selection.
func (m *Model) wantLogs(c *core.Context) tea.Cmd {
	if m.sel == "" || m.sel == m.logWant || c.Daemon == nil {
		return nil
	}
	m.logWant = m.sel
	d, name := c.Daemon, m.sel
	return func() tea.Msg {
		lines, err := d.Logs(context.Background(), control.LogQuery{Instance: name, Tail: 40})
		return core.Logs{Instance: name, Lines: lines, Err: err}
	}
}

// copyItem is one thing the copy menu offers.
type copyItem struct{ label, text string }

func (m *Model) copyItems(c *core.Context) []copyItem {
	v, ok := m.selected(c)
	if !ok {
		return nil
	}
	var out []copyItem
	add := func(label, text string) {
		if text != "" {
			out = append(out, copyItem{label, text})
		}
	}
	add("name", v.Domain)
	add("address", v.Endpoint)
	add("connection string", v.URL)
	if v.EnvVar != "" && v.URL != "" {
		add("env line", v.EnvVar+"="+v.URL)
	}
	add("instance name", v.Name)
	return out
}

func (m *Model) menuView(c *core.Context) []string {
	st := c.Styles
	out := []string{" " + st.Title.Render("copy"), ""}
	items := m.copyItems(c)
	if len(items) == 0 {
		out = append(out, " "+st.Dim.Render("nothing to copy"))
	}
	for i, it := range items {
		out = append(out, fmt.Sprintf(" %s %s  %s", st.Key.Render(fmt.Sprint(i+1)), st.Dim.Render(fmt.Sprintf("%-18s", it.label)), st.Text.Render(it.text)))
	}
	out = append(out, "", " "+st.Dim.Render("esc closes"))
	return out
}

func (m *Model) menuKey(c *core.Context, key string) tea.Cmd {
	switch {
	case key == "esc" || key == "c" || key == "q":
		m.menu = false
	case len(key) == 1 && key[0] >= '1' && key[0] <= '9':
		items := m.copyItems(c)
		if i := int(key[0] - '1'); i < len(items) {
			m.menu = false
			text, label := items[i].text, items[i].label
			return tea.Batch(copymode.Clipboard(text), func() tea.Msg { return core.Notice{Text: "copied the " + label} })
		}
	}
	return nil
}

func (m *Model) footer(c *core.Context, w int) string {
	st := c.Styles
	var parts []string
	for _, k := range m.Keys() {
		parts = append(parts, st.Key.Render(k.Keys)+" "+st.KeyLabel.Render(k.Label))
	}
	return ansi.Truncate(strings.Join(parts, st.Faint.Render("  ·  ")), w, "…")
}

// --- small helpers ---

func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

func pad(s string, w int) string {
	n := w - ansi.StringWidth(s)
	if n <= 0 {
		return ansi.Truncate(s, w, "")
	}
	return s + strings.Repeat(" ", n)
}

func padR(s string, w int) string { return pad(s, w) }

func padL(s string, w int) string {
	n := w - ansi.StringWidth(s)
	if n <= 0 {
		return s
	}
	return strings.Repeat(" ", n) + s
}

func bytes(b int64) string {
	const mb = 1 << 20
	switch {
	case b <= 0:
		return "0"
	case b >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(b)/(1<<30))
	case b >= mb:
		return fmt.Sprintf("%dM", (b+mb/2)/mb)
	}
	return fmt.Sprintf("%dK", (b+512)/1024)
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
