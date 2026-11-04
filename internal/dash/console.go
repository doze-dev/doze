package dash

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/display"
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

type rateSample = display.RateSample

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

// withRates works out each rate column from successive totals, keeping the
// last totals in the console state.
func (m *Model) withRates(panel string, rows []control.ConsoleRow) []control.ConsoleRow {
	return display.Rates(m.con.spec, panel, rows, m.con.prev, m.now)
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
	p := display.PanelOf(c.spec, panel)
	if p == nil {
		return ""
	}
	return rows[i].Cells[display.FirstCol(p)]
}

// followStream makes the subscription follow the selected row of the table the
// visible stream is bound to, when that has changed.
func (m *Model) followStream(body control.ConsoleNode) {
	c := m.con
	if c == nil {
		return
	}
	var stream *control.ConsoleNode
	display.Walk(body, func(n control.ConsoleNode) {
		if n.Kind == "stream" && stream == nil {
			x := n
			stream = &x
		}
	})

	if stream == nil {
		return
	}
	p := display.PanelOf(c.spec, stream.Panel)
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
		display.Walk(body, func(n control.ConsoleNode) {
			if n.Kind == "table" && !found && (n.ID == from || n.Panel == from) {
				found = true
				sector = m.selectedKey(display.BlockID(n), n.Panel)
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

// consoleKey handles a key on a console tab. It returns whether it was the
// console's.
func (m *Model) consoleKey(k string, body control.ConsoleNode) (tea.Cmd, bool) {
	c := m.con
	if c == nil {
		return nil, false
	}
	var tables []control.ConsoleNode
	display.Walk(body, func(n control.ConsoleNode) {
		if n.Kind == "table" {
			tables = append(tables, n)
		}
	})

	focused := -1
	for i, t := range tables {
		if display.BlockID(t) == c.focus {
			focused = i
		}
	}
	if focused < 0 && len(tables) > 0 {
		focused = 0
		c.focus = display.BlockID(tables[0])
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
			c.focus = display.BlockID(tables[(focused+1)%len(tables)])
			return nil, true
		}
		return nil, false // the one table: tab moves to the next tab
	}
	// an action: its key is the first letter of its id, capitalised
	if len(k) == 1 {
		for _, a := range c.actions {
			if display.ActionKey(a) == k {
				return m.runAction(a, tables, focused), true
			}
		}
	}
	return nil, false
}

// runAction runs a module's action on the selected row of the table it is for,
// asking first when it deletes something and for input when it takes some.
func (m *Model) runAction(a control.ActionView, tables []control.ConsoleNode, focused int) tea.Cmd {
	c := m.con
	resource := ""
	for _, t := range tables {
		if p := display.PanelOf(c.spec, t.Panel); p != nil && p.Source == a.Kind {
			resource = m.selectedKey(display.BlockID(t), t.Panel)
		}
	}
	if resource == "" && focused >= 0 && a.Kind != "" {
		resource = m.selectedKey(display.BlockID(tables[focused]), tables[focused].Panel)
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
				return failMsg(a.Label + ": " + display.Humanise(err))
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
