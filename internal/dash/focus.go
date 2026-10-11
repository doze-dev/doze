package dash

import (
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/display"
	"github.com/doze-dev/doze/internal/journal"
)

// Focus is one instance in the whole frame. Every kind opens in the same frame
// and only the front tab differs (ViewMap.dc.html); the tabs below are the
// ones whose data the daemon supplies. The rest of a kind's tabs (env, config,
// history, data, console) are on their boards and not built.

type tab struct{ id, title string }

const (
	tabLogs     = "logs"
	tabOverview = "overview"
	tabLastRun  = "last run"
	tabWeb      = "web console"
)

// hasWeb says the instance has a console of its own to open.
func hasWeb(v control.InstanceView) bool { return v.Console != "" || v.WebURL != "" }

func webURL(v control.InstanceView) string { return orStr(v.Console, v.WebURL) }

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// tabs is what the instance opens on first, then the rest, by kind.
func (m *Model) tabs(v control.InstanceView) []tab {
	if v.HasConsole && v.Kind != "task" {
		var spec *control.ConsoleSpec
		if m.con != nil && m.con.inst == v.Name {
			spec = m.con.spec
		}
		out := consoleTabs(spec)
		if v.ConsoleFront != "console" && v.ConsoleFront != "" {
			out = append([]tab{{tabOverview, "overview"}}, out...)
		}
		return append(out, tab{tabLogs, "logs"})
	}
	switch {
	case v.Kind == "task":
		return []tab{{tabLastRun, "last run"}}
	case hasWeb(v):
		// ViewMap.dc.html: a module that ships a good UI (temporal) and a service
		// that was told where its own is both open on it.
		return []tab{{tabWeb, "web console"}, {tabLogs, "logs"}}
	case v.Kind == "module" || v.Kind == "":
		return []tab{{tabOverview, "overview"}, {tabLogs, "logs"}}
	}
	return []tab{{tabLogs, "logs"}}
}

// open shows the selected instance in the frame, on its front tab.
func (m *Model) open() {
	if v, ok := m.selected(); ok {
		m.page, m.tab = pageFocus, 0
		if v.HasConsole {
			m.startConsole(v.Name)
		}
	}
}

// level ranks, for the logs tab's "info and up".
const (
	lvDebug = iota
	lvInfo
	lvWarn
	lvError
)

var levelNames = []string{"debug and up", "info and up", "warnings and up", "errors only"}

func rank(line string) (int, string) {
	switch display.Level(line) {
	case "error":
		return lvError, "ERR"
	case "warn":
		return lvWarn, "WRN"
	case "info":
		return lvInfo, "INF"
	case "debug":
		return lvDebug, "DBG"
	}
	return lvInfo, ""
}

// stream is what the instance printed and what doze did about it, newest
// last, at min level and up. Untagged lines count as info.
func (m *Model) stream(v control.InstanceView, h, min int, onlyRun int) []string {
	s := m.st
	type item struct {
		at   time.Time
		text string
		seq  int
	}
	var items []item
	for i, l := range m.logs {
		if onlyRun > 0 && l.Run != onlyRun {
			continue
		}
		r, tag := rank(l.Line)
		if r < min {
			continue
		}
		text := l.Line
		if tag != "" {
			style := s.c
			switch tag {
			case "ERR":
				style = s.r
			case "WRN":
				style = s.y
			case "DBG":
				style = s.d
			}
			text = style.Render(tag) + " " + s.text.Render(levelWord.ReplaceAllString(text, ""))
		} else {
			text = s.text.Render(text)
		}
		items = append(items, item{l.Time, text, i})
	}
	for i, e := range m.events {
		if e.Instance != v.Name && e.Cause.By != v.Name {
			continue
		}
		style := s.a
		if e.Kind == journal.Crash || e.Kind == journal.GiveUp || e.Kind == journal.Failed {
			style = s.r
		}
		items = append(items, item{e.Time, style.Render("zzz " + e.Instance + " " + display.Event(e)), 1<<20 + i})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].at.Equal(items[j].at) {
			return items[i].seq < items[j].seq
		}
		return items[i].at.Before(items[j].at)
	})
	if len(items) == 0 {
		switch {
		case awake(v):
			return []string{s.d.Render("nothing printed yet")}
		case v.Kind == "task":
			return []string{s.d.Render("it has not run")}
		}
		return []string{s.d.Render("asleep: its output is kept; connecting to it wakes it")}
	}
	if len(items) > h {
		items = items[len(items)-h:]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		stamp := "        "
		if !it.at.IsZero() {
			stamp = it.at.In(m.loc).Format("15:04:05")
		}
		out = append(out, s.d.Render(stamp)+" "+it.text)
	}
	return out
}

// logLines is Home's bottom box: everything, newest last.
func (m *Model) logLines(v control.InstanceView, _, h int) []string {
	return m.stream(v, h, lvDebug, 0)
}

// --- the frame ---

const (
	pageHome = iota
	pageFocus
)

// focus draws the frame at w x h: the header with the tab bar, the tab, and
// the keys that work here.
func (m *Model) focus(w, h int) []string {
	v, ok := m.selected()
	if !ok {
		m.page = pageHome
		return m.home(w, h)
	}
	s := m.st
	ts := m.tabs(v)
	if m.tab >= len(ts) {
		m.tab = 0
	}
	cur := ts[m.tab]

	lk := s.look(v)
	pill := s.pillQ
	switch lk.word {
	case "awake", "done":
		pill = s.pillG
	case "dozing":
		pill = s.pillY
	case "waking":
		pill = s.pillC
	case "crashing", "failed":
		pill = s.pillR
	}
	kind := v.Kind
	if kind == "" || kind == "module" {
		kind = strings.TrimSpace(v.Engine + " " + strings.TrimPrefix(v.Version, "builtin"))
	}
	l := s.pill.Render("doze")
	if m.snap.Stack != "" {
		l += " " + s.d.Render(m.snap.Stack+" ›")
	}
	l += " " + s.b.Render(v.Name) + " " + s.d.Render(kind) + " " + pill.Render(lk.word)
	var bar []string
	for i, t := range ts {
		if i == m.tab {
			bar = append(bar, s.pillOn.Render(t.title))
		} else {
			bar = append(bar, s.d.Render(t.title))
		}
	}
	r := strings.Join(bar, "  ")
	gap := w - width(l) - width(r)
	if gap < 1 {
		gap = 1
	}
	out := []string{l + strings.Repeat(" ", gap) + r}

	bodyH := h - 2
	var body []string
	m.currentTabID = cur.id
	switch {
	case strings.HasPrefix(cur.id, "console:"):
		body = m.consoleTab(v, cur.id, w, bodyH)
	case cur.id == tabOverview:
		body = m.overview(v, w, bodyH)
	case cur.id == tabLastRun:
		body = m.lastRun(v, w, bodyH)
	case cur.id == tabWeb:
		body = m.web(v, w, bodyH)
	default:
		body = m.logsTab(v, w, bodyH)
	}
	for len(body) < bodyH {
		body = append(body, "")
	}
	out = append(out, body[:bodyH]...)
	return append(out, m.focusKeys(v, ts, cur))
}

func (m *Model) focusKeys(v control.InstanceView, ts []tab, cur tab) string {
	s := m.st
	var ks [][2]string
	if len(ts) > 1 {
		ks = append(ks, [2]string{"tab", "next tab"})
	}
	if cur.id == tabLogs {
		ks = append(ks, [2]string{"f", "level"})
	}
	if strings.HasPrefix(cur.id, "console:") && m.con != nil {
		ks = append(ks, [2]string{"↑↓", "select"})
		for _, a := range m.con.actions {
			ks = append(ks, [2]string{actionKey(a), strings.ToLower(a.Label)})
		}
	}
	if cur.id == tabWeb {
		ks = append(ks, [2]string{"⏎", "open in browser"})
	} else if hasWeb(v) {
		ks = append(ks, [2]string{"o", "open console"})
	}
	if cur.id != tabLogs && v.Kind != "task" {
		ks = append(ks, [2]string{"l", "logs"})
	}
	if v.Kind == "task" {
		ks = append(ks, [2]string{"r", "run now"})
	} else {
		ks = append(ks, [2]string{"w", "wake"}, [2]string{"s", "sleep"}, [2]string{"r", "restart"})
		if v.Kind == "module" || v.Kind == "" {
			ks = append(ks, [2]string{"p", "pin awake"})
		}
	}
	ks = append(ks, [2]string{":", "command"}, [2]string{"esc", "back to the stack"})
	var parts []string
	for _, k := range ks {
		parts = append(parts, s.cap(k[0], k[1]))
	}
	line := strings.Join(parts, "  ")
	if m.notice != "" {
		line += "   " + s.y.Render(m.notice)
	}
	return line
}

// --- tabs ---

// logsTab is the front tab of an app (View-App.dc.html): the stream, and
// beside it what it needs, this run, and the levels.
func (m *Model) logsTab(v control.InstanceView, w, h int) []string {
	s := m.st
	rw := 40
	if w < 100 {
		rw = 0
	}
	lw := w
	if rw > 0 {
		lw = w - rw - 1
	}
	var counts [4]int
	n := 0
	for _, l := range m.logs {
		r, _ := rank(l.Line)
		counts[r]++
		n++
	}
	lines := m.stream(v, h-2, m.level, 0)
	left := box(s.border, s.d.Render("following · "+levelNames[m.level]), s.d.Render(fmt.Sprintf("%d lines · this run", n)), lines, lw, h)
	if rw == 0 {
		return left
	}

	var needs []string
	for _, name := range v.Needs {
		o, ok := m.find(name)
		if !ok {
			needs = append(needs, s.d.Render("·  ")+s.text.Render(name))
			continue
		}
		lk := s.look(o)
		glyph, detail := lk.glyph, lk.word
		if lk.word == "awake" || lk.word == "done" {
			glyph = "✓"
		}
		if o.Task != nil {
			detail = "ran first"
		}
		needs = append(needs, lk.style.Render(glyph)+"  "+s.w.Render(fit(name, 8))+" "+s.d.Render(detail))
	}
	var col []string
	if len(needs) > 0 {
		col = append(col, box(s.border, s.a.Bold(true).Render("needs"), "", needs, rw, len(needs)+2)...)
	}
	label := func(k string) string { return s.d.Render(fit(k, 5)) + " " }
	run := []string{}
	if u := m.url(v); u != "" {
		run = append(run, label("url")+u)
	}
	if awake(v) && !v.StartedAt.IsZero() {
		up := ago(m.now.Sub(v.StartedAt))
		if v.PID > 0 {
			up += fmt.Sprintf(" · pid %d", v.PID)
		}
		run = append(run, label("up")+s.text.Render(up))
		sw := rw - 4 - 6 - 8
		run = append(run,
			label("cpu")+s.a.Render(spark(m.cpu[v.Name], sw))+" "+s.text.Render(fmt.Sprintf("%.1f%%", v.CPU)),
			label("mem")+s.a.Render(spark(m.ram[v.Name], sw))+" "+s.text.Render(mb(v.RAM)))
	}
	if len(run) > 0 {
		col = append(col, box(s.border, s.a.Bold(true).Render("this run"), "", run, rw, len(run)+2)...)
	}
	rest := h - len(col)
	if rest >= 3 {
		bar := func(tag string, st func(...string) string, c int) string {
			cells := c
			if cells > rw-4-5-6 {
				cells = rw - 4 - 5 - 6
			}
			return st(tag) + " " + st(strings.Repeat("▇", cells)) + strings.Repeat(" ", rw-4-4-cells-5) + right(s.d.Render(fmt.Sprint(c)), 5)
		}
		lv := []string{
			bar("ERR", s.r.Render, counts[lvError]),
			bar("WRN", s.y.Render, counts[lvWarn]),
			bar("INF", s.c.Render, counts[lvInfo]),
			s.d.Render("DBG") + " " + s.d.Render(fit("hidden", rw-4-4-5)) + right(s.d.Render(fmt.Sprint(counts[lvDebug])), 5),
		}
		col = append(col, box(s.border, s.a.Bold(true).Render("levels"), "", lv, rw, rest)...)
	}
	for len(col) < h {
		col = append(col, strings.Repeat(" ", rw))
	}
	var out []string
	for i := 0; i < h; i++ {
		c := ""
		if i < len(col) {
			c = col[i]
		}
		out = append(out, left[i]+" "+c)
	}
	return out
}

// overview is the front tab of a database module (Focus.dc.html): how to
// connect, why it is awake, what it costs, and the recent past.
func (m *Model) overview(v control.InstanceView, w, h int) []string {
	s := m.st
	label := func(k string) string { return s.d.Render(fit(k, 7)) + " " }

	conn := []string{}
	if v.URL != "" {
		conn = append(conn, label("url")+s.w.Render(v.URL))
	} else if a := address(v); a != "" {
		conn = append(conn, label("address")+s.w.Render(a))
	}
	if v.URL != "" {
		if a := address(v); a != "" {
			conn = append(conn, label("address")+s.text.Render(a))
		}
	}
	kind := v.Kind
	if kind == "" {
		kind = "module"
	}
	conn = append(conn, label("ref")+s.w.Render(kind+"."+v.Name+".url")+" "+s.d.Render("in doze.hcl"))
	if v.EnvVar != "" {
		conn = append(conn, label("env")+s.text.Render(v.EnvVar))
	}
	if strings.HasPrefix(v.Bind, "unix:") {
		conn = append(conn, label("socket")+s.d.Render(strings.TrimPrefix(v.Bind, "unix:")))
	}

	var why []string
	for _, hd := range v.Holders {
		switch hd.Kind {
		case "dependant":
			why = append(why, s.w.Render(fit(hd.Name, 8))+" "+s.text.Render("needs it"))
		case "connection":
			why = append(why, s.w.Render(fit("clients", 8))+" "+s.text.Render(fmt.Sprintf("%d connection%s", hd.Count, plural(hd.Count))))
		case "pin":
			why = append(why, s.w.Render(fit("you", 8))+" "+s.text.Render("pinned it awake"))
		}
	}
	if len(why) == 0 {
		if awake(v) {
			why = append(why, s.d.Render("nothing: it sleeps when the idle time is up"))
		} else {
			why = append(why, s.d.Render("asleep: it costs nothing until something connects"))
		}
	}
	idle := m.snap.IdleTimeout
	if v.IdleAfter > 0 {
		idle = v.IdleAfter
	}
	if idle > 0 && awake(v) {
		why = append(why, s.d.Render("sleeps "+clock(idle)+" after the last closes · ")+s.cap("p", "pin awake"))
	}

	lw := w * 55 / 100
	rw := w - lw - 1
	n := len(conn)
	if len(why) > n {
		n = len(why)
	}
	row1 := n + 2
	if row1 < 5 {
		row1 = 5
	}
	l := box(s.border, s.a.Bold(true).Render("connect"), "", conn, lw, row1)
	r := box(s.border, s.a.Bold(true).Render("awake because"), "", why, rw, row1)
	var out []string
	for i := 0; i < row1; i++ {
		out = append(out, l[i]+" "+r[i])
	}
	// what the module adds after the connect row
	out = append(out, m.extensions("connect", w)...)
	out = append(out, m.extensions("awake", w)...)

	// the chart fills the box, newest at the right edge, the figure under the edge
	sw := w - 4 - 8 - 2 - 8
	if sw < 8 {
		sw = 8
	}
	vit := []string{}
	if awake(v) {
		// each metric gets a band of its own, as tall as the screen can spare
		band := 1
		if h >= len(out)+3+3+2+4+2+len(vit)+10 {
			band = 3
		}
		metric := func(name string, vals []float64, floor float64, st lipgloss.Style, fig string, rows int) {
			if band == 1 {
				vit = append(vit, label(name)+st.Render(spark(vals, sw))+"  "+right(s.text.Render(fig), 8))
				return
			}
			ch := area(vals, sw, rows, floor)
			for i, line := range ch {
				left := label("")
				switch i {
				case 0:
					left = label(name)
				case 1:
					left = label("")
				}
				tail := strings.Repeat(" ", 10)
				if i == 0 {
					tail = "  " + right(s.text.Render(fig), 8)
				}
				vit = append(vit, left+st.Render(line)+tail)
			}
		}
		metric("cpu", m.cpu[v.Name], 5, s.a, fmt.Sprintf("%.1f%%", v.CPU), 3)
		metric("mem", m.ram[v.Name], 0, s.a, mb(v.RAM), 3)
		metric("conns", m.conns[v.Name], 8, s.g, fmt.Sprint(v.Conns), 2)
	} else {
		vit = append(vit, s.d.Render("asleep: no process, no memory"))
	}
	if v.DataDir != "" {
		vit = append(vit, label("data")+s.d.Render(tilde(v.DataDir)))
	}
	gap := 0
	if h > len(out)+len(vit)+2+8 {
		gap = 1 // air between the boxes, when there is room
	}
	if gap == 1 {
		out = append(out, "")
	}
	out = append(out, box(s.border, s.a.Bold(true).Render("vitals"), "", vit, w, len(vit)+2)...)
	out = append(out, m.extensions("vitals", w)...)
	if gap == 1 {
		out = append(out, "")
	}

	after := m.extensions("recent", w)
	rest := h - len(out) - len(after)
	if rest >= 3 {
		out = append(out, box(s.border, s.d.Render("recent"), "", m.stream(v, rest-2, lvDebug, 0), w, rest)...)
	}
	return append(out, after...)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// lastRun is the front tab of a task (View-Task.dc.html): how it last ran, and
// when it runs.
func (m *Model) lastRun(v control.InstanceView, w, h int) []string {
	s := m.st
	rw := 48
	if w < 100 {
		rw = 0
	}
	lw := w
	if rw > 0 {
		lw = w - rw - 1
	}
	caption := s.d.Render("has not run")
	var lines []string
	if v.Task != nil {
		res := s.g.Render("exit 0")
		if v.Task.Result == "failed" {
			res = s.r.Render(fmt.Sprintf("exit %d", v.Task.ExitCode))
		}
		took := (time.Duration(v.Task.TookMS) * time.Millisecond).Round(100 * time.Millisecond)
		caption = s.d.Render("ran ") + s.w.Render(v.Task.RanAt.In(m.loc).Format("15:04:05")) + s.d.Render(" · ") + res + s.d.Render(" · "+took.String())
		run := 0
		for _, l := range m.logs {
			if l.Run > run {
				run = l.Run
			}
		}
		lines = m.stream(v, h-2, lvDebug, run)
	} else {
		lines = []string{s.d.Render("it has not run yet · ") + s.cap("r", "run now")}
	}
	left := box(s.border, caption, "", lines, lw, h)
	if rw == 0 {
		return left
	}

	var when []string
	when = append(when, s.g.Render("✓")+"  "+s.text.Render("you ask · ")+s.a.Render("doze task "+v.Name))
	var users []string
	for _, o := range m.snap.Instances {
		for _, n := range o.Needs {
			if n == v.Name {
				users = append(users, o.Name)
			}
		}
	}
	sort.Strings(users)
	if len(users) > 0 {
		when = append(when, s.g.Render("✓")+"  "+s.text.Render("something that needs it starts · ")+s.d.Render(strings.Join(users, ", ")))
	}
	col := box(s.border, s.a.Bold(true).Render("runs when"), "", when, rw, len(when)+2)
	for len(col) < h {
		col = append(col, strings.Repeat(" ", rw))
	}
	var out []string
	for i := 0; i < h; i++ {
		out = append(out, left[i]+" "+col[i])
	}
	return out
}

// web is the front tab of an outside service with a console of its own
// (View-Web.dc.html).
func (m *Model) web(v control.InstanceView, w, h int) []string {
	s := m.st
	label := func(k string) string { return s.d.Render(fit(k, 10)) + " " }
	var top []string
	top = append(top, s.b.Render(webURL(v)))
	if awake(v) {
		top = append(top, s.d.Render("It is awake."))
	} else {
		top = append(top, s.d.Render("It is asleep. Opening the console wakes it first."))
	}
	top = append(top, "", s.pill.Render("⏎ Open in browser")+" "+s.pillQ.Render("w Just wake it"))
	out := box(s.borderOn, s.a.Bold(true).Render(v.Name+" has its own console"), "", top, w, len(top)+2)

	var facts []string
	if v.URL != "" {
		facts = append(facts, label("connect")+s.w.Render(v.URL))
	} else if a := address(v); a != "" {
		facts = append(facts, label("address")+s.w.Render(a))
	}
	if v.EnvVar != "" {
		facts = append(facts, label("env")+s.text.Render(v.EnvVar))
	}
	var users []string
	for _, o := range m.snap.Instances {
		for _, n := range o.Needs {
			if n == v.Name {
				users = append(users, o.Name)
			}
		}
	}
	if len(users) > 0 {
		facts = append(facts, label("used by")+s.text.Render(strings.Join(users, ", ")))
	}
	facts = append(facts, label("at rest")+s.text.Render("0 MB"))
	rest := h - len(out)
	if rest >= len(facts)+2 {
		lines := append([]string{}, facts...)
		if room := rest - 2 - len(facts) - 2; room > 0 {
			lines = append(lines, "", s.d.Render("logs · last lines"))
			lines = append(lines, m.stream(v, room, lvDebug, 0)...)
		}
		out = append(out, box(s.border, s.a.Bold(true).Render("what doze can still tell you"), "", lines, w, rest)...)
	}
	return out
}

// openURL opens an address with the system's opener, off the UI goroutine.
func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		if err := cmd.Start(); err != nil {
			return noticeMsg("could not open " + url + ": " + err.Error())
		}
		go func() { _ = cmd.Wait() }()
		return noticeMsg("opened " + url)
	}
}

type noticeMsg string

// focusKey handles a key inside the frame. handled says it was one of its own;
// the rest (verbs, quit) fall through to the shared keys.
func (m *Model) focusKey(k string) (tea.Cmd, bool) {
	v, ok := m.selected()
	if !ok {
		m.page = pageHome
		return nil, true
	}
	ts := m.tabs(v)
	if m.tab < len(ts) && strings.HasPrefix(ts[m.tab].id, "console:") && m.con != nil {
		if body, ok := bodyOf(m.con.spec, ts[m.tab].id); ok {
			if cmd, handled := m.consoleKey(k, body); handled {
				return cmd, true
			}
		}
	}
	switch k {
	case "esc", "left", "h", "backspace":
		m.page, m.tab = pageHome, 0
		m.stopConsole()
		return nil, true
	case "tab":
		m.tab = (m.tab + 1) % len(ts)
		return nil, true
	case "shift+tab":
		m.tab = (m.tab + len(ts) - 1) % len(ts)
		return nil, true
	case "1", "2", "3":
		if i := int(k[0] - '1'); i < len(ts) {
			m.tab = i
		}
		return nil, true
	case "l":
		for i, t := range ts {
			if t.id == tabLogs {
				m.tab = i
			}
		}
		return nil, true
	case "f":
		m.level = (m.level + 1) % len(levelNames)
		return nil, true
	case "o":
		if hasWeb(v) {
			return openURL(webURL(v)), true
		}
		m.notice = v.Name + " has no console of its own"
		return nil, true
	case "enter":
		if ts[m.tab].id == tabWeb {
			return openURL(webURL(v)), true
		}
		return nil, true
	case "p":
		if v.Kind == "module" || v.Kind == "" {
			return m.verb(control.VerbPin), true
		}
		return nil, true
	case "r":
		if v.Kind == "task" {
			return m.verb(control.VerbTask), true
		}
	case "up", "down", "j", "k", "home", "end", "g", "G", "right":
		return nil, true // the frame is one instance; the list is behind esc
	}
	return nil, false
}
