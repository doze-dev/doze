package dash

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/doze-dev/doze/internal/control"
)

// Home as a table, the way htop is one (boards/V-C.dc.html): meters across the
// top for what the whole stack costs, then every instance as a row with real
// columns, and under it the logs of the one selected.

// Sort orders. The groups stay: apps, modules, outside, tasks; a sort orders
// the rows within each.
const (
	sortName = iota
	sortMem
	sortState
	sortModes
)

var sortTitles = [sortModes]string{"name", "mem", "state"}

// matches says whether v is in what the filter asks for: every word of it is
// in the name, kind, engine or state.
func (m *Model) matches(v control.InstanceView) bool {
	hay := strings.ToLower(strings.Join([]string{v.Name, v.Kind, v.Engine, v.Version, m.st.look(v).word}, " "))
	for _, w := range strings.Fields(strings.ToLower(m.filter)) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// tree orders rs as the dependency tree: what nothing needs is a root, and what
// it needs is under it, once, at the first place it is needed. depth is how far
// down each is.
func treeOrder(rs []row) (out []row, depth map[string]int) {
	byName := map[string]row{}
	for _, r := range rs {
		byName[r.v.Name] = r
	}
	needed := map[string]bool{}
	for _, r := range rs {
		for _, n := range r.v.Needs {
			if _, ok := byName[n]; ok {
				needed[n] = true
			}
		}
	}
	depth = map[string]int{}
	seen := map[string]bool{}
	var walk func(r row, d int)
	walk = func(r row, d int) {
		if seen[r.v.Name] {
			return
		}
		seen[r.v.Name] = true
		depth[r.v.Name] = d
		out = append(out, r)
		for _, n := range r.v.Needs {
			if c, ok := byName[n]; ok {
				walk(c, d+1)
			}
		}
	}
	for _, r := range rs {
		if !needed[r.v.Name] {
			walk(r, 0)
		}
	}
	for _, r := range rs { // a cycle, or what only a cycle needs
		walk(r, 0)
	}
	return out, depth
}

// ordered is rows, in the order the table shows them.
func (m *Model) ordered() []row {
	rs := rows(m.snap)
	if m.filter != "" {
		var keep []row
		for _, r := range rs {
			if m.matches(r.v) {
				keep = append(keep, r)
			}
		}
		rs = keep
	}
	if m.tree {
		rs, _ = treeOrder(rs)
		return rs
	}
	if m.sortBy == sortName {
		return rs
	}
	rank := func(v control.InstanceView) int {
		switch {
		case troubled(v):
			return 0
		case v.State == "booting":
			return 1
		case v.State == "active":
			return 2
		case v.State == "idle":
			return 3
		}
		return 4
	}
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if a.group != b.group {
			return a.group < b.group
		}
		switch m.sortBy {
		case sortMem:
			return a.v.RAM > b.v.RAM
		case sortState:
			return rank(a.v) < rank(b.v)
		}
		return false
	})
	return rs
}

// segColours are what each awake instance is drawn in on the meters, in the
// order they are listed.
var segColours = []string{hexGood, hexCool, hexAccent, hexWarn, "#FF9E64", hexBad}

func (m *Model) segStyle(i int) lipgloss.Style {
	if m.st.plain {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(segColours[i%len(segColours)]))
}

// meter draws [|||||||     value] cells wide, one run of bars per segment.
func (m *Model) meter(name string, segs []float64, scale float64, val string, cells int) string {
	s := m.st
	inner := cells - len(name) - 1 - 2
	if inner < 8 {
		inner = 8
	}
	var bars strings.Builder
	used := 0
	for i, v := range segs {
		n := 0
		if scale > 0 {
			n = int(v/scale*float64(inner) + 0.5)
		}
		if v > 0 && n == 0 {
			n = 1
		}
		n = min(n, inner-len(val)-used)
		if n <= 0 {
			continue
		}
		bars.WriteString(m.segStyle(i).Render(strings.Repeat("|", n)))
		used += n
	}
	pad := max(inner-used-len(val), 0)
	return s.a.Bold(true).Render(name) + " " + s.d.Render("[") + bars.String() + strings.Repeat(" ", pad) + s.w.Render(val) + s.d.Render("]")
}

// why is the last column: what keeps it awake, what it waits for, what it runs.
func (m *Model) why(v control.InstanceView) string {
	switch {
	case troubled(v):
		return m.what(v)
	case v.State == "booting", v.State == "idle":
		if h := m.hint(v); h != "" {
			return h
		}
	}
	if awake(v) {
		if v.Kind == "module" {
			var by []string
			for _, h := range v.Holders {
				switch h.Kind {
				case "dependant":
					by = append(by, h.Name+" needs it")
				case "connection":
					by = append(by, fmt.Sprintf("%d connection%s", h.Count, plural(h.Count)))
				case "pin":
					by = append(by, "pinned awake")
				}
			}
			if len(by) > 0 {
				return strings.Join(by, " · ")
			}
			return "nothing holds it: it sleeps when idle"
		}
		if v.Kind == "app" || v.Kind == "service" {
			return short(v.Run)
		}
	}
	if v.Kind == "module" && !awake(v) && v.State != "booting" {
		return "asleep: costs nothing until something connects"
	}
	return m.what(v)
}

type tcol struct {
	title string
	w     int
	right bool
	cell  func(v control.InstanceView) string
}

// homeTable draws Home at w x h.
func (m *Model) homeTable(w, h int) []string {
	s := m.st
	rs := m.ordered()
	if len(rs) > 0 {
		found := false
		for _, r := range rs {
			found = found || r.v.Name == m.sel
		}
		if !found {
			m.sel = rs[0].v.Name // the filter took the selection away
		}
	}
	var depth map[string]int
	if m.tree {
		_, depth = treeOrder(rs)
	}

	// --- header and meters ---
	var out []string
	var awakeN, dozing, waking, asleep, need, done, ready, conns int
	var segMem, segCPU []float64
	var ram, budget int64
	var cpu float64
	for _, r := range rs {
		v := r.v
		peak := v.RAM
		for _, x := range m.ram[v.Name] {
			peak = max(peak, int64(x))
		}
		budget += peak
		switch {
		case troubled(v):
			need++
		case v.State == "active":
			awakeN++
		case v.State == "idle":
			dozing++
		case v.State == "booting":
			waking++
		case v.Task != nil && v.Task.Result == "done":
			done++
		case v.Kind == "task":
			ready++
		default:
			asleep++
		}
		if awake(v) {
			segMem = append(segMem, float64(v.RAM))
			segCPU = append(segCPU, v.CPU)
			ram += v.RAM
			cpu += v.CPU
			conns += v.Conns
		}
	}
	// against what everything has been seen to take, when something sleeps
	memVal := mb(ram)
	if budget > ram {
		memVal += " of ~" + mb(budget)
	}
	{
		l := s.pill.Render("doze")
		if m.snap.Stack != "" {
			l += " " + s.b.Render(m.snap.Stack)
		}
		if m.snap.Dir != "" && w > 60 {
			l += " " + s.d.Render(tilde(m.snap.Dir))
		}
		r := s.b.Render(mb(ram)) + " " + s.d.Render("resident")
		if need > 0 {
			r = s.pillR.Render(fmt.Sprintf("%d needs you", need)) + "  " + r
		}
		out = append(out, l+strings.Repeat(" ", max(w-width(l)-width(r), 1))+r)
	}
	half := (w - 4) / 2
	mc := min(half, 64)
	if w < 100 {
		mc = min(w-2, 64)
	}
	left := []string{
		m.meter("mem", segMem, float64(max(budget, ram)), memVal, mc),
		m.meter("cpu", segCPU, 100, fmt.Sprintf("%.1f%%", cpu), mc),
		s.a.Bold(true).Render("conns") + " " + s.text.Render(fmt.Sprint(conns)) + s.d.Render(" open across the stack"),
	}
	state := s.g.Render("●") + fmt.Sprintf(" %d awake", awakeN)
	if dozing > 0 {
		state += "   " + s.y.Render("◐") + fmt.Sprintf(" %d dozing", dozing)
	}
	if waking > 0 {
		state += "   " + s.c.Render("◌") + fmt.Sprintf(" %d waking", waking)
	}
	state += "   " + s.d.Render(fmt.Sprintf("· %d asleep", asleep))
	rightCol := []string{
		s.a.Bold(true).Render("state") + " " + state,
		s.a.Bold(true).Render("sleep") + " " + s.text.Render("after "+clock(m.snap.IdleTimeout)) + s.d.Render(" with nothing holding it"),
		s.a.Bold(true).Render("tasks") + " " + s.g.Render("✓") + fmt.Sprintf(" %d done   ", done) + s.d.Render(fmt.Sprintf("▸ %d ready", ready)),
	}
	if need > 0 {
		rightCol[0] = s.a.Bold(true).Render("state") + " " + s.r.Render(fmt.Sprintf("%d needs you", need)) + "   " + state
	}
	if w < 100 {
		// not room for two columns: the meters and one line of state
		out = append(out, left[0], left[1], rightCol[0])
	} else {
		for i := 0; i < 3; i++ {
			l, r := left[i], rightCol[i]
			out = append(out, l+strings.Repeat(" ", max(2+half-width(l), 2))+r)
		}
	}
	out = append(out, "")

	// --- the table's columns, fewest that leave the last room to say something ---
	nameW := 8
	for _, r := range rs {
		nameW = max(nameW, min(width(r.v.Name), 16))
	}
	all := []tcol{
		{"", 2, false, func(v control.InstanceView) string { return s.look(v).glyph }},
		{"NAME", nameW, false, func(v control.InstanceView) string { return v.Name }},
		{"KIND", 8, false, func(v control.InstanceView) string { return v.Kind }},
		{"ENGINE", 14, false, func(v control.InstanceView) string {
			if v.Kind == "module" {
				return strings.TrimSpace(v.Engine + " " + strings.TrimPrefix(v.Version, "builtin"))
			}
			return v.Engine
		}},
		{"STATE", 8, false, func(v control.InstanceView) string { return s.look(v).word }},
		{"MEM", 8, true, mem},
		{"CPU%", 5, true, func(v control.InstanceView) string {
			if awake(v) {
				return fmt.Sprintf("%.1f", v.CPU)
			}
			return ""
		}},
		{"CONN", 4, true, func(v control.InstanceView) string {
			if awake(v) {
				return fmt.Sprint(v.Conns)
			}
			return ""
		}},
		{"UP", 6, true, func(v control.InstanceView) string {
			switch {
			case awake(v) && !v.StartedAt.IsZero():
				return ago(m.now.Sub(v.StartedAt))
			}
			return ""
		}},
	}
	drop := []string{"UP", "KIND", "CONN", "CPU%", "ENGINE"}
	fixed := func(cs []tcol) int {
		n := 2 // the table's left and right padding
		for _, c := range cs {
			n += c.w + 2
		}
		return n
	}
	cols := all
	for _, d := range drop {
		if w-fixed(cols) >= 28 {
			break
		}
		var keep []tcol
		for _, c := range cols {
			if c.title != d {
				keep = append(keep, c)
			}
		}
		cols = keep
	}
	whyW := max(w-fixed(cols), 8)

	// --- body ---
	type line struct {
		text string
		name string // the instance, "" for a heading
	}
	var body []line
	last := -1
	for _, r := range rs {
		if r.group != last && !m.tree {
			g := groups[r.group]
			n, mbs := 0, int64(0)
			for _, o := range rs {
				if o.group == r.group {
					n++
					mbs += o.v.RAM
				}
			}
			hd := s.a.Bold(true).Render(g.title) + " " + s.d.Render(fmt.Sprintf("%d", n))
			if mbs > 0 {
				hd += s.d.Render(" · " + mb(mbs))
			}
			hd += "  " + s.d.Render(g.what)
			body = append(body, line{" " + hd, ""})
			last = r.group
		}
		v := r.v
		lk := s.look(v)
		sel := v.Name == m.sel
		var b strings.Builder
		put := func(text string, st lipgloss.Style) {
			if sel {
				st = s.onPick(st)
			}
			b.WriteString(st.Render(text))
		}
		gap := "  "
		put(" ", lipgloss.NewStyle())
		for i, c := range cols {
			t := c.cell(v)
			if c.title == "NAME" && m.tree && depth[v.Name] > 0 {
				t = strings.Repeat("  ", depth[v.Name]-1) + "└ " + t
			}
			var cell string
			if c.right {
				cell = right(t, c.w)
			} else {
				cell = fit(t, c.w)
			}
			st := s.text
			switch c.title {
			case "":
				st = lk.style
			case "NAME":
				st = s.w
				if sel {
					st = s.b
				}
			case "STATE":
				st = lk.style
			case "MEM":
				st = s.w
				if !awake(v) {
					st = s.d
				}
			case "KIND", "UP", "CPU%", "CONN":
				st = s.d
				if c.title == "CPU%" || c.title == "CONN" {
					st = s.text
				}
			}
			put(cell, st)
			if i < len(cols)-1 || whyW > 0 {
				put(gap, lipgloss.NewStyle())
			}
		}
		put(fit(m.why(v), whyW), s.d)
		put(" ", lipgloss.NewStyle())
		body = append(body, line{b.String(), v.Name})
	}

	// --- heights: the table as tall as its rows, the logs the rest ---
	room := h - len(out) - 1 - 1 // the table's header row, and the key bar
	minLogs := 6
	if room < 16 {
		minLogs = 3
	}
	tableH := min(len(body), max(room-minLogs, 3))
	logsH := room - tableH
	if len(body) > tableH {
		// a window that keeps the selection in view and moves only when it must
		at := 0
		for i, l := range body {
			if l.name == m.sel {
				at = i
			}
		}
		if at < m.listTop {
			m.listTop = at
		}
		if at >= m.listTop+tableH {
			m.listTop = at - tableH + 1
		}
		m.listTop = min(max(m.listTop, 0), len(body)-tableH)
		body = body[m.listTop : m.listTop+tableH]
	} else {
		m.listTop = 0
	}

	// the header row, with the sorted column marked
	var hb strings.Builder
	hs := s.on(s.w.Bold(true))
	hput := func(t string) { hb.WriteString(hs.Render(t)) }
	hput(" ")
	for _, c := range cols {
		t := c.title
		if (c.title == "MEM" && m.sortBy == sortMem) || (c.title == "STATE" && m.sortBy == sortState) || (c.title == "NAME" && m.sortBy == sortName) {
			t += "▾"
		}
		if c.right {
			hput(right(t, c.w))
		} else {
			hput(fit(t, c.w))
		}
		hput("  ")
	}
	hput(fit("WHY AWAKE · WHAT IT RUNS", whyW))
	hput(" ")
	out = append(out, hb.String())

	m.hit = m.hit[:0]
	m.hitY0, m.hitX1 = len(out), w
	for _, l := range body {
		out = append(out, l.text)
		m.hit = append(m.hit, l.name)
	}

	// --- the selected instance: what went wrong, or what it is saying ---
	if v, ok := m.selected(); ok {
		border := s.border
		caption := s.b.Render(v.Name) + " " + s.d.Render("· logs · following")
		var lines []string
		if troubled(v) {
			var rcap string
			lines, _, rcap, border = m.detail(v, w-4, logsH-2)
			caption = s.b.Render(v.Name) + " " + s.pillR.Render("crashing")
			if rcap != "" {
				caption += "  " + rcap
			}
			room := logsH - 2 - len(lines)
			if room > 2 {
				lines = append(lines, "")
				lines = append(lines, m.stream(v, room-1, lvDebug, 0)...)
			}
		} else {
			lines = m.stream(v, logsH-2, lvDebug, 0)
		}
		out = append(out, box(border, caption, "", lines, w, logsH)...)
	} else {
		out = append(out, box(s.border, s.d.Render("nothing selected"), "", nil, w, logsH)...)
	}
	if len(rs) == 0 {
		out = append(out, s.d.Render(" nothing matches "+m.filter))
	}
	out = append(out, m.keys(w))
	return out
}
