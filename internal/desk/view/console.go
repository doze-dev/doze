package view

import (
	"strconv"
	"strings"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// A module's console as the page draws it: the tab's tree of rows, columns
// and blocks from the module's spec, each block resolved to what it shows
// now (the PgActivity board, docs/design/next/boards/Blocks.dc.html). The
// page keeps only which row of each table is selected; what a detail shows,
// which stream is followed and what each cell says are decided here, as
// the dash decided them.

// Console is one console tab, resolved.
type Console struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Tab  string `json:"tab"`
	Body Node   `json:"body"`
	// State is the instance's state as the module last said it; Awake says
	// the console is being read. A console never wakes what it looks at.
	State string `json:"state,omitempty"`
	Awake bool   `json:"awake"`
	Err   string `json:"err,omitempty"`
	// Note is the line beside the tabs: how the console is read.
	Note string `json:"note"`
	// Keys is the keys bar: select, each action's key, next tab.
	Keys [][2]string `json:"keys"`
	// Missing says the spec has no such tab.
	Missing bool `json:"missing,omitempty"`
}

// Node is a container (row, col) or a block (table, stat, detail, stream,
// actions, note, chart), with what the block shows.
type Node struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Weight int    `json:"weight"`
	// Flexible blocks take the room that is left; the rest are as tall as
	// their content.
	Flexible bool   `json:"flexible,omitempty"`
	Children []Node `json:"children,omitempty"`
	// Cols and Rows are a table's or a stream's; Selected the table's
	// selected row.
	Cols     []Col  `json:"cols,omitempty"`
	Rows     []CRow `json:"rows,omitempty"`
	Selected int    `json:"selected,omitempty"`
	// Stats are a stat block's counters.
	Stats []Stat `json:"stats,omitempty"`
	// Detail is a detail block's key and value pairs; nil says nothing is
	// selected.
	Detail [][2]string `json:"detail,omitempty"`
	// Actions are an actions block's, each with the row it would act on.
	Actions []Action `json:"actions,omitempty"`
	// Text is a note's.
	Text string `json:"text,omitempty"`
	// Values are a chart's, oldest first; Last is the last one as a word.
	Values []float64 `json:"values,omitempty"`
	Last   string    `json:"last,omitempty"`
	// Missing says why a block cannot draw: "no data source x".
	Missing string `json:"missing,omitempty"`
}

// Col is one column of a table or a stream.
type Col struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Align string `json:"align,omitempty"`
	Role  string `json:"role,omitempty"`
}

// CRow is one row: what names it, and its cells in column order.
type CRow struct {
	Key   string `json:"key"`
	Cells []Cell `json:"cells"`
	Tone  string `json:"tone,omitempty"`
}

// Cell is one cell's text and tone.
type Cell struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

// Stat is one counter of a stat block.
type Stat struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Tone  string `json:"tone,omitempty"`
}

// Action is one of the module's actions: its key, and the row it would act
// on now (the selected row of the table whose source it is for).
type Action struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Key         string `json:"key"`
	Destructive bool   `json:"destructive,omitempty"`
	Hint        string `json:"hint,omitempty"`
	Resource    string `json:"resource,omitempty"`
}

// ConsoleOf resolves an instance's console tab. sel is the selected row of
// each table, by block id.
func ConsoleOf(st hub.StackView, name, tabID string, sel map[string]int) Console {
	out := Console{Key: st.Key, Name: name, Tab: tabID, Note: "the module's console · re-read every 2s while awake; it never wakes " + name}
	c, ok := st.Consoles[name]
	if !ok || c.Spec == nil {
		out.Body = Node{Kind: "col"}
		out.Keys = [][2]string{{"tab", "next tab"}}
		if c.Err != "" {
			out.Err = c.Err
		}
		return out
	}
	out.State, out.Err = c.State, c.Err
	out.Awake = display.Awake(c.State)
	body, ok := display.ConsoleBody(c.Spec, tabID)
	if !ok {
		out.Missing = true
		out.Body = Node{Kind: "col"}
		return out
	}
	r := resolver{c: c, tabID: tabID, sel: sel, body: body}
	out.Body = r.node(body)
	out.Keys = [][2]string{{"↑↓", "select"}}
	for _, a := range c.Actions {
		out.Keys = append(out.Keys, [2]string{display.ActionKey(a), strings.ToLower(a.Label)})
	}
	out.Keys = append(out.Keys, [2]string{"tab", "next tab"})
	return out
}

// Follow is the stream a tab follows and the row of its table it is for,
// from the selection, as the dash's followStream decided: for the hub's
// subscription.
func Follow(st hub.StackView, name, tabID string, sel map[string]int) (panel, sector string) {
	c, ok := st.Consoles[name]
	if !ok || c.Spec == nil {
		return "", ""
	}
	body, ok := display.ConsoleBody(c.Spec, tabID)
	if !ok {
		return "", ""
	}
	var stream *control.ConsoleNode
	display.Walk(body, func(n control.ConsoleNode) {
		if n.Kind == "stream" && stream == nil {
			x := n
			stream = &x
		}
	})
	if stream == nil {
		return "", ""
	}
	p := display.PanelOf(c.Spec, stream.Panel)
	if p == nil {
		return "", ""
	}
	from := stream.From
	if from == "" {
		from = p.Select
	}
	if from != "" {
		found := false
		display.Walk(body, func(n control.ConsoleNode) {
			if n.Kind == "table" && !found && (n.ID == from || n.Panel == from) {
				found = true
				sector = selectedKey(c, display.BlockID(n), n.Panel, sel)
			}
		})
	}
	return stream.Panel, sector
}

// selectedKey is what names the selected row of a table block.
func selectedKey(c hub.ConsoleView, blockID, panel string, sel map[string]int) string {
	rows := c.Tables[panel]
	p := display.PanelOf(c.Spec, panel)
	if len(rows) == 0 || p == nil {
		return ""
	}
	i := min(max(sel[blockID], 0), len(rows)-1)
	return rows[i].Cells[display.FirstCol(p)]
}

type resolver struct {
	c     hub.ConsoleView
	tabID string
	sel   map[string]int
	body  control.ConsoleNode
}

func (r resolver) node(n control.ConsoleNode) Node {
	out := Node{Kind: n.Kind, ID: display.BlockID(n), Title: n.Title, Weight: display.Weight(n), Flexible: display.Flexible(n)}
	switch n.Kind {
	case "row", "col":
		for _, ch := range n.Children {
			out.Children = append(out.Children, r.node(ch))
		}
	case "table":
		r.table(&out, n)
	case "stat":
		r.stat(&out, n)
	case "detail":
		if out.Title == "" {
			out.Title = "detail"
		}
		out.Detail = display.DetailRows(r.c.Spec, r.tabID, r.c.Tables, r.sel, n)
	case "stream":
		r.stream(&out, n)
	case "actions":
		if out.Title == "" {
			out.Title = "actions"
		}
		out.Actions = r.actions(n)
	case "note":
		if out.Title == "" {
			out.Title = "note"
		}
		out.Text = n.Text
	case "chart":
		r.chart(&out, n)
	}
	return out
}

func (r resolver) table(out *Node, n control.ConsoleNode) {
	p := display.PanelOf(r.c.Spec, n.Panel)
	if p == nil {
		out.Missing = "no data source " + n.Panel
		return
	}
	if out.Title == "" {
		out.Title = p.Title
	}
	rows := r.c.Tables[n.Panel]
	var cols []control.ConsoleColumn
	for _, c := range p.Columns {
		if c.Role != "detail" { // a field of the selected row's detail, not a column
			cols = append(cols, c)
		}
	}
	// Rows made while the stack ran, and not in the config, say so: a
	// column of its own when any row is one.
	runtime, hasFrom := false, false
	for _, row := range rows {
		if row.Cells[control.CellRuntime] != "" {
			runtime = true
		}
	}
	for _, c := range cols {
		hasFrom = hasFrom || c.Key == "from"
	}
	if runtime && !hasFrom {
		cols = append(cols, control.ConsoleColumn{Key: "from", Title: "from"})
	}
	for _, c := range cols {
		out.Cols = append(out.Cols, Col{Key: c.Key, Title: c.Title, Align: c.Align, Role: c.Role})
	}
	first := display.FirstCol(p)
	for _, row := range rows {
		rr := CRow{Key: row.Cells[first], Tone: row.Tone}
		for _, c := range cols {
			t, tone := display.Cell(c, row)
			rr.Cells = append(rr.Cells, Cell{Text: t, Tone: string(tone)})
		}
		out.Rows = append(out.Rows, rr)
	}
	if len(rows) > 0 {
		out.Selected = min(max(r.sel[out.ID], 0), len(rows)-1)
	}
}

func (r resolver) stat(out *Node, n control.ConsoleNode) {
	p := display.PanelOf(r.c.Spec, n.Panel)
	if p == nil {
		out.Missing = "no data source " + n.Panel
		return
	}
	if out.Title == "" {
		out.Title = p.Title
	}
	row := control.ConsoleRow{Cells: map[string]string{}}
	if rows := r.c.Tables[n.Panel]; len(rows) > 0 {
		row = rows[0]
	}
	for _, col := range p.Columns {
		t, tone := display.Cell(col, row)
		if t == "" {
			t, tone = "—", display.ToneDim
		}
		out.Stats = append(out.Stats, Stat{Label: col.Title, Value: t, Tone: string(tone)})
	}
}

func (r resolver) stream(out *Node, n control.ConsoleNode) {
	p := display.PanelOf(r.c.Spec, n.Panel)
	if p == nil {
		out.Missing = "no data source " + n.Panel
		return
	}
	if out.Title == "" {
		out.Title = p.Title
	}
	for _, c := range p.Columns {
		out.Cols = append(out.Cols, Col{Key: c.Key, Title: c.Title, Align: c.Align, Role: c.Role})
	}
	first := display.FirstCol(p)
	for _, row := range r.c.Streams[n.Panel] {
		rr := CRow{Key: row.Cells[first], Tone: row.Tone}
		for _, c := range p.Columns {
			t, tone := display.Cell(c, row)
			rr.Cells = append(rr.Cells, Cell{Text: t, Tone: string(tone)})
		}
		out.Rows = append(out.Rows, rr)
	}
}

// actions are the module's actions this block lists, each with the row it
// would act on: the selected row of the table whose source is its kind, else
// of the first table, as the dash's runAction chose.
func (r resolver) actions(n control.ConsoleNode) []Action {
	var tables []control.ConsoleNode
	display.Walk(r.body, func(x control.ConsoleNode) {
		if x.Kind == "table" {
			tables = append(tables, x)
		}
	})
	var out []Action
	for _, a := range r.c.Actions {
		if len(n.Actions) > 0 && !contains(n.Actions, a.ID) {
			continue
		}
		res := ""
		for _, t := range tables {
			if p := display.PanelOf(r.c.Spec, t.Panel); p != nil && p.Source == a.Kind {
				res = selectedKey(r.c, display.BlockID(t), t.Panel, r.sel)
			}
		}
		if res == "" && len(tables) > 0 && a.Kind != "" {
			res = selectedKey(r.c, display.BlockID(tables[0]), tables[0].Panel, r.sel)
		}
		out = append(out, Action{ID: a.ID, Label: a.Label, Key: display.ActionKey(a), Destructive: a.Destructive, Hint: a.InputHint, Resource: res})
	}
	return out
}

func (r resolver) chart(out *Node, n control.ConsoleNode) {
	p := display.PanelOf(r.c.Spec, n.Panel)
	if p == nil {
		out.Missing = "no data source " + n.Panel
		return
	}
	if out.Title == "" {
		out.Title = "chart"
	}
	first := display.FirstCol(p)
	for _, row := range r.c.Tables[n.Panel] {
		for _, col := range p.Columns {
			if col.Key == first {
				continue
			}
			if f, err := strconv.ParseFloat(strings.ReplaceAll(row.Cells[col.Key], ",", ""), 64); err == nil {
				out.Values = append(out.Values, f)
			}
		}
	}
	if len(out.Values) > 0 {
		out.Last = display.Count(out.Values[len(out.Values)-1])
	}
}

// ActionDialog is the confirmation for a module's action: input when it
// takes some, a question when it deletes something, in the dash's words.
func ActionDialog(st hub.StackView, name string, a Action) Dialog {
	d := Dialog{Kind: "action", Needed: a.Hint != "" || a.Destructive, Title: a.Label, Instance: name, Action: true}
	if a.Resource != "" {
		d.Sentence = []Span{{Text: "for "}, {Text: a.Resource, Strong: true, Mono: true}, {Text: " · " + name}}
	}
	b := Button{Label: a.Label, Verb: a.ID, Targets: []string{a.Resource}, Primary: true}
	switch {
	case a.Hint != "":
		d.Input, d.Hint = true, a.Hint
		b.Label = "Do it"
	case a.Destructive:
		d.Danger = true
		d.Title = a.Label + "?"
		d.Rows = []KV{{K: "", V: "This deletes data and cannot be undone."}}
		b.Primary, b.Danger = false, true
	}
	d.Buttons = []Button{b, {Label: "Cancel", Cancel: true}}
	return d
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
