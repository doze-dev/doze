package view

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/display"
)

// The Grid, as MapGrid.dc.html draws it: the stack as a table that reads
// the same at thirty services as at five. A row needs each column it has
// a dot in. The rows are the apps in the map's order, callers first; the
// columns are the apps something needs, then what the stack shares, most
// used first, then the tasks that run before each app.

// GridView is the stack as the Grid view draws it.
type GridView struct {
	// Heads are the groups over the columns, each spanning its columns.
	Heads []GridHead `json:"heads"`
	Cols  []GridCol  `json:"cols"`
	Rows  []GridRow  `json:"rows"`
	Foot  []string   `json:"foot"` // "needed by", a count per column
	// Later says what the grid cannot show yet, when the stack has a
	// broker whose topics it would want to.
	Later string `json:"later,omitempty"`
}

// GridHead is a group's caption over its columns.
type GridHead struct {
	Text string `json:"text"`
	Span int    `json:"span"`
}

// GridCol is a column: an app, a shared service, or the tasks.
type GridCol struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // app, shared, tasks
	// Sep starts a group: a line on its left.
	Sep bool `json:"sep,omitempty"`
	// Sub is the tasks column's tally: "9 ✓".
	Sub string `json:"sub,omitempty"`
}

// GridRow is an app and what it needs.
type GridRow struct {
	Name  string     `json:"name"`
	Lang  string     `json:"lang,omitempty"`
	Cells []GridCell `json:"cells"`
}

// GridCell is one row against one column.
type GridCell struct {
	Dot  bool `json:"dot,omitempty"`
	Self bool `json:"self,omitempty"`
	// Text is the tasks column's word: "migrate-orders ✓".
	Text string `json:"text,omitempty"`
	Tone string `json:"tone,omitempty"`
	// Tip is what hovering or focusing the cell says; nil for an empty one.
	Tip *GridTip `json:"tip,omitempty"`
}

// GridTip is the tooltip: what the dot means, why it matters, the line of
// doze.hcl that makes it so, and what the keys do.
type GridTip struct {
	Head string `json:"head"`
	Why  string `json:"why,omitempty"`
	// Ref is the reference as doze.hcl writes it: "CATALOG_URL =
	// app.catalog.url"; the reference alone ("app.catalog") when the file
	// was not read.
	Ref  string `json:"ref"`
	Keys string `json:"keys"`
	// Open is what ⏎ opens: the column's service, or the first task.
	Open string `json:"open"`
	// From and To are the row and the column, for refer.
	From string `json:"-"`
	To   string `json:"-"`
}

// gridOf tabulates the stack.
func gridOf(rows []display.Row, now time.Time) GridView {
	g := GridView{Heads: []GridHead{}, Cols: []GridCol{}, Rows: []GridRow{}, Foot: []string{}}
	f := readFlow(rows)
	cols, lane := f.order()
	slot, _ := f.arrange(cols)
	var order []string
	for _, c := range cols {
		c = append([]string(nil), c...)
		sort.SliceStable(c, func(i, j int) bool { return slot[c[i]] < slot[c[j]] })
		for _, v := range c {
			if f.byName[v].Kind == "app" {
				order = append(order, v)
			}
		}
	}
	order = append(order, lane...)

	// The columns: the apps something needs, in the same order; what is
	// shared, most used first; the tasks.
	var apps []string
	for _, a := range order {
		if len(f.callers[a]) > 0 {
			apps = append(apps, a)
		}
	}
	var shared []string
	for _, s := range f.shared {
		if len(f.users[s]) > 0 {
			shared = append(shared, s)
		}
	}
	sort.SliceStable(shared, func(i, j int) bool {
		if a, b := len(f.users[shared[i]]), len(f.users[shared[j]]); a != b {
			return a > b
		}
		return shared[i] < shared[j]
	})
	hasTasks := false
	for _, a := range order {
		if len(f.tasks[a]) > 0 {
			hasTasks = true
		}
	}
	if len(apps) > 0 {
		g.Heads = append(g.Heads, GridHead{Text: "Other services", Span: len(apps)})
		for _, a := range apps {
			g.Cols = append(g.Cols, GridCol{Name: a, Kind: "app"})
		}
	}
	if len(shared) > 0 {
		g.Heads = append(g.Heads, GridHead{Text: "Shared", Span: len(shared)})
		for i, s := range shared {
			g.Cols = append(g.Cols, GridCol{Name: s, Kind: "shared", Sep: i == 0 && len(apps) > 0})
		}
	}
	if hasTasks {
		done := 0
		seen := map[string]bool{}
		for _, a := range order {
			for _, t := range f.tasks[a] {
				if v := f.byName[t]; !seen[t] && v.Task != nil && v.Task.Result == "done" && v.State != "booting" {
					done++
				}
				seen[t] = true
			}
		}
		g.Heads = append(g.Heads, GridHead{Text: "Before it", Span: 1})
		g.Cols = append(g.Cols, GridCol{Name: "tasks", Kind: "tasks", Sep: len(g.Cols) > 0, Sub: strconv.Itoa(done) + " ✓"})
	}

	count := make([]int, len(g.Cols))
	for _, a := range order {
		row := GridRow{Name: a, Lang: lang(f.byName[a].Run), Cells: make([]GridCell, len(g.Cols))}
		needs := map[string]bool{}
		for _, x := range f.calls[a] {
			needs[x] = true
		}
		for _, x := range f.uses[a] {
			needs[x] = true
		}
		for ci, c := range g.Cols {
			cell := &row.Cells[ci]
			switch {
			case c.Kind == "tasks":
				ts := f.tasks[a]
				if len(ts) == 0 {
					continue
				}
				cell.Text, cell.Tone = taskCell(f, ts)
				var lines []string
				var refs []string
				for _, t := range ts {
					lines = append(lines, taskLine(f.byName[t], now))
					refs = append(refs, "task."+t)
				}
				cell.Tip = &GridTip{Head: andList(ts) + " run before " + a, Why: strings.Join(lines, " · "),
					Ref: "after = [" + strings.Join(refs, ", ") + "]", Keys: "⏎ opens " + ts[0] + " · o opens " + a, Open: ts[0], From: a, To: "after"}
			case c.Name == a:
				cell.Self = true
			case needs[c.Name]:
				cell.Dot = true
				count[ci]++
				verb := "needs"
				kind := "app"
				if c.Kind == "shared" {
					verb = "uses"
					kind = f.byName[c.Name].Kind
					if kind == "" {
						kind = "module"
					}
				}
				tip := &GridTip{Head: a + " " + verb + " " + c.Name, Ref: kind + "." + c.Name,
					Keys: "⏎ opens " + c.Name + " · o opens " + a, Open: c.Name, From: a, To: c.Name}
				if display.AwakeNow(f.byName[a]) && display.AwakeNow(f.byName[c.Name]) {
					tip.Why = "keeps it awake"
				}
				cell.Tip = tip
			}
		}
		g.Rows = append(g.Rows, row)
	}
	for ci, c := range g.Cols {
		if c.Kind == "tasks" {
			g.Foot = append(g.Foot, "")
			continue
		}
		g.Foot = append(g.Foot, strconv.Itoa(count[ci]))
	}
	for _, c := range g.Cols {
		if e := f.byName[c.Name].Engine; c.Kind == "shared" && eventEngines[e] {
			g.Later = "Not drawn yet: which " + e + " topics each service writes and reads. doze sees that a service connects to " + c.Name + ", not what it publishes; that needs the module to tell doze which connection is which service."
			break
		}
	}
	return g
}

// taskCell is the tasks column's word for an app: the task when it is
// one, how many when more, marked as the last runs went.
func taskCell(f flow, ts []string) (string, string) {
	word, tone := "✓", "ok"
	for _, t := range ts {
		v := f.byName[t]
		switch {
		case v.State == "booting":
			if tone != "bad" {
				word, tone = "running", "cool"
			}
		case v.Task == nil:
			if tone == "ok" {
				word, tone = "not run", "dim"
			}
		case v.Task.Result == "failed":
			word, tone = "✗", "bad"
		}
	}
	name := ts[0]
	if len(ts) > 1 {
		name = strconv.Itoa(len(ts)) + " tasks"
	}
	return name + " " + word, tone
}

// Refs are the references doze.hcl writes, by the block that writes them
// and the block referred to: refs["orders"]["catalog"] is "CATALOG_URL =
// app.catalog.url"; refs["orders"]["after"] is the after line.
type Refs map[string]map[string]string

// Refer puts the file's own words in the grid's tooltips.
func (g *GridView) Refer(refs Refs) {
	if refs == nil {
		return
	}
	for ri := range g.Rows {
		for ci := range g.Rows[ri].Cells {
			t := g.Rows[ri].Cells[ci].Tip
			if t == nil {
				continue
			}
			if s := refs[t.From][t.To]; s != "" {
				t.Ref = s
			}
		}
	}
}
