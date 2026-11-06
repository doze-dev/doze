package view

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// Stopped is what the overview adds for a stack that is not running: when
// it was last up, the banner's words, and a doze.hcl that does not load.
// The rows themselves are the overview's own (Home.Groups), each down and
// ticked as last time, so stopping a stack changes states, not the page.
type Stopped struct {
	// When is "last up 3 days ago", or "never up".
	When string `json:"when"`
	// LastLine says what Up as last time does: "Up starts api, web and
	// db, as last time, and what they need."
	LastLine string `json:"lastLine"`
	// LastSet is what was started last time; empty means everything.
	LastSet []string `json:"lastSet,omitempty"`
	Problem string   `json:"problem,omitempty"` // doze.hcl did not load
}

// StoppedHome fills h for a stack that is not running, from its doze.hcl
// since there is no daemon to ask: the same groups, rows in the same
// order and the same map as when it runs, every row down, with a tick
// that says whether Up starts it.
func StoppedHome(h *Home, st hub.StackView, now time.Time) {
	out := Stopped{LastSet: st.LastSet}
	if st.LastUp.IsZero() {
		out.When = "never up"
	} else {
		out.When = "last up " + display.Ago(now.Sub(st.LastUp)) + " ago"
	}
	if len(st.LastSet) > 0 {
		out.LastLine = "Up starts " + andList(st.LastSet) + ", as last time, and what they need."
	} else {
		out.LastLine = "Up starts everything, as last time."
	}
	h.Stopped = &out
	h.Groups, h.Side = []Group{}, []Side{}
	h.Map = MapView{Columns: []MapColumn{}, Nodes: []MapNode{}, Edges: []MapEdge{}, W: mapLeft + 3*mapColW, H: 580}
	cfg, err := config.LoadShape(filepath.Join(st.Dir, "doze.hcl"), nil)
	if err != nil {
		out.Problem = display.Humanise(err)
		return
	}
	stack := cfg.Stack()
	decls := cfg.Decls()

	// Which rows start on Up: the apps of the last set (every app when
	// nothing is remembered), and whatever those need, all the way down.
	on := map[string]bool{}
	byName := map[string]*config.InstanceDecl{}
	for _, d := range decls {
		byName[d.Name] = d
	}
	last := map[string]bool{}
	for _, n := range st.LastSet {
		last[n] = true
	}
	var mark func(name string)
	mark = func(name string) {
		d := byName[name]
		if d == nil || on[name] || !d.Enabled {
			return
		}
		on[name] = true
		for _, dep := range d.Deps {
			mark(dep.Name)
		}
	}
	for _, d := range decls {
		if d.Kind == "task" || !d.Enabled {
			continue
		}
		if len(st.LastSet) == 0 || last[d.Name] {
			mark(d.Name)
		}
	}
	dependants := map[string][]string{}
	for _, d := range decls {
		for _, dep := range d.Deps {
			dependants[dep.Name] = append(dependants[dep.Name], d.Name)
		}
	}

	// The instances as the daemon would list them, so the rows sort and
	// the map places exactly as they will once the stack is up.
	snap := control.Snapshot{Stack: stack, Dir: st.Dir}
	for _, d := range decls {
		v := control.InstanceView{Name: d.Name, Kind: d.Kind}
		for _, dep := range d.Deps {
			v.Needs = append(v.Needs, dep.Name)
		}
		snap.Instances = append(snap.Instances, v)
	}
	rows := display.Rows(snap)
	_, depth := display.TreeOrder(rows)
	for gi, g := range display.Groups {
		grp := Group{Title: g.Title, What: g.What}
		for _, r := range rows {
			if r.Group != gi {
				continue
			}
			d := byName[r.V.Name]
			row := downRow(d, r.V.Needs, on, dependants[d.Name], stack)
			row.Depth = depth[d.Name]
			grp.Rows = append(grp.Rows, row)
			grp.Count++
			if d.Kind != "task" {
				h.Total++
			}
			h.Side = append(h.Side, Side{Name: d.Name, Kind: d.Kind, Look: row.Look, Note: "down"})
		}
		if len(grp.Rows) > 0 {
			h.Groups = append(h.Groups, grp)
		}
	}
	h.Map = mapOf(hub.StackView{}, rows)
}

// downRow is one declared instance of a stopped stack: down, with what it
// needs and what Up will do with it as the because, and where it will
// answer.
func downRow(d *config.InstanceDecl, needs []string, on map[string]bool, dependants []string, stack string) Row {
	// A stopped stack runs nothing, so every switch is off. The because
	// says what the service is to the others; what Up starts is the strip's
	// to say, and the start sheet's to change.
	row := Row{Name: d.Name, Kind: d.Kind, Engine: engineOf(d), Down: true, BecauseTone: "dim",
		Look: Look{Word: "down", Glyph: "·", Tone: "dim"}, Needs: needs}
	if r, ok := d.Spec.(interface{ RunLine() string }); ok {
		row.Run = r.RunLine()
	}
	row.Address = addressWhenUp(d, stack)
	row.Last = on[d.Name] && d.Kind != "task"
	switch {
	case !d.Enabled:
		row.Because, row.Off = "paused in doze.hcl (enabled = false)", true
	case d.Kind == "task" && len(needs) > 0:
		row.Because = "needs " + andList(needs) + " · runs once the stack is up"
	case d.Kind == "task":
		row.Because = "runs once the stack is up"
	case d.Kind == "module" && len(dependants) == 1:
		row.Because = dependants[0] + " uses it"
	case d.Kind == "module" && len(dependants) > 1:
		row.Because = andList(dependants) + " use it"
	case d.Kind == "module":
		row.Because = "nothing uses it yet"
	case len(needs) > 0:
		row.Because = "needs " + andList(needs)
	default:
		row.Because = "needs nothing"
	}
	return row
}

// addressWhenUp is where the row answers once the stack is up: its own
// domain if it has one, else its .doze name with the port when it has one.
func addressWhenUp(d *config.InstanceDecl, stack string) string {
	if len(d.Domains) > 0 {
		return d.Domains[0]
	}
	if stack == "" || d.Kind == "task" {
		return ""
	}
	name := fmt.Sprintf("%s.%s.%s", d.Name, stack, config.DomainSuffix)
	if d.Port > 0 {
		return fmt.Sprintf("%s:%d", name, d.Port)
	}
	return name
}

// declaredIn counts what a stack's doze.hcl declares, tasks aside, for a
// stack with no daemon to ask. Zero when the file does not load.
func declaredIn(dir string) int {
	cfg, err := config.LoadShape(filepath.Join(dir, "doze.hcl"), nil)
	if err != nil {
		return 0
	}
	n := 0
	for _, d := range cfg.Decls() {
		if d.Kind != "task" {
			n++
		}
	}
	return n
}

// andList joins names as a sentence does: "api", "api and web", "api,
// web and db".
func andList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func engineOf(d *config.InstanceDecl) string {
	switch d.Kind {
	case "module":
		return strings.TrimSpace(d.Type + " " + d.Version.String())
	case "task":
		return "task"
	}
	return "process"
}
