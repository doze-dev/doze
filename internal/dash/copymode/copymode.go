// Package copymode is how a log view lets a person take lines out of the
// terminal: select a run of lines, copy them (with or without their
// timestamps) to the clipboard, or save them to a file.
//
// Copying uses OSC 52, the terminal's own clipboard sequence, so it works
// over SSH and inside tmux where a clipboard program on the host would copy
// to the wrong machine.
package copymode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/control"
)

// Selection is a run of lines picked in a view. Lines are indexed in the
// view's own order; the view maps them to what it shows.
type Selection struct {
	Active         bool
	Anchor, Cursor int
}

// Start begins a selection at line i.
func (s *Selection) Start(i int) { s.Active, s.Anchor, s.Cursor = true, i, i }

// Move extends the selection by n lines, within [0, last].
func (s *Selection) Move(n, last int) {
	s.Cursor += n
	if s.Cursor < 0 {
		s.Cursor = 0
	}
	if s.Cursor > last {
		s.Cursor = last
	}
}

// Stop ends the selection.
func (s *Selection) Stop() { s.Active = false }

// Range is the selected lines, [from, to] inclusive, whichever way it was drawn.
func (s Selection) Range() (from, to int) {
	if s.Anchor < s.Cursor {
		return s.Anchor, s.Cursor
	}
	return s.Cursor, s.Anchor
}

// Contains reports whether line i is selected.
func (s Selection) Contains(i int) bool {
	if !s.Active {
		return false
	}
	from, to := s.Range()
	return i >= from && i <= to
}

// Text renders lines as they would be pasted: with their timestamps (and, in
// a merged view, their source), or just what the program printed.
func Text(lines []control.LogFrame, loc *time.Location, withTime, withSource bool) string {
	var b strings.Builder
	for _, l := range lines {
		if withTime && !l.Time.IsZero() {
			b.WriteString(l.Time.In(loc).Format("15:04:05"))
			b.WriteByte(' ')
		}
		if withSource {
			b.WriteString(l.Instance)
			b.WriteByte(' ')
		}
		b.WriteString(l.Line)
		b.WriteByte('\n')
	}
	return b.String()
}

// Clipboard is the command that puts text on the clipboard, by OSC 52.
func Clipboard(text string) tea.Cmd { return tea.SetClipboard(text) }

// Save writes text to a file in dir and returns its path.
func Save(dir, name, text string, now time.Time) (string, error) {
	path := filepath.Join(dir, fmt.Sprintf("doze-%s-%s.log", name, now.Format("20060102-150405")))
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
