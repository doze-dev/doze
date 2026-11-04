package display

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/journal"
)

// What a program prints is not ours to trust on a screen. A log line can carry
// colour codes, a window-title sequence, a bell, a carriage return that a
// progress bar uses to overwrite itself, or bytes that are not text at all,
// and any of them, drawn raw, corrupts a terminal or injects into a page.
// Every string that comes from outside is made plain text before it is kept.

// Clean makes s one line of printable text: escape sequences removed, text
// before the last carriage return dropped (what a progress bar overwrote),
// tabs as spaces, other control bytes gone, and invalid UTF-8 replaced.
func Clean(s string) string {
	if s == "" {
		return s
	}
	if strings.IndexByte(s, 0x1b) >= 0 {
		s = ansi.Strip(s)
	}
	if i := strings.LastIndexByte(s, '\r'); i >= 0 {
		if rest := strings.TrimRight(s[i+1:], "\n"); rest != "" {
			s = rest
		} else {
			s = s[:i]
		}
	}
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString("    ")
		case r == '\n':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			// dropped
		case r == 0x200b || r == 0xfeff || r == 0x202e || r == 0x2066 || r == 0x2067 || r == 0x2069:
			// zero-width and direction-changing marks, which move text about
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CleanView makes the text an instance carries safe to draw.
func CleanView(v control.InstanceView) control.InstanceView {
	v.LastError = CleanLines(v.LastError)
	if v.Task != nil {
		t := *v.Task
		t.Why = Clean(t.Why)
		v.Task = &t
	}
	v.Name, v.Run, v.URL, v.Domain = Clean(v.Name), Clean(v.Run), Clean(v.URL), Clean(v.Domain)
	return v
}

// CleanLines is Clean for text that may have several lines, which it keeps.
func CleanLines(s string) string {
	if s == "" {
		return s
	}
	parts := strings.Split(s, "\n")
	for i, p := range parts {
		parts[i] = Clean(p)
	}
	return strings.Join(parts, "\n")
}

// CleanEvent makes a journal event's detail safe to draw.
func CleanEvent(e journal.Event) journal.Event {
	e.Detail = Clean(e.Detail)
	return e
}
