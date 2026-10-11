package home

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/core"
	"github.com/doze-dev/doze/internal/dash/layout"
	"github.com/doze-dev/doze/internal/display"
	"github.com/doze-dev/doze/internal/journal"
)

const (
	// gutter is the blank margin either side of the screen.
	gutter = 2
	// previewWide and previewNarrow bound the preview pane: it is a card with a
	// handful of facts, not half the screen.
	previewWide, previewNarrow = 52, 36
	// barCells is the width of the memory bar.
	barCells = 5
)

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
	_, rest := layout.SplitTop(whole, 2)
	body, _ := layout.SplitBottom(rest, 2)
	inner := layout.Rect{X: gutter, Y: body.Y, W: body.W - 2*gutter, H: body.H}
	list, preview := inner, layout.Rect{}
	if w >= previewMin {
		pw := inner.W * 2 / 5
		if pw > previewWide {
			pw = previewWide
		}
		if pw < previewNarrow {
			pw = previewNarrow
		}
		list, preview = layout.SplitLeft(inner, inner.W-pw)
	}
	margin := strings.Repeat(" ", gutter)
	rule := st.Faint.Render(strings.Repeat("─", w-2*gutter))

	var out []string
	out = append(out, margin+m.header(c, rs, inner.W), margin+rule)

	lines := m.list(c, rs, list)
	var pv []string
	if !preview.Empty() {
		pv = m.preview(c, preview)
	}
	if m.menu {
		// The menu takes the whole body: what it lists is too long for a pane.
		lines, list, preview = m.menuView(c), inner, layout.Rect{}
	}
	for i := 0; i < body.H; i++ {
		l := margin + pad(lineAt(lines, i), list.W)
		if !preview.Empty() {
			l += st.Faint.Render("│") + pad(lineAt(pv, i), preview.W-1)
		}
		out = append(out, l)
	}
	out = append(out, margin+rule, margin+m.footer(c, inner.W))
	return strings.Join(out, "\n")
}

// header is the title and what the stack is doing in total.
func (m *Model) header(c *core.Context, rs []row, w int) string {
	st := c.Styles
	var awake, dozing, asleep, failing int
	var ram int64
	names := false
	for _, r := range rs {
		switch r.v.State {
		case "active":
			awake++
		case "crashing", "failed":
			failing++
		case "idle":
			dozing++
		default:
			if r.v.Kind != "task" {
				asleep++
			}
		}
		ram += r.v.RAM
		names = names || r.v.Domain != ""
	}
	left := st.Title.Render("doze")
	if len(rs) == 0 {
		return left + st.Dim.Render("  nothing declared")
	}
	parts := []string{fmt.Sprintf("%d awake", awake)}
	if dozing > 0 {
		parts = append(parts, fmt.Sprintf("%d dozing", dozing))
	}
	parts = append(parts, fmt.Sprintf("%d asleep", asleep), bytes(ram)+" resident")
	if c.Snap.IdleTimeout > 0 {
		parts = append(parts, "sleeps after "+c.Snap.IdleTimeout.Round(time.Second).String())
	}
	if names {
		parts = append(parts, "names on")
	} else {
		parts = append(parts, "names off")
	}
	right := st.Dim.Render(strings.Join(parts, " · "))
	if failing > 0 {
		right = st.Bad.Render(fmt.Sprintf("%d failing", failing)) + st.Dim.Render(" · ") + right
	}
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// column is one column of the list. Narrow screens drop them, last first.
type column struct {
	key   string
	title string
	w     int
	right bool // right-aligned
}

// dropOrder is which columns go first when the list is too narrow.
var dropOrder = []string{"cpu", "type", "note", "conns", "addr", "mem"}

// columns sizes the list's columns to what is in them, and drops the ones that
// do not fit in w.
func (m *Model) columns(c *core.Context, rs []row, w int) []column {
	longest := func(f func(control.InstanceView) string, min, max int) int {
		n := min
		for _, r := range rs {
			if l := ansi.StringWidth(f(r.v)); l > n {
				n = l
			}
		}
		if n > max {
			n = max
		}
		return n
	}
	cols := []column{
		{key: "name", title: "NAME", w: longest(func(v control.InstanceView) string { return v.Name }, 8, 22)},
		{key: "type", title: "TYPE", w: longest(kindOf, 4, 10)},
		{key: "state", title: "STATE", w: longest(func(v control.InstanceView) string { return stateWord(c, v) }, 6, 12)},
		{key: "addr", title: "ADDRESS", w: longest(address, 7, 30)},
		{key: "conns", title: "CONNS", w: 5, right: true},
		{key: "mem", title: "MEMORY", w: barCells + 1 + 5},
		{key: "cpu", title: "CPU", w: 4, right: true},
		{key: "note", title: "NOTE", w: 14},
	}
	width := func() int {
		n := 2 // the glyph and its space
		for _, col := range cols {
			n += col.w + 2
		}
		return n
	}
	for _, key := range dropOrder {
		if width() <= w {
			break
		}
		for i, col := range cols {
			if col.key == key {
				cols = append(cols[:i], cols[i+1:]...)
				break
			}
		}
	}
	return cols
}

// list draws the table: a header, then each group under its heading.
func (m *Model) list(c *core.Context, rs []row, r layout.Rect) []string {
	st := c.Styles
	cols := m.columns(c, rs, r.W)
	var maxRAM int64
	for _, row := range rs {
		if row.v.RAM > maxRAM {
			maxRAM = row.v.RAM
		}
	}

	var out []string
	var head strings.Builder
	head.WriteString("  ")
	for _, col := range cols {
		head.WriteString(cell(col.title, col.w, col.right) + "  ")
	}
	out = append(out, st.Faint.Render(ansi.Truncate(head.String(), r.W, "")))

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
			out = append(out, st.Text.Render(groups[row.group].title)+st.Dim.Render(fmt.Sprintf("  %d", count)))
			lastGroup = row.group
		}
		line := m.row(c, row.v, cols, maxRAM, r.W)
		if row.v.Name == m.sel {
			line = st.Selected.Render(pad(ansi.Strip(line), r.W))
		}
		m.lay.Add("row:"+row.v.Name, layout.Rect{X: r.X, Y: r.Y + len(out), W: r.W, H: 1})
		out = append(out, line)
	}
	out = append(out, m.recent(c, cols[0].w, r, len(out))...)
	if m.notice != "" && len(out) < r.H {
		style := st.Good
		if m.noticeBad {
			style = st.Bad
		}
		out = append(out, "", style.Render(ansi.Truncate(m.notice, r.W, "…")))
	}
	return out
}

// recent is what happened lately, newest last, in the room under the table.
func (m *Model) recent(c *core.Context, nameW int, r layout.Rect, used int) []string {
	st := c.Styles
	room := r.H - used - 3 // a blank, the title, and a line for a notice
	if room < 2 {
		return nil
	}
	if room > 12 {
		room = 12
	}
	out := []string{"", st.Text.Render("Recent")}
	if len(m.events) == 0 {
		return append(out, st.Dim.Render("nothing yet"))
	}
	evs := m.events
	if len(evs) > room {
		evs = evs[len(evs)-room:]
	}
	loc := c.Loc
	if loc == nil {
		loc = time.Local
	}
	for _, e := range evs {
		style := st.Dim
		switch e.Kind {
		case journal.Crash, journal.GiveUp, journal.Failed:
			style = st.Bad
		case journal.Wake, journal.Ready:
			style = st.Text
		}
		line := st.Faint.Render(e.Time.In(loc).Format("15:04:05")) + "  " +
			st.Text.Render(cell(e.Instance, nameW, false)) + "  " + style.Render(display.Event(e))
		out = append(out, ansi.Truncate(line, r.W, "…"))
	}
	return out
}

// row draws one instance across the columns.
func (m *Model) row(c *core.Context, v control.InstanceView, cols []column, maxRAM int64, w int) string {
	st := c.Styles
	_, gl, style := core.State(v, st)
	word := stateWord(c, v)
	note, bad := m.note(c, v)
	noteStyle := st.Dim
	if bad {
		noteStyle = st.Bad
	}
	var b strings.Builder
	b.WriteString(style.Render(gl) + " ")
	for _, col := range cols {
		var s string
		switch col.key {
		case "name":
			s = st.Text.Render(cell(v.Name, col.w, false))
		case "type":
			s = st.Dim.Render(cell(kindOf(v), col.w, false))
		case "state":
			s = style.Render(cell(word, col.w, false))
		case "addr":
			a := address(v)
			if a == "" {
				a = "-"
			}
			text := st.Text
			if !display.Awake(v.State) {
				text = st.Dim
			}
			s = text.Render(cell(a, col.w, false))
		case "conns":
			n := ""
			if v.Conns > 0 {
				n = fmt.Sprint(v.Conns)
			}
			s = st.Dim.Render(cell(n, col.w, true))
		case "mem":
			s = memBar(v.RAM, maxRAM, col.w, st.Good.Render, st.Faint.Render, st.Dim.Render)
		case "cpu":
			p := ""
			if v.CPU >= 1 {
				p = fmt.Sprintf("%.0f%%", v.CPU)
			}
			s = st.Dim.Render(cell(p, col.w, true))
		case "note":
			s = noteStyle.Render(note)
		}
		b.WriteString(s)
		if col.key != "note" {
			b.WriteString("  ")
		}
	}
	return ansi.Truncate(b.String(), w, "…")
}

// stateWord is the state as the list says it: "crashing 2/5" while restarting.
func stateWord(c *core.Context, v control.InstanceView) string {
	word, _, _ := core.State(v, c.Styles)
	if v.State == "crashing" && v.RestartMax > 0 {
		word = fmt.Sprintf("crashing %d/%d", v.RestartCount, v.RestartMax)
	}
	return word
}

// note is the sentence beside a row.
func (m *Model) note(c *core.Context, v control.InstanceView) (string, bool) {
	note, bad := display.Note(v, c.Snap.IdleTimeout, c.Now)
	if v.State == "crashing" && !v.NextRestart.IsZero() {
		if left := v.NextRestart.Sub(c.Now).Round(time.Second); left > 0 {
			note += " · trying again in " + left.String()
		}
	}
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
	return note, bad
}

// kindOf is what an instance is, for the TYPE column: the engine for a module,
// the block kind for the rest.
func kindOf(v control.InstanceView) string {
	switch v.Kind {
	case "app", "service", "task":
		return v.Kind
	}
	return v.Engine
}

// address is where a client connects: the name and port where there is a name,
// else the address itself. "" when there is nothing to connect to.
func address(v control.InstanceView) string {
	if v.Endpoint == "" {
		return ""
	}
	if v.Domain != "" {
		if _, port, ok := strings.Cut(v.Endpoint, ":"); ok && !strings.HasPrefix(v.Endpoint, "unix:") {
			return v.Domain + ":" + port
		}
		return v.Domain
	}
	return strings.TrimPrefix(v.Endpoint, "http://")
}

// cell fits s into w cells, padded on the side the column aligns to.
func cell(s string, w int, right bool) string {
	s = ansi.Truncate(s, w, "…")
	if right {
		return padL(s, w)
	}
	return pad(s, w)
}

// memBar is a small bar, scaled to the largest on screen, and the figure.
func memBar(ram, max int64, w int, fill, empty, dim func(...string) string) string {
	if ram <= 0 || max <= 0 {
		return strings.Repeat(" ", w)
	}
	n := int((ram*int64(barCells) + max - 1) / max)
	if n < 1 {
		n = 1
	}
	if n > barCells {
		n = barCells
	}
	bar := fill(strings.Repeat("█", n)) + empty(strings.Repeat("░", barCells-n))
	return bar + " " + dim(padL(bytes(ram), w-barCells-1))
}

// preview is the card for the instance under the cursor: how to reach it, what
// it costs, why it is awake, and its last lines. It never says less than the
// row does.
func (m *Model) preview(c *core.Context, r layout.Rect) []string {
	st := c.Styles
	v, ok := m.selected(c)
	if !ok {
		return []string{"  " + st.Dim.Render("nothing selected")}
	}
	word, gl, style := core.State(v, st)
	var out []string
	add := func(s string) { out = append(out, "  "+s) }
	fact := func(label, value string) {
		if value != "" {
			add(st.Dim.Render(fmt.Sprintf("%-9s", label)) + st.Text.Render(ansi.Truncate(value, r.W-14, "…")))
		}
	}
	kind := kindOf(v)
	add(st.Title.Render(v.Name) + st.Dim.Render("  "+kind) + "  " + style.Render(gl+" "+word))
	add("")

	// How to reach it.
	if v.Kind != "task" {
		add(st.Dim.Render("connect"))
		fact("name", v.Domain)
		fact("address", address(v))
		if v.Domain == "" && v.Endpoint == "" {
			add(st.Dim.Render("  no address of its own"))
		}
		fact("url", v.URL)
		fact("env", v.EnvVar)
		fact("console", orString(v.Console, v.WebURL))
		if !display.Awake(v.State) && v.State != "crashing" && v.State != "failed" && v.Endpoint != "" {
			add(st.Dim.Render("  asleep: a connection wakes it"))
		}
		add("")
	}

	// What it costs.
	if display.Awake(v.State) {
		add(st.Dim.Render("now"))
		fact("memory", memText(v))
		if v.Conns > 0 {
			fact("conns", fmt.Sprint(v.Conns))
		}
		if !v.StartedAt.IsZero() {
			fact("up", ago(c.Now.Sub(v.StartedAt)))
		}
		if note, _ := m.note(c, v); note != "" && v.State == "idle" {
			fact("sleeps", strings.TrimPrefix(note, "sleeps in "))
		}
		fact("pid", pidText(v))
		fact("data", v.DataDir)
		add("")
	} else if v.DataDir != "" && v.Kind != "task" {
		add(st.Dim.Render("data"))
		fact("folder", v.DataDir)
		add("")
	}

	// A task: how it last ran.
	if v.Kind == "task" {
		add(st.Dim.Render("last run"))
		if v.Task == nil {
			add(st.Dim.Render("  never ran"))
		} else {
			fact("result", v.Task.Result)
			fact("ran", ago(c.Now.Sub(v.Task.RanAt))+" ago")
			fact("took", (time.Duration(v.Task.TookMS) * time.Millisecond).Round(time.Millisecond).String())
			fact("why", v.Task.Why)
			if v.Task.Result == "failed" {
				fact("exit", fmt.Sprint(v.Task.ExitCode))
			}
		}
		add("")
	}

	if v.LastError != "" && (v.State == "crashing" || v.State == "failed") {
		add(st.Bad.Render("what went wrong"))
		for i, l := range strings.Split(strings.TrimSpace(v.LastError), "\n") {
			if i >= 4 {
				break
			}
			add("  " + st.Text.Render(ansi.Truncate(l, r.W-6, "…")))
		}
		if v.State == "crashing" && v.RestartMax > 0 {
			again := fmt.Sprintf("restart %d of %d", v.RestartCount, v.RestartMax)
			if left := v.NextRestart.Sub(c.Now).Round(time.Second); !v.NextRestart.IsZero() && left > 0 {
				again += " · trying again in " + left.String()
			}
			add("  " + st.Dim.Render(again))
		}
		add("")
	}

	if len(v.Holders) > 0 {
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
		add("")
	}

	// Its last lines fill whatever is left of the pane.
	if lines := m.logs[v.Name]; len(lines) > 0 {
		room := r.H - len(out) - 2
		if room > 0 {
			add(st.Dim.Render("last lines"))
			if len(lines) > room {
				lines = lines[len(lines)-room:]
			}
			for _, l := range lines {
				add(st.Dim.Render(ansi.Truncate(l.Line, r.W-6, "…")))
			}
		}
	}
	return out
}

func memText(v control.InstanceView) string {
	if v.RAM <= 0 {
		return ""
	}
	return fmt.Sprintf("%s · cpu %.0f%%", bytes(v.RAM), v.CPU)
}

func pidText(v control.InstanceView) string {
	if v.PID <= 0 {
		return ""
	}
	return fmt.Sprint(v.PID)
}

func orString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
