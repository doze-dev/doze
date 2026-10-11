package dash

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/doze-dev/doze/internal/control"
)

// The block engine. A module publishes a tree of rows, columns and blocks with
// its module; this draws it. Nothing here is configured by a person and
// nothing here knows which module it is drawing.

const (
	maxDepth   = 3 // deeper than this is not drawn: a module bug, not a layout
	minFlexRow = 5 // the fewest lines a table or stream is given
)

// bodyOf is the layout of a tab: the one the module published, or for a spec
// of panels with none, its tables side by side and its streams below, which is
// what View-Kafka.dc.html draws.
func bodyOf(spec *control.ConsoleSpec, tabID string) (control.ConsoleNode, bool) {
	if spec == nil {
		return control.ConsoleNode{}, false
	}
	for _, t := range spec.Tabs {
		if "console:"+t.ID == tabID {
			return t.Body, true
		}
	}
	if len(spec.Tabs) > 0 {
		return control.ConsoleNode{}, false
	}
	var tables, streams []control.ConsoleNode
	for _, p := range spec.Panels {
		switch p.Kind {
		case "table":
			tables = append(tables, control.ConsoleNode{Kind: "table", Panel: p.ID, Title: p.Title, Weight: 1})
		case "counters":
			tables = append(tables, control.ConsoleNode{Kind: "stat", Panel: p.ID, Title: p.Title, Weight: 1})
		case "stream":
			streams = append(streams, control.ConsoleNode{Kind: "stream", Panel: p.ID, From: p.Select, Title: p.Title, Weight: 1})
		}
	}
	kids := []control.ConsoleNode{}
	if len(tables) > 0 {
		kids = append(kids, control.ConsoleNode{Kind: "row", Weight: 1, Children: tables})
	}
	kids = append(kids, streams...)
	kids = append(kids, control.ConsoleNode{Kind: "actions"})
	return control.ConsoleNode{Kind: "col", Children: kids}, true
}

func weight(n control.ConsoleNode) int {
	if n.Weight > 0 {
		return n.Weight
	}
	return 1
}

// flexible blocks take the room that is left; the rest are as tall as their
// content.
func flexible(n control.ConsoleNode) bool {
	switch n.Kind {
	case "table", "stream", "row", "col":
		return true
	}
	return false
}

// hasStream says a node is or contains a stream block.
func hasStream(n control.ConsoleNode) bool {
	found := false
	walk(n, func(x control.ConsoleNode) {
		if x.Kind == "stream" {
			found = true
		}
	})
	return found
}

// natural is the height a node asks for at width w.
func (m *Model) natural(n control.ConsoleNode, w int) int {
	c := m.con
	switch n.Kind {
	case "stat":
		return 4
	case "detail":
		lines := 0
		for _, kv := range m.detailRows(n) {
			lines += min(len(wrap(kv[1], w-4-9)), 4)
		}
		return max(3, lines+2)
	case "note":
		return len(wrap(n.Text, w-4)) + 2
	case "actions":
		return 3
	case "chart":
		return 5
	case "table":
		rows := 0
		if c != nil {
			rows = len(c.tables[n.Panel])
		}
		return rows + 3
	case "stream":
		return minFlexRow + 2
	case "row":
		h := 0
		for _, ch := range n.Children {
			h = max(h, m.natural(ch, w/max(len(n.Children), 1)))
		}
		return h
	case "col":
		h := 0
		for _, ch := range n.Children {
			h += m.natural(ch, w)
		}
		return h
	}
	return 3
}

// drawNode draws n into w x h cells.
func (m *Model) drawNode(n control.ConsoleNode, w, h, depth int) []string {
	if w < 6 || h < 2 {
		return blank(w, h)
	}
	if depth > maxDepth && (n.Kind == "row" || n.Kind == "col") {
		return m.unknown(n, w, h, "nested too deeply")
	}
	switch n.Kind {
	case "row":
		return m.drawRow(n, w, h, depth)
	case "col":
		return m.drawCol(n, w, h, depth)
	case "table":
		return m.drawTable(n, w, h)
	case "stat":
		return m.drawStat(n, w, h)
	case "detail":
		return m.drawDetail(n, w, h)
	case "stream":
		return m.drawStream(n, w, h)
	case "actions":
		return m.drawActions(n, w, h)
	case "note":
		return m.drawNote(n, w, h)
	case "chart":
		return m.drawChart(n, w, h)
	}
	return m.unknown(n, w, h, "a block kind this doze does not know")
}

func blank(w, h int) []string {
	out := make([]string, max(h, 0))
	for i := range out {
		out[i] = strings.Repeat(" ", max(w, 0))
	}
	return out
}

func (m *Model) unknown(n control.ConsoleNode, w, h int, why string) []string {
	s := m.st
	return box(s.border, s.d.Render("⌗ "+n.Kind), "", []string{s.d.Render(why), s.d.Render("the rest still draws")}, w, h)
}

// drawRow splits the width by weight, a cell between.
func (m *Model) drawRow(n control.ConsoleNode, w, h, depth int) []string {
	kids := n.Children
	if len(kids) == 0 {
		return blank(w, h)
	}
	// a narrow terminal stacks the columns, in order
	if w < 20*len(kids) {
		return m.drawCol(control.ConsoleNode{Kind: "col", Children: kids}, w, h, depth)
	}
	total := 0
	for _, k := range kids {
		total += weight(k)
	}
	avail := w - (len(kids) - 1)
	widths := make([]int, len(kids))
	used := 0
	for i, k := range kids {
		widths[i] = avail * weight(k) / total
		used += widths[i]
	}
	widths[len(widths)-1] += avail - used
	cols := make([][]string, len(kids))
	for i, k := range kids {
		cols[i] = m.drawNode(k, widths[i], h, depth+1)
	}
	out := make([]string, h)
	for y := 0; y < h; y++ {
		var parts []string
		for i := range cols {
			l := ""
			if y < len(cols[i]) {
				l = cols[i][y]
			} else {
				l = strings.Repeat(" ", widths[i])
			}
			parts = append(parts, l)
		}
		out[y] = strings.Join(parts, " ")
	}
	return out
}

// drawCol stacks its children: the ones that ask for a height get it, and the
// flexible ones share what is left by weight.
func (m *Model) drawCol(n control.ConsoleNode, w, h, depth int) []string {
	kids := n.Children
	if len(kids) == 0 {
		return blank(w, h)
	}
	heights := make([]int, len(kids))
	settled := make([]bool, len(kids))
	fixed, flexW := 0, 0
	// With a stream in the column, the tables above it are as tall as their rows
	// and the stream takes the rest (View-Kafka.dc.html).
	streamHere := false
	for _, k := range kids {
		if hasStream(k) {
			streamHere = true
		}
	}
	for i, k := range kids {
		if streamHere && !hasStream(k) {
			heights[i] = min(m.natural(k, w), max(h/2, 5))
			settled[i] = true
			fixed += heights[i]
			continue
		}
		if flexible(k) {
			flexW += weight(k)
			heights[i] = 0
		} else {
			heights[i] = m.natural(k, w)
			fixed += heights[i]
		}
	}
	left := h - fixed
	if left < 0 {
		left = 0
	}
	if flexW > 0 {
		given := 0
		for i, k := range kids {
			if flexible(k) && !settled[i] {
				heights[i] = left * weight(k) / flexW
				given += heights[i]
			}
		}
		// the remainder goes to the last flexible child
		for i := len(kids) - 1; i >= 0; i-- {
			if flexible(kids[i]) && !settled[i] {
				heights[i] += left - given
				break
			}
		}
	}
	var out []string
	for i, k := range kids {
		if len(out) >= h {
			break
		}
		hh := min(heights[i], h-len(out))
		if hh <= 0 {
			continue
		}
		out = append(out, m.drawNode(k, w, hh, depth+1)...)
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	return out[:h]
}

// --- cells ---

// render turns a cell into text and a colour by what the module said it is.
func (m *Model) render(col control.ConsoleColumn, row control.ConsoleRow) (string, func(...string) string) {
	s := m.st
	raw := row.Cells[col.Key]
	text, style := raw, s.text.Render
	switch col.Role {
	case "count":
		if n, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", ""), 64); err == nil {
			text = thousands(int64(n))
		}
	case "bytes":
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			text = mb(n)
		}
	case "rate":
		text = row.Cells["_rate:"+col.Key]
		if text == "" {
			text, style = "—", s.d.Render
		}
	case "lag":
		if n, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", ""), 64); err == nil {
			text = thousands(int64(n))
			if n == 0 {
				style = s.g.Render
			} else {
				style = s.r.Render
			}
		}
	case "state":
		switch strings.ToLower(raw) {
		case "ok", "active", "stable", "awake", "running", "ready":
			text, style = "● "+raw, s.g.Render
		case "warn", "waiting", "preparing", "rebalancing":
			text, style = "◐ "+raw, s.y.Render
		case "bad", "error", "failed", "dead":
			text, style = "✗ "+raw, s.r.Render
		default:
			text, style = "· "+raw, s.d.Render
		}
	case "time", "id", "url", "text", "":
	}
	if row.Tone == "dim" && col.Role != "state" {
		style = s.d.Render
	}
	if col.Key == "from" && col.Title == "from" && raw == "" {
		text, style = "doze.hcl", s.d.Render
		if row.Cells[control.CellRuntime] != "" {
			text, style = "runtime", s.y.Render
		}
	}
	return text, style
}

func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []string
	for len(s) > 3 {
		out = append([]string{s[len(s)-3:]}, out...)
		s = s[:len(s)-3]
	}
	out = append([]string{s}, out...)
	r := strings.Join(out, ",")
	if neg {
		r = "-" + r
	}
	return r
}

func wrap(s string, w int) []string {
	if w < 8 {
		w = 8
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			if line != "" && width(line)+1+width(word) > w {
				out = append(out, line)
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		out = append(out, line)
	}
	return out
}

// --- blocks ---

func (m *Model) title(n control.ConsoleNode, fallback string) string {
	if n.Title != "" {
		return n.Title
	}
	if p := panelOf(m.conSpec(), n.Panel); p != nil && p.Title != "" {
		return p.Title
	}
	return fallback
}

func (m *Model) conSpec() *control.ConsoleSpec {
	if m.con == nil {
		return nil
	}
	return m.con.spec
}

func (m *Model) drawTable(n control.ConsoleNode, w, h int) []string {
	s, c := m.st, m.con
	p := panelOf(m.conSpec(), n.Panel)
	if p == nil || c == nil {
		return m.unknown(n, w, h, "no data source "+n.Panel)
	}
	id := blockID(n)
	focused := c.focus == id
	rows := c.tables[n.Panel]
	var cols []control.ConsoleColumn
	for _, c := range p.Columns {
		if c.Role != "detail" { // a field of the selected row's detail, not a column
			cols = append(cols, c)
		}
	}
	// Rows made while the stack ran, and not in the config, say so, and how to
	// keep them: a column of its own when any row is one.
	runtime := false
	for _, r := range rows {
		if r.Cells[control.CellRuntime] != "" {
			runtime = true
		}
	}
	hasFrom := false
	for _, c := range cols {
		hasFrom = hasFrom || c.Key == "from"
	}
	if runtime && !hasFrom {
		cols = append(cols, control.ConsoleColumn{Key: "from", Title: "from"})
	}
	inner := w - 4
	// column widths from the content, the last column taking what is left
	widths := make([]int, len(cols))
	for i, col := range cols {
		widths[i] = width(col.Title)
		for _, r := range rows {
			t, _ := m.render(col, r)
			widths[i] = max(widths[i], width(t))
		}
	}
	used := 2 // the pointer
	for i := range widths {
		used += widths[i] + 2
	}
	if over := used - inner; over > 0 && len(widths) > 0 {
		widths[len(widths)-1] = max(4, widths[len(widths)-1]-over)
	}
	line := func(cells []string, styles []func(...string) string, picked bool) string {
		var b strings.Builder
		if picked {
			b.WriteString(s.a.Render("▸") + " ")
		} else {
			b.WriteString("  ")
		}
		for i, cell := range cells {
			var t string
			if cols[i].Align == "right" {
				t = right(cell, widths[i])
			} else {
				t = fit(cell, widths[i])
			}
			b.WriteString(styles[i](t) + "  ")
		}
		return b.String()
	}
	var lines []string
	hd := make([]string, len(cols))
	hs := make([]func(...string) string, len(cols))
	for i, col := range cols {
		hd[i], hs[i] = col.Title, s.d.Render
	}
	lines = append(lines, line(hd, hs, false))
	if len(rows) == 0 {
		msg := "nothing yet"
		if c.state != "" && c.state != "active" && c.state != "idle" {
			msg = "asleep: the rows start when it wakes"
		}
		lines = append(lines, s.d.Render("  "+msg))
	}
	pick := c.sel[id]
	if pick >= len(rows) {
		pick = max(len(rows)-1, 0)
	}
	// scroll so the picked row is in view
	room := h - 3
	first := 0
	if pick >= room {
		first = pick - room + 1
	}
	for i := first; i < len(rows) && i < first+room; i++ {
		cells := make([]string, len(cols))
		styles := make([]func(...string) string, len(cols))
		for j, col := range cols {
			cells[j], styles[j] = m.render(col, rows[i])
		}
		l := line(cells, styles, i == pick && focused)
		if i == pick && focused {
			l = s.sel.Render(stripAnsi(l))
		}
		lines = append(lines, l)
	}
	border := s.border
	if focused {
		border = s.borderOn
	}
	return box(border, s.a.Bold(true).Render(m.title(n, "table")), "", lines, w, h)
}

func (m *Model) drawStat(n control.ConsoleNode, w, h int) []string {
	s, c := m.st, m.con
	p := panelOf(m.conSpec(), n.Panel)
	if p == nil || c == nil {
		return m.unknown(n, w, h, "no data source "+n.Panel)
	}
	row := control.ConsoleRow{Cells: map[string]string{}}
	if rows := c.tables[n.Panel]; len(rows) > 0 {
		row = rows[0]
	}
	var labels, values []string
	for _, col := range p.Columns {
		t, style := m.render(col, row)
		if t == "" {
			t = "—"
			style = s.d.Render
		}
		cell := max(width(col.Title), width(t)) + 3
		labels = append(labels, s.d.Render(fit(col.Title, cell)))
		values = append(values, style(s.b.Render(fit(t, cell))))
	}
	caption := ""
	if t := m.title(n, ""); t != "" {
		caption = s.a.Bold(true).Render(t)
	}
	return box(s.border, caption, "", []string{strings.Join(labels, ""), strings.Join(values, "")}, w, h)
}

// detailRows are the key and value pairs of a detail block: the columns of the
// selected row of the table it follows, or of the first row of its own panel.
func (m *Model) detailRows(n control.ConsoleNode) [][2]string {
	c := m.con
	if c == nil {
		return nil
	}
	panel := n.Panel
	var row *control.ConsoleRow
	if n.From != "" {
		var src *control.ConsoleNode
		if body, ok := bodyOf(c.spec, m.currentTabID); ok {
			walk(body, func(x control.ConsoleNode) {
				if x.Kind == "table" && (blockID(x) == n.From || x.Panel == n.From || x.ID == n.From) && src == nil {
					y := x
					src = &y
				}
			})
		}
		if src != nil {
			rows := c.tables[src.Panel]
			if len(rows) > 0 {
				i := min(c.sel[blockID(*src)], len(rows)-1)
				row = &rows[i]
				if panel == "" {
					panel = src.Panel
				}
			}
		}
	} else if rows := c.tables[panel]; len(rows) > 0 {
		row = &rows[0]
	}
	p := panelOf(c.spec, panel)
	if row == nil || p == nil {
		return nil
	}
	var out [][2]string
	for _, col := range p.Columns {
		t, _ := m.render(col, *row)
		out = append(out, [2]string{col.Title, t})
	}
	return out
}

func (m *Model) drawDetail(n control.ConsoleNode, w, h int) []string {
	s := m.st
	rows := m.detailRows(n)
	var lines []string
	if len(rows) == 0 {
		lines = []string{s.d.Render("nothing selected")}
	}
	for _, kv := range rows {
		// a long value, a query, wraps under its key and is not cut
		parts := wrap(kv[1], w-4-9)
		if len(parts) > 4 {
			parts = append(parts[:3], parts[3][:min(len(parts[3]), w-4-10)]+"…")
		}
		for i, p := range parts {
			key := strings.Repeat(" ", 9)
			if i == 0 {
				key = fit(strings.ToLower(kv[0]), 9)
			}
			lines = append(lines, s.d.Render(key)+s.w.Render(p))
		}
	}
	return box(s.border, s.a.Bold(true).Render(m.title(n, "detail")), "", lines, w, h)
}

func (m *Model) drawStream(n control.ConsoleNode, w, h int) []string {
	s, c := m.st, m.con
	p := panelOf(m.conSpec(), n.Panel)
	if p == nil || c == nil {
		return m.unknown(n, w, h, "no data source "+n.Panel)
	}
	rows := c.streams[n.Panel]
	cols := p.Columns
	if len(rows) > h-3 {
		rows = rows[len(rows)-(h-3):]
	}
	widths := make([]int, len(cols))
	for i, col := range cols {
		for _, r := range rows {
			t, _ := m.render(col, r)
			widths[i] = max(widths[i], width(t))
		}
		widths[i] = min(widths[i], 28)
	}
	var lines []string
	if len(rows) > 0 {
		var hb strings.Builder
		for i, col := range cols {
			w := max(widths[i], width(col.Title))
			widths[i] = w
			if i == len(cols)-1 {
				hb.WriteString(s.d.Render(col.Title))
			} else if col.Align == "right" {
				hb.WriteString(s.d.Render(right(col.Title, w)) + " ")
			} else {
				hb.WriteString(s.d.Render(fit(col.Title, w)) + " ")
			}
		}
		lines = append(lines, hb.String())
	}
	for _, r := range rows {
		var b strings.Builder
		for i, col := range cols {
			t, style := m.render(col, r)
			switch {
			case col.Role == "time":
				style = s.d.Render
			case i == len(cols)-1:
				// the last field is the message: the rest of the width
			}
			if i == len(cols)-1 {
				b.WriteString(style(t))
			} else if col.Align == "right" {
				b.WriteString(style(right(t, widths[i])) + " ")
			} else {
				b.WriteString(style(fit(t, widths[i])) + " ")
			}
		}
		lines = append(lines, b.String())
	}
	if len(lines) == 0 {
		lines = []string{s.d.Render("nothing yet")}
	}
	follow := ""
	if c.sector != "" {
		follow = s.d.Render(" · following " + c.sector)
	}
	return box(s.border, s.a.Bold(true).Render(m.title(n, "stream"))+follow, "", lines, w, h)
}

func (m *Model) drawActions(n control.ConsoleNode, w, h int) []string {
	s, c := m.st, m.con
	if c == nil {
		return blank(w, h)
	}
	var parts []string
	for _, a := range c.actions {
		if len(n.Actions) > 0 && !contains(n.Actions, a.ID) {
			continue
		}
		label := s.cap(actionKey(a), a.Label)
		if a.Destructive {
			label = s.key.Render(actionKey(a)) + " " + s.r.Render(a.Label)
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return blank(w, h)
	}
	return box(s.border, s.a.Bold(true).Render(m.title(n, "actions")), "", []string{strings.Join(parts, "   ")}, w, h)
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func (m *Model) drawNote(n control.ConsoleNode, w, h int) []string {
	s := m.st
	var lines []string
	for _, l := range wrap(n.Text, w-4) {
		lines = append(lines, s.d.Render(l))
	}
	return box(s.border, s.d.Render(m.title(n, "note")), "", lines, w, h)
}

func (m *Model) drawChart(n control.ConsoleNode, w, h int) []string {
	s, c := m.st, m.con
	p := panelOf(m.conSpec(), n.Panel)
	if p == nil || c == nil {
		return m.unknown(n, w, h, "no data source "+n.Panel)
	}
	var vals []float64
	for _, r := range c.tables[n.Panel] {
		for _, col := range p.Columns {
			if f, err := strconv.ParseFloat(strings.ReplaceAll(r.Cells[col.Key], ",", ""), 64); err == nil && col.Key != firstCol(p) {
				vals = append(vals, f)
			}
		}
	}
	if rows := h - 2 - 2; rows >= 2 {
		last := 0.0
		if len(vals) > 0 {
			last = vals[len(vals)-1]
		}
		t := trace{"", vals, 0, count, count(last), s.a}
		return box(s.border, s.a.Bold(true).Render(m.title(n, "chart")), "", m.charts([]trace{t}, w-4, rows), w, h)
	}
	line := s.a.Render(spark(vals, w-4))
	return box(s.border, s.a.Bold(true).Render(m.title(n, "chart")), "", []string{line}, w, h)
}

func stripAnsi(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			in = true
		case in && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var _ = fmt.Sprint

// consoleTab draws a tab the module published.
func (m *Model) consoleTab(v control.InstanceView, tabID string, w, h int) []string {
	s, c := m.st, m.con
	if c == nil || c.inst != v.Name {
		return box(s.border, s.d.Render("console"), "", []string{s.d.Render("starting")}, w, h)
	}
	if c.err != "" {
		return box(s.borderBad, s.r.Render("console"), "", []string{s.r.Render(c.err)}, w, h)
	}
	body, ok := bodyOf(c.spec, tabID)
	if !ok {
		return box(s.border, s.d.Render("console"), "", []string{s.d.Render("this module has not described a console")}, w, h)
	}
	if c.focus == "" {
		walk(body, func(n control.ConsoleNode) {
			if n.Kind == "table" && c.focus == "" {
				c.focus = blockID(n)
			}
		})
	}
	return m.drawNode(body, w, h, 0)
}

// syncConsole keeps the subscription following the row the visible stream is
// bound to.
func (m *Model) syncConsole() {
	if m.con == nil || m.page != pageFocus {
		return
	}
	v, ok := m.selected()
	if !ok {
		return
	}
	ts := m.tabs(v)
	if m.tab >= len(ts) || !strings.HasPrefix(ts[m.tab].id, "console:") {
		return
	}
	if body, ok := bodyOf(m.con.spec, ts[m.tab].id); ok {
		m.followStream(body)
	}
}

// extensions are the blocks a module adds after a block of the generic
// overview, each in a box of its own height, with a blank line before.
func (m *Model) extensions(after string, w int) []string {
	c := m.con
	if c == nil || c.spec == nil {
		return nil
	}
	var out []string
	for _, e := range c.spec.Extend {
		if e.After != after {
			continue
		}
		h := min(m.natural(e.Node, w), 14)
		out = append(out, "")
		out = append(out, m.drawNode(e.Node, w, h, 1)...)
	}
	return out
}
