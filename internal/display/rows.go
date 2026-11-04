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
