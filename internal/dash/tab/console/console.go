// Package console is the generic console tab. A module describes its console
// as panels — tables, counters, a stream — and this draws them; it holds no
// knowledge of any one module, which is the point of the contract.
package console

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/core"
	"github.com/doze-dev/doze/internal/dash/theme"
)

// keepStream is how many rows of a stream the tab holds.
const keepStream = 500

// sample is a running total at a moment, for the rate of a counter.
type sample struct {
	v float64
	t time.Time
}

// Tab is the console tab of one instance.
type Tab struct {
	name    string
	id      string
	spec    *control.ConsoleSpec
	actions []control.ActionView
	state   string
	err     string

	tables  map[string][]control.ConsoleRow // by panel ID
	stream  []control.ConsoleRow
	rates   map[string]map[string]sample // panel -> row name + key -> last sample
	rateOf  map[string]float64           // panel + row + key -> per second
	focus   int                          // index into table panels
	cursor  map[string]int               // by panel ID
	paused  bool
	selNow  string // the selector the stream is following
	menu    *menu
	notice  string
	noticeB bool
}

// menu is the action list or the input prompt of one row.
type menu struct {
	row     control.ConsoleRow
	actions []control.ActionView
	cursor  int
	asking  *control.ActionView // the action whose input is being typed
	typed   string
}

// New returns the console tab for an instance.
func New(name string) *Tab {
	return &Tab{
		name: name, id: "console:" + name,
		tables: map[string][]control.ConsoleRow{}, rates: map[string]map[string]sample{},
		rateOf: map[string]float64{}, cursor: map[string]int{},
	}
}

// Title implements core.Tab.
func (t *Tab) Title() string { return "Console" }

// Show implements core.Tab.
func (t *Tab) Show(*core.Context, control.InstanceView) tea.Cmd { return t.follow() }

// Hide implements core.Tab.
func (t *Tab) Hide() tea.Cmd { return func() tea.Msg { return core.Unfollow{ID: t.id} } }

// follow subscribes to the console, naming the stream and its selection.
func (t *Tab) follow() tea.Cmd {
	panel, selector := "", ""
	if sp := t.streamPanel(); sp != nil {
		panel, selector = sp.ID, t.selection(sp.Select)
	}
	t.selNow = selector
	msg := core.Follow{ID: t.id, Topic: control.TopicConsole, Names: []string{t.name}, Panel: panel, Selector: selector}
	return func() tea.Msg { return msg }
}

// Keys implements core.Tab.
func (t *Tab) Keys() []core.Key {
	if t.menu != nil {
		return []core.Key{{Keys: "↑↓", Label: "choose"}, {Keys: "enter", Label: "run"}, {Keys: "esc", Label: "cancel"}}
	}
	keys := []core.Key{{Keys: "↑↓", Label: "row"}}
	if len(t.tablePanels()) > 1 {
		keys = append(keys, core.Key{Keys: "[ ]", Label: "table"})
	}
	if len(t.actions) > 0 {
		keys = append(keys, core.Key{Keys: "a", Label: "actions"})
	}
	keys = append(keys, core.Key{Keys: "f", Label: "pause stream"})
	if t.spec != nil && t.spec.WebURL != "" {
		keys = append(keys, core.Key{Keys: "o", Label: "open web console"})
	}
	return keys
}

// Capturing implements core.Capturer: a menu owns the keys while it is open.
func (t *Tab) Capturing() bool { return t.menu != nil }

func (t *Tab) tablePanels() []control.ConsolePanel {
	var out []control.ConsolePanel
	if t.spec == nil {
		return nil
	}
	for _, p := range t.spec.Panels {
		if p.Kind == "table" {
			out = append(out, p)
		}
	}
	return out
}

func (t *Tab) streamPanel() *control.ConsolePanel {
	if t.spec == nil {
		return nil
	}
	for i := range t.spec.Panels {
		if t.spec.Panels[i].Kind == "stream" {
			return &t.spec.Panels[i]
		}
	}
	return nil
}

// selection is the name of the selected row of a table panel.
func (t *Tab) selection(panel string) string {
	rows := t.tables[panel]
	if len(rows) == 0 {
		return ""
	}
	i := t.cursor[panel]
	if i >= len(rows) {
		i = len(rows) - 1
	}
	return rows[i].Cells["name"]
}

// focused is the table panel the cursor keys act on.
func (t *Tab) focused() (control.ConsolePanel, bool) {
	ps := t.tablePanels()
	if len(ps) == 0 {
		return control.ConsolePanel{}, false
	}
	if t.focus >= len(ps) {
		t.focus = 0
	}
	return ps[t.focus], true
}

// Update implements core.Tab.
func (t *Tab) Update(msg tea.Msg, c *core.Context, inst control.InstanceView) (core.Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case core.ConsoleLine:
		if msg.ID != t.id {
			return t, nil
		}
		return t, t.frame(msg.Frame, c)
	case core.Notice:
		t.notice, t.noticeB = msg.Text, msg.Bad
	case tea.KeyPressMsg:
		if t.menu != nil {
			return t, t.menuKey(msg, c)
		}
		t.notice = ""
		p, ok := t.focused()
		switch msg.String() {
		case "up", "k":
			if ok && t.cursor[p.ID] > 0 {
				t.cursor[p.ID]--
				return t, t.refollow()
			}
		case "down", "j":
			if ok && t.cursor[p.ID] < len(t.tables[p.ID])-1 {
				t.cursor[p.ID]++
				return t, t.refollow()
			}
		case "[":
			if n := len(t.tablePanels()); n > 0 {
				t.focus = (t.focus + n - 1) % n
			}
		case "]":
			if n := len(t.tablePanels()); n > 0 {
				t.focus = (t.focus + 1) % n
			}
		case "f":
			t.paused = !t.paused
		case "a":
			if ok {
				t.openMenu(p)
			}
		}
	}
	return t, nil
}

// refollow restarts the stream when the selection it follows has changed.
func (t *Tab) refollow() tea.Cmd {
	sp := t.streamPanel()
	if sp == nil || t.selection(sp.Select) == t.selNow {
		return nil
	}
	t.stream = nil
	return t.follow()
}

// frame folds one console frame in.
func (t *Tab) frame(f control.ConsoleFrame, c *core.Context) tea.Cmd {
	if f.State != "" {
		t.state = f.State
	}
	if f.Spec != nil && t.spec == nil {
		t.spec, t.actions = f.Spec, f.Actions
		return t.follow() // now that the stream and its selector are known
	}
	switch {
	case f.Error != "":
		t.err = f.Error
	case f.Row != nil:
		if !t.paused {
			t.stream = append(t.stream, *f.Row)
			if len(t.stream) > keepStream {
				t.stream = t.stream[len(t.stream)-keepStream:]
			}
		}
	case f.Panel != "":
		t.err = ""
		t.tables[f.Panel] = f.Rows
		t.noteRates(f.Panel, f.Rows, c.Now)
		return t.refollow()
	}
	return nil
}

// noteRates works out per-second rates for the rate columns of a panel from
// successive running totals.
func (t *Tab) noteRates(panel string, rows []control.ConsoleRow, now time.Time) {
	var p *control.ConsolePanel
	for i := range t.spec.Panels {
		if t.spec.Panels[i].ID == panel {
			p = &t.spec.Panels[i]
		}
	}
	if p == nil {
		return
	}
	for _, col := range p.Columns {
		if col.Role != "rate" {
			continue
		}
		for _, r := range rows {
			v, err := strconv.ParseFloat(r.Cells[col.Key], 64)
			if err != nil {
				continue
			}
			key := r.Cells["name"] + "\x00" + col.Key
			if t.rates[panel] == nil {
				t.rates[panel] = map[string]sample{}
			}
			if prev, ok := t.rates[panel][key]; ok && now.After(prev.t) {
				per := (v - prev.v) / now.Sub(prev.t).Seconds()
				if per < 0 {
					per = 0
				}
				t.rateOf[panel+"\x00"+key] = per
			}
			t.rates[panel][key] = sample{v, now}
		}
	}
}

// ── actions ──

func (t *Tab) openMenu(p control.ConsolePanel) {
	rows := t.tables[p.ID]
	if len(rows) == 0 {
		return
	}
	i := t.cursor[p.ID]
	if i >= len(rows) {
		i = len(rows) - 1
	}
	var acts []control.ActionView
	for _, a := range t.actions {
		if a.Kind == p.Source || a.Kind == "" {
			acts = append(acts, a)
		}
	}
	if len(acts) == 0 {
		t.notice, t.noticeB = "nothing to do to a "+p.Source, false
		return
	}
	t.menu = &menu{row: rows[i], actions: acts}
}

func (t *Tab) menuKey(msg tea.KeyPressMsg, c *core.Context) tea.Cmd {
	m := t.menu
	if m.asking != nil {
		switch msg.String() {
		case "esc":
			m.asking, m.typed = nil, ""
		case "enter":
			return t.run(c, *m.asking, m.typed)
		case "backspace":
			if r := []rune(m.typed); len(r) > 0 {
				m.typed = string(r[:len(r)-1])
			}
		default:
			if msg.Text != "" {
				m.typed += msg.Text
			}
		}
		return nil
	}
	switch msg.String() {
	case "esc", "q":
		t.menu = nil
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.actions)-1 {
			m.cursor++
		}
	case "enter":
		a := m.actions[m.cursor]
		if a.InputHint != "" {
			m.asking = &a
			return nil
		}
		return t.run(c, a, "")
	}
	return nil
}

// run does an action on the row the menu was opened for.
func (t *Tab) run(c *core.Context, a control.ActionView, input string) tea.Cmd {
	row := t.menu.row.Cells["name"]
	t.menu = nil
	d, instance := c.Daemon, t.name
	return func() tea.Msg {
		out, err := d.Admin(context.Background(), instance, a.ID, row, input)
		if err != nil {
			return core.Notice{Text: a.Label + " " + row + ": " + err.Error(), Bad: true}
		}
		if out == "" {
			out = a.Label + " " + row + ": done"
		}
		return core.Notice{Text: strings.SplitN(out, "\n", 2)[0]}
	}
}

// ── drawing ──

// View implements core.Tab.
func (t *Tab) View(c *core.Context, inst control.InstanceView, w, h int) string {
	st := c.Styles
	var out []string
	switch {
	case t.spec == nil && t.err != "":
		out = append(out, st.Bad.Render(t.err))
	case t.spec == nil:
		out = append(out, st.Dim.Render("asking the module how to show it…"))
	case t.menu != nil:
		out = t.viewMenu(c)
	default:
		out = t.viewPanels(c, w, h)
	}
	if t.notice != "" {
		style := st.Good
		if t.noticeB {
			style = st.Bad
		}
		out = append(out, "", style.Render(ansi.Truncate(t.notice, w, "…")))
	}
	for i := range out {
		out[i] = ansi.Truncate(out[i], w, "…")
	}
	for len(out) < h {
		out = append(out, "")
	}
	return strings.Join(out[:h], "\n")
}

func (t *Tab) viewMenu(c *core.Context) []string {
	st := c.Styles
	m := t.menu
	out := []string{st.Title.Render(m.row.Cells["name"]), ""}
	if m.asking != nil {
		out = append(out, st.Text.Render(m.asking.Label)+st.Dim.Render("  "+m.asking.InputHint))
		out = append(out, st.Accent.Render("› ")+st.Text.Render(m.typed)+st.Accent.Render("▏"))
		return out
	}
	for i, a := range m.actions {
		line := "  " + a.Label
		if a.Destructive {
			line += st.Bad.Render("  destructive")
		}
		if i == m.cursor {
			out = append(out, st.Selected.Render(line))
		} else {
			out = append(out, st.Text.Render(line))
		}
	}
	return out
}

func (t *Tab) viewPanels(c *core.Context, w, h int) []string {
	st := c.Styles
	var out []string
	if t.state == "reaped" || t.state == "" && len(t.tables) == 0 {
		out = append(out, st.Dim.Render("asleep — the console shows live data once something wakes it"), "")
	}
	if t.err != "" {
		out = append(out, st.Bad.Render(t.err), "")
	}
	focused, _ := t.focused()
	var stream *control.ConsolePanel
	for i := range t.spec.Panels {
		p := t.spec.Panels[i]
		switch p.Kind {
		case "table":
			out = append(out, t.viewTable(c, p, p.ID == focused.ID, w)...)
			out = append(out, "")
		case "counters":
			out = append(out, t.viewCounters(c, p)...)
			out = append(out, "")
		case "stream":
			stream = &t.spec.Panels[i]
		}
	}
	if stream != nil {
		room := h - len(out) - 1
		if room < 3 {
			room = 3
		}
		out = append(out, t.viewStream(c, *stream, room, w)...)
	}
	return out
}

func (t *Tab) viewTable(c *core.Context, p control.ConsolePanel, focused bool, w int) []string {
	st := c.Styles
	rows := t.tables[p.ID]
	title := st.Title.Render(p.Title)
	if focused && len(t.tablePanels()) > 1 {
		title = st.Accent.Render("▸ ") + title
	}
	out := []string{title + st.Dim.Render(fmt.Sprintf("  %d", len(rows)))}
	if len(rows) == 0 {
		return append(out, "  "+st.Dim.Render("none"))
	}
	cells := make([][]string, len(rows))
	widths := make([]int, len(p.Columns))
	for ci, col := range p.Columns {
		widths[ci] = ansi.StringWidth(col.Title)
	}
	for ri, r := range rows {
		cells[ri] = make([]string, len(p.Columns))
		for ci, col := range p.Columns {
			s := t.cell(p, col, r)
			cells[ri][ci] = s
			if n := ansi.StringWidth(s); n > widths[ci] {
				widths[ci] = n
			}
		}
	}
	head := "  "
	for ci, col := range p.Columns {
		head += st.Dim.Render(align(col.Title, widths[ci], col.Align)) + "  "
	}
	out = append(out, head)
	cur := t.cursor[p.ID]
	for ri := range rows {
		line := "  "
		for ci, col := range p.Columns {
			line += styled(st, col, rows[ri], cells[ri][ci], widths[ci]) + "  "
		}
		if focused && ri == cur {
			line = st.Selected.Render(pad(ansi.Strip(line), w))
		}
		out = append(out, line)
	}
	return out
}

// cell is the text of one cell, formatted by its column's role.
func (t *Tab) cell(p control.ConsolePanel, col control.ConsoleColumn, r control.ConsoleRow) string {
	v := r.Cells[col.Key]
	switch col.Role {
	case "rate":
		per, ok := t.rateOf[p.ID+"\x00"+r.Cells["name"]+"\x00"+col.Key]
		if !ok {
			return "–"
		}
		return fmt.Sprintf("%.1f/s", per)
	case "count":
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return thousands(n)
		}
	}
	return v
}

// styled colours a cell by what its column means, so the host does it for a
// module it knows nothing about: lag above zero is a problem, a group state is
// good or not, a status code has its class.
func styled(st theme.Styles, col control.ConsoleColumn, r control.ConsoleRow, text string, w int) string {
	cell := align(text, w, col.Align)
	style := st.Text
	switch col.Role {
	case "lag":
		if n, err := strconv.ParseInt(strings.ReplaceAll(text, ",", ""), 10, 64); err == nil && n > 0 {
			style = st.Bad
		} else {
			style = st.Dim
		}
	case "state":
		switch strings.ToLower(text) {
		case "stable", "ok", "active", "ready":
			style = st.Good
		case "empty", "dead", "none", "":
			style = st.Dim
		default:
			style = st.Warn
		}
	case "code":
		switch {
		case strings.HasPrefix(text, "5"):
			style = st.Bad
		case strings.HasPrefix(text, "4"):
			style = st.Warn
		case strings.HasPrefix(text, "2"):
			style = st.Good
		}
	case "time", "rate":
		style = st.Dim
	}
	switch r.Tone {
	case "bad":
		style = st.Bad
	case "warn":
		style = st.Warn
	case "ok":
		style = st.Good
	}
	return style.Render(cell)
}

func (t *Tab) viewCounters(c *core.Context, p control.ConsolePanel) []string {
	st := c.Styles
	rows := t.tables[p.ID]
	out := []string{st.Title.Render(p.Title)}
	if len(rows) == 0 {
		return append(out, "  "+st.Dim.Render("none"))
	}
	var parts []string
	for _, r := range rows {
		parts = append(parts, st.Text.Render(r.Cells["name"])+" "+st.Accent.Render(r.Cells["status"]))
	}
	return append(out, "  "+strings.Join(parts, st.Faint.Render("  ·  ")))
}

func (t *Tab) viewStream(c *core.Context, p control.ConsolePanel, room, w int) []string {
	st := c.Styles
	title := st.Title.Render(p.Title)
	if sel := t.selNow; sel != "" {
		title += st.Dim.Render("  " + sel)
	}
	if t.paused {
		title += st.Warn.Render("  paused")
	}
	out := []string{title}
	rows := t.stream
	if len(rows) == 0 {
		return append(out, "  "+st.Dim.Render("nothing yet"))
	}
	if len(rows) > room-1 {
		rows = rows[len(rows)-(room-1):]
	}
	for _, r := range rows {
		var parts []string
		for _, col := range p.Columns {
			parts = append(parts, r.Cells[col.Key])
		}
		style := st.Text
		switch r.Tone {
		case "ok":
			style = st.Good
		case "warn":
			style = st.Warn
		case "bad":
			style = st.Bad
		}
		out = append(out, "  "+style.Render(ansi.Truncate(strings.Join(parts, "  "), w-2, "…")))
	}
	return out
}

func align(s string, w int, a string) string {
	if a == "right" {
		return padLeft(s, w)
	}
	return pad(s, w)
}

func pad(s string, w int) string {
	if n := w - ansi.StringWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func padLeft(s string, w int) string {
	if n := w - ansi.StringWidth(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}
