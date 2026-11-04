package view

import (
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// Instance is an instance's page: its state and story at the top, its tabs,
// and what the overview tab shows (vitals, needs, the address).
type Instance struct {
	Key     string `json:"key"`
	Stack   string `json:"stack"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Engine  string `json:"engine"`
	Version string `json:"version,omitempty"`
	Look    Look   `json:"look"`
	What    string `json:"what"`
	Because string `json:"because"`
	Hint    string `json:"hint,omitempty"`
	// Up is how long it has been awake ("14m"), "" when it is not.
	Up      string `json:"up,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Address string `json:"address,omitempty"`
	URL     string `json:"url,omitempty"`
	Run     string `json:"run,omitempty"`
	// InUse is who is using it right now ("api, 4 connection(s)"), the rule
	// for whether sleep and restart ask first.
	InUse string `json:"inUse,omitempty"`
	// Web is the web console's URL when it has one.
	Web       string    `json:"web,omitempty"`
	Tabs      []Tab     `json:"tabs"`
	Needs     []Need    `json:"needs,omitempty"`
	Vitals    *Vitals   `json:"vitals,omitempty"`
	Runs      []Run     `json:"runs,omitempty"`
	LastError string    `json:"lastError,omitempty"`
	Banner    Banner    `json:"banner"`
	Missing   bool      `json:"missing,omitempty"`
	Found     time.Time `json:"-"`
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

// Vitals are the overview's three charts, from the instance's series.
type Vitals struct {
	RAM   Chart `json:"ram"`
	CPU   Chart `json:"cpu"`
	Conns Chart `json:"conns"`
}

// Chart is one series ready to draw: the points, the scale and the two
// figures beside it. Points are the raw readings, oldest first; the page
// squeezes them to its width.
type Chart struct {
	Points []float64 `json:"points"`
	Lo     float64   `json:"lo"`
	Hi     float64   `json:"hi"`
	Peak   string    `json:"peak"`
	Mean   string    `json:"mean"`
	Unit   string    `json:"unit"`
	// Span is how long the points cover, in words ("4 min").
	Span string `json:"span"`
}

// Run is one run of a task, newest first on the page.
type Run struct {
	At   string `json:"at"`
	Ago  string `json:"ago"`
	Why  string `json:"why"`
	Took string `json:"took,omitempty"`
	OK   bool   `json:"ok"`
	Done bool   `json:"done"`
}

// InstanceOf computes an instance's page. Missing is set when the stack has
// no instance of that name.
func InstanceOf(st hub.StackView, name string, now time.Time) Instance {
	snap := snapshot(st)
	v, ok := display.Find(snap, name)
	out := Instance{Key: st.Key, Stack: st.Name, Name: name, Banner: BannerOf(st, now)}
	if !ok {
		out.Missing = true
		return out
	}
	out.Kind, out.Engine, out.Version, out.PID, out.Run = v.Kind, v.Engine, v.Version, v.PID, v.Run
	out.Look = lookOf(v)
	out.What, out.Because, out.Hint = display.What(v, snap, now), display.Because(v, snap, now), display.Hint(v, snap, now)
	out.Address, out.URL, out.InUse = display.Address(v), v.URL, display.InUse(v)
	out.LastError = display.FirstLine(v.LastError)
	if display.HasWeb(v) {
		out.Web = display.WebURL(v)
	}
	if display.Awake(v.State) && !v.StartedAt.IsZero() {
		out.Up = display.Ago(now.Sub(v.StartedAt))
	}
	for _, t := range display.Tabs(v, nil) {
		out.Tabs = append(out.Tabs, Tab{ID: t.ID, Title: t.Title})
	}
	for _, n := range display.Needs(v, snap) {
		out.Needs = append(out.Needs, Need{Name: n.Name, Known: n.Known, Look: Look{n.Look.Word, n.Look.Glyph, string(n.Look.Tone)}, Glyph: n.Glyph, Task: n.Task})
	}
	if s, ok := st.Series[name]; ok && len(s.RAM) > 0 {
		out.Vitals = &Vitals{
			RAM:   chartOf(s.RAM, display.BytesLabel, "bytes", len(s.RAM)),
			CPU:   chartOf(s.CPU, display.Pct, "%", len(s.CPU)),
			Conns: chartOf(s.Conns, display.Count, "", len(s.Conns)),
		}
	}
	if v.Kind == "task" {
		runs := display.RunHistory(st.Events, name, v.Task)
		for i := len(runs) - 1; i >= 0; i-- {
			r := runs[i]
			out.Runs = append(out.Runs, Run{
				At: r.At.UTC().Format(time.RFC3339), Ago: ago(r.At, now), Why: r.Why,
				Took: took(r), OK: r.OK, Done: r.Done,
			})
		}
	}
	return out
}

func took(r display.Run) string {
	if !r.Done || r.Took == 0 {
		return ""
	}
	return display.Clock(r.Took)
}

func chartOf(vs []float64, label func(float64) string, unit string, n int) Chart {
	peak, mean := display.Stats(vs)
	lo, hi := display.Scale(vs, 0, false)
	return Chart{
		Points: append([]float64(nil), vs...), Lo: lo, Hi: hi,
		Peak: label(peak), Mean: label(mean), Unit: unit,
		Span: display.SpanWord(n),
	}
}

var _ = control.InstanceView{}
