package dash

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// Selecting text in the logs, as in an editor or a terminal: press on a
// character, drag to another, and what is between them is copied on release;
// double-click is a word (a path or a URL is one), triple-click a line. The
// keyboard's v selects whole lines and ↑↓ extend them. Either way the lines hold
// still while it is open, so what is selected is what was seen.

type selMode int

const (
	selByLine selMode = iota // whole lines: the keyboard, and a triple-click
	selByChar                // from a column of one line to a column of another
)

// clickRun counts rapid clicks on the same place, for double and triple.
type clickRun struct {
	at        time.Time
	line, col int
	n         int
}

const multiClick = 500 * time.Millisecond

// clicked records a press and says how many there have been in a row.
func (m *Model) clicked(line, col int) int {
	now := m.nowf()
	c := &m.click3
	if c.n > 0 && now.Sub(c.at) <= multiClick && c.line == line && abs(c.col-col) <= 2 {
		c.n = c.n%3 + 1
	} else {
		c.n = 1
	}
	c.at, c.line, c.col = now, line, col
	return c.n
}

// plain is the frozen line i as text.
func (m *Model) plain(i int) string {
	if i < 0 || i >= len(m.frozen) {
		return ""
	}
	return ansi.Strip(m.frozen[i])
}

// span is the selection as it is on the page: the first and last line, and the
// column each begins and ends at (the ends of lines for whole lines).
func (m *Model) span() (l0, c0, l1, c1 int) {
	if m.selMode == selByLine {
		return min(m.selA, m.selB), 0, max(m.selA, m.selB), 1 << 20
	}
	l0, c0, l1, c1 = m.selA, m.selAc, m.selB, m.selBc
	if l0 > l1 || (l0 == l1 && c0 > c1) {
		l0, c0, l1, c1 = l1, c1, l0, c0
	}
	return
}

// bounds is where the selection is on line i: [a, b) in cells.
func (m *Model) bounds(i, l0, c0, l1, c1 int) (a, b int) {
	w := ansi.StringWidth(m.plain(i))
	a, b = 0, w
	if i == l0 {
		a = min(c0, w)
	}
	if i == l1 {
		b = min(c1, w)
	}
	return a, max(b, a)
}

// selText is the selected text, a line per line.
func (m *Model) selText() []string {
	l0, c0, l1, c1 := m.span()
	var out []string
	for i := l0; i <= l1 && i < len(m.frozen); i++ {
		a, b := m.bounds(i, l0, c0, l1, c1)
		out = append(out, strings.TrimRight(ansi.Cut(m.plain(i), a, b), " "))
	}
	return out
}

// selEmpty says whether nothing is selected: a press that did not move.
func (m *Model) selEmpty() bool {
	if m.selMode == selByLine {
		return false
	}
	return m.selA == m.selB && m.selAc == m.selBc
}

// decorate marks the selected text of the frozen lines, in the pick colour.
func (m *Model) decorate() []string {
	out := make([]string, len(m.frozen))
	copy(out, m.frozen)
	if m.selEmpty() {
		return out
	}
	l0, c0, l1, c1 := m.span()
	for i := l0; i <= l1 && i < len(out); i++ {
		a, b := m.bounds(i, l0, c0, l1, c1)
		w := ansi.StringWidth(m.plain(i))
		mark := func(s string) string { return m.st.onPick(m.st.w).Render(s) }
		switch {
		case b <= a && w > 0:
			// an empty part of a line: nothing to mark
		case a == 0 && b >= w:
			out[i] = mark(m.plain(i))
		default:
			out[i] = ansi.Cut(m.frozen[i], 0, a) + mark(ansi.Cut(m.plain(i), a, b)) + ansi.Cut(m.frozen[i], b, w)
		}
	}
	return out
}

// wordAt is the cells [a, b) of the word at column col of s. A word is a run
// of what is not a space or a quote or a bracket, so a path or a URL is one;
// on a space it is the run of spaces.
func wordAt(s string, col int) (int, int) {
	type cell struct {
		r          rune
		start, end int
	}
	var cs []cell
	x := 0
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		cs = append(cs, cell{r, x, x + w})
		x += w
	}
	if len(cs) == 0 {
		return 0, 0
	}
	at := len(cs) - 1
	for i, c := range cs {
		if col < c.end {
			at = i
			break
		}
	}
	sep := func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("\"'`()[]{}<>,;|", r)
	}
	class := sep(cs[at].r)
	lo, hi := at, at
	for lo > 0 && sep(cs[lo-1].r) == class {
		lo--
	}
	for hi < len(cs)-1 && sep(cs[hi+1].r) == class {
		hi++
	}
	return cs[lo].start, cs[hi].end
}

// selWhat names what was selected, for the message.
func (m *Model) selWhat(lines []string) string {
	if m.selMode == selByChar && len(lines) == 1 {
		n := len([]rune(lines[0]))
		if n == 1 {
			return "1 character"
		}
		return fmt.Sprintf("%d characters", n)
	}
	return lineWord(len(lines))
}

// textCol is the column of the line a screen column is on: the box has its
// border and a space before the first character.
func (m *Model) textCol(x int) int { return max(x-(m.area.x0+2), 0) }

// dragMode says whether moving the mouse after a press changes the selection: a
// plain press drags it out; a double or triple click has already chosen.
type dragMode int

const (
	dragFree dragMode = iota
	dragFixed
)
