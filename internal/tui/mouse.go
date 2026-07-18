// Mouse handling: wheel and click routing for the dash and copy mode, plus
// the screen-to-content mapping math for the logs region.

package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// handleMouse routes the wheel and clicks like mprocs: wheel over the sidebar
// moves the selection, wheel over the right pane scrolls the logs, and a click
// in the sidebar selects that instance.
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	const headerRows = 2 // title + rule above the body
	if m.paletteMode || m.dashPending != "" || m.showHelp {
		return m, nil // keyboard-driven overlays own the input
	}
	if m.copyMode { // scroll the frozen logs; drag to extend the selection
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.logVP.ScrollUp(2)
		case tea.MouseButtonWheelDown:
			m.logVP.ScrollDown(2)
		case tea.MouseButtonLeft:
			switch msg.Action {
			case tea.MouseActionPress: // re-anchor a fresh character-precise selection
				ln := m.logLineAt(msg.Y)
				m.copyCharMode = true
				m.copyCursor, m.copyColCh = ln, m.logRuneColAt(ln, msg.X)
				m.copyAnchor, m.copyAnchorColCh = m.copyCursor, m.copyColCh
				m.refreshCopyView()
			case tea.MouseActionMotion: // drag → extend the character span
				ln := m.logLineAt(msg.Y)
				m.copyCursor = ln
				if m.copyCharMode {
					m.copyColCh = m.logRuneColAt(ln, msg.X)
				}
				m.refreshCopyView()
			case tea.MouseActionRelease:
				dragged := m.copyAnchor != m.copyCursor ||
					(m.copyCharMode && m.copyAnchorColCh != m.copyColCh)
				if dragged {
					return m.copySelection() // dragged → copy exactly what was spanned
				}
				m.copyAnchor, m.copyAnchorColCh = -1, 0 // plain click → position only
				m.refreshCopyView()
			}
		}
		return m, nil
	}
	overSidebar := msg.X < m.sidebarW()
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if overSidebar {
			if m.cursor > 0 {
				m.cursor--
			}
			return m, m.onSelect()
		}
		m.logVP.ScrollUp(3)
		return m, nil
	case tea.MouseButtonWheelDown:
		if overSidebar {
			if m.cursor < len(m.visible())-1 {
				m.cursor++
			}
			return m, m.onSelect()
		}
		m.logVP.ScrollDown(3)
		return m, nil
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		if overSidebar {
			// Map the clicked body row to a sidebar line through the same window the
			// renderer uses (headers interspersed); only instance lines are selectable,
			// and the overflow-marker rows at the window's edges are not.
			lines, above, below := m.sidebarWindow(m.sidebarAvail())
			if row := msg.Y - headerRows; row >= 0 && row < len(lines) {
				if (above > 0 && row == 0) || (below > 0 && row == len(lines)-1) {
					return m, nil
				}
				if ln := lines[row]; ln.header == "" {
					m.cursor = ln.di
					return m, m.onSelect()
				}
			}
		} else if m.logsRegion(msg.Y) && len(m.logLines) > 0 {
			// Enter copy mode with a character-precise anchor at the click, so a drag
			// selects an exact span (including within one line), like a terminal.
			m.copyMode, m.copyCharMode = true, true
			m.copyLines = m.logLines
			ln := m.logLineAt(msg.Y)
			m.copyCursor, m.copyColCh = ln, m.logRuneColAt(ln, msg.X)
			m.copyAnchor, m.copyAnchorColCh = m.copyCursor, m.copyColCh
			m.refreshCopyView()
		}
	}
	return m, nil
}

// logsTop is the screen row of the first visible log line (header + detail box
// + the logs region's inline-title rule).
func (m model) logsTop() int { return 2 + m.detailBoxH() + 1 }

// logsRegion reports whether screen row y falls inside the log viewport.
func (m model) logsRegion(y int) bool {
	return y >= m.logsTop() && y < m.logsTop()+m.logVP.Height
}

// logLineAt maps a screen row to a log line index (clamped).
func (m model) logLineAt(y int) int {
	return clampi(m.logVP.YOffset+(y-m.logsTop()), 0, max(0, len(m.copyLines)-1))
}

// ── character-precise selection (mouse drag) ────────────────────────────────

// logRuneColAt maps a screen X to a rune index on the given log line (char
// granularity for mouse selection). The end position (len) is allowed so a drag
// can include the last character. The logs content starts after the sidebar
// (sidebarW) and the 2-col gap — the region has no border or padding. Screen
// cells map to runes through their display width, so a click past a CJK or
// emoji span still lands on the right character.
func (m model) logRuneColAt(line, x int) int {
	contentX := x - (m.sidebarW() + 2)
	if contentX < 0 {
		contentX = 0
	}
	if line < 0 || line >= len(m.copyLines) {
		return 0
	}
	col, cells := 0, 0
	for _, r := range m.copyLines[line] {
		w := ansi.StringWidth(string(r))
		if cells+w > contentX {
			return col
		}
		cells += w
		col++
	}
	return col
}
