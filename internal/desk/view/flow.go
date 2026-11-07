package view

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/display"
)

// The Map, as MapFlow.dc.html and MapFlowSelected.dc.html draw it: the
// apps left to right the way a request travels, a column per step of
// calls; the apps nothing calls and that call nothing in a lane of their
// own; and what the stack shares (modules and outside services) in a band
// below, a card each, with a chip on every app card for what it uses.
//
// Everything comes from doze.hcl's references, as the daemon lists them in
// each instance's needs: an app that refers to another app calls it; one
// that refers to a module or a service uses it; one that lists a task in
// `after` runs it first, and the task folds into a ✓ on the app's card
// unless it is running or failed, when it is a card of its own.

// MapView is the stack as the Map view draws it.
type MapView struct {
	// W and H are the drawing's size; the page scrolls it when the window
	// is smaller.
	W int `json:"w"`
	H int `json:"h"`
	// Caps are the captions over the parts: the calls, the lane, the band.
	Caps []MapCap `json:"caps"`
	// Band is the top of the shared band, 0 when the stack shares nothing.
	Band   int         `json:"band"`
	Nodes  []MapNode   `json:"nodes"`
	Shared []MapShared `json:"shared"`
	// Edges are the calls, each a path placed so it passes behind no card.
	Edges []MapEdge `json:"edges"`
	// Lines are the dashed lines from a card down to what it uses in the
	// band, drawn only while one of their ends is selected.
	Lines []MapEdge `json:"lines"`
	// Detail is where the selected card's detail card sits: clear of the
	// cards where the drawing has room.
	Detail MapAt `json:"detail"`
}

// MapCap is a caption: the part's name and a quieter line after it.
type MapCap struct {
	Text string `json:"text"`
	Sub  string `json:"sub"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
}

// MapAt is a place in the drawing.
type MapAt struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// MapNode is an app's card, or a task's while it runs or after it failed.
type MapNode struct {
	Name string `json:"name"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	// Lane says it is in the lane of apps nothing calls.
	Lane bool `json:"lane,omitempty"`
	Task bool `json:"task,omitempty"`
	// Lang is the badge, read from the run command: go, py, ts, js; empty
	// when the command does not say.
	Lang string `json:"lang,omitempty"`
	// Chips are the shared services it uses, in the order doze.hcl refers
	// to them, as many as fit; More counts the rest.
	Chips []string `json:"chips"`
	More  int      `json:"more,omitempty"`
	// Quiet is the chip row's word when it uses nothing shared.
	Quiet string `json:"quiet,omitempty"`
	// Fold is its setup tasks, folded into one mark.
	Fold *MapFold `json:"fold,omitempty"`
	// Calls, Callers and Uses are its neighbours, for what a selection
	// keeps lit.
	Calls   []string `json:"calls"`
	Callers []string `json:"callers"`
	Uses    []string `json:"uses"`
	Detail  MapCard  `json:"detail"`
}

// MapFold is the mark for an app's tasks: ✓ when every one has run, ▸
// when one has not yet; the title names them and when they ran.
type MapFold struct {
	Glyph string `json:"glyph"`
	Tone  string `json:"tone"`
	Title string `json:"title"`
}

// MapShared is a card in the band: a module or an outside service.
type MapShared struct {
	Name string `json:"name"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	// What it is: "postgres 18", "brought in by you".
	What string `json:"what"`
	// Used is "used by 3", or "used by 1" and Only its one user.
	Used   string   `json:"used"`
	Only   string   `json:"only,omitempty"`
	Users  []string `json:"users"`
	Detail MapCard  `json:"detail"`
}

// MapEdge is one line: a call, or a use drawn dashed.
type MapEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	D    string `json:"d"`
}

// MapCard is the selected card's detail: a head line and labelled rows.
type MapCard struct {
	Head string    `json:"head"`
	Rows []MapLine `json:"rows"`
}

// MapLine is one row of a detail card.
type MapLine struct {
	K    string `json:"k"`
	V    string `json:"v"`
	Tone string `json:"tone,omitempty"`
	// Dim is a quieter tail: "16m ago".
	Dim string `json:"dim,omitempty"`
}

// The board's measures, in CSS pixels.
const (
	flowLeft    = 24  // the first column's left
	flowGap     = 40  // between columns: room for a line to bend and an arrowhead to land
	flowCardW   = 214 // a card's usual width
	flowWide    = 36  // a column grows by this for a long name
	flowCardH   = 74  // an app card
	flowStep    = 116 // card to card, down a column
	flowTop     = 52  // the first card under a caption
	flowCapGap  = 36  // a caption sits this far above its cards
	flowShareH  = 88  // a band card
	flowShareDY = 16  // between rows of the band
	flowMinW    = 1222
	flowPerRow  = 5    // band cards in a row, at least
	flowDetailW = 448  // the detail card
	flowDetailH = 112  // and its height, about
	flowFold    = 12.0 // the ✓, about
)

// eventEngines are the modules an app consumes events from: an app that
// nothing calls but that uses one of these works from events.
var eventEngines = map[string]bool{"kafka": true, "redpanda": true, "nats": true, "rabbitmq": true, "sqs": true, "pulsar": true}

// flow is the stack read as calls and uses, before it is placed.
type flow struct {
	byName  map[string]control.InstanceView
	apps    []string            // every app, in display order
	calls   map[string][]string // app → apps and task cards it calls, in reference order
	callers map[string][]string
	uses    map[string][]string // app → shared it uses, in reference order
	users   map[string][]string // shared → apps that use it, in display order
	tasks   map[string][]string // app → the tasks it lists in after
	shared  []string            // modules and services, in display order
	carded  map[string]bool     // tasks drawn as cards
	// rank is each card's place in reading order, once placed: by
	// column, then down it, the lane last.
	rank map[string]int
}

func isShared(kind string) bool { return kind == "module" || kind == "" || kind == "service" }

// readFlow sorts every reference into a call, a use or a task.
func readFlow(rows []display.Row) flow {
	f := flow{byName: map[string]control.InstanceView{}, calls: map[string][]string{}, callers: map[string][]string{},
		uses: map[string][]string{}, users: map[string][]string{}, tasks: map[string][]string{}, carded: map[string]bool{}, rank: map[string]int{}}
	for _, r := range rows {
		f.byName[r.V.Name] = r.V
	}
	for _, r := range rows {
		v := r.V
		switch {
		case v.Kind == "app":
			f.apps = append(f.apps, v.Name)
		case isShared(v.Kind):
			f.shared = append(f.shared, v.Name)
		}
	}
	for _, a := range f.apps {
		seen := map[string]bool{}
		for _, need := range f.byName[a].Needs {
			d, ok := f.byName[need]
			if !ok || seen[need] || need == a {
				continue
			}
			seen[need] = true
			switch {
			case d.Kind == "app":
				f.calls[a] = append(f.calls[a], need)
				f.callers[need] = append(f.callers[need], a)
			case isShared(d.Kind):
				f.uses[a] = append(f.uses[a], need)
				f.users[need] = append(f.users[need], a)
			case d.Kind == "task":
				f.tasks[a] = append(f.tasks[a], need)
				if taskBusy(d) {
					// Running or failed: a card of its own, after the app.
					f.carded[need] = true
					f.calls[a] = append(f.calls[a], need)
					f.callers[need] = append(f.callers[need], a)
				}
			}
		}
	}
	return f
}

// taskBusy is a task that is running, or whose last run failed: worth a
// card rather than a mark.
func taskBusy(v control.InstanceView) bool {
	return v.State == "booting" || (v.Task != nil && v.Task.Result == "failed")
}

// order is the flow's columns: each app's depth is its longest chain of
// callers, so a request reads left to right; a cycle is cut where it
// closes. The apps in no call at all are the lane.
func (f flow) order() (cols [][]string, lane []string) {
	inFlow := map[string]bool{}
	var nodes []string
	for _, a := range f.apps {
		if len(f.calls[a]) > 0 || len(f.callers[a]) > 0 {
			inFlow[a] = true
			nodes = append(nodes, a)
		} else {
			lane = append(lane, a)
		}
	}
	// The lane, the most connected first.
	sort.SliceStable(lane, func(i, j int) bool { return len(f.uses[lane[i]]) > len(f.uses[lane[j]]) })
	for i, a := range lane {
		f.rank[a] = 1_000_000 + i
	}
	for t := range f.carded {
		inFlow[t] = true
	}
	nodes = append(nodes, sortedKeys(f.carded)...)
	// Cut the cycles: a depth-first walk in display order drops the edge
	// that leads back to a node still on the walk.
	back := map[[2]string]bool{}
	state := map[string]int{}
	var walk func(string)
	walk = func(v string) {
		state[v] = 1
		for _, w := range f.calls[v] {
			switch state[w] {
			case 0:
				walk(w)
			case 1:
				back[[2]string{v, w}] = true
			}
		}
		state[v] = 2
	}
	for _, v := range nodes {
		if state[v] == 0 && len(f.callers[v]) == 0 {
			walk(v)
		}
	}
	for _, v := range nodes {
		if state[v] == 0 {
			walk(v)
		}
	}
	depth := map[string]int{}
	var deep func(string, map[string]bool) int
	deep = func(v string, on map[string]bool) int {
		if d, ok := depth[v]; ok {
			return d
		}
		d := 0
		for _, c := range f.callers[v] {
			if back[[2]string{c, v}] || on[c] {
				continue
			}
			on[v] = true
			if x := deep(c, on) + 1; x > d {
				d = x
			}
			delete(on, v)
		}
		depth[v] = d
		return d
	}
	for _, v := range nodes {
		d := deep(v, map[string]bool{})
		for len(cols) <= d {
			cols = append(cols, nil)
		}
		cols[d] = append(cols[d], v)
	}
	return cols, lane
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// arrange orders each column so the lines cross little: by where their
// callers sit; where every card of a column hangs off the same callers,
// the busiest goes in the middle and the rest alternate below and above,
// so the main road runs straight. It returns each card's slot, on one
// grid shared by every column.
func (f flow) arrange(cols [][]string) (slot map[string]int, rows int) {
	for _, c := range cols {
		if len(c) > rows {
			rows = len(c)
		}
	}
	slot = map[string]int{}
	degree := func(v string) int { return len(f.calls[v]) + len(f.callers[v]) }
	for ci, c := range cols {
		bary := map[string]float64{}
		for _, v := range c {
			sum, k := 0.0, 0
			for _, u := range f.callers[v] {
				if s, ok := slot[u]; ok {
					sum += float64(s)
					k++
				}
			}
			if k > 0 {
				bary[v] = sum / float64(k)
			} else {
				bary[v] = float64(rows-1) / 2
			}
		}
		same := true
		for _, v := range c {
			if bary[v] != bary[c[0]] {
				same = false
			}
		}
		sort.SliceStable(c, func(i, j int) bool {
			if !same && bary[c[i]] != bary[c[j]] {
				return bary[c[i]] < bary[c[j]]
			}
			if di, dj := degree(c[i]), degree(c[j]); di != dj {
				return di > dj
			}
			return c[i] < c[j]
		})
		first := (rows - len(c)) / 2
		if same && len(c) > 1 {
			// Centre out: the first in the middle, then below, then above.
			mid := first + (len(c)-1)/2
			placed := make([]string, len(c))
			for i, v := range c {
				off := (i + 1) / 2
				if i%2 == 0 {
					off = -off
				}
				placed[mid+off-first] = v
			}
			c = placed
			cols[ci] = c
		}
		for i, v := range c {
			slot[v] = first + i
			f.rank[v] = ci*1000 + first + i
		}
	}
	return slot, rows
}

// mapOf lays the stack out as the Map boards do.
func mapOf(rows []display.Row, now time.Time) MapView {
	m := MapView{Caps: []MapCap{}, Nodes: []MapNode{}, Shared: []MapShared{}, Edges: []MapEdge{}, Lines: []MapEdge{}}
	f := readFlow(rows)
	cols, lane := f.order()
	slot, nrows := f.arrange(cols)

	// The grid of columns: each as wide as its widest card needs.
	var xs, ws []int
	x := flowLeft
	addCol := func(w int) {
		xs, ws = append(xs, x), append(ws, w)
		x += w + flowGap
	}
	for _, c := range cols {
		w := flowCardW
		for _, v := range c {
			if cw := f.cardWidth(v); cw > w {
				w = cw
			}
		}
		addCol(w)
	}
	colAt := func(i int, w int) (int, int) {
		for len(xs) <= i {
			addCol(w)
		}
		// A later part keeps the grid: its card is as wide as the column.
		return xs[i], ws[i]
	}

	at := map[string]box{}
	y := 16
	if len(cols) > 0 {
		m.Caps = append(m.Caps, MapCap{Text: "Who calls whom", Sub: "left to right, the way a request travels", X: flowLeft, Y: y})
		top := y + flowCapGap
		for ci, c := range cols {
			for _, v := range c {
				b := box{xs[ci], top + slot[v]*flowStep, ws[ci], flowCardH}
				at[v] = b
				m.Nodes = append(m.Nodes, f.node(v, b, false, now))
			}
		}
		y = top + nrows*flowStep - (flowStep - flowCardH) + 14
	}
	if len(lane) > 0 {
		lc := MapCap{Text: "No callers", Sub: "nothing in doze.hcl refers to these", X: flowLeft, Y: y}
		events := true
		for _, a := range lane {
			ev := false
			for _, s := range f.uses[a] {
				if eventEngines[f.byName[s].Engine] {
					ev = true
				}
			}
			events = events && ev
		}
		if events {
			lc.Text, lc.Sub = "Nobody calls these", "they work from events"
		}
		m.Caps = append(m.Caps, lc)
		top := y + flowCapGap - 12
		if len(cols) == 0 {
			top = y + flowCapGap
		}
		for i, a := range lane {
			lx, lw := colAt(i, flowCardW)
			b := box{lx, top, lw, flowCardH}
			at[a] = b
			n := f.node(a, b, true, now)
			m.Nodes = append(m.Nodes, n)
		}
		y = top + flowCardH
	}

	// The band: what the stack shares, ordered under the cards that use it.
	shared := append([]string(nil), f.shared...)
	if len(shared) > 0 {
		pull := func(s string) float64 {
			us := f.users[s]
			if len(us) == 0 {
				return 1e9
			}
			sum := 0.0
			for _, u := range us {
				sum += float64(at[u].cx())
			}
			return sum / float64(len(us))
		}
		sort.SliceStable(shared, func(i, j int) bool {
			pi, pj := pull(shared[i]), pull(shared[j])
			if pi != pj {
				return pi < pj
			}
			return shared[i] < shared[j]
		})
		m.Band = y + 16
		sub := "the chips on each card say who uses what"
		if n := len(f.usesAll()); n > 1 {
			sub = "one line each would be " + strconv.Itoa(n) + " lines; " + sub
		}
		m.Caps = append(m.Caps, MapCap{Text: "Shared by the stack", Sub: sub, X: flowLeft, Y: m.Band + 10})
		perRow := len(xs)
		if perRow < flowPerRow {
			perRow = flowPerRow
		}
		top := m.Band + 36
		for i, s := range shared {
			r, c := i/perRow, i%perRow
			sx, sw := colAt(c, flowCardW)
			b := box{sx, top + r*(flowShareH+flowShareDY), sw, flowShareH}
			at[s] = b
			m.Shared = append(m.Shared, f.share(s, b))
		}
		y = top + ((len(shared)-1)/perRow+1)*(flowShareH+flowShareDY) - flowShareDY
	}
	m.H = y + 16
	m.W = flowMinW
	for _, b := range at {
		if b.right()+12 > m.W {
			m.W = b.right() + 12
		}
	}

	// The lines. Every card is an obstacle to every line but its own two.
	var cards []box
	for _, n := range m.Nodes {
		cards = append(cards, at[n.Name])
	}
	for _, s := range m.Shared {
		cards = append(cards, at[s.Name])
	}
	r := router{cards: cards, xs: gaps(cards, true), ys: gaps(cards, false), used: map[string]bool{}}
	for _, n := range m.Nodes {
		targets := append([]string(nil), n.Calls...)
		sort.SliceStable(targets, func(i, j int) bool { return at[targets[i]].x < at[targets[j]].x })
		for _, t := range targets {
			m.Edges = append(m.Edges, MapEdge{From: n.Name, To: t, D: r.call(n.Name, at[n.Name], at[t]).d})
		}
	}
	for _, n := range m.Nodes {
		for _, s := range n.Uses {
			m.Lines = append(m.Lines, MapEdge{From: n.Name, To: s, D: r.use(at[n.Name], at[s]).d})
		}
	}

	// The detail card: top right where that is clear of the cards, else
	// the first clear place down the right side, else top right over them.
	m.Detail = MapAt{m.W - 12 - flowDetailW, 14}
	for yy := 14; yy+flowDetailH < m.H; yy += 8 {
		d := box{m.W - 12 - flowDetailW, yy, flowDetailW, flowDetailH}
		if !overlaps(d, cards) {
			m.Detail.Y = yy
			break
		}
	}
	return m
}

func overlaps(d box, cards []box) bool {
	for _, c := range cards {
		if d.x < c.right()+8 && c.x < d.right()+8 && d.y < c.bottom()+8 && c.y < d.bottom()+8 {
			return true
		}
	}
	return false
}

// cardWidth is how wide a card must be for its name and its chips: the
// board's 214, or wider by steps of 36 for a long name.
func (f flow) cardWidth(name string) int {
	need := textW(name, 7.4) + 8 + 7 + 7 + 44 + 26 // dot, gaps, MB, padding
	if lang(f.byName[name].Run) != "" {
		need += 22 + 7
	}
	w := flowCardW
	for w < need {
		w += flowWide
	}
	return w
}

func textW(s string, per float64) int { return int(float64(len([]rune(s)))*per + 0.5) }

// chipW is a chip's width at the board's 11.5px.
func chipW(s string) int { return textW(s, 6.4) + 14 }

// node is an app's or a task's card.
func (f flow) node(name string, b box, lane bool, now time.Time) MapNode {
	v := f.byName[name]
	n := MapNode{Name: name, X: b.x, Y: b.y, W: b.w, H: b.h, Lane: lane, Task: v.Kind == "task", Lang: lang(v.Run),
		Chips: []string{}, Calls: orEmpty(f.calls[name]), Callers: orEmpty(f.callers[name]), Uses: orEmpty(f.uses[name])}
	room := b.w - 26
	fold := f.fold(name, now)
	if fold != nil {
		n.Fold = fold
		room -= int(flowFold) + 5
	}
	for i, s := range n.Uses {
		w := chipW(s)
		rest := len(n.Uses) - i - 1
		// The last chip needs only its own room; one before it, room for a
		// "+n" after it too.
		fits := w <= room
		if rest > 0 {
			fits = w+chipW("+9")+5 <= room
		}
		if !fits {
			n.More = len(n.Uses) - i
			break
		}
		n.Chips = append(n.Chips, s)
		room -= w + 5
	}
	if len(n.Uses) == 0 {
		n.Quiet = "no shared services"
		if n.Task {
			n.Quiet = lookOf(v).Word
		}
	}
	n.Detail = f.card(name, now)
	return n
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// fold is the mark for the tasks an app lists in after and that are not
// drawn as cards: ✓ when all have run.
func (f flow) fold(app string, now time.Time) *MapFold {
	var parts []string
	ran, all := true, 0
	for _, t := range f.tasks[app] {
		if f.carded[t] {
			continue
		}
		all++
		parts = append(parts, taskLine(f.byName[t], now))
		if v := f.byName[t]; v.Task == nil {
			ran = false
		}
	}
	if all == 0 {
		return nil
	}
	if ran {
		return &MapFold{Glyph: "✓", Tone: "good", Title: "before it: " + strings.Join(parts, " · ")}
	}
	return &MapFold{Glyph: "▸", Tone: "dim", Title: "before it: " + strings.Join(parts, " · ")}
}

// taskLine is a task as its fold's title says it: "migrate-orders ✓ 16m ago".
func taskLine(v control.InstanceView, now time.Time) string {
	switch {
	case v.State == "booting":
		return v.Name + " running"
	case v.Task == nil:
		return v.Name + " not run yet"
	case v.Task.Result == "failed":
		return v.Name + " failed " + display.Ago(now.Sub(v.Task.RanAt)) + " ago"
	}
	return v.Name + " ✓ " + display.Ago(now.Sub(v.Task.RanAt)) + " ago"
}

// card is a selected app's detail: what it is, what it needs, who needs
// it, what runs before it.
func (f flow) card(name string, now time.Time) MapCard {
	v := f.byName[name]
	head := []string{}
	if l := langName(lang(v.Run)); l != "" {
		head = append(head, l)
	}
	if v.Kind == "task" {
		head = append(head, "task")
	}
	if display.AwakeNow(v) && v.RAM > 0 {
		head = append(head, display.MB(v.RAM))
	}
	head = append(head, lookOf(v).Word)
	c := MapCard{Head: strings.Join(head, " · "), Rows: []MapLine{}}
	if v.Kind == "task" {
		c.Rows = append(c.Rows, MapLine{K: "before", V: andList(f.callers[name])})
		tone := "bad"
		if v.State == "booting" {
			tone = "cool"
		}
		c.Rows = append(c.Rows, MapLine{K: "last run", V: taskLine(v, now), Tone: tone})
		return c
	}
	calls := []string{}
	for _, x := range f.calls[name] {
		if f.byName[x].Kind == "app" {
			calls = append(calls, x)
		}
	}
	needs := strings.Join(calls, ", ")
	if us := f.uses[name]; len(us) > 0 {
		if needs != "" {
			needs += " · and "
		}
		needs += strings.Join(us, ", ") + " below"
	}
	if needs == "" {
		needs = "nothing"
	}
	c.Rows = append(c.Rows, MapLine{K: "needs", V: needs})
	by := "nobody calls it"
	if cs := f.ranked(f.callers[name]); len(cs) > 0 {
		by = strings.Join(cs, ", ")
		var holding []string
		for _, h := range v.Holders {
			if h.Kind == "dependant" {
				for _, c := range cs {
					if c == h.Name {
						holding = append(holding, c)
					}
				}
			}
		}
		switch {
		case len(holding) == len(cs) && len(cs) > 1:
			by += " · they keep it awake"
		case len(holding) == 1 && len(cs) == 1:
			by += " · it keeps it awake"
		case len(holding) > 0:
			by += " · " + andList(holding) + " keep it awake"
		}
	}
	c.Rows = append(c.Rows, MapLine{K: "needed by", V: by})
	before := MapLine{K: "before it", V: "nothing", Tone: "dim"}
	if ts := f.tasks[name]; len(ts) > 0 {
		var words []string
		var last time.Time
		tone := "ok"
		for _, t := range ts {
			tv := f.byName[t]
			switch {
			case tv.State == "booting":
				words = append(words, t+" running")
				if tone == "ok" {
					tone = "cool"
				}
			case tv.Task == nil:
				words = append(words, t+" not run")
				if tone == "ok" {
					tone = "dim"
				}
			case tv.Task.Result == "failed":
				words = append(words, t+" ✗")
				tone = "bad"
			default:
				words = append(words, t+" ✓")
			}
			if tv.Task != nil && tv.Task.RanAt.After(last) {
				last = tv.Task.RanAt
			}
		}
		before = MapLine{K: "before it", V: strings.Join(words, " · "), Tone: tone}
		if !last.IsZero() {
			before.Dim = display.Ago(now.Sub(last)) + " ago"
		}
	}
	c.Rows = append(c.Rows, before)
	return c
}

// share is a band card: what it is and how many use it.
func (f flow) share(name string, b box) MapShared {
	v := f.byName[name]
	s := MapShared{Name: name, X: b.x, Y: b.y, W: b.w, H: b.h, Users: orEmpty(f.users[name])}
	if v.Kind == "service" {
		s.What = "brought in by you"
	} else {
		s.What = engine(v)
		if s.What == "" {
			s.What = "module"
		}
	}
	switch len(s.Users) {
	case 0:
		s.Used = "nothing uses it"
	case 1:
		s.Used, s.Only = "used by 1", s.Users[0]
	default:
		s.Used = "used by " + strconv.Itoa(len(s.Users))
	}
	head := []string{s.What}
	if display.AwakeNow(v) && v.RAM > 0 {
		head = append(head, display.MB(v.RAM))
	}
	head = append(head, lookOf(v).Word)
	by := "no app"
	if len(s.Users) > 0 {
		by = strings.Join(s.Users, ", ")
	}
	s.Detail = MapCard{Head: strings.Join(head, " · "), Rows: []MapLine{{K: "used by", V: by}}}
	return s
}

// lang is the badge for a run command: what runs it says what it is in.
func lang(run string) string {
	fields := strings.Fields(run)
	for len(fields) > 0 && strings.Contains(fields[0], "=") {
		fields = fields[1:] // FOO=bar go run .
	}
	if len(fields) == 0 {
		return ""
	}
	first := fields[0]
	if i := strings.LastIndex(first, "/"); i >= 0 {
		first = first[i+1:]
	}
	ts := strings.Contains(run, ".ts") || strings.Contains(run, "tsx") || strings.Contains(run, "ts-node")
	switch {
	case first == "go":
		return "go"
	case first == "uv" || first == "poetry" || first == "pipenv" || strings.HasPrefix(first, "python") || first == "uvicorn" || first == "gunicorn":
		return "py"
	case first == "bun" || first == "deno":
		return "ts"
	case first == "npm" || first == "pnpm" || first == "node" || first == "yarn" || first == "npx":
		if ts {
			return "ts"
		}
		return "js"
	}
	return ""
}

func langName(l string) string {
	return map[string]string{"go": "Go", "py": "Python", "ts": "TypeScript", "js": "JavaScript"}[l]
}

// router places lines among the cards.
type router struct {
	cards []box
	xs    []float64 // the free columns between cards
	ys    []float64 // the free rows between cards
	// used marks the sides of a card a line already left from, so a second
	// line from the same card takes the other side.
	used map[string]bool
}

// call is a call's line, from the caller's card to the called one's:
// the board's shapes first, each taken only if it passes behind no card.
func (r *router) call(from string, s, t box) path {
	sx, sy := float64(s.right()), float64(s.cy())
	tx, ty := float64(t.x), float64(t.cy())
	try := func(p path) bool { return p.clear(r.cards, s, t) }
	// Side to side, when nothing stands between: straight across on one row;
	// to a card above or below, out a short way, a rounded bend, and in
	// level, so the arrowhead lands flat on the card's side. Lines leaving
	// one card for several share that first stretch, as a trunk.
	if t.x > s.right() {
		if sy == ty {
			if p := curve(pt{sx, sy}, pt{(sx + tx) / 2, sy}, pt{(sx + tx) / 2, ty}, pt{tx, ty}); try(p) {
				return p
			}
		}
		bx := sx + math.Min(14, (tx-sx)/3)
		if p := elbow(pt{sx, sy}, pt{bx, sy}, pt{bx, ty}, pt{tx, ty}); try(p) {
			return p
		}
	}
	over := func() path {
		cy := float64(min(s.y, t.y)) - 40
		return curve(pt{float64(s.right() - 24), float64(s.y)}, pt{float64(s.right() - 24), cy}, pt{float64(t.x + 24), cy}, pt{float64(t.x + 24), float64(t.y - 1)})
	}
	under := func() path {
		cy := float64(max(s.bottom(), t.bottom())) + 30
		return curve(pt{float64(s.right() - 24), float64(s.bottom())}, pt{float64(s.right() - 24), cy}, pt{float64(t.x + 30), cy}, pt{float64(t.x + 30), float64(t.bottom() + 1)})
	}
	// Along its own row, then up or down into the card.
	along := func() path {
		ex := float64(t.cx() + 23)
		if s.cy() > t.bottom() {
			return curve(pt{sx, sy}, pt{sx + (ex-sx)*0.52, sy}, pt{ex, sy - (sy-float64(t.bottom()))*0.13}, pt{ex, float64(t.bottom() + 1)})
		}
		return curve(pt{sx, sy}, pt{sx + (ex-sx)*0.52, sy}, pt{ex, sy + (float64(t.y)-sy)*0.13}, pt{ex, float64(t.y - 1)})
	}
	if t.x > s.right() {
		if !r.used[from+"^"] {
			if p := over(); try(p) {
				r.used[from+"^"] = true
				return p
			}
		}
		if s.cy() != t.cy() {
			if p := along(); try(p) {
				return p
			}
		}
		if !r.used[from+"v"] {
			if p := under(); try(p) {
				r.used[from+"v"] = true
				return p
			}
		}
		for _, p := range []path{over(), under()} {
			if try(p) {
				return p
			}
		}
	}
	// Along the gaps: out of its side, along a free row, into the other's.
	if p, ok := r.elbow(s, t); ok {
		return p
	}
	mx := (sx + tx) / 2
	return curve(pt{sx, sy}, pt{mx, sy}, pt{mx, ty}, pt{tx, ty})
}

// elbow routes out of the source's right side (left, for a line going
// back), along the free rows and columns, into the target's side.
func (r *router) elbow(s, t box) (path, bool) {
	sy, ty := float64(s.cy()), float64(t.cy())
	sx, gx1 := float64(s.right()), r.nextX(float64(s.right()), 1)
	tx, gx2 := float64(t.x), r.nextX(float64(t.x), -1)
	if t.x <= s.x {
		tx, gx2 = float64(t.right()), r.nextX(float64(t.right()), 1)
	}
	rows := append([]float64{sy, ty}, r.ys...)
	sort.SliceStable(rows, func(i, j int) bool {
		return abs(rows[i]-sy)+abs(rows[i]-ty) < abs(rows[j]-sy)+abs(rows[j]-ty)
	})
	for _, cy := range rows {
		p := elbow(pt{sx, sy}, pt{gx1, sy}, pt{gx1, cy}, pt{gx2, cy}, pt{gx2, ty}, pt{tx, ty})
		if p.clear(r.cards, s, t) {
			return p, true
		}
	}
	return path{}, false
}

// nextX is the free column nearest x on the given side.
func (r *router) nextX(x float64, dir int) float64 {
	best := x + float64(dir)*10
	found := false
	for _, g := range r.xs {
		if (dir > 0 && g > x || dir < 0 && g < x) && (!found || abs(g-x) < abs(best-x)) {
			best, found = g, true
		}
	}
	return best
}

// use is the dashed line from an app's card down to a band card: the
// board's S from the bottom of one to the top of the other when that is
// clear, else down the gap beside the card and along the row above the
// band card.
func (r *router) use(s, t box) path {
	a := pt{float64(s.cx()), float64(s.bottom())}
	b := pt{float64(t.cx()), float64(t.y)}
	if t.y < s.y {
		a.y, b.y = float64(s.y), float64(t.bottom())
	}
	my := (a.y + b.y) / 2
	if p := curve(a, pt{a.x, my}, pt{b.x, my}, b); p.clear(r.cards, s, t) {
		return p
	}
	above := r.rowAbove(float64(t.y))
	if p := elbow(a, pt{a.x, above}, pt{b.x, above}, b); p.clear(r.cards, s, t) {
		return p
	}
	for _, side := range []int{1, -1} {
		x0 := float64(s.right())
		if side < 0 {
			x0 = float64(s.x)
		}
		gx := r.nextX(x0, side)
		p := elbow(pt{x0, float64(s.cy())}, pt{gx, float64(s.cy())}, pt{gx, above}, pt{b.x, above}, b)
		if p.clear(r.cards, s, t) {
			return p
		}
	}
	return curve(a, pt{a.x, my}, pt{b.x, my}, b)
}

// rowAbove is the free row nearest above y.
func (r *router) rowAbove(y float64) float64 {
	best, found := y-14, false
	for _, g := range r.ys {
		if g < y && (!found || g > best) {
			best, found = g, true
		}
	}
	return best
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// ranked is names in reading order: the map's columns left to right, each
// top to bottom.
func (f flow) ranked(names []string) []string {
	out := append([]string(nil), names...)
	sort.SliceStable(out, func(i, j int) bool { return f.rank[out[i]] < f.rank[out[j]] })
	return out
}

// usesAll is every app's every use, the lines the band saves drawing.
func (f flow) usesAll() [][2]string {
	var out [][2]string
	for _, a := range f.apps {
		for _, s := range f.uses[a] {
			out = append(out, [2]string{a, s})
		}
	}
	return out
}
