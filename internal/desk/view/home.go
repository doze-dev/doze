package view

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// Home is a stack's home page, as the Main board lays it out: the four
// vitals cards, then every instance under its group in the table's columns,
// and the sidebar's instance list.
type Home struct {
	// Key is the stack's hub key, so the page can tell a push for this
	// stack from one computed for the last.
	Key     string `json:"key"`
	Stack   string `json:"stack"`
	Dir     string `json:"dir"`
	Running bool   `json:"running"`
	Banner  Banner `json:"banner"`
	// Setup is set while an app's own domains are not set up on this
	// machine: what is missing, and the command that does it.
	Setup *Setup `json:"setup,omitempty"`
	// Config is set while the running stack's doze.hcl does not load.
	Config *ConfigError `json:"config,omitempty"`
	// Changed is set while doze.hcl is newer than what the stack runs.
	Changed *ConfigChanged `json:"changed,omitempty"`
	Vitals  Vitals4        `json:"vitals"`
	Groups  []Group        `json:"groups"`
	Side    []Side         `json:"side"`
	// Awake and Total count the instances up and declared, tasks aside.
	Awake int `json:"awake"`
	Total int `json:"total"`
	// View is the overview's view, remembered per stack: tiles, list, map,
	// grid.
	// The service fills it from prefs.
	View string `json:"view"`
	// Map is the stack as a map: the calls left to right, the shared
	// services in a band below. Grid is the same as a table.
	Map  MapView  `json:"map"`
	Grid GridView `json:"grid"`
	// Up is how long the daemon has been up: "14m".
	Up string `json:"up,omitempty"`
	// Busy is the stack action in flight ("starting", "stopping",
	// "restarting"); the page shows it at once and takes no second one.
	// Failed is the last action that did not work, with what it said.
	Busy   string        `json:"busy,omitempty"`
	Failed *StackFailure `json:"failed,omitempty"`
	// Stopped is set for a stack that is not running: when it was up, and
	// the banner in place of the figures. Its rows are down (Row.Down).
	Stopped *Stopped `json:"stopped,omitempty"`
}

// Vitals4 are the four cards across the top of home.
type Vitals4 struct {
	Mem   Meter  `json:"mem"`
	CPU   Meter  `json:"cpu"`
	State States `json:"state"`
	Conns Conns  `json:"conns"`
}

// Meter is one bar card: the total as the figure, the shares as the bar,
// and the key line under it.
type Meter struct {
	Total  string  `json:"total"`
	Shares []Share `json:"shares"`
	Key    string  `json:"key"`
}

// Share is one instance's part of a meter.
type Share struct {
	Name string `json:"name"`
	// Part is the share of the sum, 0 to 1, for the bar's width.
	Part  float64 `json:"part"`
	Label string  `json:"label"`
}

// States counts the instances by state, tasks aside, and says when the
// stack sleeps.
type States struct {
	Awake  int `json:"awake"`
	Dozing int `json:"dozing"`
	Waking int `json:"waking"`
	Asleep int `json:"asleep"`
	// Off counts the ones turned off (doze off), Paused the ones with
	// enabled = false, Crashing the ones in trouble. None is in the four
	// above.
	Off      int `json:"off"`
	Paused   int `json:"paused"`
	Crashing int `json:"crashing"`
	// Sleeps is the sentence under the counts: "sleeps after 5:00 with
	// nothing holding it".
	Sleeps string `json:"sleeps"`
}

// Conns is the connections card: open across the stack, and the tasks.
type Conns struct {
	Open  int    `json:"open"`
	Tasks string `json:"tasks"` // "tasks ✓ 1 done · ▸ 1 not run"
}

// Group is a heading over part of the stack.
type Group struct {
	Title string `json:"title"`
	What  string `json:"what"`
	Count int    `json:"count"`
	Rows  []Row  `json:"rows"`
}

// Row is one instance in the table: the Main board's columns, computed.
type Row struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Look    Look   `json:"look"`
	Engine  string `json:"engine"`
	Mem     string `json:"mem,omitempty"`
	CPU     string `json:"cpu,omitempty"`
	Conns   string `json:"conns,omitempty"`
	Up      string `json:"up,omitempty"`
	Because string `json:"because"`
	// BecauseTone is the because column's tone: warn while dozing or
	// waking, bad in trouble, dim otherwise.
	BecauseTone string `json:"becauseTone"`
	Addr        string `json:"addr,omitempty"`
	Port        string `json:"port,omitempty"`
	Awake       bool   `json:"awake"`
	Trouble     bool   `json:"trouble"`
	// Depth is the row's depth in the dependency tree, for the tree view.
	Depth int `json:"depth"`
	// Address is the row's address as shown: port-less for an app the
	// front door serves, the name and port for a module; URL is what Open
	// uses, empty when it is not http.
	Address string `json:"address,omitempty"`
	URL     string `json:"url,omitempty"`
	// On is the switch (display.SwitchOn): up or on its way, crashing
	// included; off when asleep, failed, off or paused. Off says doze off
	// took it out, so the switch is greyed and turning it on brings it
	// back. Paused is enabled = false in doze.hcl: no switch at all, since
	// only the file can change it. Pinned says it never sleeps.
	On     bool `json:"on"`
	Off    bool `json:"off,omitempty"`
	Paused bool `json:"paused,omitempty"`
	Pinned bool `json:"pinned"`
	// Frozen is set while the stack's daemon is not answering: what the
	// row shows is the last word heard, and its switch cannot act.
	Frozen bool `json:"frozen,omitempty"`
	// Err is the first line of what it last said before exiting, while it
	// is crashing.
	Err string `json:"err,omitempty"`
	// Down is a row of a stopped stack, read from doze.hcl: its switch is
	// off, since nothing runs. Last says Up as last time starts it; Needs
	// is what it needs, for the start sheet; Run is its command.
	Down  bool     `json:"down,omitempty"`
	Last  bool     `json:"last,omitempty"`
	Needs []string `json:"needs,omitempty"`
	Run   string   `json:"run,omitempty"`
}

// Side is one instance in the sidebar's list: its dot and a word or figure.
type Side struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // app, module, service, task: the sidebar's groups
	Look    Look   `json:"look"`
	Note    string `json:"note"` // "84 MB", "dozing", "asleep", "done"
	Trouble bool   `json:"trouble"`
}

// HomeOf computes the home page of a stack at now.
func HomeOf(st hub.StackView, now time.Time) Home {
	snap := snapshot(st)
	h := Home{Key: st.Key, Stack: st.Name, Dir: display.Tilde(st.Dir), Running: st.Running, Banner: BannerOf(st, now)}
	frozen := st.Running && st.Conn == hub.ConnDown
	rows := display.Rows(snap)
	_, depth := display.TreeOrder(rows)
	for gi, g := range display.Groups {
		grp := Group{Title: g.Title, What: g.What}
		for _, r := range rows {
			if r.Group != gi {
				continue
			}
			v := r.V
			addr, port := display.URL(v)
			row := Row{
				Name: v.Name, Kind: v.Kind, Look: lookOf(v), Engine: engine(v),
				Because: display.Because(v, snap, now), BecauseTone: becauseTone(v),
				Addr: addr, Port: port,
				Awake: display.AwakeNow(v), Trouble: display.Troubled(v), Depth: depth[v.Name],
			}
			if row.Awake {
				row.Mem = display.MB(v.RAM)
				row.CPU = fmt.Sprintf("%.1f", v.CPU)
				row.Conns = fmt.Sprint(display.Connections(v))
				if !v.StartedAt.IsZero() {
					row.Up = display.Ago(now.Sub(v.StartedAt))
				}
			}
			row.On, row.Off, row.Paused, row.Pinned = display.SwitchOn(v), v.Disabled && !v.Paused, v.Paused, v.KeepAwake
			row.Frozen = frozen
			if row.Trouble && v.LastError != "" {
				row.Err = display.FirstLine(v.LastError)
			}
			row.Address, row.URL = addressOf(v)
			grp.Rows = append(grp.Rows, row)
			grp.Count++
			if v.Kind != "task" {
				h.Total++
				if row.Awake {
					h.Awake++
				}
			}
		}
		if len(grp.Rows) > 0 {
			h.Groups = append(h.Groups, grp)
		}
	}
	h.Vitals = vitalsOf(st)
	h.Side = SideOf(st)
	if st.Running && !st.Since.IsZero() {
		h.Up = display.Ago(now.Sub(st.Since))
	}
	h.Map = mapOf(rows, now)
	h.Grid = gridOf(rows, now)
	return h
}

// SideOf is the sidebar's instance list, in the table's order.
func SideOf(st hub.StackView) []Side {
	var out []Side
	for _, r := range display.Rows(snapshot(st)) {
		v := r.V
		s := Side{Name: v.Name, Kind: v.Kind, Look: lookOf(v), Trouble: display.Troubled(v)}
		switch {
		case s.Trouble && v.Kind != "task":
			s.Note = "!"
		case display.AwakeNow(v) && v.Kind != "task":
			s.Note = display.MB(v.RAM)
		default:
			s.Note = s.Look.Word
		}
		out = append(out, s)
	}
	return out
}

// engine is the Engine column: a module's engine and version, "process"
// for the rest, "task" for a task.
func engine(v control.InstanceView) string {
	switch v.Kind {
	case "module":
		return strings.TrimSpace(v.Engine + " " + strings.TrimPrefix(v.Version, "builtin"))
	case "task":
		return "task"
	}
	if v.Engine != "" {
		return v.Engine
	}
	return "process"
}

func becauseTone(v control.InstanceView) string {
	switch {
	case display.Troubled(v):
		return "bad"
	case v.State == "idle" || v.State == "booting":
		return "warn"
	}
	return "dim"
}

// vitalsOf composes the four cards from the instances' latest readings.
func vitalsOf(st hub.StackView) Vitals4 {
	var mem, cpu []display.Share
	var v4 Vitals4
	// The counts and the connections are the tally every surface uses.
	t := display.TallyOf(st.Instances)
	v4.State.Awake, v4.State.Dozing, v4.State.Waking, v4.State.Asleep = t.Awake, t.Dozing, t.Waking, t.Asleep
	v4.State.Off, v4.State.Paused, v4.State.Crashing = t.Off, t.Paused, t.Trouble
	v4.Conns.Open = t.Conns
	done, ready, failed := 0, 0, 0
	for _, v := range st.Instances {
		if v.Kind == "task" {
			switch {
			case v.Task != nil && v.Task.Result == "failed":
				failed++
			case v.Task != nil:
				done++
			default:
				ready++
			}
			continue
		}
		if display.AwakeNow(v) {
			mem = append(mem, display.Share{Name: v.Name, V: float64(v.RAM)})
			cpu = append(cpu, display.Share{Name: v.Name, V: v.CPU})
		}
	}
	memShares, _, memSum := display.Shares(mem, 0)
	cpuShares, _, cpuSum := display.Shares(cpu, 0.05)
	unit := display.MemUnit(memSum)
	v4.Mem = Meter{Total: display.MB(int64(memSum))}
	var key []string
	for i, s := range memShares {
		v4.Mem.Shares = append(v4.Mem.Shares, Share{Name: s.Name, Part: part(s.V, memSum), Label: unit(s.V)})
		if i < 3 {
			key = append(key, s.Name+" "+unit(s.V))
		}
	}
	if n := len(memShares) - 3; n > 0 {
		key = append(key, fmt.Sprintf("+%d more", n))
	}
	v4.Mem.Key = strings.Join(key, " · ")
	v4.CPU = Meter{Total: display.Cores(cpuSum)}
	if cpuSum <= 0 && len(cpu) > 0 {
		// Running, and too little to measure: not a broken zero.
		v4.CPU.Total = "<0.01 cores"
	}
	key = nil
	for i, s := range cpuShares {
		v4.CPU.Shares = append(v4.CPU.Shares, Share{Name: s.Name, Part: part(s.V, cpuSum), Label: pct1(s.V)})
		if i < 2 {
			key = append(key, s.Name+" "+pct1(s.V))
		}
	}
	switch {
	case len(key) > 0:
		key = append(key, "the rest quiet")
	case len(cpu) > 0:
		key = append(key, "nothing busy right now")
	}
	v4.CPU.Key = strings.Join(key, " · ")
	if st.IdleTimeout > 0 {
		v4.State.Sleeps = "sleeps after " + display.Clock(st.IdleTimeout) + " with nothing holding it"
	}
	var tasks []string
	if done > 0 {
		tasks = append(tasks, fmt.Sprintf("✓ %d done", done))
	}
	if failed > 0 {
		tasks = append(tasks, fmt.Sprintf("✗ %d failed", failed))
	}
	if ready > 0 {
		tasks = append(tasks, fmt.Sprintf("▸ %d not run", ready))
	}
	if len(tasks) > 0 {
		v4.Conns.Tasks = "tasks " + strings.Join(tasks, " · ")
	}
	return v4
}

// pct1 is a CPU share with one decimal, as the Main board's key shows it.
func pct1(v float64) string { return fmt.Sprintf("%.1f%%", v) }

func part(v, sum float64) float64 {
	if sum <= 0 {
		return 0
	}
	return float64(int(v/sum*1000+0.5)) / 1000
}

// Setup is the banner for domains that are not set up here yet.
type Setup struct {
	Text    string `json:"text"`    // "dev.acme.com is not reachable here yet"
	Detail  string `json:"detail"`  // what is missing, in doctor's words
	Command string `json:"command"` // "doze trust"
	// Prompt is set where the app can ask for the password itself; without
	// it the banner offers only the command.
	Prompt bool `json:"prompt,omitempty"`
}

// Domains lists every domain the stack's apps declare, for the trust check.
func Domains(st hub.StackView) []string {
	var out []string
	for _, v := range st.Instances {
		out = append(out, v.Domains...)
	}
	return out
}

// StackFailure is a stack action that did not work: what was tried, and
// the last lines it printed, kept on the page until dismissed.
type StackFailure struct {
	What  string   `json:"what"` // "Couldn't start shop"
	Lines []string `json:"lines"`
	// Problem is what went wrong, in one sentence; Config is set when it is
	// a mistake in doze.hcl, with the file, the line and what to write.
	Problem string       `json:"problem,omitempty"`
	Config  *ConfigError `json:"config,omitempty"`
}

// FailureOf reads what a stack action printed when it failed: a mistake in
// doze.hcl becomes the file, the line and the fix; anything else, its most
// telling line.
func FailureOf(what, dir, out string, err error) *StackFailure {
	out = strings.TrimSpace(out)
	if out == "" && err != nil {
		out = err.Error()
	}
	lines := strings.Split(out, "\n")
	f := &StackFailure{What: what, Lines: lines}
	if reOnFile.MatchString(out) || strings.Contains(out, "config has a mistake") {
		f.Config = ConfigErrorOf(filepath.Join(dir, "doze.hcl"), errors.New(out), time.Time{}, time.Now())
		f.Config.Text = ""
		f.Problem = f.Config.Summary
		return f
	}
	// The telling line: the first that says error, else the last with words.
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if low := strings.ToLower(l); strings.HasPrefix(low, "error") || strings.Contains(low, "failed") {
			f.Problem = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(l, "Error:"), "error:"))
			break
		}
	}
	if f.Problem == "" {
		for i := len(lines) - 1; i >= 0; i-- {
			if l := strings.TrimSpace(lines[i]); l != "" {
				f.Problem = l
				break
			}
		}
	}
	return f
}
