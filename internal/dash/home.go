package dash

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/display"
)

// group is a heading on the stack box, and what belongs under it (Main.dc.html).
type group struct {
	title, what string
	kinds       []string
}

var groups = []group{
	{"APPS", "your code", []string{"app"}},
	{"MODULES", "from the doze registry", []string{"module", ""}},
	{"OUTSIDE", "brought in by you", []string{"service"}},
	{"TASKS", "run once, then out of the way", []string{"task"}},
}

// row is one instance in display order.
type row struct {
	v     control.InstanceView
	group int
}

// rows lists the stack: by group, then by name.
func rows(snap control.Snapshot) []row {
	var out []row
	for gi, g := range groups {
		var in []control.InstanceView
		for _, v := range snap.Instances {
			for _, k := range g.kinds {
				if v.Kind == k {
					in = append(in, v)
					break
				}
			}
		}
		sort.Slice(in, func(i, j int) bool { return in[i].Name < in[j].Name })
		for _, v := range in {
			out = append(out, row{v: v, group: gi})
		}
	}
	return out
}

// look is how an instance's state reads: the word, the glyph and its colour.
type look struct {
	word, glyph string
	style       lipgloss.Style
}

func (s styles) look(v control.InstanceView) look {
	switch {
	case v.Disabled:
		return look{"disabled", "⊘", s.d}
	case v.Task != nil && v.State != "booting":
		if v.Task.Result == "failed" {
			return look{"failed", "✗", s.r}
		}
		return look{"done", "✓", s.g}
	case v.Kind == "task" && v.State != "booting":
		return look{"ready", "▸", s.d}
	}
	switch v.State {
	case "active":
		return look{"awake", "●", s.g}
	case "idle":
		return look{"dozing", "◐", s.y}
	case "booting":
		return look{"waking", "◌", s.c}
	case "crashing":
		return look{"crashing", "✗", s.r}
	case "failed", "error":
		return look{"failed", "✗", s.r}
	}
	return look{"asleep", "·", s.d}
}

func awake(v control.InstanceView) bool { return v.State == "active" || v.State == "idle" }

func troubled(v control.InstanceView) bool {
	return v.State == "crashing" || v.State == "failed" || v.State == "error" || (v.Task != nil && v.Task.Result == "failed")
}

// mb is a size as the board writes it: 84 MB, 1.2 GB.
func mb(b int64) string {
	switch {
	case b <= 0:
		return "0"
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	}
	return fmt.Sprintf("%d MB", (b+(1<<19))>>20)
}

// clock is a duration as m:ss, the way the board counts down.
func clock(d time.Duration) string {
	d = d.Round(time.Second)
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// what is the third column: what the thing is, then what it is doing about it.
func (m *Model) what(v control.InstanceView) string {
	var base string
	switch v.Kind {
	case "app", "service":
		base = short(v.Run)
	case "task":
		if v.Task != nil {
			base = fmt.Sprintf("ran %s ago in %s", ago(m.now.Sub(v.Task.RanAt)), (time.Duration(v.Task.TookMS) * time.Millisecond).Round(time.Millisecond))
			if v.Task.Result == "failed" {
				base = fmt.Sprintf("exit %d · %s", v.Task.ExitCode, base)
			}
		} else {
			base = "not run yet"
		}
	default:
		base = strings.TrimSpace(v.Engine + " " + strings.TrimPrefix(v.Version, "builtin"))
	}
	var extra string
	switch {
	case troubled(v) && v.Task == nil:
		// What went wrong is the long part, so it goes last and is the part that
		// gets cut.
		base = display.FirstLine(v.LastError)
		if v.RestartMax > 0 {
			base, extra = fmt.Sprintf("try %d of %d", v.RestartCount, v.RestartMax), display.FirstLine(v.LastError)
		}
	case v.State == "booting":
		if n := m.firstUnmet(v); n != "" {
			base, extra = "waiting on "+n, ""
		} else if by := holderNamed(v); by != "" {
			extra = "woken by " + by
		}
	case v.State == "idle" && !v.IdleSince.IsZero():
		after := m.snap.IdleTimeout
		if v.IdleAfter > 0 {
			after = v.IdleAfter
		}
		if left := after - m.now.Sub(v.IdleSince); left > 0 {
			extra = "sleeps in " + clock(left)
		}
	}
	switch {
	case base == "":
		return extra
	case extra == "":
		return base
	}
	return base + " · " + extra
}

// short reads a command line the way the board does: what it runs, not the
// whole line. A container command says which image.
func short(run string) string {
	f := strings.Fields(run)
	if len(f) >= 2 && (f[0] == "docker" || f[0] == "podman") && f[1] == "run" {
		image := ""
		for _, a := range f[2:] {
			if !strings.HasPrefix(a, "-") && !strings.Contains(a, ":") && !strings.Contains(a, "$") {
				image = a
			}
		}
		if image == "" {
			return f[0] + " run"
		}
		return f[0] + " run " + image
	}
	return run
}

// firstUnmet is the first thing v needs that is not up yet.
func (m *Model) firstUnmet(v control.InstanceView) string {
	for _, n := range v.Needs {
		if o, ok := m.find(n); ok && !awake(o) && !(o.Task != nil && o.Task.Result == "done") {
			return n
		}
	}
	return ""
}

func holderNamed(v control.InstanceView) string {
	for _, h := range v.Holders {
		if h.Kind == "dependant" {
			return h.Name
		}
	}
	return ""
}

// mem is the memory column: a figure, a dash while it wakes, 0 asleep.
func mem(v control.InstanceView) string {
	switch {
	case v.Kind == "task":
		return ""
	case v.State == "booting":
		return "—"
	case awake(v):
		return mb(v.RAM)
	}
	return "0"
}

// --- the screen ---

// narrow is the width below which Home is one column (Narrow.dc.html).
const narrow = 100

// View draws Home at w x h.
func (m *Model) home(w, h int) []string {
	if w < narrow {
		return m.homeNarrow(w, h)
	}
	s := m.st
	out := []string{m.header(w)}

	bodyH := h - 2 // the header and the key bar
	// a third for the list, which says what each is and how it is; the rest is
	// for the one selected, which has the room for what it runs and how it is doing
	lw := w / 3
	if lw < 40 {
		lw = 40
	}
	rw := w - lw - 1
	if rw < 30 {
		rw = 30
		lw = w - rw - 1
	}

	left := box(s.borderOn, s.a.Bold(true).Render("stack"), "", m.stack(lw-4, bodyH-2, true, false), lw, bodyH)

	v, ok := m.selected()
	var top, bottom []string
	if ok {
		topLines, caption, rcap, st := m.detail(v, rw-4)
		topH := len(topLines) + 2
		if topH > bodyH-4 {
			topH = bodyH - 4
		}
		gap := 0
		if bodyH-topH > 8 {
			gap = 1 // air between the boxes, when there is room
		}
		top = box(st, caption, rcap, topLines, rw, topH)
		if gap == 1 {
			top = append(top, strings.Repeat(" ", rw))
		}
		bottom = box(s.border, s.d.Render("logs · following"), "", m.logLines(v, rw-4, bodyH-topH-gap-2), rw, bodyH-topH-gap)
	} else {
		top = box(s.border, s.d.Render("nothing selected"), "", nil, rw, bodyH)
	}
	right := append(top, bottom...)

	for i := 0; i < bodyH; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, l+" "+r)
	}
	out = append(out, m.keys(w))
	return out
}

func (m *Model) header(w int) string {
	s := m.st
	var awakeN, dozing, asleep, need int
	var ram int64
	for _, r := range rows(m.snap) {
		switch {
		case troubled(r.v):
			need++
		case r.v.State == "active":
			awakeN++
		case r.v.State == "idle":
			dozing++
		case r.v.Kind != "task":
			asleep++
		}
		ram += r.v.RAM
	}
	l := s.pill.Render("doze")
	if m.snap.Stack != "" {
		l += " " + s.b.Render(m.snap.Stack)
	}
	if m.snap.Dir != "" {
		l += " " + s.d.Render(tilde(m.snap.Dir))
	}
	var parts []string
	if need > 0 {
		parts = append(parts, s.pillR.Render(fmt.Sprintf("%d needs you", need)))
	}
	parts = append(parts, s.g.Render("●")+fmt.Sprintf(" %d awake", awakeN))
	if dozing > 0 {
		parts = append(parts, s.y.Render("◐")+fmt.Sprintf(" %d dozing", dozing))
	}
	parts = append(parts, s.d.Render(fmt.Sprintf("· %d asleep", asleep)))
	parts = append(parts, s.b.Render(mb(ram))+" "+s.d.Render("resident"))
	r := strings.Join(parts, "  ")
	if width(l)+width(r)+2 > w && m.snap.Dir != "" {
		l = s.pill.Render("doze")
		if m.snap.Stack != "" {
			l += " " + s.b.Render(m.snap.Stack)
		}
	}
	gap := w - width(l) - width(r)
	if gap < 1 {
		gap = 1
	}
	return l + strings.Repeat(" ", gap) + r
}

// stack is the left box: groups of rows in the board's five columns.
func (m *Model) stack(w, h int, subtitles, describe bool) []string {
	s := m.st
	rs := rows(m.snap)
	nameW := 8
	for _, r := range rs {
		if n := width(r.v.Name); n > nameW {
			nameW = n
		}
	}
	if nameW > 14 {
		nameW = 14
	}
	stateW := 7
	for _, r := range rs {
		if n := width(s.look(r.v).word); n > stateW {
			stateW = n
		}
	}
	memW := 7
	descW := w - 2 - 2 - nameW - 2 - 2 - stateW - 2 - 2 // gaps, padding
	descW -= memW + 2
	if descW < 8 {
		descW = 8
	}
	if !describe {
		// the list is a third of the screen: name, state, memory; what it runs
		// is on the right, where there is room
		descW = 0
		nameW = max(nameW, w-2-2-2-stateW-2-memW-2-2)
		nameW = min(nameW, 24)
	}

	var out []string
	last := -1
	for _, r := range rs {
		if r.group != last {
			if last != -1 && len(rs)+len(groups)*2 < h {
				out = append(out, "")
			}
			g := groups[r.group]
			hd := s.a.Bold(true).Render(g.title)
			if subtitles {
				hd += "  " + s.d.Render(g.what)
			}
			out = append(out, " "+hd)
			last = r.group
		}
		lk := s.look(r.v)
		name, what, state, memS := fit(r.v.Name, nameW), fit(m.what(r.v), descW), fit(lk.word, stateW), right(mem(r.v), memW)
		var line string
		if r.v.Name == m.sel {
			bg := s.on(lipgloss.NewStyle())
			gap := bg.Render("  ")
			desc := ""
			if describe {
				desc = s.on(s.text).Render(what) + gap
			}
			line = bg.Render(" ") + s.on(lk.style).Render(lk.glyph) + gap + s.on(s.b).Render(name) + gap + desc +
				s.on(lk.style).Render(state) + gap + s.on(s.w).Render(memS) + bg.Render(" ")
		} else {
			nameS := s.w.Render(name)
			if r.v.State != "active" && r.v.State != "idle" && r.v.State != "booting" && r.v.Kind != "task" {
				nameS = s.text.Render(name)
			}
			whatS := s.d.Render(what)
			memStyled := s.text.Render(memS)
			if !awake(r.v) {
				memStyled = s.d.Render(memS)
			}
			desc := ""
			if describe {
				desc = whatS + "  "
			}
			line = " " + lk.style.Render(lk.glyph) + "  " + nameS + "  " + desc + lk.style.Render(state) + "  " + memStyled + " "
		}
		out = append(out, line)
	}
	return out
}

// detail is the right box's content for the selected instance: its caption,
// right caption and border, and the rows between (Main.dc.html, api).
func (m *Model) detail(v control.InstanceView, w int) (lines []string, caption, rcap string, border lipgloss.Style) {
	s := m.st
	lk := s.look(v)
	border = s.border
	pill := s.pillQ
	switch lk.word {
	case "awake", "done":
		pill = s.pillG
	case "dozing":
		pill = s.pillY
	case "waking":
		pill = s.pillC
	case "crashing", "failed":
		pill, border = s.pillR, s.borderBad
	}
	caption = s.b.Render(v.Name) + " " + pill.Render(lk.word)

	var facts []string
	if awake(v) && !v.StartedAt.IsZero() {
		facts = append(facts, "up "+ago(m.now.Sub(v.StartedAt)))
	}
	if v.PID > 0 && awake(v) {
		facts = append(facts, fmt.Sprintf("pid %d", v.PID))
	}
	if (v.Kind == "app" || v.Kind == "service") && (awake(v) || v.State == "crashing") {
		facts = append(facts, fmt.Sprintf("%d restarts", v.RestartCount))
	}
	rcap = s.d.Render(strings.Join(facts, " · "))

	label := func(k string) string { return s.d.Render(fit(k, 6)) + "  " }
	if troubled(v) && v.LastError != "" {
		if v.RestartMax > 0 {
			lines = append(lines, label("try")+s.text.Render(fmt.Sprintf("%d of %d", v.RestartCount, v.RestartMax)))
		}
		for i, l := range strings.Split(strings.TrimSpace(v.LastError), "\n") {
			if i >= 4 {
				break
			}
			if i == 0 {
				lines = append(lines, s.r.Bold(true).Render(l))
			} else {
				lines = append(lines, s.d.Render(l))
			}
		}
		if v.State == "crashing" && !v.NextRestart.IsZero() {
			if left := v.NextRestart.Sub(m.now).Round(time.Second); left > 0 {
				rcap = s.d.Render("next try in ") + s.w.Render(fmt.Sprintf("%d s", int(left.Seconds())))
			}
		}
		lines = append(lines, "", s.cap("r", "retry now")+"  "+s.cap("l", "full logs"))
	}
	if v.Kind == "task" {
		if v.Task == nil {
			lines = append(lines, label("last")+s.d.Render("has not run"))
		} else {
			lines = append(lines, label("ran")+s.text.Render(fmt.Sprintf("%s ago in %s", ago(m.now.Sub(v.Task.RanAt)), (time.Duration(v.Task.TookMS)*time.Millisecond).Round(time.Millisecond))))
			if v.Task.Why != "" {
				lines = append(lines, label("why")+s.text.Render(v.Task.Why))
			}
		}
		if len(v.Needs) > 0 {
			lines = append(lines, label("needs")+m.needs(v))
		}
		return lines, caption, rcap, border
	}

	if troubled(v) && v.LastError != "" {
		return lines, caption, rcap, border
	}
	if u := m.url(v); u != "" {
		lines = append(lines, label("url")+u)
	}
	if len(v.Needs) > 0 {
		lines = append(lines, label("needs")+m.needs(v))
	}
	// what it is and how it is: the list has no room for these
	switch v.Kind {
	case "app", "service":
		if r := short(v.Run); r != "" {
			lines = append(lines, label("runs")+s.text.Render(r))
		}
	default:
		if e := strings.TrimSpace(v.Engine + " " + strings.TrimPrefix(v.Version, "builtin")); e != "" {
			lines = append(lines, label("is")+s.text.Render(e))
		}
	}
	if st := m.hint(v); st != "" {
		lines = append(lines, label("now")+s.d.Render(st))
	}
	if awake(v) {
		ts := []trace{
			{"cpu", m.cpu[v.Name], 5, false, pct, fmt.Sprintf("%.1f%%", v.CPU), s.a},
			{"mem", m.ram[v.Name], 0, true, bytesLabel, mb(v.RAM), s.a},
		}
		lines = append(lines, "")
		lines = append(lines, m.charts(ts, w, 3)...)
	} else if v.State != "booting" && !troubled(v) {
		lines = append(lines, label("")+s.d.Render("asleep: it costs nothing until something connects"))
	}
	return lines, caption, rcap, border
}

// url is where to reach it: the connection string of a module, the name of an
// app with the port it listens on behind it.
func (m *Model) url(v control.InstanceView) string {
	s := m.st
	switch {
	case v.URL != "":
		return s.w.Render(v.URL)
	case v.Resource != "":
		return s.w.Render(v.Resource)
	case v.Domain != "":
		out := s.w.Render("http://" + v.Domain)
		if _, port, ok := strings.Cut(v.Bind, ":"); ok && !strings.HasPrefix(v.Bind, "unix:") {
			out += s.d.Render(" → :" + port)
		}
		return out
	case v.Endpoint != "":
		return s.w.Render(v.Endpoint)
	}
	return ""
}

// needs is each thing v waits on, with how it is.
func (m *Model) needs(v control.InstanceView) string {
	s := m.st
	var parts []string
	for _, n := range v.Needs {
		o, ok := m.find(n)
		if !ok {
			parts = append(parts, s.d.Render("· "+n))
			continue
		}
		lk := s.look(o)
		glyph := lk.glyph
		if lk.word == "awake" || lk.word == "done" {
			glyph = "✓"
		}
		parts = append(parts, lk.style.Render(glyph)+" "+s.text.Render(n))
	}
	return strings.Join(parts, "  ")
}

var levelWord = regexp.MustCompile(`(?i)^(level=)?(info|inf|warn|warning|wrn|error|err|debug|dbg)\b[:\s]*`)

// keys is the bar along the bottom: the keys that do something.
func (m *Model) keys(w int) string {
	s := m.st
	var parts []string
	for _, k := range []struct{ key, label string }{
		{"↑↓", "move"}, {"⏎", "focus"}, {"w", "wake"}, {"s", "sleep"}, {"r", "restart"}, {":", "command"}, {"q", "quit"},
	} {
		parts = append(parts, s.cap(k.key, k.label))
	}
	line := strings.Join(parts, "  ")
	if m.notice != "" {
		line += "   " + s.y.Render(m.notice)
	}
	return line
}

// homeNarrow is Home at 80 x 24 (Narrow.dc.html): one column, no boxes.
func (m *Model) homeNarrow(w, h int) []string {
	s := m.st
	var awakeN, dozing, asleep int
	var ram int64
	for _, r := range rows(m.snap) {
		switch {
		case r.v.State == "active":
			awakeN++
		case r.v.State == "idle":
			dozing++
		case r.v.Kind != "task":
			asleep++
		}
		ram += r.v.RAM
	}
	l := s.pill.Render("doze")
	if m.snap.Stack != "" {
		l += " " + s.b.Render(m.snap.Stack)
	}
	r := s.g.Render("●") + fmt.Sprintf(" %d  ", awakeN) + s.y.Render("◐") + fmt.Sprintf(" %d  ", dozing) + s.d.Render(fmt.Sprintf("· %d", asleep)) + "  " + s.b.Render(mb(ram))
	gap := w - width(l) - width(r)
	if gap < 1 {
		gap = 1
	}
	out := []string{l + strings.Repeat(" ", gap) + r, ""}
	body := m.stack(w-2, h-4, false, true)
	out = append(out, body...)
	for len(out) < h-2 {
		out = append(out, "")
	}
	if n := len(m.events); n > 0 {
		e := m.events[n-1]
		out = append(out[:h-2], s.d.Render(e.Time.In(m.loc).Format("15:04:05"))+" "+s.a.Render("zzz "+e.Instance+" "+display.Event(e)))
	}
	for len(out) < h-1 {
		out = append(out, "")
	}
	return append(out[:h-1], m.keys(w))
}

// tilde writes a path under the home directory with a ~.
func tilde(dir string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" && strings.HasPrefix(dir, h) {
		return "~" + strings.TrimPrefix(dir, h)
	}
	return dir
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

// hint is the part of what a row says that is about its moment: that it waits
// on something, that it sleeps soon, who woke it.
func (m *Model) hint(v control.InstanceView) string {
	switch {
	case v.State == "booting":
		if n := m.firstUnmet(v); n != "" {
			return "waiting on " + n
		}
		if by := holderNamed(v); by != "" {
			return "woken by " + by
		}
	case v.State == "idle" && !v.IdleSince.IsZero():
		after := m.snap.IdleTimeout
		if v.IdleAfter > 0 {
			after = v.IdleAfter
		}
		if left := after - m.now.Sub(v.IdleSince); left > 0 {
			return "sleeps in " + clock(left)
		}
	}
	return ""
}
