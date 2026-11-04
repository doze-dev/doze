package dash

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/display"
)

// The words on this page (the groups, the state words, what a row says) are
// display's: the app says the same ones. This file draws them.

// row is one instance in display order, as display lists the stack.
type row = display.Row

// look is how an instance's state reads: the word, the glyph and its colour.
type look struct {
	word, glyph string
	style       lipgloss.Style
}

func (s styles) look(v control.InstanceView) look {
	lk := display.LookOf(v)
	return look{lk.Word, lk.Glyph, s.tone(lk.Tone)}
}

// tone is the style a display tone is drawn in: the board's colour classes.
func (s styles) tone(t display.Tone) lipgloss.Style {
	switch t {
	case display.ToneGood:
		return s.g
	case display.ToneWarn:
		return s.y
	case display.ToneCool:
		return s.c
	case display.ToneBad:
		return s.r
	case display.ToneDim:
		return s.d
	case display.ToneBright:
		return s.w
	}
	return s.text
}

// --- the screen ---

// narrow is the width below which Home is the plain one-column list; from here
// up it is the table, which drops columns as the width goes (Narrow.dc.html).
const narrow = 60

// View draws Home at w x h.
func (m *Model) home(w, h int) []string {
	if w < narrow {
		return m.homeNarrow(w, h)
	}
	return m.homeTable(w, h)
}

// stack is the left box: groups of rows in the board's five columns.
func (m *Model) stack(w, h int, subtitles, describe bool) []string {
	s := m.st
	rs := display.Rows(m.snap)
	nameW := 8
	for _, r := range rs {
		if n := width(r.V.Name); n > nameW {
			nameW = n
		}
	}
	if nameW > 14 {
		nameW = 14
	}
	stateW := 7
	for _, r := range rs {
		if n := width(s.look(r.V).word); n > stateW {
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

	var out, names []string // names: which instance each line is, for the mouse
	last := -1
	for _, r := range rs {
		if r.Group != last {
			if last != -1 && len(rs)+len(display.Groups)*2 < h {
				out, names = append(out, ""), append(names, "")
			}
			g := display.Groups[r.Group]
			hd := s.a.Bold(true).Render(g.Title)
			if subtitles {
				hd += "  " + s.d.Render(g.What)
			}
			out, names = append(out, " "+hd), append(names, "")
			last = r.Group
		}
		lk := s.look(r.V)
		name, what, state, memS := fit(r.V.Name, nameW), fit(display.What(r.V, m.snap, m.now), descW), fit(lk.word, stateW), right(display.Mem(r.V), memW)
		var line string
		if r.V.Name == m.sel {
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
			if r.V.State != "active" && r.V.State != "idle" && r.V.State != "booting" && r.V.Kind != "task" {
				nameS = s.text.Render(name)
			}
			whatS := s.d.Render(what)
			memStyled := s.text.Render(memS)
			if !display.AwakeNow(r.V) {
				memStyled = s.d.Render(memS)
			}
			desc := ""
			if describe {
				desc = whatS + "  "
			}
			line = " " + lk.style.Render(lk.glyph) + "  " + nameS + "  " + desc + lk.style.Render(state) + "  " + memStyled + " "
		}
		out, names = append(out, line), append(names, r.V.Name)
	}
	if len(out) > h && h > 0 {
		// more rows than room: a window that keeps the selection in view and
		// moves only when it must
		at := 0
		for i, n := range names {
			if n == m.sel {
				at = i
			}
		}
		if at < m.listTop {
			m.listTop = at
		}
		if at >= m.listTop+h {
			m.listTop = at - h + 1
		}
		m.listTop = min(max(m.listTop, 0), len(out)-h)
		out, names = out[m.listTop:m.listTop+h], names[m.listTop:m.listTop+h]
	} else {
		m.listTop = 0
	}
	m.hit = names
	return out
}

// detail is what a crashed or task instance says about itself, for the box
// under the table: its caption, right caption and border, and the rows between
// (Crash.dc.html). Others have no detail; the table row says it.
func (m *Model) detail(v control.InstanceView, w, h int) (lines []string, caption, rcap string, border lipgloss.Style) {
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
	if display.AwakeNow(v) && !v.StartedAt.IsZero() {
		facts = append(facts, "up "+display.Ago(m.now.Sub(v.StartedAt)))
	}
	if v.PID > 0 && display.AwakeNow(v) {
		facts = append(facts, fmt.Sprintf("pid %d", v.PID))
	}
	if (v.Kind == "app" || v.Kind == "service") && (display.AwakeNow(v) || v.State == "crashing") {
		facts = append(facts, fmt.Sprintf("%d restarts", v.RestartCount))
	}
	rcap = s.d.Render(strings.Join(facts, " · "))

	label := func(k string) string { return s.d.Render(fit(k, 6)) + "  " }
	if display.Troubled(v) && v.LastError != "" {
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
		lines = append(lines, "", s.cap("r", "retry now")+"  "+s.cap("l", "logs"))
	}
	if v.Kind == "task" {
		if v.Task == nil {
			lines = append(lines, label("last")+s.d.Render("has not run"))
		} else {
			lines = append(lines, label("ran")+s.text.Render(fmt.Sprintf("%s ago in %s", display.Ago(m.now.Sub(v.Task.RanAt)), (time.Duration(v.Task.TookMS)*time.Millisecond).Round(time.Millisecond))))
			if v.Task.Why != "" {
				lines = append(lines, label("why")+s.text.Render(v.Task.Why))
			}
		}
		if len(v.Needs) > 0 {
			lines = append(lines, label("needs")+m.needs(v))
		}
		return lines, caption, rcap, border
	}

	return lines, caption, rcap, border
}

// url is where to reach it, drawn: the address bright, and the port a name
// stands in front of dim after it.
func (m *Model) url(v control.InstanceView) string {
	s := m.st
	addr, port := display.URL(v)
	if addr == "" {
		return ""
	}
	out := s.w.Render(addr)
	if port != "" {
		out += s.d.Render(" → " + port)
	}
	return out
}

// needs is each thing v waits on, with how it is, drawn.
func (m *Model) needs(v control.InstanceView) string {
	s := m.st
	var parts []string
	for _, n := range display.Needs(v, m.snap) {
		if !n.Known {
			parts = append(parts, s.d.Render("· "+n.Name))
			continue
		}
		parts = append(parts, s.tone(n.Look.Tone).Render(n.Glyph)+" "+s.text.Render(n.Name))
	}
	return strings.Join(parts, "  ")
}

var levelWord = regexp.MustCompile(`(?i)^(level=)?(info|inf|warn|warning|wrn|error|err|debug|dbg)\b[:\s]*`)

// keys is the bar along the bottom: the keys that do something.
func (m *Model) keys(w int) string {
	s := m.st
	extra := m.noticeText()
	switch {
	case m.selecting:
		return m.selectBar()
	case m.filtering:
		return s.cap("⏎", "keep") + "  " + s.cap("esc", "clear") + "  " + s.a.Bold(true).Render("filter") + " " + s.w.Render(m.filter) + s.a.Render("▏")
	case m.filter != "":
		return s.cap("esc", "clear filter") + "  " + s.a.Bold(true).Render("filter") + " " + s.w.Render(m.filter) + "   " + m.bar(m.homeHints(), w-width(m.filter)-24, extra)
	}
	return m.bar(m.homeHints(), w, extra)
}

// homeNarrow is Home at 80 x 24 (Narrow.dc.html): one column, no boxes.
func (m *Model) homeNarrow(w, h int) []string {
	s := m.st
	var awakeN, dozing, asleep int
	var ram int64
	for _, r := range display.Rows(m.snap) {
		switch {
		case r.V.State == "active":
			awakeN++
		case r.V.State == "idle":
			dozing++
		case r.V.Kind != "task":
			asleep++
		}
		ram += r.V.RAM
	}
	l := s.pill.Render("doze")
	if m.snap.Stack != "" {
		l += " " + s.b.Render(m.snap.Stack)
	}
	r := s.g.Render("●") + fmt.Sprintf(" %d  ", awakeN) + s.y.Render("◐") + fmt.Sprintf(" %d  ", dozing) + s.d.Render(fmt.Sprintf("· %d", asleep)) + "  " + s.b.Render(display.MB(ram))
	gap := w - width(l) - width(r)
	if gap < 1 {
		gap = 1
	}
	out := []string{l + strings.Repeat(" ", gap) + r, ""}
	body := m.stack(w-2, h-4, false, true)
	m.hitY0, m.hitX1 = 2, w
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

// click handles a left click on Home: on a row of the stack it selects that
// row, and a second click on the same row soon after opens it, as enter does.
func (m *Model) click(at tea.Mouse) {
	if m.selecting {
		m.selecting, m.dragging, m.frozen = false, false, nil // a click elsewhere leaves it
	}
	if at.Button != tea.MouseLeft || m.page != pageHome || m.modal != nil || at.X >= m.hitX1 {
		return
	}
	i := at.Y - m.hitY0
	if i < 0 || i >= len(m.hit) || m.hit[i] == "" {
		return
	}
	name := m.hit[i]
	if name == m.sel && name == m.clickName && m.now.Sub(m.clickAt) < 700*time.Millisecond {
		m.open()
		m.clickName = ""
		return
	}
	m.sel, m.clickName, m.clickAt = name, name, m.now
}

// yankURL puts an instance's address on the clipboard, over OSC 52, so it
// works across ssh. It says what it copied, or that there was nothing to.
func (m *Model) yankURL(v control.InstanceView, file bool) tea.Cmd {
	text := display.AddressOf(v)
	if text == "" {
		m.say(v.Name + " has no address to copy")
		return nil
	}
	return m.out(text, text, v.Name+"-address", file)
}

// yankLines puts the lines the logs tab is showing on the clipboard.
func (m *Model) yankLines(v control.InstanceView, file bool) tea.Cmd {
	lines := m.stream(v, 500, m.level, 0)
	var out []string
	for _, l := range lines {
		if t := strings.TrimRight(ansi.Strip(l), " "); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		m.say("nothing to copy yet")
		return nil
	}
	return m.out(strings.Join(out, "\n"), lineWord(len(out)), v.Name+"-logs", file)
}
