package dash

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/control"
)

// A module's console, as the dash holds it: the spec the module described, the
// rows its tables last read and the lines its stream has produced. The dash
// knows no module by name; it draws what the spec says in the blocks of
// docs/design/next/boards/Blocks.dc.html.

type consoleState struct {
	inst    string
	spec    *control.ConsoleSpec
	actions []control.ActionView
	state   string
	err     string
	tables  map[string][]control.ConsoleRow // by panel id
	streams map[string][]control.ConsoleRow // by panel id
	sel     map[string]int                  // the selected row, by block id
	focus   string                          // the block the keys are for
	prev    map[string]rateSample
	panel   string // the stream being followed, and which row of its table
	sector  string
	cancel  context.CancelFunc
	gen     int
}

type rateSample struct {
	v  float64
	at time.Time
}

type consoleMsg struct {
	inst string
	gen  int
	f    control.ConsoleFrame
}

// startConsole opens the subscription for an instance, closing the last.
func (m *Model) startConsole(inst string) {
	m.stopConsole()
	m.con = &consoleState{
		inst: inst, tables: map[string][]control.ConsoleRow{}, streams: map[string][]control.ConsoleRow{},
		sel: map[string]int{}, prev: map[string]rateSample{},
	}
	m.subscribeConsole()
}

func (m *Model) stopConsole() {
	if m.con != nil && m.con.cancel != nil {
		m.con.cancel()
	}
	m.con = nil
}

func (m *Model) subscribeConsole() {
	c := m.con
	if c == nil || m.send == nil {
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	c.gen++
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	d, send, inst, gen := m.d, m.send, c.inst, c.gen
	sub := control.Subscription{Topics: []string{control.TopicConsole}, Names: []string{inst}, Panel: c.panel, Selector: c.sector}
	go func() {
		_ = d.Subscribe(ctx, sub, func(fr control.Frame) error {
			if fr.Console != nil {
				send(consoleMsg{inst, gen, *fr.Console})
			}
			return nil
		})
	}()
}

// applyConsole folds a console frame in. Rows of a table replace what was
// there; rows of a stream are appended.
func (m *Model) applyConsole(msg consoleMsg) {
	c := m.con
	if c == nil || c.inst != msg.inst || (m.send != nil && msg.gen != c.gen) {
		return
	}
	f := msg.f
	if f.Spec != nil {
		c.spec, c.actions, c.state = f.Spec, f.Actions, f.State
	}
	if f.Error != "" && f.Panel == "" {
		c.err = f.Error
	}
	if f.Panel == "" {
		return
	}
	switch {
	case f.Rows != nil:
		c.tables[f.Panel] = m.withRates(f.Panel, f.Rows)
	case f.Row != nil:
		rows := append(c.streams[f.Panel], *f.Row)
		if len(rows) > 300 {
			rows = rows[len(rows)-300:]
		}
		c.streams[f.Panel] = rows
	}
}

// withRates works out each rate column from successive totals, as the module
// contract says: the module sends a running total and the dash the speed.
func (m *Model) withRates(panel string, rows []control.ConsoleRow) []control.ConsoleRow {
	c := m.con
	spec := panelOf(c.spec, panel)
	if spec == nil {
		return rows
	}
	out := make([]control.ConsoleRow, len(rows))
	for i, r := range rows {
		cells := make(map[string]string, len(r.Cells)+1)
		for k, v := range r.Cells {
			cells[k] = v
		}
		for _, col := range spec.Columns {
			if col.Role != "rate" {
				continue
			}
			key := panel + "/" + firstCol(spec) + "=" + r.Cells[firstCol(spec)] + "/" + col.Key
			v, err := strconv.ParseFloat(strings.ReplaceAll(r.Cells[col.Key], ",", ""), 64)
			if err != nil {
				continue
			}
			if p, ok := c.prev[key]; ok && m.now.After(p.at) && v >= p.v {
				per := (v - p.v) / m.now.Sub(p.at).Seconds()
				cells["_rate:"+col.Key] = fmt.Sprintf("%.0f/s", per)
			}
			c.prev[key] = rateSample{v, m.now}
		}
		out[i] = control.ConsoleRow{Cells: cells, Tone: r.Tone}
	}
	return out
}

func panelOf(spec *control.ConsoleSpec, id string) *control.ConsolePanel {
	if spec == nil {
		return nil
	}
	for i := range spec.Panels {
		if spec.Panels[i].ID == id {
			return &spec.Panels[i]
		}
	}
	return nil
}

func firstCol(p *control.ConsolePanel) string {
	if len(p.Columns) > 0 {
		return p.Columns[0].Key
	}
	return ""
}

// consoleTabs are the tabs a console contributes, from its spec: each Tab, or
// one called "console" for a spec of panels with none.
func consoleTabs(spec *control.ConsoleSpec) []tab {
	if spec == nil {
		return []tab{{"console:_", "console"}}
	}
	var out []tab
	for _, t := range spec.Tabs {
		out = append(out, tab{"console:" + t.ID, t.Title})
	}
	if len(out) == 0 && len(spec.Panels) > 0 {
		out = append(out, tab{"console:_", "console"})
	}
	return out
}

// selectedKey is the key of the selected row of a table block.
func (m *Model) selectedKey(blockID, panel string) string {
	c := m.con
	if c == nil {
		return ""
	}
	rows := c.tables[panel]
	if len(rows) == 0 {
		return ""
	}
	i := c.sel[blockID]
	if i >= len(rows) {
		i = len(rows) - 1
	}
	p := panelOf(c.spec, panel)
	if p == nil {
		return ""
	}
	return rows[i].Cells[firstCol(p)]
}

// followStream makes the subscription follow the selected row of the table the
// visible stream is bound to, when that has changed.
func (m *Model) followStream(body control.ConsoleNode) {
	c := m.con
	if c == nil {
		return
	}
	var stream *control.ConsoleNode
	walk(body, func(n control.ConsoleNode) {
		if n.Kind == "stream" && stream == nil {
			x := n
			stream = &x
		}
	})
	if stream == nil {
		return
	}
	p := panelOf(c.spec, stream.Panel)
	if p == nil {
		return
	}
	from := stream.From
	if from == "" {
		from = p.Select
	}
	sector := ""
	if from != "" {
		// the table block it follows, named by its id or by the panel it draws
		found := false
		walk(body, func(n control.ConsoleNode) {
			if n.Kind == "table" && !found && (n.ID == from || n.Panel == from) {
				found = true
				sector = m.selectedKey(blockID(n), n.Panel)
			}
		})
	}
	if c.panel == stream.Panel && c.sector == sector {
		return
	}
	c.panel, c.sector = stream.Panel, sector
	delete(c.streams, stream.Panel)
	m.subscribeConsole()
}

func blockID(n control.ConsoleNode) string {
	if n.ID != "" {
		return n.ID
	}
	return n.Kind + ":" + n.Panel
}

func walk(n control.ConsoleNode, f func(control.ConsoleNode)) {
	f(n)
	for _, ch := range n.Children {
		walk(ch, f)
	}
}

// consoleKey handles a key on a console tab. It returns whether it was the
// console's.
func (m *Model) consoleKey(k string, body control.ConsoleNode) (tea.Cmd, bool) {
	c := m.con
	if c == nil {
		return nil, false
	}
	var tables []control.ConsoleNode
	walk(body, func(n control.ConsoleNode) {
		if n.Kind == "table" {
			tables = append(tables, n)
		}
	})
	focused := -1
	for i, t := range tables {
		if blockID(t) == c.focus {
			focused = i
		}
	}
	if focused < 0 && len(tables) > 0 {
		focused = 0
		c.focus = blockID(tables[0])
	}
	switch k {
	case "up", "k":
		if focused >= 0 && c.sel[c.focus] > 0 {
			c.sel[c.focus]--
		}
		return nil, true
	case "down", "j":
		if focused >= 0 {
			if n := len(c.tables[tables[focused].Panel]); c.sel[c.focus] < n-1 {
				c.sel[c.focus]++
			}
		}
		return nil, true
	case "tab":
		if len(tables) > 1 {
			c.focus = blockID(tables[(focused+1)%len(tables)])
			return nil, true
		}
		return nil, false // the one table: tab moves to the next tab
	}
	// an action: its key is the first letter of its id, capitalised
	if len(k) == 1 {
		for _, a := range c.actions {
			if actionKey(a) == k {
				return m.runAction(a, tables, focused), true
			}
		}
	}
	return nil, false
}

func actionKey(a control.ActionView) string {
	r := []rune(a.ID)
	if len(r) == 0 {
		return ""
	}
	return strings.ToUpper(string(r[0]))
}

// runAction runs a module's action on the selected row of the table it is for,
// asking first when it deletes something and for input when it takes some.
func (m *Model) runAction(a control.ActionView, tables []control.ConsoleNode, focused int) tea.Cmd {
	c := m.con
	resource := ""
	for _, t := range tables {
		if p := panelOf(c.spec, t.Panel); p != nil && p.Source == a.Kind {
			resource = m.selectedKey(blockID(t), t.Panel)
		}
	}
	if resource == "" && focused >= 0 && a.Kind != "" {
		resource = m.selectedKey(blockID(tables[focused]), tables[focused].Panel)
	}
	inst := c.inst
	do := func(input string) tea.Cmd {
		if r := m.refuse(); r != nil {
			return r
		}
		d := m.d
		return func() tea.Msg {
			out, err := d.Admin(contextBackground(), inst, a.ID, resource, input)
			if err != nil {
				return failMsg(a.Label + ": " + humanise(err))
			}
			if out == "" {
				out = a.Label + " done"
			}
			return noticeMsg(out)
		}
	}
	switch {
	case a.InputHint != "":
		m.modal = &modal{kind: modalInput, target: resource, title: a.Label, hint: a.InputHint, then: do}
		return nil
	case a.Destructive:
		m.modal = &modal{kind: modalAsk, target: resource, title: a.Label, then: do}
		return nil
	}
	return do("")
}
