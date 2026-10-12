package dash

import (
	"fmt"
	"strings"
	"time"
)

// What the dash tells the person about what they did. A success is said and
// fades; a failure is red and stays until esc or a while has passed, whatever
// else is pressed meanwhile, and every message is kept so a long or missed one
// can be read again (the palette's "recent messages").

const (
	okFor      = 4 * time.Second
	badFor     = 12 * time.Second
	historyCap = 20
)

type message struct {
	text string
	bad  bool
	at   time.Time
}

func (m *Model) say(text string)  { m.tell(text, false) }
func (m *Model) fail(text string) { m.tell(text, true) }

func (m *Model) tell(text string, bad bool) {
	at := m.now
	if at.IsZero() {
		at = time.Now()
	}
	m.notice, m.noticeBad, m.noticeAt = text, bad, at
	m.history = append(m.history, message{text, bad, at})
	if len(m.history) > historyCap {
		m.history = m.history[len(m.history)-historyCap:]
	}
}

func (m *Model) clearNotice() { m.notice, m.noticeBad = "", false }

// fadeNotice drops a message that has been there long enough.
func (m *Model) fadeNotice() {
	if m.notice == "" {
		return
	}
	ttl := okFor
	if m.noticeBad {
		ttl = badFor
	}
	if m.now.Sub(m.noticeAt) >= ttl {
		m.clearNotice()
	}
}

// noticeText is the message as the bar shows it, after the keys.
func (m *Model) noticeText() string {
	if m.notice == "" {
		return ""
	}
	if m.noticeBad {
		return "   " + m.st.r.Render("✗ "+m.notice)
	}
	return "   " + m.st.y.Render(m.notice)
}

// recentMessages opens the kept messages, newest last, whole.
func (m *Model) recentMessages() {
	var lines []string
	for _, h := range m.history {
		mark := "  "
		if h.bad {
			mark = "✗ "
		}
		lines = append(lines, fmt.Sprintf("%s  %s%s", h.at.Format("15:04:05"), mark, strings.TrimSpace(h.text)))
	}
	if len(lines) == 0 {
		lines = []string{"nothing has been said yet"}
	}
	m.modal = &modal{kind: modalText, lines: lines, title: "recent messages"}
}

// tellMouse says once that the dash holds the mouse, and how to get it back for
// the terminal's own selection.
func (m *Model) tellMouse() {
	if m.mouseTold {
		return
	}
	m.mouseTold = true
	m.say("shift-drag selects text in the terminal; drag over logs here copies them")
}

// Under this the pages have nothing left to use.
const (
	minW = 60
	minH = 16
)

// tooSmall is the whole screen when the window is under minW x minH: one line,
// centred, that says what to do. Keys still work.
func tooSmall(s styles, w, h int) string {
	msg := fmt.Sprintf("make the window larger · %d×%d or more", minW, minH)
	if width(msg) > w {
		msg = fmt.Sprintf("larger: %d×%d", minW, minH)
	}
	if width(msg) > w {
		msg = ansiTruncate(msg, w)
	}
	pad := max((w-width(msg))/2, 0)
	rows := make([]string, h)
	rows[h/2] = strings.Repeat(" ", pad) + s.d.Render(msg)
	return strings.Join(rows, "\n")
}
