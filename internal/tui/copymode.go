package tui

import (
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// copySelection writes the selected text to the clipboard and leaves copy mode,
// flashing exactly what was copied ("✓ copied 3 lines" / "✓ copied 27 chars").
func (m model) copySelection() (tea.Model, tea.Cmd) {
	var text, what string
	if m.copyCharMode && m.copyAnchor >= 0 { // character-precise span (mouse drag)
		text = m.selectedCharText()
		what = plural(len([]rune(text)), "char")
	} else { // whole line(s) — the keyboard default
		lo, hi := m.copyRange()
		text = strings.Join(m.copyLines[lo:hi+1], "\n")
		what = plural(hi-lo+1, "line")
	}
	err := clipboard.WriteAll(text)
	m.copyMode, m.copyCharMode = false, false
	m.copyAnchor, m.copyAnchorColCh = -1, 0
	m.logVP.SetContent(renderLogs(m.logLines, m.logVP.Width))
	m.logVP.GotoBottom() // leaving copy mode returns to the live tail
	if err != nil {
		m.setFlashErr("✗ copy failed: " + err.Error())
	} else {
		m.setFlash(stGreen, "✓ copied "+what)
	}
	return m, nil
}

// charSel returns the ordered span (loLine, loCol, hiLine, hiCol) of the
// character selection (anchor → cursor), in rune coordinates.
func (m model) charSel() (lL, lC, hL, hC int) {
	if m.copyAnchor > m.copyCursor || (m.copyAnchor == m.copyCursor && m.copyAnchorColCh > m.copyColCh) {
		return m.copyCursor, m.copyColCh, m.copyAnchor, m.copyAnchorColCh
	}
	return m.copyAnchor, m.copyAnchorColCh, m.copyCursor, m.copyColCh
}

// selCharRange is the selected rune span [start,end) on line i for the active
// character selection (whole intermediate lines are fully covered). ok is false
// when line i is outside the selection.
func (m model) selCharRange(i int) (int, int, bool) {
	if !m.copyCharMode || m.copyAnchor < 0 {
		return 0, 0, false
	}
	lL, lC, hL, hC := m.charSel()
	if i < lL || i > hL {
		return 0, 0, false
	}
	n := len([]rune(m.copyLines[i]))
	start, end := 0, n
	if i == lL {
		start = clampi(lC, 0, n)
	}
	if i == hL {
		end = clampi(hC, 0, n)
	}
	return start, end, true
}

// selectedCharText extracts the character-precise selected span across lines.
func (m model) selectedCharText() string {
	lL, lC, hL, hC := m.charSel()
	if lL < 0 || hL >= len(m.copyLines) {
		return ""
	}
	if lL == hL {
		r := []rune(m.copyLines[lL])
		return string(r[clampi(lC, 0, len(r)):clampi(hC, 0, len(r))])
	}
	rFirst := []rune(m.copyLines[lL])
	parts := []string{string(rFirst[clampi(lC, 0, len(rFirst)):])}
	for i := lL + 1; i < hL; i++ {
		parts = append(parts, m.copyLines[i])
	}
	rLast := []rune(m.copyLines[hL])
	parts = append(parts, string(rLast[:clampi(hC, 0, len(rLast))]))
	return strings.Join(parts, "\n")
}

// handleCopyKey drives copy mode: ↑↓ move the line cursor, v anchors a line
// range, a selects everything, y/c/enter copy, esc leaves. (The mouse keeps its
// own character-precise drag selection.)
func (m model) handleCopyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	last := len(m.copyLines) - 1
	exit := func() {
		m.copyMode, m.copyCharMode = false, false
		m.copyAnchor, m.copyAnchorColCh = -1, 0
		m.logVP.SetContent(renderLogs(m.logLines, m.logVP.Width))
		m.logVP.GotoBottom() // leaving copy mode returns to the live tail
	}
	if m.copyFinding { // the `/` find input owns keystrokes
		switch msg.String() {
		case "enter": // keep the query, jump to its nearest match
			m.copyFinding = false
			m.jumpFind(1, true)
		case "esc": // abandon the search
			m.copyFinding, m.copyFind = false, ""
		case "backspace":
			if r := []rune(m.copyFind); len(r) > 0 {
				m.copyFind = string(r[:len(r)-1])
			}
		default:
			if msg.Type == tea.KeyRunes || msg.String() == " " {
				m.copyFind += string(msg.Runes)
			}
		}
		return m, nil
	}
	// Any keyboard motion leaves the mouse's character mode (the copy/exit keys
	// keep it so a dragged span still copies).
	switch msg.String() {
	case "c", "y", "enter", "esc", "q":
	default:
		m.copyCharMode = false
	}
	switch msg.String() {
	case "esc", "q":
		exit()
		return m, nil
	case "up", "k":
		m.copyCursor--
	case "down", "j":
		m.copyCursor++
	case "pgup", "ctrl+u": // a real half page, matching the other panes
		m.copyCursor -= max(1, m.logVP.Height/2)
	case "pgdown", "ctrl+d":
		m.copyCursor += max(1, m.logVP.Height/2)
	case "g", "home":
		m.copyCursor = 0
	case "G", "end":
		m.copyCursor = last
	case "/": // find a line — the logs' only search surface
		m.copyFinding, m.copyFind = true, ""
		return m, nil
	case "n": // repeat the find, forward
		m.jumpFind(1, false)
	case "N": // …and back
		m.jumpFind(-1, false)
	case "v", " ": // anchor / drop a line range
		if m.copyAnchor < 0 {
			m.copyAnchor = m.copyCursor
		} else {
			m.copyAnchor = -1
		}
	case "a": // select all lines
		m.copyAnchor, m.copyCursor = 0, last
	case "c", "y", "enter":
		return m.copySelection()
	default:
		return m, nil
	}
	m.copyCursor = clampi(m.copyCursor, 0, last)
	m.refreshCopyView()
	return m, nil
}

// jumpFind moves the copy cursor to the next line matching the kept find query
// (case-insensitive substring), stepping dir from the cursor and wrapping once
// around the buffer. includeCurrent starts the scan on the cursor's own line —
// what `enter` wants right after typing a query. No match flashes, not beeps.
func (m *model) jumpFind(dir int, includeCurrent bool) {
	q := strings.ToLower(m.copyFind)
	n := len(m.copyLines)
	if q == "" || n == 0 {
		return
	}
	start := m.copyCursor
	if !includeCurrent {
		start += dir
	}
	for i := 0; i < n; i++ {
		at := ((start+dir*i)%n + n) % n
		if strings.Contains(strings.ToLower(m.copyLines[at]), q) {
			m.copyCursor = at
			m.refreshCopyView()
			return
		}
	}
	m.setFlash(stDim, "no match for “"+m.copyFind+"”")
}

// copyRange is the inclusive selected line range (just the cursor if no anchor).
func (m model) copyRange() (int, int) {
	if m.copyAnchor < 0 {
		return m.copyCursor, m.copyCursor
	}
	lo, hi := m.copyAnchor, m.copyCursor
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

// refreshCopyView re-renders the frozen logs with the active selection
// highlighted — whole lines for the keyboard, an inline span for a mouse drag —
// and keeps the cursor in view.
func (m *model) refreshCopyView() {
	w := m.logVP.Width
	loL, hiL := m.copyRange()
	curFull := lipgloss.NewStyle().Background(cAccent).Foreground(cSelFg).Width(w)
	selFull := lipgloss.NewStyle().Background(cSel).Foreground(cText).Width(w)
	selSeg := lipgloss.NewStyle().Background(cSel).Foreground(cText)

	var b strings.Builder
	for i, ln := range m.copyLines {
		disp := truncate(ln, w)
		if m.copyCharMode { // character-precise span (mouse drag)
			dr := []rune(disp)
			cs, ce, has := m.selCharRange(i)
			if !has {
				b.WriteString(stText.Render(disp))
				b.WriteByte('\n')
				continue
			}
			cs, ce = clampi(cs, 0, len(dr)), clampi(ce, 0, len(dr))
			b.WriteString(stText.Render(string(dr[:cs])))
			b.WriteString(selStyled(selSeg, string(dr[cs:ce])))
			b.WriteString(stText.Render(string(dr[ce:])))
			b.WriteByte('\n')
			continue
		}
		// Line granularity — highlight whole lines.
		switch {
		case i == m.copyCursor:
			b.WriteString(selStyled(curFull, disp))
		case m.copyAnchor >= 0 && i >= loL && i <= hiL:
			b.WriteString(selStyled(selFull, disp))
		default:
			b.WriteString(stText.Render(disp))
		}
		b.WriteByte('\n')
	}
	m.logVP.SetContent(strings.TrimRight(b.String(), "\n"))
	// Keep the cursor in view without re-centering on every move (so dragging
	// and arrow-stepping stay smooth); only scroll when it leaves the window.
	off, h := m.logVP.YOffset, m.logVP.Height
	if m.copyCursor < off {
		off = m.copyCursor
	} else if m.copyCursor >= off+h {
		off = m.copyCursor - h + 1
	}
	m.logVP.SetYOffset(max(0, off))
}
