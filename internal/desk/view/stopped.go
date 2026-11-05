package view

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// Stopped is a stack that is not running, as the StackStopped board lays
// it out: what doze.hcl declares, ticked as last time, with what each
// needs and where it will be once up. It is read from the config, since
// there is no daemon to ask.
type Stopped struct {
	// When is "last up 3 days ago", or "never up".
	When string `json:"when"`
	// LastLine is the banner's second sentence: "Last time you started
	// api, web and db. That is ticked for you."
	LastLine string `json:"lastLine"`
	// LastSet is what was started last time; empty means everything.
	LastSet []string    `json:"lastSet,omitempty"`
	Groups  []StopGroup `json:"groups"`
	Problem string      `json:"problem,omitempty"` // doze.hcl did not load
}

// StopGroup is one heading of the stopped page: "APPS 3 · 2 ticked",
// "MODULES 3 · wake when needed". What is empty for the apps, whose
// ticked count the page keeps as it changes.
type StopGroup struct {
	Title string    `json:"title"`
	Count int       `json:"count"`
	What  string    `json:"what,omitempty"`
	Rows  []StopRow `json:"rows"`
}

// StopRow is one declared instance.
type StopRow struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Engine string `json:"engine"`
	Run    string `json:"run,omitempty"`
	// Needs is the first part of the sentence ("db, cache" or "nothing");
	// Note the rest ("they start first", "not last time").
	Needs   string `json:"needs"`
	Note    string `json:"note"`
	Address string `json:"address,omitempty"`
	// Tick: "on" starts on Up, "half" wakes when something needs it, ""
	// runs by hand (a task), "off" is declared but paused.
	Tick string `json:"tick"`
}

// StoppedOf reads the stack's config and lays it out. A config that does
// not load is reported, not fatal: the page says so and offers the file.
func StoppedOf(st hub.StackView, now time.Time) Stopped {
	out := Stopped{LastSet: st.LastSet, Groups: []StopGroup{}}
	if st.LastUp.IsZero() {
		out.When = "never up"
	} else {
		out.When = "last up " + display.Ago(now.Sub(st.LastUp)) + " ago"
	}
	if len(st.LastSet) > 0 {
		out.LastLine = "Last time you started " + andList(st.LastSet) + ". That is ticked for you."
	} else {
		out.LastLine = "Everything is ticked, as last time."
	}
	cfg, err := config.LoadShape(filepath.Join(st.Dir, "doze.hcl"), nil)
	if err != nil {
		out.Problem = display.Humanise(err)
		return out
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
	// Who needs each row, in declaration order.
	dependants := map[string][]string{}
	for _, d := range decls {
		for _, dep := range d.Deps {
			dependants[dep.Name] = append(dependants[dep.Name], d.Name)
		}
	}

	byGroup := map[int][]StopRow{}
	for _, d := range decls {
		row := StopRow{Name: d.Name, Kind: d.Kind, Engine: engineOf(d)}
		if r, ok := d.Spec.(interface{ RunLine() string }); ok {
			row.Run = r.RunLine()
		}
		var needs []string
		for _, dep := range d.Deps {
			needs = append(needs, dep.Name)
		}
		row.Needs = "nothing"
		if len(needs) > 0 {
			row.Needs = strings.Join(needs, ", ")
		}
		row.Address = addressWhenUp(d, stack)
		var onDependants []string
		for _, n := range dependants[d.Name] {
			if on[n] {
				onDependants = append(onDependants, n)
			}
		}
		switch {
		case !d.Enabled:
			row.Tick, row.Note = "off", "paused in doze.hcl (enabled = false)"
		case d.Kind == "task":
			row.Tick, row.Note = "", "runs when you say, not on Up"
		case d.Kind == "module" && on[d.Name] && len(onDependants) > 0:
			row.Tick = "on"
			if len(onDependants) == 1 {
				row.Note = onDependants[0] + " needs it, so it starts first"
			} else {
				row.Note = andList(onDependants) + " need it, so they start after it"
			}
		case on[d.Name] && len(needs) == 1:
			row.Tick, row.Note = "on", "it starts first"
		case on[d.Name] && len(needs) > 1:
			row.Tick, row.Note = "on", "they start first"
		case on[d.Name]:
			row.Tick, row.Note = "on", "starts on Up"
		case d.Kind == "module" && len(dependants[d.Name]) > 0:
			row.Tick, row.Note = "half", "starts when "+dependants[d.Name][0]+" asks for it"
		case d.Kind == "module":
			row.Tick, row.Note = "half", "wakes when something needs it"
		default:
			row.Tick, row.Note = "half", "not last time"
		}
		if len(d.Domains) > 0 && row.Tick != "off" {
			row.Note += " · https://" + d.Domains[0]
		}
		byGroup[groupOf(d.Kind)] = append(byGroup[groupOf(d.Kind)], row)
	}
	for gi, g := range display.Groups {
		rows := byGroup[gi]
		if len(rows) == 0 {
			continue
		}
		grp := StopGroup{Title: g.Title, Count: len(rows), Rows: rows}
		switch g.Kinds[0] {
		case "module":
			grp.What = "wake when needed"
		case "task":
			grp.What = "run by hand"
		case "service":
			grp.What = "started by you"
		}
		out.Groups = append(out.Groups, grp)
	}
	return out
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

func groupOf(kind string) int {
	for i, g := range display.Groups {
		for _, k := range g.Kinds {
			if k == kind {
				return i
			}
		}
	}
	return 1
}
