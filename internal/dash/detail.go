package dash

import (
	"fmt"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/display"
	"github.com/doze-dev/doze/internal/journal"
)

// logsBox is the box under (or beside) the list that says what the selected
// instance is saying, or, when it is in trouble, what went wrong.
func (m *Model) logsBox(v control.InstanceView, w, h int) []string {
	s := m.st
	border := s.border
	caption := s.b.Render(v.Name) + " " + s.d.Render("· logs · following")
	var lines []string
	if troubled(v) {
		var rcap string
		lines, _, rcap, border = m.detail(v, w-4, h-2)
		caption = s.b.Render(v.Name) + " " + s.pillR.Render("crashing")
		if rcap != "" {
			caption += "  " + rcap
		}
		room := h - 2 - len(lines)
		if room > 2 {
			lines = append(lines, "")
			lines = append(lines, m.stream(v, room-1, lvDebug, 0)...)
		}
	} else {
		if m.page == pageHome {
			// each instance's logs are followed until scrolled, and the scroll is not
			// carried from one instance to the next
			if m.scrolledFor != v.Name {
				m.scrolledFor, m.pinned = v.Name, false
			}
			var newer int
			lines, newer = m.window(m.streamAll(v, lvDebug, 0), h-2)
			if newer > 0 {
				caption = s.b.Render(v.Name) + " " + s.d.Render(fmt.Sprintf("· scrolled back · %d newer lines · wheel down to follow", newer))
			}
		} else {
			lines = m.stream(v, h-2, lvDebug, 0)
		}
		if m.selecting && m.page == pageHome {
			lines = m.decorate()
		} else {
			m.shown = lines
		}
		m.area = logArea{y0: 1, x0: 0, x1: w, n: h - 2} // the caller adds where the box is
	}
	return box(border, caption, "", lines, w, h)
}

// detailPane is the right of a wide Home (boards/W-Home.dc.html): the selected
// instance's vitals as charts, as tall as the height allows, and its logs under
// them. It is p rows tall and dw wide.
func (m *Model) detailPane(v control.InstanceView, dw, p int) []string {
	var out []string
	logsH := p
	// vitals are for something that is running, and take a share of the height
	// that leaves the logs their room
	if awake(v) && !troubled(v) && p >= 24 {
		if strip := m.vitalsStrip(v, dw, min(p*50/100, 22), false); len(strip) > 0 && p-len(strip) >= minLogRows {
			out = append(out, strip...)
			logsH = p - len(strip)
		}
	}
	box := m.logsBox(v, dw, logsH)
	if m.area.x1 > 0 {
		m.area.y0 += len(out) // under the vitals
	}
	return append(out, box...)
}

// activityBox is the stack's recent events, newest last, in the room a short
// list leaves under it (boards/W-Home.dc.html's "activity").
func (m *Model) activityBox(w, h int) []string {
	s := m.st
	var lines []string
	for _, e := range m.events {
		style := s.a
		if e.Kind == journal.Crash || e.Kind == journal.GiveUp || e.Kind == journal.Failed {
			style = s.r
		}
		lines = append(lines, s.d.Render(m.stamp(e.Time))+" "+style.Render("zzz "+e.Instance+" "+display.Event(e)))
	}
	if n := h - 2; len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) == 0 {
		lines = []string{s.d.Render("nothing has happened yet")}
	}
	return box(s.border, s.b.Render("activity"), "", lines, w, h)
}
