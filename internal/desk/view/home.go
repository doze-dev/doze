package view

import (
	"fmt"
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
	// View is the overview's view, remembered per stack: tiles, list, map.
	// The service fills it from prefs.
	View string `json:"view"`
	// Map is the stack as a map: nodes placed by role, an edge per need.
	Map MapView `json:"map"`
	// Up is how long the daemon has been up: "14m".
	Up string `json:"up,omitempty"`
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
	// Off counts the ones declared but not part of this run; Crashing the
	// ones in trouble. Neither is in the four above.
	Off      int `json:"off"`
	Crashing int `json:"crashing"`
	// Sleeps is the sentence under the counts: "sleeps after 5:00 with
	// nothing holding it".
	Sleeps string `json:"sleeps"`
}

// Conns is the connections card: open across the stack, and the tasks.
type Conns struct {
	Open  int    `json:"open"`
	Tasks string `json:"tasks"` // "tasks ✓ 1 done · ▸ 1 ready"
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
	// On is the switch: awake, or on its way (waking, dozing, even crashing);
	// off is asleep. Off says doze off took it out of this run, so the
	// switch is off and greyed. Pinned says it never sleeps.
	On     bool `json:"on"`
	Off    bool `json:"off,omitempty"`
	Pinned bool `json:"pinned"`
	// Err is the first line of what it last said before exiting, while it
	// is crashing.
	Err string `json:"err,omitempty"`
	// Down is a row of a stopped stack, read from doze.hcl. Tick is then
	// what Up does with it: "on" starts it, "half" leaves it to wake when
	// needed, "off" is paused in doze.hcl, "" is a task. Run is its command.
	Down bool   `json:"down,omitempty"`
	Tick string `json:"tick,omitempty"`
	Run  string `json:"run,omitempty"`
}

// MapView is the stack as the Map view draws it: nodes in columns by
// role, each with a place, and an edge per need.
type MapView struct {
	Columns []MapColumn `json:"columns"`
	Nodes   []MapNode   `json:"nodes"`
	Edges   []MapEdge   `json:"edges"`
	// W and H are the drawing's size, so the page can scale it.
	W int `json:"w"`
	H int `json:"h"`
}

// MapColumn is a heading over a column of nodes.
type MapColumn struct {
	Title string `json:"title"`
	What  string `json:"what"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
}

// MapNode is one instance, placed.
type MapNode struct {
	Name string `json:"name"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	Task bool   `json:"task"`
}

// MapEdge is one need, from the one that needs to the one needed, with the
// points to draw it between and its tone: on (keeps it awake), dim (needs
// it, not right now), off (would need it, if on).
type MapEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	X1   int    `json:"x1"`
	Y1   int    `json:"y1"`
	X2   int    `json:"x2"`
	Y2   int    `json:"y2"`
	Tone string `json:"tone"`
	// Same says both ends are in one column, so the line runs vertically.
	Same bool `json:"same"`
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
				row.Conns = fmt.Sprint(v.Conns)
				if !v.StartedAt.IsZero() {
					row.Up = display.Ago(now.Sub(v.StartedAt))
				}
			}
			row.On, row.Off, row.Pinned = !v.Disabled && display.LookOf(v).Word != "asleep", v.Disabled, v.KeepAwake
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
	h.Map = mapOf(st, rows)
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
		switch {
		case v.Disabled:
			v4.State.Off++
		case display.Troubled(v):
			v4.State.Crashing++
		case v.State == "active":
			v4.State.Awake++
		case v.State == "idle":
			v4.State.Dozing++
		case v.State == "booting":
			v4.State.Waking++
		default:
			v4.State.Asleep++
		}
		v4.Conns.Open += v.Conns
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
	key = nil
	for i, s := range cpuShares {
		v4.CPU.Shares = append(v4.CPU.Shares, Share{Name: s.Name, Part: part(s.V, cpuSum), Label: pct1(s.V)})
		if i < 2 {
			key = append(key, s.Name+" "+pct1(s.V))
		}
	}
	if v4.State.Awake > 0 {
		key = append(key, fmt.Sprintf("%d awake, the rest idle", v4.State.Awake))
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
		tasks = append(tasks, fmt.Sprintf("▸ %d ready", ready))
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
}

// Domains lists every domain the stack's apps declare, for the trust check.
func Domains(st hub.StackView) []string {
	var out []string
	for _, v := range st.Instances {
		out = append(out, v.Domains...)
	}
	return out
}
