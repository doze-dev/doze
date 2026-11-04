package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// Instance is an instance's page, as the Instance, Cache, Mailpit, Task,
// AppLogs and Crash boards lay it out: the head line, the tabs, and the
// cards of the overview.
type Instance struct {
	Key     string `json:"key"`
	Stack   string `json:"stack"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Engine  string `json:"engine"`
	Version string `json:"version,omitempty"`
	Look    Look   `json:"look"`
	// Path is the line beside the name: "postgres 17 · awake for 14m ·
	// pid 612", "task · done · ran 2m ago in 1.2s".
	Path    string `json:"path"`
	Because string `json:"because"`
	Hint    string `json:"hint,omitempty"`
	Up      string `json:"up,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Run     string `json:"run,omitempty"`
	// Connect is the connect card's rows.
	Connect []KV `json:"connect,omitempty"`
	// Holders is why it is awake, as pills with a word on each; Sleeps is
	// the sentence under them.
	Holders []Pill `json:"holders,omitempty"`
	Sleeps  string `json:"sleeps,omitempty"`
	// Dozing is set while it counts down: the clock left and the fraction
	// of the idle time gone, for the bar.
	Dozing *Countdown `json:"dozing,omitempty"`
	// Web is the web console's URL when it has one.
	Web  string `json:"web,omitempty"`
	Tabs []Tab  `json:"tabs"`
	// Needs is what it boots first, with each one's state.
	Needs  []Need  `json:"needs,omitempty"`
	Vitals *Vitals `json:"vitals,omitempty"`
	// Recent is doze's own events for it, newest last.
	Recent []Recent `json:"recent,omitempty"`
	// Runs is a task's history, newest first.
	Runs []Run `json:"runs,omitempty"`
	// Crash is set while it is crashing or has failed.
	Crash *Crash `json:"crash,omitempty"`
	// Procs are the processes under it, for the logs page's box.
	Procs   []Proc `json:"procs,omitempty"`
	Side    []Side `json:"side"`
	Banner  Banner `json:"banner"`
	Missing bool   `json:"missing,omitempty"`
}

// KV is one row of a card: a key, a value, and what the copy button copies.
type KV struct {
	K    string `json:"k"`
	V    string `json:"v"`
	Copy string `json:"copy,omitempty"`
	Mono bool   `json:"mono"`
	Dim  bool   `json:"dim,omitempty"`
}

// Pill is one reason it is awake, and a word on it.
type Pill struct {
	Text    string `json:"text"`
	Explain string `json:"explain"`
}

// Countdown is how long until a dozing instance sleeps.
type Countdown struct {
	Left     string  `json:"left"`
	Fraction float64 `json:"fraction"`
}

// Tab is one tab of the page.
type Tab struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Need is one thing an instance needs, with its state.
type Need struct {
	Name  string `json:"name"`
	Known bool   `json:"known"`
	Look  Look   `json:"look"`
	Glyph string `json:"glyph"`
	Task  bool   `json:"task"`
}

// Vitals are the overview's charts, from the instance's series, with the
// latest reading as the figure.
type Vitals struct {
	RAM   Chart `json:"ram"`
	CPU   Chart `json:"cpu"`
	Conns Chart `json:"conns"`
}

// Chart is one series ready to draw: the points, the scale and the figures
// beside it. Points are the raw readings, oldest first.
type Chart struct {
	Points []float64 `json:"points"`
	Lo     float64   `json:"lo"`
	Hi     float64   `json:"hi"`
	// Now is the latest reading, as the card's figure; Sub the word beside it.
	Now  string `json:"now"`
	Sub  string `json:"sub"`
	Peak string `json:"peak"`
	Mean string `json:"mean"`
	// Span is how long the points cover, in words ("4 min").
	Span string `json:"span"`
}

// Recent is one of doze's events for the instance.
type Recent struct {
	At   string `json:"at"`   // clock time
	Kind string `json:"kind"` // woke, ready, idle, slept, crashed…
	Text string `json:"text"` // the rest of the sentence
	Tone string `json:"tone"`
}

// Run is one run of a task, newest first on the page.
type Run struct {
	At   string `json:"at"`
	When string `json:"when"` // clock time today, else the date
	Ago  string `json:"ago"`
	Why  string `json:"why"`
	Took string `json:"took,omitempty"`
	OK   bool   `json:"ok"`
	Done bool   `json:"done"`
}

// Crash is what the Crash board shows: the error, the tries, the next try.
type Crash struct {
	Error    string  `json:"error"`
	Tries    string  `json:"tries,omitempty"` // "try 3 of 5"
	Fraction float64 `json:"fraction"`
	NextIn   string  `json:"nextIn,omitempty"` // "4 s"
	GaveUp   bool    `json:"gaveUp"`
}

// Proc is one process under an instance.
type Proc struct {
	Cmd   string `json:"cmd"`
	PID   int    `json:"pid"`
	Mem   string `json:"mem"`
	Depth int    `json:"depth"`
}

// InstanceOf computes an instance's page. Missing is set when the stack has
// no instance of that name.
func InstanceOf(st hub.StackView, name string, now time.Time) Instance {
	snap := snapshot(st)
	v, ok := display.Find(snap, name)
	out := Instance{Key: st.Key, Stack: st.Name, Name: name, Banner: BannerOf(st, now), Side: SideOf(st)}
	if !ok {
		out.Missing = true
		return out
	}
	out.Kind, out.Engine, out.Version, out.PID, out.Run = v.Kind, v.Engine, v.Version, v.PID, v.Run
	out.Look = lookOf(v)
	out.Because, out.Hint = display.Because(v, snap, now), display.Hint(v, snap, now)
	if display.Awake(v.State) && !v.StartedAt.IsZero() {
		out.Up = display.Ago(now.Sub(v.StartedAt))
	}
	out.Path = pathLine(v, out, now)
	if display.HasWeb(v) {
		out.Web = display.WebURL(v)
	}
	for _, t := range display.Tabs(v, nil) {
		out.Tabs = append(out.Tabs, Tab{ID: t.ID, Title: t.Title})
	}
	out.Connect = connectRows(v)
	out.Holders, out.Sleeps = holders(v, st.IdleTimeout)
	if v.State == "idle" && !v.IdleSince.IsZero() && st.IdleTimeout > 0 {
		gone := now.Sub(v.IdleSince)
		if left := st.IdleTimeout - gone; left > 0 {
			out.Dozing = &Countdown{Left: display.Clock(left), Fraction: part(float64(gone), float64(st.IdleTimeout))}
		}
	}
	for _, n := range display.Needs(v, snap) {
		out.Needs = append(out.Needs, Need{Name: n.Name, Known: n.Known, Look: Look{n.Look.Word, n.Look.Glyph, string(n.Look.Tone)}, Glyph: n.Glyph, Task: n.Task})
	}
	if s, ok := st.Series[name]; ok && len(s.RAM) > 0 {
		out.Vitals = &Vitals{
			RAM:   chartOf(s.RAM, display.BytesLabel, "resident", len(s.RAM)),
			CPU:   chartOf(s.CPU, display.Pct, "of one core", len(s.CPU)),
			Conns: chartOf(s.Conns, display.Count, "connections", len(s.Conns)),
		}
		out.Vitals.CPU.Now = display.Cores(s.CPU[len(s.CPU)-1])
		out.Vitals.CPU.Sub = "cores · " + display.Pct(s.CPU[len(s.CPU)-1]) + " of one"
	}
	for _, e := range st.Events {
		if e.Instance != name {
			continue
		}
		kind, rest, _ := strings.Cut(display.Event(e), " ")
		out.Recent = append(out.Recent, Recent{At: e.Time.Local().Format("15:04:05"), Kind: kind, Text: rest, Tone: eventTone(kind)})
	}
	if len(out.Recent) > 8 {
		out.Recent = out.Recent[len(out.Recent)-8:]
	}
	if v.Kind == "task" {
		runs := display.RunHistory(st.Events, name, v.Task)
		for i := len(runs) - 1; i >= 0; i-- {
			r := runs[i]
			out.Runs = append(out.Runs, Run{
				At: r.At.UTC().Format(time.RFC3339), When: when(r.At, now), Ago: ago(r.At, now), Why: r.Why,
				Took: took(r), OK: r.OK, Done: r.Done,
			})
		}
	}
	if display.Troubled(v) && v.Kind != "task" {
		c := &Crash{Error: display.FirstLine(v.LastError), GaveUp: v.State == "failed" || v.State == "error"}
		if v.RestartMax > 0 {
			c.Tries = fmt.Sprintf("try %d of %d", v.RestartCount, v.RestartMax)
			c.Fraction = part(float64(v.RestartCount), float64(v.RestartMax))
		}
		if !v.NextRestart.IsZero() && v.NextRestart.After(now) {
			c.NextIn = fmt.Sprintf("%d s", int(v.NextRestart.Sub(now).Seconds()+0.5))
		}
		out.Crash = c
	}
	for _, p := range v.Children {
		out.Procs = append(out.Procs, Proc{Cmd: p.Cmd, PID: p.PID, Mem: display.MB(p.RSS), Depth: p.Depth})
	}
	return out
}

// pathLine is the line beside the name on the head.
func pathLine(v control.InstanceView, in Instance, now time.Time) string {
	var parts []string
	switch v.Kind {
	case "task":
		parts = append(parts, "task", in.Look.Word)
		if v.Task != nil {
			parts = append(parts, fmt.Sprintf("ran %s ago in %s", display.Ago(now.Sub(v.Task.RanAt)), (time.Duration(v.Task.TookMS)*time.Millisecond).Round(100*time.Millisecond)))
		} else {
			parts = append(parts, "has not run")
		}
	case "module":
		parts = append(parts, engine(v))
		if in.Up != "" {
			parts = append(parts, "awake for "+in.Up)
		} else {
			parts = append(parts, in.Look.Word)
		}
		if v.PID > 0 && display.Awake(v.State) {
			parts = append(parts, fmt.Sprintf("pid %d", v.PID))
		}
	default:
		parts = append(parts, engine(v))
		if v.Run != "" {
			parts = append(parts, display.ShortRun(v.Run))
		}
		if display.Troubled(v) {
			parts = append(parts, in.Look.Word)
		} else if in.Up != "" {
			parts = append(parts, "awake "+in.Up)
		} else {
			parts = append(parts, in.Look.Word)
		}
	}
	if !display.Awake(v.State) && !display.Troubled(v) && v.Kind != "task" {
		parts = append(parts, in.Because)
	}
	return strings.Join(parts, " · ")
}

// connectRows are the connect card's rows, as the Instance board lists them.
func connectRows(v control.InstanceView) []KV {
	var rows []KV
	if v.URL != "" {
		rows = append(rows, KV{K: "URL", V: v.URL, Copy: v.URL, Mono: true})
	}
	if v.EnvVar != "" {
		rows = append(rows, KV{K: "Env var", V: v.EnvVar, Copy: "export " + v.EnvVar + "=" + v.URL, Mono: true})
	}
	if a := display.Address(v); a != "" {
		rows = append(rows, KV{K: "Address", V: a, Copy: a, Mono: true})
	}
	if v.DataDir != "" {
		rows = append(rows, KV{K: "Data", V: display.Tilde(v.DataDir), Mono: true, Dim: true})
	}
	return rows
}

// holders are the "why it's awake" pills, and the sentence under them.
func holders(v control.InstanceView, idle time.Duration) ([]Pill, string) {
	var pills []Pill
	for _, h := range v.Holders {
		switch h.Kind {
		case "dependant":
			pills = append(pills, Pill{Text: h.Name + " needs it", Explain: "a dependant that's awake keeps it awake"})
		case "connection":
			pills = append(pills, Pill{Text: fmt.Sprintf("%d connection%s", h.Count, display.Plural(h.Count)), Explain: "open clients, through doze's proxy"})
		case "pin":
			pills = append(pills, Pill{Text: "pinned awake", Explain: "stays up past the idle time until unpinned"})
		}
	}
	if v.AlwaysOn {
		return pills, "It runs until stopped: it has no port, so nothing can hold it, and it never sleeps on its own."
	}
	after := ""
	if idle > 0 {
		after = " after " + display.Clock(idle)
	}
	if len(pills) == 0 {
		if v.State == "idle" {
			return nil, "nothing: it sleeps when the idle time is up"
		}
		return nil, "Nothing holds it. It sleeps" + after + " with nothing connected; the next connection wakes it."
	}
	return pills, "With neither, it sleeps" + after + ". Its data stays; the next connection wakes it."
}

func eventTone(kind string) string {
	switch kind {
	case "woke":
		return "accent"
	case "ready":
		return "good"
	case "idle", "slept":
		return "warn"
	case "crashed", "failed":
		return "bad"
	}
	return "dim"
}

func when(t, now time.Time) string {
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Local().Format("15:04:05")
	}
	return t.Local().Format("Jan 2")
}

func took(r display.Run) string {
	if !r.Done || r.Took == 0 {
		return ""
	}
	return r.Took.Round(100 * time.Millisecond).String()
}

func chartOf(vs []float64, label func(float64) string, sub string, n int) Chart {
	peak, mean := display.Stats(vs)
	lo, hi := display.Scale(vs, 0, false)
	return Chart{
		Points: append([]float64(nil), vs...), Lo: lo, Hi: hi,
		Now: label(vs[len(vs)-1]), Sub: sub,
		Peak: label(peak), Mean: label(mean),
		Span: display.SpanWord(n),
	}
}
