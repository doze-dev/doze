package dash

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/display"
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
	if m.sortBy != sortName && m.now.Before(m.holdUntil) && m.heldBy == m.sortBy && len(m.held) > 0 {
		at := map[string]int{}
		for i, n := range m.held {
			at[n] = i
		}
		pos := func(r row) int {
			if i, ok := at[r.v.Name]; ok {
				return i
			}
			return len(at)
		}
		sort.SliceStable(rs, func(i, j int) bool {
			if rs[i].group != rs[j].group {
				return rs[i].group < rs[j].group
			}
			return pos(rs[i]) < pos(rs[j])
		})
		return rs
	}
	defer func() {
		m.held, m.heldBy = m.held[:0], m.sortBy
		for _, r := range rs {
			m.held = append(m.held, r.v.Name)
		}
	}()
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

// because is what an instance's state is down to, in a few words: what holds
// it awake, what it waits for, when it will sleep, why it is not well.
func (m *Model) because(v control.InstanceView) string {
	switch {
	case v.Kind == "task":
		switch {
		case v.State == "booting":
			if !v.StartedAt.IsZero() && m.now.After(v.StartedAt) {
				return "running now · " + ago(m.now.Sub(v.StartedAt))
			}
			return "running now"
		case v.Task == nil:
			return "not run yet"
		case v.Task.Result == "failed":
			return fmt.Sprintf("exit %d · ran %s ago", v.Task.ExitCode, ago(m.now.Sub(v.Task.RanAt)))
		}
		return fmt.Sprintf("ran %s ago in %s", ago(m.now.Sub(v.Task.RanAt)), (time.Duration(v.Task.TookMS) * time.Millisecond).Round(100*time.Millisecond))
	case troubled(v):
		if v.RestartMax > 0 {
			out := fmt.Sprintf("try %d of %d", v.RestartCount, v.RestartMax)
			if left := v.NextRestart.Sub(m.now).Round(time.Second); left > 0 {
				out += fmt.Sprintf(" · next in %ds", int(left.Seconds()))
			}
			return out
		}
		return display.FirstLine(v.LastError)
	case v.State == "booting":
		if h := m.hint(v); h != "" {
			return h
		}
		return "starting"
	}
	if awake(v) {
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
		for _, o := range m.snap.Instances { // what declares it a dependency
			if v.State == "idle" {
				break // a dozing instance is held by nothing, whatever declared it
			}
			for _, n := range o.Needs {
				if n == v.Name && awake(o) && !strings.Contains(strings.Join(by, " "), o.Name+" needs") {
					by = append(by, o.Name+" needs it")
				}
			}
		}
		switch {
		case len(by) > 0:
			return strings.Join(by, " · ")
		case v.State == "idle":
			if h := m.hint(v); h != "" {
				return h
			}
			return "idle: sleeps soon"
		case v.Kind == "module":
			return "nothing holds it"
		}
		return "runs until stopped"
	}
	switch v.Kind {
	case "module":
		return "wakes on a connection"
	}
	return "starts when needed"
}

// runs is what an instance is running: the command of an app or a service, the
// address of a module.
func (m *Model) runs(v control.InstanceView) string {
	switch v.Kind {
	case "module":
		if a := address(v); a != "" {
			return a
		}
		return strings.TrimSpace(v.Engine + " " + strings.TrimPrefix(v.Version, "builtin"))
	}
	return short(v.Run)
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
	p := profileOf(w, h)
	// Wide: the list at its natural width, and the selected instance beside it
	// (boards/W-Home.dc.html). tw is the list's width, dw the detail's.
	tw, dw := w, 0
	if p.wc >= widthWide && h >= 24 && len(rs) > 0 {
		tw = min(max(w*55/100, 100), 128)
		dw = w - tw - 1
	}
	split := dw > 0
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
	var awakeN, dozing, waking, asleep, need, done, ready, running, conns int
	var memShares, cpuShares []share
	var ram int64
	var cpu float64
	for _, r := range rs {
		v := r.v
		switch {
		case troubled(v):
			need++
		case v.State == "active":
			awakeN++
		case v.State == "idle":
			dozing++
		case v.State == "booting" && v.Kind == "task":
			running++
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
			memShares = append(memShares, share{v.Name, float64(v.RAM)})
			cpuShares = append(cpuShares, share{v.Name, v.CPU})
			ram += v.RAM
			cpu += v.CPU
			conns += v.Conns
		}
	}
	{
		l := s.pill.Render("doze")
		if m.snap.Stack != "" {
			l += " " + s.b.Render(m.snap.Stack)
		}
		r := s.b.Render("≈ "+mb(ram)) + " " + s.d.Render("resident")
		if need > 0 {
			r = s.pillR.Render(fmt.Sprintf("%d needs you", need)) + "  " + r
		}
		// the folder only if it fits beside the rest, cut with an ellipsis to what is left
		if room := w - width(l) - width(r) - 3; m.snap.Dir != "" && room >= 12 {
			l += " " + s.d.Render(ansiTruncate(tilde(m.snap.Dir), room))
		}
		out = append(out, l+strings.Repeat(" ", max(w-width(l)-width(r), 1))+r)
	}
	half := (w - 4) / 2
	mc := min(half, 64)
	if p.wc >= widthWide {
		mc = min(half, 100) // the key can name more of them
	}
	if profileOf(w, 0).wc == widthCompact {
		mc = min(w-2, 64)
	}
	// with room, each bar has the line that names its colours under it
	tableRows := len(rs)
	for g := -1; ; {
		next := -1
		for _, r := range rs {
			if r.group > g && (next < 0 || r.group < next) {
				next = r.group
			}
		}
		if next < 0 {
			break
		}
		g = next
		tableRows++ // each group's heading is a row
	}
	keyed := p.keyedMeters(tableRows)
	memTotal := mb(ram)
	if ram == 0 {
		memTotal = "0 MB"
	}
	left := m.composition("mem", memTotal, memShares, 0, mc, memUnit(float64(ram)), func(v float64) string { return mb(int64(v)) }, keyed)
	left = append(left, m.composition("cpu", cores(cpu), cpuShares, 0.5, mc, coresUnit, cores, keyed)...)
	left = append(left, s.a.Bold(true).Render(fit("conns", 6))+s.text.Render(fmt.Sprint(conns))+s.d.Render(" open across the stack"))
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
		s.a.Bold(true).Render("tasks") + " " + s.g.Render("✓") + fmt.Sprintf(" %d done   ", done) + s.d.Render(fmt.Sprintf("▸ %d ready", ready)) + runningNote(s, running),
	}
	if need > 0 {
		rightCol[0] = s.a.Bold(true).Render("state") + " " + s.r.Render(fmt.Sprintf("%d needs you", need)) + "   " + state
	}
	if w < 120 {
		// not room for two columns: the meters and one line of state
		out = append(out, left[:len(left)-1]...)
		out = append(out, rightCol[0])
	} else {
		for i := 0; i < len(left); i++ {
			r := ""
			if i < len(rightCol) {
				r = rightCol[i]
			}
			l := left[i]
			out = append(out, l+strings.Repeat(" ", max(2+half-width(l), 2))+r)
		}
	}
	out = append(out, "")
	if len(rs) == 0 {
		out = out[:1] // nothing to measure: the title, and then what to do
	}

	// --- the table's columns, fewest that leave the last room to say something ---
	// a name is given what the window can spare, to a limit past which it is
	// only the beginning that tells instances apart
	nameCap := p.nameCap()
	nameW := 8
	for _, r := range rs {
		nameW = max(nameW, min(width(r.v.Name), nameCap))
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
	// at the grand size there is room to show where memory has been going
	if p.wc == widthGrand {
		trend := tcol{"TREND", 12, false, func(v control.InstanceView) string {
			if !awake(v) {
				return ""
			}
			return spark(m.ram[v.Name], 12)
		}}
		all = append(all[:6], append([]tcol{trend}, all[6:]...)...)
	}
	// what goes first when the window is narrow: the kind (the group says it),
	// then the counts, then the uptime, then the engine. The last two columns
	// (because, runs) are worth more than any of them.
	drop := []string{"TREND", "KIND", "CONN", "CPU%", "UP", "ENGINE"}
	needRest := 64
	fixed := func(cs []tcol) int {
		n := 2 // the table's left and right padding
		for _, c := range cs {
			n += c.w + 2
		}
		return n
	}
	// a wide window's list is as wide as its content needs, every column and what
	// they say, and what is left is the detail's
	if split {
		natWhy, natRuns := 0, 0
		for _, r := range rs {
			natWhy = max(natWhy, width(m.because(r.v)))
			natRuns = max(natRuns, width(m.runs(r.v)))
		}
		needRest = min(needRest, min(natWhy, 38)+2+min(natRuns, 48))
		nat := fixed(all) + min(natWhy, 38) + 2 + min(natRuns, 48)
		tw = min(max(nat, 100), max(w*60/100, 100))
		dw = w - tw - 1
	}
	cols := all
	for _, d := range drop {
		if tw-fixed(cols) >= needRest {
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
	// what is left is two columns: why it is as it is, and what it runs. They are
	// two things, so they have two headings and a gap, and when there is no room
	// for both, the second goes.
	rest := max(tw-fixed(cols), 8)
	whyW, runsW := rest, 0
	if rest >= 60 {
		whyW = min(max(rest*38/100, 24), 38)
		runsW = rest - whyW - 2
		// a column stops at its widest cell: RUNS beside BECAUSE, not across the room
		natWhy, natRuns := 0, 0
		for _, r := range rs {
			natWhy = max(natWhy, width(m.because(r.v)))
			natRuns = max(natRuns, width(m.runs(r.v)))
		}
		if natRuns > 0 && natWhy+2+natRuns <= rest {
			whyW, runsW = max(natWhy, 12), natRuns
		} else if natRuns > 0 {
			whyW = min(whyW, max(natWhy, 12))
			runsW = min(rest-whyW-2, natRuns)
		}
	}

	// --- body ---
	type line struct {
		text string
		name string // the instance, "" for a heading
		head string // the heading of the group it is in, as drawn
	}
	var body []line
	curHead := ""
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
			body = append(body, line{" " + hd, "", " " + hd})
			curHead = " " + hd
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
			case "TREND":
				st = s.a
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
		because := s.d
		if troubled(v) {
			because = s.r
		} else if v.State == "idle" || v.State == "booting" {
			because = s.y
		}
		put(fit(m.because(v), whyW), because)
		if runsW > 0 {
			put("  ", lipgloss.NewStyle())
			put(fit(m.runs(v), runsW), s.text)
		}
		put(" ", lipgloss.NewStyle())
		if pad := tw - width(b.String()); pad > 0 {
			put(strings.Repeat(" ", pad), lipgloss.NewStyle())
		}
		body = append(body, line{b.String(), v.Name, curHead})
	}

	// --- heights: the table as tall as its rows, the logs the rest ---
	room := h - len(out) - 1 - 1 // the table's header row, and the key bar
	// The logs are what the table is for looking at, so they are not squeezed:
	// at least eight lines, and a third of the room on a tall window. The table
	// takes the rest and scrolls inside it, with a line saying what is out of
	// sight. At 40 lines that is about twenty rows of the table; at 24, about
	// eight.
	minLogs := min(max(10, room*35/100), 16) // the box: eight lines and its borders
	if room < 20 {
		minLogs = max(4, room/2)
	}
	if v, ok := m.selected(); ok && troubled(v) {
		// what went wrong needs the room: a trace is not three lines. The table
		// scrolls to keep the row in view.
		minLogs = max(minLogs, min(room/2, 16))
	}
	if split {
		minLogs = 0 // the detail is beside the list, not under it
	}
	tableH := min(len(body), max(room-minLogs, 3))
	scrolls := len(body) > tableH
	if scrolls {
		tableH = max(tableH-1, 2) // a line of the room says what is out of sight
	}
	logsH := room - tableH
	if scrolls {
		logsH--
	}
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
		above, below := m.listTop, len(body)-m.listTop-tableH
		body = body[m.listTop : m.listTop+tableH]
		// scrolled into a group: its heading stays, in place of the first row
		if len(body) > 2 && body[0].name != "" && body[0].name != m.sel && body[0].head != "" {
			body[0] = line{body[0].head, "", body[0].head}
		}
		note := " "
		if above > 0 {
			note += fmt.Sprintf("↑ %d above", above)
		}
		if below > 0 {
			if above > 0 {
				note += " · "
			}
			note += fmt.Sprintf("↓ %d more", below)
		}
		m.tableNote = s.d.Render(note + "   / filter to narrow · o sort")
	} else {
		m.listTop = 0
		m.tableNote = ""
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
	hput(fit("BECAUSE", whyW))
	if runsW > 0 {
		hput("  ")
		hput(fit("RUNS", runsW))
	}
	hput(" ")
	if pad := tw - width(ansi.Strip(hb.String())); pad > 0 {
		hput(strings.Repeat(" ", pad))
	}
	if len(rs) > 0 {
		out = append(out, hb.String())
	}

	m.hit = m.hit[:0]
	leftAt := len(out)
	m.hitY0, m.hitX1 = len(out), tw
	for _, l := range body {
		out = append(out, l.text)
		m.hit = append(m.hit, l.name)
	}
	if m.tableNote != "" {
		out = append(out, m.tableNote)
	}

	// --- the selected instance: what went wrong, or what it is saying ---
	if v, ok := m.selected(); ok && split {
		left := out[leftAt-1:] // the header row and what follows
		out = out[:leftAt-1]
		P := room + 1 // the header row and the rows under it
		m.area = logArea{}
		right := m.detailPane(v, dw, P)
		if m.area.x1 > 0 {
			m.area.y0 += leftAt - 1 // the pane's first row is the list's header row
			m.area.x0, m.area.x1 = tw+1, w
		}
		// a short list leaves room under it: what has been happening
		if free := min(P-len(left)-1, 16); free >= 6 {
			left = append(left, "")
			left = append(left, m.activityBox(tw, free)...)
		}
		for i := 0; i < P; i++ {
			l := strings.Repeat(" ", tw)
			if i < len(left) {
				l = left[i] + strings.Repeat(" ", max(tw-width(left[i]), 0))
			}
			r := ""
			if i < len(right) {
				r = right[i]
			}
			out = append(out, l+" "+r)
		}
	} else if v, ok := m.selected(); ok {
		m.area = logArea{}
		box := m.logsBox(v, w, logsH)
		if m.area.x1 > 0 {
			m.area.y0 += len(out)
		}
		out = append(out, box...)
	} else {
		msg := []string{s.d.Render("Nothing is declared in this stack yet."), "",
			s.text.Render("  doze add postgres") + s.d.Render("      add a module from the registry"),
			s.text.Render("  doze init") + s.d.Render("             write a doze.hcl from this project"), "",
			s.d.Render("or edit doze.hcl: the dash picks the change up.")}
		cap := "empty"
		switch {
		case m.waiting:
			// not yet told what is in the stack: not the same as nothing in it
			msg, cap = []string{s.d.Render("Asking the daemon what is in this stack…")}, "connecting"
			if m.conn == connDown {
				msg = []string{s.d.Render("The daemon is not responding, so the stack is not known yet."), "", s.d.Render("  doze up      start it"), s.d.Render("  doze status  see what is running")}
			}
		case m.filter != "":
			msg, cap = []string{s.d.Render("Nothing matches " + m.filter + ". esc clears the filter.")}, "filter"
		}
		out = append(out, box(s.border, s.d.Render(cap), "", msg, w, logsH)...)
	}
	out = append(out, m.keys(w))
	return out
}
