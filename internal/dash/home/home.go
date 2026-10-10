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
	"github.com/doze-dev/doze/internal/dash/core"
	"github.com/doze-dev/doze/internal/dash/frame"
	"github.com/doze-dev/doze/internal/dash/layout"
	"github.com/doze-dev/doze/internal/dash/stacklogs"
	"github.com/doze-dev/doze/internal/display"
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
	w, h    int
	sel     string
	lay     layout.Map
	logs    map[string][]control.LogFrame
	logWant string // the instance whose lines are being fetched
	notice  string // the last thing a verb said, until the next key
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
		{Keys: "r", Label: "restart"}, {Keys: "p", Label: "pin"}, {Keys: "q", Label: "quit"},
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
	case core.Logs:
		if msg.Err == nil {
			m.logs[msg.Instance] = msg.Lines
		}
	case core.Done:
		if msg.Err != nil {
			m.notice = fmt.Sprintf("%s %s: %v", msg.Verb, msg.Target, msg.Err)
		} else {
			m.notice = ""
		}
	case tea.KeyPressMsg:
		m.notice = ""
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
			return m, func() tea.Msg { return core.Navigate{To: stacklogs.New()} }
		case "enter", "l", "right":
			if v, ok := m.selected(c); ok {
				return m, func() tea.Msg { return core.Navigate{To: frame.New(v.Name, v)} }
			}
		case "w":
			return m, m.verb(c, control.VerbWake)
		case "s":
			return m, m.verb(c, control.VerbSleep)
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
		lines, err := d.Logs(context.Background(), control.LogQuery{Instance: name, Tail: 8})
		return core.Logs{Instance: name, Lines: lines, Err: err}
	}
}

// View implements core.Page.
func (m *Model) View(c *core.Context, w, h int) string {
	m.w, m.h = w, h
	st := c.Styles
	rs := rows(c.Snap)
	m.lay = layout.Map{}
	if w <= 0 || h <= 0 {
		return ""
	}

	whole := layout.Rect{W: w, H: h}
	head, rest := layout.SplitTop(whole, 2)
	body, foot := layout.SplitBottom(rest, 2)
	list, preview := body, layout.Rect{}
	if w >= previewMin {
		list, preview = layout.SplitLeft(body, w*3/5)
	}

	var out []string
	out = append(out, m.header(c, rs, head.W), st.Faint.Render(strings.Repeat("─", head.W)))

	lines := m.list(c, rs, list)
	pv := []string(nil)
	if !preview.Empty() {
		pv = m.preview(c, preview)
	}
	for i := 0; i < body.H; i++ {
		l := pad(lineAt(lines, i), list.W)
		if !preview.Empty() {
			l += st.Faint.Render("│") + pad(lineAt(pv, i), preview.W-1)
		}
		out = append(out, l)
	}
	out = append(out, st.Faint.Render(strings.Repeat("─", foot.W)), m.footer(c, foot.W))
	return strings.Join(out, "\n")
}

func (m *Model) header(c *core.Context, rs []row, w int) string {
	st := c.Styles
	var awake, dozing, asleep int
	var ram int64
	for _, r := range rs {
		switch r.v.State {
		case "active":
			awake++
		case "idle":
			dozing++
		default:
			if r.v.Kind != "task" {
				asleep++
			}
		}
		ram += r.v.RAM
	}
	left := st.Title.Render("doze")
	if len(rs) == 0 {
		return left + st.Dim.Render("  nothing declared")
	}
	parts := []string{fmt.Sprintf("%d awake", awake)}
	if dozing > 0 {
		parts = append(parts, fmt.Sprintf("%d dozing", dozing))
	}
	parts = append(parts, fmt.Sprintf("%d asleep", asleep))
	right := st.Dim.Render(strings.Join(parts, " · ") + "   " + bytes(ram) + " resident")
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) list(c *core.Context, rs []row, r layout.Rect) []string {
	st := c.Styles
	var out []string
	lastGroup := -1
	for _, row := range rs {
		if row.group != lastGroup {
			if lastGroup != -1 {
				out = append(out, "")
			}
			count := 0
			for _, o := range rs {
				if o.group == row.group {
					count++
				}
			}
			out = append(out, st.Dim.Render(fmt.Sprintf("%s  %d", groups[row.group].title, count)))
			lastGroup = row.group
		}
		line := m.row(c, row.v, r.W)
		if row.v.Name == m.sel {
			line = st.Selected.Render(pad(ansi.Strip(line), r.W))
		}
		m.lay.Add("row:"+row.v.Name, layout.Rect{X: r.X, Y: r.Y + len(out), W: r.W, H: 1})
		out = append(out, line)
	}
	if m.notice != "" && len(out) < r.H {
		out = append(out, "", st.Bad.Render(ansi.Truncate(m.notice, r.W, "…")))
	}
	return out
}

// row draws one instance: glyph, name, what it is, its state, memory, a note.
func (m *Model) row(c *core.Context, v control.InstanceView, w int) string {
	st := c.Styles
	word, gl, style := core.State(v, st)
	kind := v.Engine
	switch v.Kind {
	case "app", "service", "task":
		kind = v.Kind
	}
	mem := ""
	if v.RAM > 0 {
		mem = bytes(v.RAM)
	}
	note, bad := display.Note(v, c.Snap.IdleTimeout, c.Now)
	if v.Task != nil {
		note = fmt.Sprintf("ran %s ago in %s", ago(c.Now.Sub(v.Task.RanAt)), (time.Duration(v.Task.TookMS) * time.Millisecond).Round(time.Millisecond))
		if v.Task.Why != "" {
			note += " · " + v.Task.Why
		}
		if v.Task.Result == "failed" {
			note, bad = "failed (exit "+fmt.Sprint(v.Task.ExitCode)+") · "+note, true
		}
	}
	if v.Kind == "task" && v.Task == nil {
		note = "never ran"
	}
	noteStyle := st.Dim
	if bad {
		noteStyle = st.Bad
	}
	line := "  " + style.Render(gl) + " " +
		st.Text.Render(padR(v.Name, 14)) + " " +
		st.Dim.Render(padR(kind, 10)) + " " +
		style.Render(padR(word, 9)) + " " +
		st.Dim.Render(padL(mem, 5)) + "  " +
		noteStyle.Render(note)
	return ansi.Truncate(line, w, "…")
}

func (m *Model) preview(c *core.Context, r layout.Rect) []string {
	st := c.Styles
	v, ok := m.selected(c)
	if !ok {
		return []string{" " + st.Dim.Render("nothing selected")}
	}
	word, gl, style := core.State(v, st)
	var out []string
	add := func(s string) { out = append(out, " "+s) }
	kind := v.Engine
	if v.Kind != "" && v.Kind != "module" {
		kind = v.Kind
	}
	add(st.Title.Render(v.Name) + st.Dim.Render("  "+kind) + "  " + style.Render(gl+" "+word))
	add("")
	if v.Domain != "" {
		add(st.Dim.Render("name     ") + st.Text.Render(v.Domain))
	}
	if v.Endpoint != "" {
		add(st.Dim.Render("address  ") + st.Text.Render(v.Endpoint))
	}
	if v.Console != "" {
		add(st.Dim.Render("console  ") + st.Text.Render(v.Console))
	}
	if v.RAM > 0 {
		add(st.Dim.Render("memory   ") + st.Text.Render(fmt.Sprintf("%s · cpu %.0f%%", bytes(v.RAM), v.CPU)))
	}
	if len(v.Holders) > 0 {
		add("")
		add(st.Dim.Render("awake because"))
		for _, h := range v.Holders {
			switch h.Kind {
			case "dependant":
				add("  " + st.Text.Render("held by "+h.Name))
			case "connection":
				add("  " + st.Text.Render(fmt.Sprintf("%d connection%s", h.Count, plural(h.Count))))
			case "pin":
				add("  " + st.Text.Render("pinned"))
			default:
				add("  " + st.Text.Render(h.Kind))
			}
		}
	}
	if lines := m.logs[v.Name]; len(lines) > 0 {
		add("")
		add(st.Dim.Render("last lines"))
		for _, l := range lines {
			add(" " + st.Dim.Render(ansi.Truncate(l.Line, r.W-4, "…")))
		}
	}
	return out
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
