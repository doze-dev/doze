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
		m.notice = "nothing to select yet"
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
func (m *Model) copySelection() tea.Cmd {
	lo, hi := min(m.selA, m.selB), max(m.selA, m.selB)
	var out []string
	for i := lo; i <= hi && i < len(m.frozen); i++ {
		out = append(out, strings.TrimRight(ansi.Strip(m.frozen[i]), " "))
	}
	m.selecting, m.dragging, m.frozen = false, false, nil
	if len(out) == 0 {
		return nil
	}
	m.notice = fmt.Sprintf("copied %d line%s", len(out), map[bool]string{true: "", false: "s"}[len(out) == 1])
	return tea.SetClipboard(strings.Join(out, "\n"))
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
	case "y", "enter":
		return m.copySelection()
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
				return m.copySelection(), true
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
