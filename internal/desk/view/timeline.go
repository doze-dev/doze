package view

import (
	"sort"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
	"github.com/doze-dev/doze/internal/journal"
)

// TimelineSpan is how far back the timeline looks.
const TimelineSpan = 30 * time.Minute

// Timeline is a stack's last half hour from its journal: a lane per
// service with the spans it was awake, the moments it crashed, and the
// events themselves, newest first. Positions are fractions of the span, so
// the page draws without knowing about time.
type Timeline struct {
	Key   string `json:"key"`
	Stack string `json:"stack"`
	// From and To label the axis: "13:42" and "now".
	From  string         `json:"from"`
	Ticks []TimelineTick `json:"ticks"`
	Lanes []TimelineLane `json:"lanes"`
	// Events are the journal's lines in the span, newest first.
	Events []Recent `json:"events"`
	// Empty says nothing happened in the span.
	Empty bool `json:"empty,omitempty"`
}

// TimelineTick is one mark on the axis.
type TimelineTick struct {
	At    float64 `json:"at"`
	Label string  `json:"label"`
}

// TimelineLane is one service.
type TimelineLane struct {
	Name  string            `json:"name"`
	Look  Look              `json:"look"`
	Spans []TimelineSpanBar `json:"spans"`
	Marks []TimelineMark    `json:"marks"`
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

// TimelineOf lays out the last TimelineSpan of st's journal at now.
func TimelineOf(st hub.StackView, now time.Time) Timeline {
	from := now.Add(-TimelineSpan)
	pos := func(t time.Time) float64 {
		f := float64(t.Sub(from)) / float64(TimelineSpan)
		return min(1, max(0, f))
	}
	out := Timeline{Key: st.Key, Stack: st.Name, From: from.Local().Format("15:04"), Lanes: []TimelineLane{}, Events: []Recent{}}
	for i := 0; i <= 6; i++ {
		t := from.Add(time.Duration(i) * TimelineSpan / 6)
		label := t.Local().Format("15:04")
		if i == 6 {
			label = "now"
		}
		out.Ticks = append(out.Ticks, TimelineTick{At: float64(i) / 6, Label: label})
	}

	type open struct {
		at    time.Time
		ready time.Time
		cause string
	}
	byName := map[string]*TimelineLane{}
	opened := map[string]*open{}
	lane := func(name string) *TimelineLane {
		if l, ok := byName[name]; ok {
			return l
		}
		l := &TimelineLane{Name: name, Spans: []TimelineSpanBar{}, Marks: []TimelineMark{}}
		byName[name] = l
		return l
	}
	closeSpan := func(name string, at time.Time, how string) {
		o := opened[name]
		if o == nil {
			return
		}
		delete(opened, name)
		if at.Before(from) {
			return
		}
		bar := TimelineSpanBar{From: pos(o.at), To: pos(at), Title: "awake " + display.Ago(at.Sub(o.at)) + " · woke " + o.cause + " · " + how}
		if !o.ready.IsZero() && o.ready.After(from) {
			bar.Ready = pos(o.ready)
		}
		lane(name).Spans = append(lane(name).Spans, bar)
	}
	events := append([]journal.Event(nil), st.Events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].Time.Before(events[j].Time) })
	for _, e := range events {
		if e.Instance == "" {
			continue
		}
		switch e.Kind {
		case journal.Wake:
			cause := e.Cause.Kind
			if e.Cause.By != "" {
				cause = "for " + e.Cause.By
			}
			if cause == "" {
				cause = "on its own"
			}
			opened[e.Instance] = &open{at: e.Time, cause: cause}
		case journal.Ready:
			if o := opened[e.Instance]; o != nil {
				o.ready = e.Time
			}
		case journal.Sleep:
			closeSpan(e.Instance, e.Time, "slept ("+orWord(e.Cause.Kind, "asked")+")")
		case journal.Crash, journal.Failed, journal.GiveUp:
			closeSpan(e.Instance, e.Time, e.Kind)
			if !e.Time.Before(from) {
				lane(e.Instance).Marks = append(lane(e.Instance).Marks, TimelineMark{At: pos(e.Time), Kind: e.Kind, Title: e.Time.Local().Format("15:04:05") + " " + strings.ReplaceAll(e.Kind, "_", " ") + firstWords(e.Detail)})
			}
		}
		if !e.Time.Before(from) {
			kind, rest, _ := strings.Cut(display.Event(e), " ")
			out.Events = append(out.Events, Recent{At: e.Time.Local().Format("15:04:05"), Kind: kind, Text: e.Instance + " " + rest, Tone: eventTone(kind)})
		}
	}
	waking := map[string]bool{}
	for _, v := range st.Instances {
		waking[v.Name] = v.State == "booting"
	}
	// What is still awake runs to now.
	for name, o := range opened {
		bar := TimelineSpanBar{From: pos(o.at), To: 1, Open: true, Title: "awake " + display.Ago(now.Sub(o.at)) + " · woke " + o.cause + " · still awake"}
		if !o.ready.IsZero() && o.ready.After(from) {
			bar.Ready = pos(o.ready)
		} else if o.ready.IsZero() && waking[name] {
			// Still starting: the whole stretch is the start's part, not awake.
			bar.Ready, bar.Title = 1, "waking for "+display.Ago(now.Sub(o.at))+" · woke "+o.cause+" · not ready yet"
		}
		lane(name).Spans = append(lane(name).Spans, bar)
	}
	// A service awake since before the journal's window has no wake in it:
	// it is drawn awake across the whole span.
	for _, v := range st.Instances {
		if v.Kind == "task" || !display.AwakeNow(v) {
			continue
		}
		if _, ok := opened[v.Name]; ok {
			continue
		}
		l := lane(v.Name)
		if n := len(l.Spans); n > 0 && l.Spans[n-1].Open {
			continue
		}
		if n := len(l.Spans); n == 0 {
			l.Spans = append(l.Spans, TimelineSpanBar{From: 0, To: 1, Open: true, Title: "awake since before this half hour"})
		}
	}
	// Lanes in the stack's own order, every service shown even when quiet.
	looks := map[string]Look{}
	for _, v := range st.Instances {
		l := display.LookOf(v)
		looks[v.Name] = Look{l.Word, l.Glyph, string(l.Tone)}
	}
	for _, r := range display.Rows(snapshot(st)) {
		if r.V.Kind == "task" {
			continue
		}
		l := lane(r.V.Name)
		l.Look = looks[r.V.Name]
		out.Lanes = append(out.Lanes, *l)
	}
	for i, j := 0, len(out.Events)-1; i < j; i, j = i+1, j-1 {
		out.Events[i], out.Events[j] = out.Events[j], out.Events[i]
	}
	if len(out.Events) > 60 {
		out.Events = out.Events[:60]
	}
	out.Empty = len(out.Events) == 0
	return out
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
