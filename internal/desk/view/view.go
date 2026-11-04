// Package view computes what the app's pages show, as plain data, from what
// the hub knows. Every fact and every word comes from here and from
// internal/display; the frontend renders the JSON and never derives a stack
// fact itself (design D16). The shapes are the contract with the page, pinned
// by goldens in testdata/.
package view

import (
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// Look is how an instance's state reads: the word, the glyph and the tone
// (good, warn, cool, bad, dim, text, bright) it is drawn in.
type Look struct {
	Word  string `json:"word"`
	Glyph string `json:"glyph"`
	Tone  string `json:"tone"`
}

func lookOf(v control.InstanceView) Look {
	l := display.LookOf(v)
	return Look{Word: l.Word, Glyph: l.Glyph, Tone: string(l.Tone)}
}

// Banner is what the page says when what it shows cannot be believed.
type Banner struct {
	OK   bool   `json:"ok"`
	Text string `json:"text,omitempty"`
}

// BannerOf is the connection banner of a stack at now.
func BannerOf(st hub.StackView, now time.Time) Banner {
	if !st.Running {
		return Banner{OK: true}
	}
	var since time.Duration
	if !st.LastFrame.IsZero() {
		since = now.Sub(st.LastFrame)
	}
	ok, text := display.Banner(st.Conn == hub.ConnConnecting, st.Conn == hub.ConnDown, st.RetryAt.Sub(now), since)
	return Banner{OK: ok, Text: text}
}

// snapshot is the hub's stack as the display package reads one.
func snapshot(st hub.StackView) control.Snapshot {
	return control.Snapshot{Stack: st.Name, Dir: st.Dir, Instances: st.Instances}
}

// ago is how long ago t was, in the display package's words, or "" for the
// zero time.
func ago(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	return display.Ago(now.Sub(t))
}
