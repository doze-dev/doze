package view

import "github.com/doze-dev/doze/internal/display"

// The strip a service's Activity row draws (activity.go): its awake
// stretches and the moments it crashed, as fractions of the span.

// TimelineTick is one mark on the axis.
type TimelineTick struct {
	At    float64 `json:"at"`
	Label string  `json:"label"`
}

// TimelineSpanBar is a stretch it was awake: from its wake (or the edge of
// the span) to its sleep, crash or now. Waking is the start's own part, up
// to ready.
type TimelineSpanBar struct {
	From  float64 `json:"from"`
	To    float64 `json:"to"`
	Ready float64 `json:"ready,omitempty"` // where the start finished, when it did inside the span
	Title string  `json:"title"`
	Open  bool    `json:"open,omitempty"` // still awake now
}

// TimelineMark is a moment: a crash, a failed start, a give-up.
type TimelineMark struct {
	At    float64 `json:"at"`
	Kind  string  `json:"kind"`
	Title string  `json:"title"`
}

func orWord(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}

func firstWords(s string) string {
	s = display.FirstLine(s)
	if s == "" {
		return ""
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return " · " + s
}
