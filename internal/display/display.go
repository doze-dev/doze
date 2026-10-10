// Package display is the vocabulary every surface shares: the words for an
// instance's state, and the note that says what it is waiting on or why it is
// awake. The CLI paints them with colours and the dash with its theme; the
// words are chosen here, once, so the two cannot disagree.
package display

import (
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
)

// Awake reports whether a daemon state word means the process is up.
func Awake(state string) bool {
	return state == "active" || state == "idle" || state == "booting"
}

// Word maps the daemon's state words to the ones a person reads: awake,
// dozing (awake with nobody holding it, counting down to sleep), waking and
// asleep. JSON keeps the daemon's words.
func Word(state string) string {
	switch state {
	case "active":
		return "awake"
	case "idle":
		return "dozing"
	case "booting":
		return "waking"
	case "reaped", "":
		return "asleep"
	}
	return state
}

// Note is the sentence beside a row: why it is awake, what is wrong, when it
// sleeps. bad says it is a problem and should be painted as one. idle is the
// stack's idle time, which an instance's own overrides.
func Note(v control.InstanceView, idle time.Duration, now time.Time) (text string, bad bool) {
	switch {
	case v.State == "crashing" || v.State == "failed":
		return FirstLine(v.LastError), true
	case v.Tainted:
		note := "its last sync did not finish"
		if v.LastError != "" {
			note += ": " + FirstLine(v.LastError)
		}
		return note, true
	case v.LastError != "" && !Awake(v.State):
		return FirstLine(v.LastError), false
	}
	var parts []string
	if v.NameHeld != "" {
		parts = append(parts, v.NameHeld)
	}
	var by []string
	clients := 0
	for _, h := range v.Holders {
		switch h.Kind {
		case "dependant":
			by = append(by, h.Name)
		case "connection":
			clients = h.Count
		}
	}
	if len(by) > 0 {
		parts = append(parts, "held by "+strings.Join(by, ", "))
	}
	if v.KeepAwake {
		parts = append(parts, "pinned")
	}
	if v.NameOnly {
		parts = append(parts, "by name only: 127.0.0.1 has this port taken")
	}
	if v.IdleAfter > 0 {
		idle = v.IdleAfter // its own idle time, not the stack's
	}
	if len(parts) == 0 && clients == 0 && v.State == "idle" && idle > 0 && !v.IdleSince.IsZero() {
		if left := idle - now.Sub(v.IdleSince); left > 0 {
			parts = append(parts, "sleeps in "+left.Round(time.Second).String())
		}
	}
	return strings.Join(parts, " · "), false
}

// FirstLine is s up to its first newline.
func FirstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
