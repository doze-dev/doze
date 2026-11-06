package view

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
	"github.com/doze-dev/doze/internal/journal"
)

// Activity is what a stack did over a span (the last hour, day or week),
// read from its journal on disk: how long each service slept while the
// stack was up, and the memory that left free; what woke each one and how
// often; what crashed. It answers "is doze saving me anything, and what
// keeps waking my stack". Positions are fractions of the span, so the page
// draws without knowing about time.
type Activity struct {
	Key   string `json:"key"`
	Stack string `json:"stack"`
	// Span is "1h", "24h" or "7d"; Inst narrows to one service, or "".
	Span string `json:"span"`
	Inst string `json:"inst,omitempty"`
	// Ticks label the axis of each row's strip.
	Ticks   []TimelineTick  `json:"ticks"`
	Summary ActivitySummary `json:"summary"`
	Rows    []ActivityRow   `json:"rows"`
	// Trouble is the crashes, failed starts and give-ups, newest first.
	Trouble []Recent `json:"trouble"`
	// Events are the journal's lines in the span, newest first, at most 150.
	Events []Recent `json:"events"`
	// Short says the journal starts inside the span: "the journal goes back
	// 3 days", so the figures cover less than the span says.
	Short string `json:"short,omitempty"`
	// Empty says nothing happened in the span.
	Empty bool `json:"empty,omitempty"`
}

// ActivitySummary is the stack's line at the top.
type ActivitySummary struct {
	// Up is how long the stack was up in the span: "21h of 24h".
	Up string `json:"up"`
	// Asleep is the share of service-time asleep while up: "71%".
	Asleep string `json:"asleep"`
	// Freed is memory left free by sleeping: "about 3.2 GB·h".
	Freed string `json:"freed,omitempty"`
	// FreedNote says what the estimate rests on.
	FreedNote string `json:"freedNote,omitempty"`
	Wakes     int    `json:"wakes"`
	Crashes   int    `json:"crashes"`
}

// ActivityRow is one service over the span.
type ActivityRow struct {
	Name string `json:"name"`
	Look Look   `json:"look"`
	// AsleepPart is the share asleep while the stack was up, 0..1; Asleep
	// says it in words ("19h asleep · 79%").
	AsleepPart float64 `json:"asleepPart"`
	Asleep     string  `json:"asleep"`
	Wakes      int     `json:"wakes"`
	// WokenBy is what woke it, most often first: "9 for api · 5 by a
	// connection".
	WokenBy string `json:"wokenBy,omitempty"`
	Crashes int    `json:"crashes,omitempty"`
	Freed   string `json:"freed,omitempty"`
	// Spans and Marks draw its strip: awake stretches and crashes.
	Spans []TimelineSpanBar `json:"spans"`
	Marks []TimelineMark    `json:"marks"`
	// Never says it never slept in the span (pinned, or always in use).
	Never bool `json:"never,omitempty"`
}

// ActivitySpans are the spans the page offers, by name.
var ActivitySpans = map[string]time.Duration{"1h": time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour}

// ActivityOf lays out a stack's journal over the span ending now. ram is
// each service's last known memory in bytes, by name, for the estimate of
// what sleeping left free.
func ActivityOf(st hub.StackView, events []journal.Event, spanName, inst string, ram map[string]int64, now time.Time) Activity {
	span, ok := ActivitySpans[spanName]
	if !ok {
		spanName, span = "24h", ActivitySpans["24h"]
	}
	from := now.Add(-span)
	pos := func(t time.Time) float64 { return min(1, max(0, float64(t.Sub(from))/float64(span))) }
	name := st.Name
	if name == "" {
		name = baseName(st.Dir)
	}
	out := Activity{Key: st.Key, Stack: name, Span: spanName, Inst: inst, Rows: []ActivityRow{}, Trouble: []Recent{}, Events: []Recent{}}
	out.Ticks = activityTicks(from, span)

	sort.SliceStable(events, func(i, j int) bool { return events[i].Time.Before(events[j].Time) })
	if len(events) > 0 && events[0].Time.After(from) {
		out.Short = "the journal goes back " + display.Ago(now.Sub(events[0].Time)) + "; the figures cover that"
	}

	// The services: as the stack lists them, then any the journal names
	// that it no longer declares.
	var names []string
	seen := map[string]bool{}
	looks := map[string]Look{}
	for _, r := range display.Rows(snapshot(st)) {
		if r.V.Kind == "task" {
			seen[r.V.Name] = true // a task runs and ends; it has no sleep to count
			continue
		}
		names, seen[r.V.Name] = append(names, r.V.Name), true
		l := display.LookOf(r.V)
		looks[r.V.Name] = Look{l.Word, l.Glyph, string(l.Tone)}
	}
	for _, e := range events {
		if e.Instance != "" && !seen[e.Instance] && e.Kind != journal.Step {
			names, seen[e.Instance] = append(names, e.Instance), true
		}
	}
	// Replay: each service's state, and whether the stack is up, over time.
	type state struct {
		awake   bool
		since   time.Time
		ready   time.Time
		cause   string
		awakeD  time.Duration
		asleepD time.Duration
		wakes   int
		crashes int
		causes  map[string]int
		spans   []TimelineSpanBar
		marks   []TimelineMark
	}
	states := map[string]*state{}
	get := func(n string) *state {
		s := states[n]
		if s == nil {
			s = &state{causes: map[string]int{}, spans: []TimelineSpanBar{}, marks: []TimelineMark{}}
			states[n] = s
		}
		return s
	}
	up := false
	var upD time.Duration
	last := from
	if len(events) > 0 && events[0].Time.After(from) {
		last = events[0].Time
	}
	// advance counts the time from last to t for every service.
	advance := func(t time.Time) {
		if t.Before(from) {
			return
		}
		a := last
		if a.Before(from) {
			a = from
		}
		if !t.After(a) {
			last = t
			return
		}
		d := t.Sub(a)
		if up {
			upD += d
		}
		for _, n := range names {
			s := get(n)
			switch {
			case s.awake:
				s.awakeD += d
			case up:
				s.asleepD += d
			}
		}
		last = t
	}
	closeSpan := func(n string, at time.Time, how string) {
		s := get(n)
		if !s.awake {
			return
		}
		s.awake = false
		if at.Before(from) {
			return
		}
		begin := s.since
		if begin.Before(from) {
			begin = from
		}
		bar := TimelineSpanBar{From: pos(begin), To: pos(at), Title: "awake " + display.Ago(at.Sub(s.since)) + " · woke " + s.cause + " · " + how}
		if !s.ready.IsZero() && s.ready.After(from) {
			bar.Ready = pos(s.ready)
		}
		s.spans = append(s.spans, bar)
	}
	// A service awake now that the journal never saw wake has been awake
	// since before it begins: it starts the replay awake, and its stack up.
	woke := map[string]bool{}
	for _, e := range events {
		if e.Kind == journal.Wake {
			woke[e.Instance] = true
		}
	}
	if st.Running {
		for _, v := range st.Instances {
			if v.Kind != "task" && display.AwakeNow(v) && !woke[v.Name] {
				s := get(v.Name)
				s.awake, s.since, s.cause = true, last, "before the journal begins"
				up = true
			}
		}
	}
	for _, e := range events {
		advance(e.Time)
		in := !e.Time.Before(from)
		if e.Instance == "" {
			continue
		}
		s := get(e.Instance)
		switch e.Kind {
		case journal.Wake:
			up = true
			if !s.awake {
				s.awake, s.since, s.ready = true, e.Time, time.Time{}
				s.cause = causeWord(e.Cause)
				if in {
					s.wakes++
					s.causes[s.cause]++
				}
			}
		case journal.Ready:
			up = true
			s.ready = e.Time
		case journal.Sleep:
			closeSpan(e.Instance, e.Time, "slept ("+orWord(e.Cause.Kind, "asked")+")")
			if e.Cause.Kind == journal.ByShutdown {
				up = false // the stack is going down; it comes back with a wake
			}
		case journal.Crash, journal.Failed, journal.GiveUp:
			closeSpan(e.Instance, e.Time, strings.ReplaceAll(e.Kind, "_", " "))
			if in {
				s.crashes++
				s.marks = append(s.marks, TimelineMark{At: pos(e.Time), Kind: e.Kind, Title: e.Time.Local().Format("Mon 15:04") + " " + strings.ReplaceAll(e.Kind, "_", " ") + firstWords(e.Detail)})
				kind, rest, _ := strings.Cut(display.Event(e), " ")
				out.Trouble = append(out.Trouble, Recent{At: stamp(e.Time, span), Kind: kind, Text: strings.TrimRight(e.Instance+" "+rest, ": "), Tone: "bad"})
			}
		}
		if in && (inst == "" || e.Instance == inst) && e.Kind != journal.Step {
			kind, rest, _ := strings.Cut(display.Event(e), " ")
			out.Events = append(out.Events, Recent{At: stamp(e.Time, span), Kind: kind, Text: e.Instance + " " + rest, Tone: eventTone(kind)})
		}
	}
	// A running stack is up to now, whatever its journal last said.
	if st.Running {
		up = true
	}
	advance(now)
	for _, n := range names {
		s := get(n)
		if s.awake {
			begin := s.since
			if begin.Before(from) {
				begin = from
			}
			bar := TimelineSpanBar{From: pos(begin), To: 1, Open: true, Title: "awake " + display.Ago(now.Sub(s.since)) + " · woke " + s.cause + " · still awake"}
			if !s.ready.IsZero() && s.ready.After(from) {
				bar.Ready = pos(s.ready)
			}
			s.spans = append(s.spans, bar)
		}
	}

	// Rows and totals.
	var awakeAll, asleepAll time.Duration
	var freedGBh float64
	priced := 0
	for _, n := range names {
		if inst != "" && n != inst {
			continue
		}
		s := get(n)
		row := ActivityRow{Name: n, Look: looks[n], Wakes: s.wakes, Crashes: s.crashes, Spans: s.spans, Marks: s.marks}
		if row.Look.Word == "" {
			row.Look = Look{Word: "gone", Glyph: "·", Tone: "dim"}
		}
		total := s.awakeD + s.asleepD
		if total > 0 {
			row.AsleepPart = float64(s.asleepD) / float64(total)
		}
		row.Asleep = fmt.Sprintf("%s asleep · %d%%", hours(s.asleepD), int(row.AsleepPart*100+0.5))
		row.Never = s.asleepD < time.Minute && total > 0
		if row.Never {
			row.Asleep = "never slept"
		}
		if b := ram[n]; b > 0 && s.asleepD > 0 {
			gbh := float64(b) / (1 << 30) * s.asleepD.Hours()
			row.Freed = "~" + gbHours(gbh)
			freedGBh += gbh
			priced++
		}
		row.WokenBy = causeList(s.causes)
		awakeAll += s.awakeD
		asleepAll += s.asleepD
		out.Summary.Wakes += s.wakes
		out.Summary.Crashes += s.crashes
		out.Rows = append(out.Rows, row)
	}
	out.Summary.Up = hours(upD) + " of " + hours(span)
	if t := awakeAll + asleepAll; t > 0 {
		out.Summary.Asleep = fmt.Sprintf("%d%%", int(float64(asleepAll)/float64(t)*100+0.5))
	} else {
		out.Summary.Asleep = "—"
	}
	if priced > 0 {
		out.Summary.Freed = "about " + gbHours(freedGBh)
		out.Summary.FreedNote = fmt.Sprintf("each service's memory when last seen awake, times the hours it slept; %d of %d had a figure", priced, len(out.Rows))
	}
	reverse(out.Trouble)
	reverse(out.Events)
	if len(out.Events) > 150 {
		out.Events = out.Events[:150]
	}
	out.Empty = len(out.Events) == 0 && upD == 0
	return out
}

// causeWord is why a service woke, as the rows say it.
func causeWord(c journal.Cause) string {
	switch {
	case c.By != "" && c.Kind == journal.ByWatch:
		return "for a change to " + c.By
	case c.By != "":
		return "for " + c.By
	case c.Kind == journal.ByConnection:
		return "by a connection"
	case c.Kind == journal.ByCommand:
		return "when asked"
	case c.Kind == journal.ByRestart:
		return "to restart"
	case c.Kind == "":
		return "on its own"
	}
	return "by " + c.Kind
}

// causeList is the causes, most often first: "9 for api · 5 by a connection".
func causeList(causes map[string]int) string {
	type kv struct {
		k string
		n int
	}
	var all []kv
	for k, n := range causes {
		all = append(all, kv{k, n})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].n != all[j].n {
			return all[i].n > all[j].n
		}
		return all[i].k < all[j].k
	})
	var parts []string
	for i, c := range all {
		if i == 3 {
			break
		}
		parts = append(parts, fmt.Sprintf("%d %s", c.n, c.k))
	}
	return strings.Join(parts, " · ")
}

// activityTicks label the axis: every 10 minutes for an hour, every 4
// hours for a day, every day for a week.
func activityTicks(from time.Time, span time.Duration) []TimelineTick {
	step, format, n := 10*time.Minute, "15:04", 6
	switch {
	case span >= 7*24*time.Hour:
		step, format, n = 24*time.Hour, "Mon", 7
	case span >= 24*time.Hour:
		step, format, n = 4*time.Hour, "15:04", 6
	}
	var out []TimelineTick
	for i := 0; i <= n; i++ {
		t := from.Add(time.Duration(i) * step)
		label := t.Local().Format(format)
		if i == n {
			label = "now"
		}
		out = append(out, TimelineTick{At: float64(i) / float64(n), Label: label})
	}
	return out
}

// stamp is an event's time as the span wants it: the time of day within a
// day, the day and time over a week.
func stamp(t time.Time, span time.Duration) string {
	if span > 24*time.Hour {
		return t.Local().Format("Mon 15:04")
	}
	return t.Local().Format("15:04:05")
}

// hours is a duration as the rows say it: "45m", "19h", "3d 4h".
func hours(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()+0.5))
	}
	return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
}

// gbHours is memory over time: "1.4 GB·h", "320 MB·h".
func gbHours(gbh float64) string {
	if gbh < 1 {
		return fmt.Sprintf("%d MB·h", int(gbh*1024+0.5))
	}
	return fmt.Sprintf("%.1f GB·h", gbh)
}

func reverse(rs []Recent) {
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
}
