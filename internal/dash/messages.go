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
