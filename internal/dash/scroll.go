package dash

import "strings"

// Scrolling the logs. A page of logs follows the newest line until it is
// scrolled back; then it holds still, whatever arrives, and says how many newer
// lines there are. G (or scrolling to the end) follows again.

// scrollable says whether the screen is one of lines that scroll.
func (m *Model) scrollable() bool { return m.selectable() && !m.selecting }

// window is the page of lines to draw out of all of them, h tall, and how many
// newer lines there are below it (0 when following).
func (m *Model) window(all []string, h int) (lines []string, newer int) {
	if h < 1 {
		return nil, 0
	}
	m.page1, m.atTop = h, false
	if len(all) <= h {
		m.pinned = false
		return all, 0
	}
	if !m.pinned {
		return all[len(all)-h:], 0
	}
	top := min(max(m.pinTop, 0), len(all)-h)
	m.pinTop = top
	m.atTop = top == 0
	return all[top : top+h], len(all) - (top + h)
}

// scrollKey moves the page for a key; it says nothing when there is no page.
func (m *Model) scrollKey(k string) {
	page := max(m.page1-1, 1)
	switch k {
	case "up", "k":
		m.scrollBy(-1)
	case "down", "j":
		m.scrollBy(1)
	case "pgup", "ctrl+u":
		m.scrollBy(-page)
	case "pgdown", "ctrl+d":
		m.scrollBy(page)
	case "g", "home":
		m.pinned, m.pinTop = true, 0
	case "G", "end":
		m.pinned = false
	}
}

// scrollBy moves the first shown line by n, pinning the page on the way up and
// following again when it is back at the end.
func (m *Model) scrollBy(n int) {
	total := m.pageTotal()
	h := m.page1
	if total <= h || h < 1 {
		return
	}
	top := total - h // where following is
	if m.pinned {
		top = m.pinTop
	}
	top += n
	switch {
	case top >= total-h:
		m.pinned = false
	default:
		m.pinned, m.pinTop = true, max(top, 0)
	}
}

// pageTotal is how many lines the current page has to scroll through.
func (m *Model) pageTotal() int {
	if m.page == pageLogs {
		return len(m.stackLines(0))
	}
	if v, ok := m.selected(); ok {
		m.query = m.search
		n := len(m.streamAll(v, m.level, m.runSel))
		m.query = ""
		return n
	}
	return 0
}

var _ = strings.TrimSpace
