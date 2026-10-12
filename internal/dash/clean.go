package dash

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/journal"
)

// What a program prints is not ours to trust on a terminal. A log line can carry
// colour codes, a window-title sequence, a bell, a carriage return that a
// progress bar uses to overwrite itself, or bytes that are not text at all, and
// any of them, drawn raw, corrupts the screen or the person's terminal. Every
// string that comes from outside the dash is made plain text before it is kept.

// clean makes s one line of printable text: escape sequences removed, text
// before the last carriage return dropped (what a progress bar overwrote), tabs
// as spaces, other control bytes gone, and invalid UTF-8 replaced.
func clean(s string) string {
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

// cleanView makes the text an instance carries safe to draw.
func cleanView(v control.InstanceView) control.InstanceView {
	v.LastError = cleanLines(v.LastError)
	if v.Task != nil {
		t := *v.Task
		t.Why = clean(t.Why)
		v.Task = &t
	}
	v.Name, v.Run, v.URL, v.Domain = clean(v.Name), clean(v.Run), clean(v.URL), clean(v.Domain)
	return v
}

// cleanLines is clean for text that may have several lines, which it keeps.
func cleanLines(s string) string {
	if s == "" {
		return s
	}
	parts := strings.Split(s, "\n")
	for i, p := range parts {
		parts[i] = clean(p)
	}
	return strings.Join(parts, "\n")
}

func cleanEvent(e journal.Event) journal.Event {
	e.Detail = clean(e.Detail)
	return e
}
