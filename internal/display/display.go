// Package display is the vocabulary every surface shares: the words for an
// instance's state, and the note that says what it is waiting on or why it is
// awake. The CLI paints them with colours and the dash with its theme; the
// words are chosen here, once, so the two cannot disagree.
package display

import (
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/journal"
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

// Event is one journal event as a sentence: what happened to an instance,
// and why.
func Event(e journal.Event) string {
	because := ""
	switch e.Cause.Kind {
	case journal.ByConnection:
		because = " (a connection)"
	case journal.ByDependency:
		because = " (" + e.Cause.By + " needed it)"
	case journal.ByCommand:
		because = " (asked for)"
	case journal.ByRestart:
		because = " (restart policy)"
	case journal.ByIdle:
		because = " (idle)"
	case journal.ByShutdown:
		because = " (shutting down)"
	case journal.ByWatch:
		because = " (" + e.Cause.By + " changed)"
	}
	took := ""
	if e.Took > 0 {
		took = " in " + e.Took.Round(time.Millisecond).String()
	}
	switch e.Kind {
	case journal.Wake:
		s := "woke" + because
		// the detail is often the cause again ("asked for"): say it once
		if e.Detail != "" && !strings.Contains(because, e.Detail) {
			s += " — " + e.Detail
		}
		return s
	case journal.Ready:
		return "ready" + took
	case journal.Sleep:
		return "went to sleep" + because
	case journal.Crash:
		return "crashed: " + e.Detail
	case journal.Restart:
		return "restarting" + because
	case journal.GiveUp:
		return "gave up restarting: " + e.Detail
	case journal.Failed:
		return "failed" + took + ": " + FirstLine(e.Detail)
	case journal.Step:
		return "step " + e.Detail + took
	case journal.Pin:
		if e.Detail == "on" {
			return "pinned awake"
		}
		return "unpinned"
	}
	return e.Kind
}

// Level reads a log line's severity from what the program wrote, and says
// nothing when it cannot tell: programs print what they like, so this is a
// reading of the common shapes and not a promise. It knows a level word near
// the front ("[warn]", "ERROR:", "| info |", the "LOG:" postgres puts after its
// prefix), logfmt (level=warn, lvl=, severity=), and the JSON loggers write
// ({"level":"error"}, "severity":"WARNING", and pino's numbers, 30 for info).
// It returns "error", "warn", "info", "debug", or "" for a line that says
// nothing.
func Level(line string) string {
	head := strings.ToLower(line)
	if len(head) > 120 {
		head = head[:120]
	}
	// JSON and logfmt carry the level under a key, wherever it falls
	for _, key := range []string{"level", "lvl", "severity", "loglevel"} {
		for _, pre := range []string{"\"" + key + "\":", "\"" + key + "\": ", key + "="} {
			at := strings.Index(head, pre)
			if at < 0 {
				continue
			}
			val := strings.TrimLeft(head[at+len(pre):], "\" ")
			end := strings.IndexAny(val, "\", }")
			if end < 0 {
				end = len(val)
			}
			if l := levelWord(val[:end]); l != "" {
				return l
			}
		}
	}
	if len(head) > 60 {
		head = head[:60]
	}
	for _, w := range levelWords {
		for _, pat := range []string{"[" + w.word + "]", " " + w.word + " ", "|" + w.word + "|", " " + w.word + ":"} {
			if strings.Contains(head, pat) {
				return w.level
			}
		}
		if strings.HasPrefix(head, w.word+" ") || strings.HasPrefix(head, w.word+":") {
			return w.level
		}
	}
	return ""
}

var levelWords = []struct{ word, level string }{
	{"fatal", "error"}, {"panic", "error"}, {"critical", "error"}, {"error", "error"}, {"err", "error"},
	{"warning", "warn"}, {"warn", "warn"}, {"wrn", "warn"}, {"info", "info"}, {"inf", "info"}, {"notice", "info"}, {"log", "info"},
	{"debug", "debug"}, {"dbg", "debug"}, {"trace", "debug"}, {"trc", "debug"},
}

// levelWord maps a value found under a level key: a name, or pino's numbers.
func levelWord(v string) string {
	switch v {
	case "10", "20":
		return "debug"
	case "30":
		return "info"
	case "40":
		return "warn"
	case "50", "60":
		return "error"
	}
	for _, w := range levelWords {
		if v == w.word {
			return w.level
		}
	}
	return ""
}
