package dash

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/display"
	"github.com/doze-dev/doze/internal/journal"
)

// The logs of the whole stack, merged and in time order, each line marked with
// where it came from (boards/Logs.dc.html). It is its own page, opened by l on
// Home, with its own subscription to every instance's lines.

const allKept = 800

// openLogs shows the whole stack's logs: the lines the dash has kept of every
// instance, merged, and then the new ones as they come.
func (m *Model) openLogs() {
	m.page = pageLogs
	m.search, m.searching, m.paused = "", false, false
	m.all, m.pinned = nil, false
	for _, lines := range m.cache {
		m.all = append(m.all, lines...)
	}
	sort.SliceStable(m.all, func(i, j int) bool { return m.all[i].Time.Before(m.all[j].Time) })
	if len(m.all) > allKept {
		m.all = m.all[len(m.all)-allKept:]
	}
}

func (m *Model) closeLogs() { m.page = pageHome }

// stackLogsKey handles a key on the page; it says whether it was handled.
func (m *Model) stackLogsKey(k string) (tea.Cmd, bool) {
	switch k {
	case "esc", "left", "h", "backspace", "q":
		m.closeLogs()
		return nil, true
	case "/":
		m.searching = true
		return nil, true
	case "up", "k", "down", "j", "pgup", "pgdown", "ctrl+u", "ctrl+d", "g", "home", "G", "end":
		m.scrollKey(k)
		return nil, true
	case "f":
		m.level = (m.level + 1) % len(levelNames)
		m.pinned = false
		return nil, true
	case "t":
		m.timeRel = !m.timeRel
		return nil, true
	case "v":
		m.beginSelect(len(m.shown) - 1)
		return nil, true
	case " ", "space":
		m.paused = !m.paused
		return nil, true
	case "y":
		lines := m.stackLines(500)
		var out []string
		for _, l := range lines {
			out = append(out, strings.TrimRight(ansi.Strip(l), " "))
		}
		if len(out) == 0 {
			m.notice = "nothing to copy yet"
			return nil, true
		}
		m.notice = fmt.Sprintf("copied %d lines", len(out))
		return tea.SetClipboard(strings.Join(out, "\n")), true
	case ":":
		m.modal = &modal{kind: modalPalette}
		return nil, true
	}
	return nil, true // the page has no cursor: the other keys do nothing here
}

// stackLines is the newest n lines that pass the level and the search, oldest
// first, each as the page draws it.
func (m *Model) stackLines(n int) []string {
	s := m.st
	type item struct {
		at   time.Time
		inst string
		text string
		seq  int
	}
	names := map[string]int{}
	nameW := 4
	for i, r := range m.ordered() {
		names[r.v.Name] = i
		nameW = max(nameW, min(width(r.v.Name), 16))
	}
	q := strings.ToLower(m.search)
	var items []item
	for i, l := range m.all {
		r, tag := rank(l.Line)
		if r < m.level {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(l.Line), q) {
			continue
		}
		m.query = m.search
		text := m.mark(l.Line, s.text)
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
			text = style.Render(tag) + " " + m.mark(levelWord.ReplaceAllString(l.Line, ""), s.text)
		}
		m.query = ""
		items = append(items, item{l.Time, l.Instance, text, i})
	}
	for i, e := range m.events {
		line := "zzz " + e.Instance + " " + display.Event(e)
		if q != "" && !strings.Contains(strings.ToLower(line), q) {
			continue
		}
		style := s.a
		if e.Kind == journal.Crash || e.Kind == journal.GiveUp || e.Kind == journal.Failed {
			style = s.r
		}
		items = append(items, item{e.Time, e.Instance, style.Render(line), 1<<20 + i})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].at.Equal(items[j].at) {
			return items[i].seq < items[j].seq
		}
		return items[i].at.Before(items[j].at)
	})
	if n > 0 && len(items) > n {
		items = items[len(items)-n:]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		src := s.d.Render(fit(it.inst, nameW))
		if i, ok := names[it.inst]; ok && !s.plain {
			src = m.segStyle(i).Render(fit(it.inst, nameW))
		}
		out = append(out, s.d.Render(m.stamp(it.at))+" "+src+" "+it.text)
	}
	return out
}

// stackLogs draws the page at w x h.
func (m *Model) stackLogs(w, h int) []string {
	s := m.st
	l := s.pill.Render("doze")
	if m.snap.Stack != "" {
		l += " " + s.d.Render(m.snap.Stack+" ›") + " " + s.b.Render("all logs")
	}
	state := s.g.Render("following")
	if m.paused {
		state = s.y.Render("paused")
	}
	r := state + s.d.Render(" · "+levelNames[m.level])
	out := []string{l + strings.Repeat(" ", max(w-width(l)-width(r), 1)) + r}

	lines, newer := m.window(m.stackLines(0), h-4)
	cap := "every instance, merged"
	if newer > 0 {
		cap = fmt.Sprintf("scrolled back · %d newer lines · G to follow", newer)
	}
	if m.search != "" || m.searching {
		cursor := ""
		if m.searching {
			cursor = s.a.Render("▏")
		}
		cap = "search " + s.w.Render(m.search) + cursor + s.d.Render(" · "+cap)
	}
	if m.selecting {
		lines = m.decorate()
	} else {
		m.shown = lines
	}
	if len(lines) == 0 {
		msg := "nothing printed yet"
		if m.search != "" {
			msg = "no line holds " + m.search
		}
		lines = []string{s.d.Render(msg)}
	}
	out = append(out, box(s.border, s.d.Render(cap), s.d.Render(fmt.Sprintf("%d kept", len(m.all))), lines, w, h-2)...)

	var keys string
	if m.selecting {
		keys = s.cap("↑↓", "extend") + "  " + s.cap("y", "copy") + "  " + s.cap("⏎", "read whole") + "  " + s.cap("esc", "cancel") + s.d.Render(fmt.Sprintf("   %d lines", abs(m.selB-m.selA)+1))
	} else if m.searching {
		keys = s.cap("⏎", "keep") + "  " + s.cap("esc", "clear") + "  " + s.a.Bold(true).Render("search") + " " + s.w.Render(m.search) + s.a.Render("▏")
	} else {
		ks := [][2]string{{"↑↓", "scroll"}, {"/", "search"}, {"f", "level"}, {"t", "time"}, {"v", "select"}, {"space", "pause"}, {"y", "copy"}, {":", "command"}, {"esc", "back"}}
		var parts []string
		for _, k := range ks {
			parts = append(parts, s.cap(k[0], k[1]))
		}
		keys = strings.Join(parts, "  ")
		if m.notice != "" {
			keys += "   " + s.y.Render(m.notice)
		}
	}
	return append(out, keys)
}
