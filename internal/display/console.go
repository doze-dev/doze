package display

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
)

// A module's console as every surface reads it. A module publishes a tree of
// rows, columns and blocks; what each cell says, which layout a tab has, how
// a rate is worked out from totals and which row a detail follows are decided
// here, so the dash and the app draw the same console from the same spec.
// Nothing here knows which module it is drawing.

// ConsoleBody is the layout of a tab: the one the module published, or for a
// spec of panels with none, its tables side by side and its streams below,
// which is what View-Kafka.dc.html draws. ok is false for a tab the spec does
// not have.
func ConsoleBody(spec *control.ConsoleSpec, tabID string) (control.ConsoleNode, bool) {
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

// PanelOf is the panel of a spec by id, or nil.
func PanelOf(spec *control.ConsoleSpec, id string) *control.ConsolePanel {
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

// FirstCol is the key of a panel's first column: what names a row.
func FirstCol(p *control.ConsolePanel) string {
	if len(p.Columns) > 0 {
		return p.Columns[0].Key
	}
	return ""
}

// BlockID names a block: its own id, else its kind and panel.
func BlockID(n control.ConsoleNode) string {
	if n.ID != "" {
		return n.ID
	}
	return n.Kind + ":" + n.Panel
}

// Walk calls f on n and then on every node under it, in order.
func Walk(n control.ConsoleNode, f func(control.ConsoleNode)) {
	f(n)
	for _, ch := range n.Children {
		Walk(ch, f)
	}
}

// Weight is a node's share of its parent's room, one when it did not say.
func Weight(n control.ConsoleNode) int {
	if n.Weight > 0 {
		return n.Weight
	}
	return 1
}

// Flexible blocks take the room that is left; the rest are as tall as their
// content.
func Flexible(n control.ConsoleNode) bool {
	switch n.Kind {
	case "table", "stream", "row", "col":
		return true
	}
	return false
}

// HasStream says a node is or contains a stream block.
func HasStream(n control.ConsoleNode) bool {
	found := false
	Walk(n, func(x control.ConsoleNode) {
		if x.Kind == "stream" {
			found = true
		}
	})
	return found
}

// RateSample is the last total seen for a rate column, and when.
type RateSample struct {
	V  float64
	At time.Time
}

// Rates works out each rate column from successive totals, as the module
// contract says: the module sends a running total and the surface the speed.
// prev holds the last totals by row and column and is updated; the rows
// returned carry "_rate:<key>" cells for the rates that could be worked out.
func Rates(spec *control.ConsoleSpec, panel string, rows []control.ConsoleRow, prev map[string]RateSample, now time.Time) []control.ConsoleRow {
	p := PanelOf(spec, panel)
	if p == nil {
		return rows
	}
	out := make([]control.ConsoleRow, len(rows))
	for i, r := range rows {
		cells := make(map[string]string, len(r.Cells)+1)
		for k, v := range r.Cells {
			cells[k] = v
		}
		for _, col := range p.Columns {
			if col.Role != "rate" {
				continue
			}
			key := panel + "/" + FirstCol(p) + "=" + r.Cells[FirstCol(p)] + "/" + col.Key
			v, err := strconv.ParseFloat(strings.ReplaceAll(r.Cells[col.Key], ",", ""), 64)
			if err != nil {
				continue
			}
			if s, ok := prev[key]; ok && now.After(s.At) && v >= s.V {
				per := (v - s.V) / now.Sub(s.At).Seconds()
				cells["_rate:"+col.Key] = fmt.Sprintf("%.0f/s", per)
			}
			prev[key] = RateSample{v, now}
		}
		out[i] = control.ConsoleRow{Cells: cells, Tone: r.Tone}
	}
	return out
}

// Thousands writes an integer with commas: 1,043.
func Thousands(n int64) string {
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

// Cell is a cell's text and tone, by what the module said the column is: a
// count with commas, bytes as a size, a rate or a dash while there is none,
// a lag that is green at zero and red above it, a state with its glyph. A row
// the module marked dim is dim except for its state; an empty "from" column
// says doze.hcl, or runtime for a row made while the stack ran.
func Cell(col control.ConsoleColumn, row control.ConsoleRow) (text string, tone Tone) {
	raw := row.Cells[col.Key]
	text, tone = raw, ToneText
	switch col.Role {
	case "count":
		if n, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", ""), 64); err == nil {
			text = Thousands(int64(n))
		}
	case "bytes":
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			text = MB(n)
		}
	case "rate":
		text = row.Cells["_rate:"+col.Key]
		if text == "" {
			text, tone = "—", ToneDim
		}
	case "lag":
		if n, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", ""), 64); err == nil {
			text = Thousands(int64(n))
			if n == 0 {
				tone = ToneGood
			} else {
				tone = ToneBad
			}
		}
	case "state":
		switch strings.ToLower(raw) {
		case "ok", "active", "stable", "awake", "running", "ready":
			text, tone = "● "+raw, ToneGood
		case "warn", "waiting", "preparing", "rebalancing":
			text, tone = "◐ "+raw, ToneWarn
		case "bad", "error", "failed", "dead":
			text, tone = "✗ "+raw, ToneBad
		default:
			text, tone = "· "+raw, ToneDim
		}
	case "time", "id", "url", "text", "":
	}
	if row.Tone == "dim" && col.Role != "state" {
		tone = ToneDim
	}
	if col.Key == "from" && col.Title == "from" && raw == "" {
		text, tone = "doze.hcl", ToneDim
		if row.Cells[control.CellRuntime] != "" {
			text, tone = "runtime", ToneWarn
		}
	}
	return text, tone
}

// DetailRows are the key and value pairs of a detail block: the columns of
// the selected row of the table it follows, or of the first row of its own
// panel. tables are the rows last read by panel id; sel the selected row by
// block id; tabID the tab being drawn, whose layout names the table followed.
func DetailRows(spec *control.ConsoleSpec, tabID string, tables map[string][]control.ConsoleRow, sel map[string]int, n control.ConsoleNode) [][2]string {
	panel := n.Panel
	var row *control.ConsoleRow
	if n.From != "" {
		var src *control.ConsoleNode
		if body, ok := ConsoleBody(spec, tabID); ok {
			Walk(body, func(x control.ConsoleNode) {
				if x.Kind == "table" && (BlockID(x) == n.From || x.Panel == n.From || x.ID == n.From) && src == nil {
					y := x
					src = &y
				}
			})
		}
		if src != nil {
			rows := tables[src.Panel]
			if len(rows) > 0 {
				i := min(sel[BlockID(*src)], len(rows)-1)
				row = &rows[i]
				if panel == "" {
					panel = src.Panel
				}
			}
		}
	} else if rows := tables[panel]; len(rows) > 0 {
		row = &rows[0]
	}
	p := PanelOf(spec, panel)
	if row == nil || p == nil {
		return nil
	}
	var out [][2]string
	for _, col := range p.Columns {
		t, _ := Cell(col, *row)
		out = append(out, [2]string{col.Title, t})
	}
	return out
}

// ActionKey is the key that runs a module's action: the first letter of its
// id, capitalised.
func ActionKey(a control.ActionView) string {
	r := []rune(a.ID)
	if len(r) == 0 {
		return ""
	}
	return strings.ToUpper(string(r[0]))
}
