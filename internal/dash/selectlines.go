package dash

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Copying part of the logs. v starts a selection on the newest line and
// ↑↓ move its far end; y copies what is selected and esc leaves it. The mouse
// does the same: press on a line, drag, and the lines under it are copied when
// the button is let go. While a selection is open the lines hold still, so what
// is selected is what was seen.

// selectable says whether the screen is one of lines that can be selected.
func (m *Model) selectable() bool {
	if m.page == pageLogs {
		return true
	}
	if v, ok := m.selected(); ok && m.page == pageFocus {
		ts := m.tabs(v)
		return m.tab < len(ts) && ts[m.tab].id == tabLogs
	}
	return false
}

// shownLines are the lines the page is drawing, as the last frame had them.
func (m *Model) beginSelect(at int) {
	if len(m.shown) == 0 {
		m.say("nothing to select yet")
		return
	}
	m.selecting = true
	m.frozen = append([]string(nil), m.shown...)
	at = min(max(at, 0), len(m.frozen)-1)
	m.selA, m.selB = at, at
}

// decorate marks the selected lines of frozen, in the pick colour.
func (m *Model) decorate() []string {
	lo, hi := min(m.selA, m.selB), max(m.selA, m.selB)
	out := make([]string, len(m.frozen))
	for i, l := range m.frozen {
		if i >= lo && i <= hi {
			out[i] = m.st.onPick(m.st.w).Render(ansi.Strip(l))
		} else {
			out[i] = l
		}
	}
	return out
}

// copySelection puts the selected lines on the clipboard and leaves the mode.
func (m *Model) copySelection(file bool) tea.Cmd {
	lo, hi := min(m.selA, m.selB), max(m.selA, m.selB)
	var out []string
	for i := lo; i <= hi && i < len(m.frozen); i++ {
		out = append(out, strings.TrimRight(ansi.Strip(m.frozen[i]), " "))
	}
	m.selecting, m.dragging, m.frozen = false, false, nil
	if len(out) == 0 {
		return nil
	}
	name := "selection"
	if v, ok := m.selected(); ok && m.page == pageFocus {
		name = v.Name + "-selection"
	}
	return m.out(strings.Join(out, "\n"), lineWord(len(out)), name, file)
}

// selectKey handles a key while a selection is open.
func (m *Model) selectKey(k string) tea.Cmd {
	switch k {
	case "up", "k":
		m.selB = max(m.selB-1, 0)
	case "down", "j":
		m.selB = min(m.selB+1, len(m.frozen)-1)
	case "g", "home":
		m.selB = 0
	case "G", "end":
		m.selB = len(m.frozen) - 1
	case "y", "Y":
		return m.copySelection(k == "Y")
	case "enter", "o":
		m.readSelection()
	case "esc", "v", "q":
		m.selecting, m.dragging, m.frozen = false, false, nil
	case "ctrl+c":
		return tea.Quit
	}
	return nil
}

// logRow is which shown line a mouse row is on: the box has a header line and a
// border above its first line.
func (m *Model) logRow(y int) (int, bool) {
	i := y - 2
	return i, i >= 0 && i < len(m.shown)
}

// logMouse handles the mouse on a screen of logs. It returns what to run.
func (m *Model) logMouse(msg tea.Msg) (tea.Cmd, bool) {
	if m.modal != nil || !m.selectable() {
		return nil, false
	}
	switch e := msg.(type) {
	case tea.MouseClickMsg:
		mo := tea.Mouse(e)
		if mo.Button != tea.MouseLeft {
			return nil, false
		}
		if m.page == pageFocus && m.w >= 100 && mo.X >= m.w-41 {
			return nil, false // the column beside the logs is not lines of them
		}
		if i, ok := m.logRow(mo.Y); ok {
			if !m.selecting {
				m.beginSelect(i)
			} else {
				m.selA, m.selB = i, i
			}
			m.dragging = true
			return nil, true
		}
	case tea.MouseMotionMsg:
		if m.dragging && m.selecting {
			mo := tea.Mouse(e)
			m.selB = min(max(mo.Y-2, 0), len(m.frozen)-1)
			return nil, true
		}
	case tea.MouseReleaseMsg:
		if m.dragging {
			m.dragging = false
			// a click with no drag selects the line; a drag copies what it covered
			if m.selA != m.selB {
				return m.copySelection(false), true
			}
			return nil, true
		}
	}
	return nil, false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// readSelection opens the selected lines whole: wrapped to a box, nothing cut
// off, for the line that is wider than the screen.
func (m *Model) readSelection() {
	lo, hi := min(m.selA, m.selB), max(m.selA, m.selB)
	var lines []string
	for i := lo; i <= hi && i < len(m.frozen); i++ {
		lines = append(lines, strings.TrimRight(ansi.Strip(m.frozen[i]), " "))
	}
	m.selecting, m.dragging, m.frozen = false, false, nil
	if len(lines) == 0 {
		return
	}
	m.modal = &modal{kind: modalText, lines: lines, title: fmt.Sprintf("%d line%s", len(lines), map[bool]string{true: "", false: "s"}[len(lines) == 1])}
}

// textBox draws the lines of a modalText wrapped to the box, from the line it
// has been read down to.
func (m *Model) textBox(w, h int) []string {
	s := m.st
	md := m.modal
	bw := min(w-4, 112)
	if bw < 20 {
		bw = max(w, 4)
	}
	inner := max(bw-4, 4)
	var wrapped []string
	for _, l := range md.lines {
		if l == "" {
			wrapped = append(wrapped, "")
			continue
		}
		wrapped = append(wrapped, strings.Split(ansi.Hardwrap(l, inner, true), "\n")...)
	}
	room := max(h-6, 3)
	md.off = min(max(md.off, 0), max(len(wrapped)-room, 0))
	end := min(md.off+room, len(wrapped))
	shown := make([]string, 0, room+1)
	for _, l := range wrapped[md.off:end] {
		shown = append(shown, s.text.Render(l))
	}
	cap := s.a.Bold(true).Render("whole line") + s.d.Render(" · "+md.title)
	rcap := ""
	if len(wrapped) > room {
		rcap = s.d.Render(fmt.Sprintf("%d–%d of %d", md.off+1, end, len(wrapped)))
	}
	return box(s.borderOn, cap, rcap, shown, bw, len(shown)+2)
}

func (m *Model) textKey(k string) tea.Cmd {
	md := m.modal
	switch k {
	case "esc", "enter", "q", "o":
		m.modal = nil
	case "up", "k":
		md.off--
	case "down", "j":
		md.off++
	case "pgup", "ctrl+u":
		md.off -= 10
	case "pgdown", "ctrl+d", " ":
		md.off += 10
	case "g", "home":
		md.off = 0
	case "G", "end":
		md.off = 1 << 20
	case "y", "Y":
		text := strings.Join(md.lines, "\n")
		m.modal = nil
		return m.out(text, lineWord(len(md.lines)), "lines", k == "Y")
	case "ctrl+c":
		return tea.Quit
	}
	return nil
}
