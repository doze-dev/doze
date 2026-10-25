package dash

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The splash fills the wait before the daemon first answers: the wordmark, and
// under it that the dash is connecting, with z's that come and go. It never adds
// a wait. Until splashAfter has passed the screen is blank, so a daemon that
// answers at once goes straight to the page and nothing flashes; after that the
// splash stays only until the first snapshot or instances frame.

// splashAfter is how long the dash shows nothing before it shows the splash.
const splashAfter = 150 * time.Millisecond

// breathEvery is how often the z's under the wordmark change.
const breathEvery = 700 * time.Millisecond

// wordmark is "doze" in lowercase, drawn on the letters of figlet's ANSI Shadow.
var wordmark = []string{
	"      ██╗",
	"      ██║",
	" ███████║  ██████╗  ███████╗  ██████╗",
	"██╔═══██║ ██╔═══██╗ ╚══███╔╝ ██╔═══██╗",
	"██║   ██║ ██║   ██║   ███╔╝  ████████║",
	"██║   ██║ ██║   ██║  ███╔╝   ██╔═════╝",
	"╚███████║ ╚██████╔╝ ███████╗ ╚███████╗",
	" ╚══════╝  ╚═════╝  ╚══════╝  ╚══════╝",
}

type splashMsg time.Time

func splashTick(after time.Duration) tea.Cmd {
	return tea.Tick(after, func(t time.Time) tea.Msg { return splashMsg(t) })
}

// splashing reports whether the screen is the splash's: the dash was started
// on a daemon and has heard nothing of the stack from it yet.
func (m *Model) splashing() bool {
	return m.waiting && m.conn == connConnecting && m.modal == nil
}

// splash is the whole screen while splashing.
func (m *Model) splash(w, h int) []string {
	rows := make([]string, h)
	since := m.nowf().Sub(m.opened)
	if since < splashAfter {
		return rows
	}
	zs := "z z z"[:1+2*(int(since/breathEvery)%3)]
	// padded to the longest, so the line stays put as the z's come and go
	status := m.st.d.Render("connecting to the daemon  ") + m.st.a.Render(zs) + strings.Repeat(" ", 5-len(zs))
	lines := m.wordmark()
	if lines == nil {
		// a terminal that can't draw the wordmark's blocks gets the name in a line
		lines = []string{m.st.a.Bold(true).Render("doze")}
	}
	lines = append(lines, "", status)
	top := max((h-len(lines))/2, 0)
	wide := 0
	for _, l := range lines[:len(lines)-2] {
		wide = max(wide, width(l))
	}
	left := max((w-wide)/2, 0)
	for i, l := range lines {
		if top+i >= h {
			break
		}
		if i == len(lines)-1 {
			// the status line is centred on its own
			rows[top+i] = strings.Repeat(" ", max((w-width(l))/2, 0)) + l
			continue
		}
		rows[top+i] = strings.Repeat(" ", left) + l
	}
	return rows
}

// wordmark is the wordmark coloured across its columns from the palette's warm
// colour to its accent, or nil where the terminal draws only ASCII or draws box
// lines two cells wide.
func (m *Model) wordmark() []string {
	if m.glyphs != glyphsUnicode {
		return nil
	}
	cols := 0
	for _, l := range wordmark {
		cols = max(cols, len([]rune(l)))
	}
	out := make([]string, len(wordmark))
	for i, l := range wordmark {
		if m.st.plain {
			out[i] = l
			continue
		}
		var b strings.Builder
		for x, r := range []rune(l) {
			if r == ' ' {
				b.WriteRune(r)
				continue
			}
			t := float64(x) / float64(max(cols-1, 1))
			hex := mix(m.st.pal.accent, m.st.pal.extra, t)
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render(string(r)))
		}
		out[i] = b.String()
	}
	return out
}

// helpHead is the top of the help screen: the wordmark, centred in a box inner
// wide, with doze's version under it. It only takes rows the keys leave spare,
// so the help never scrolls because of it, and it is left out where the
// terminal can't draw the wordmark.
func (m *Model) helpHead(inner, spare int) []string {
	lines := m.wordmark()
	if lines == nil || inner < 40 {
		return nil
	}
	pad := strings.Repeat(" ", (inner-width(lines[2]))/2)
	out := make([]string, 0, len(lines)+3)
	for _, l := range lines {
		out = append(out, pad+l)
	}
	if m.version != "" {
		v := "doze " + m.version
		out = append(out, "", strings.Repeat(" ", max((inner-width(v))/2, 0))+m.st.d.Render(v))
	}
	if out = append(out, ""); len(out) > spare {
		return nil
	}
	return out
}
