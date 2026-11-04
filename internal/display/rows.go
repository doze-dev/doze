package display

import (
	"sort"

	"github.com/doze-dev/doze/internal/control"
)

// How a stack is listed: the groups a surface puts instances under, and the
// order within them. The dash's table and the app's home page list the same
// stack the same way because both take it from here.

// Group is a heading over part of the stack, and what belongs under it
// (Main.dc.html): the title, a line on what the group is, and the kinds of
// instance it holds.
type Group struct {
	Title, What string
	Kinds       []string
}

// Groups are the four headings, in the order they are drawn.
var Groups = []Group{
	{"APPS", "your code", []string{"app"}},
	{"MODULES", "from the doze registry", []string{"module", ""}},
	{"OUTSIDE", "brought in by you", []string{"service"}},
	{"TASKS", "run once, then out of the way", []string{"task"}},
}

// Row is one instance in display order, and the index of its group.
type Row struct {
	V     control.InstanceView
	Group int
}

// Rows lists the stack: by group, then by name.
func Rows(snap control.Snapshot) []Row {
	var out []Row
	for gi, g := range Groups {
		var in []control.InstanceView
		for _, v := range snap.Instances {
			for _, k := range g.Kinds {
				if v.Kind == k {
					in = append(in, v)
					break
				}
			}
		}
		sort.Slice(in, func(i, j int) bool { return in[i].Name < in[j].Name })
		for _, v := range in {
			out = append(out, Row{V: v, Group: gi})
		}
	}
	return out
}

// Find is an instance of a snapshot by name.
func Find(snap control.Snapshot, name string) (control.InstanceView, bool) {
	for _, v := range snap.Instances {
		if v.Name == name {
			return v, true
		}
	}
	return control.InstanceView{}, false
}

// Sort orders. The groups stay: apps, modules, outside, tasks; a sort orders
// the rows within each.
const (
	SortName = iota
	SortMem
	SortState
	SortModes
)

// SortTitles name the sort orders, for a key bar or a menu.
var SortTitles = [SortModes]string{"name", "mem", "state"}

// TreeOrder orders rs as the dependency tree: what nothing needs is a root,
// and what it needs is under it, once, at the first place it is needed. depth
// is how far down each is.
func TreeOrder(rs []Row) (out []Row, depth map[string]int) {
	byName := map[string]Row{}
	for _, r := range rs {
		byName[r.V.Name] = r
	}
	needed := map[string]bool{}
	for _, r := range rs {
		for _, n := range r.V.Needs {
			if _, ok := byName[n]; ok {
				needed[n] = true
			}
		}
	}
	depth = map[string]int{}
	seen := map[string]bool{}
	var walk func(r Row, d int)
	walk = func(r Row, d int) {
		if seen[r.V.Name] {
			return
		}
		seen[r.V.Name] = true
		depth[r.V.Name] = d
		out = append(out, r)
		for _, n := range r.V.Needs {
			if c, ok := byName[n]; ok {
				walk(c, d+1)
			}
		}
	}
	for _, r := range rs {
		if !needed[r.V.Name] {
			walk(r, 0)
		}
	}
	for _, r := range rs { // a cycle, or what only a cycle needs
		walk(r, 0)
	}
	return out, depth
}

// StateRank is the order a sort by state puts instances in: what needs you
// first, then what is waking, awake, dozing, and asleep last.
func StateRank(v control.InstanceView) int {
	switch {
	case Troubled(v):
		return 0
	case v.State == "booting":
		return 1
	case v.State == "active":
		return 2
	case v.State == "idle":
		return 3
	}
	return 4
}
