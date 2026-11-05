// Package tray decides what the menu bar shows: the count beside the bat,
// whether the red dot is on, and the menu, as plain data. It knows nothing
// about Wails; internal/desk/app turns a Model into native menu items. Being
// data, it has a table test, and the words come from internal/display so
// the menu bar, the CLI and the TUI say the same thing.
package tray

import (
	"fmt"
	"sort"

	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// Model is the menu bar, as data.
type Model struct {
	// Label is the text beside the bat: the number of awake instances, or
	// nothing when none is.
	Label string
	// NeedsYou turns the red dot on: something crashed, or a task failed.
	NeedsYou bool
	// Tooltip is one line for a hover.
	Tooltip string
	// Sections is one per running stack, in name order. Empty means no stack
	// is running, and the menu says so.
	Sections []Section
	// Stopped lists stacks that are known but not running, most recent first.
	Stopped []Stopped
}

// Section is one running stack's part of the menu.
type Section struct {
	// Key is the stack's hub key, for verbs; Name is the stack's name.
	Key, Name string
	// Title is the header line: the stack's name and how many are awake, or
	// what is wrong with the connection.
	Title string
	// Dir is the project folder, for "Open".
	Dir string
	// Rows are the instances, in name order, without tasks.
	Rows []Row
}

// Row is one instance in a stack's section.
type Row struct {
	Name string
	// Label is the whole line: glyph, name, state word, memory.
	Label string
	// Awake says whether Sleep applies; otherwise Wake does.
	Awake bool
	// Trouble says the row is what the red dot is about.
	Trouble bool
}

// Stopped is a stack that is not running.
type Stopped struct {
	Key, Name, Dir string
}

// Build makes the menu bar's model from what the hub knows.
func Build(stacks []hub.StackView) Model {
	m := Model{Tooltip: "doze"}
	awake := 0
	for _, st := range stacks {
		if !st.Running {
			m.Stopped = append(m.Stopped, Stopped{Key: st.Key, Name: st.Name, Dir: st.Dir})
			continue
		}
		sec := Section{Key: st.Key, Dir: st.Dir}
		n := 0
		for _, v := range st.Instances {
			if v.Kind == "task" {
				continue
			}
			up := display.Awake(v.State)
			if up {
				n++
			}
			look := display.LookOf(v)
			r := Row{Name: v.Name, Awake: up, Trouble: display.Troubled(v)}
			r.Label = fmt.Sprintf("%s %s  %s", look.Glyph, v.Name, look.Word)
			if v.RAM > 0 {
				r.Label += fmt.Sprintf("  %d MB", v.RAM>>20)
			}
			if r.Trouble {
				m.NeedsYou = true
			}
			sec.Rows = append(sec.Rows, r)
		}
		sort.SliceStable(sec.Rows, func(i, j int) bool { return sec.Rows[i].Name < sec.Rows[j].Name })
		awake += n
		name := st.Name
		if name == "" {
			name = "a stack"
		}
		sec.Name = name
		switch st.Conn {
		case hub.ConnConnecting:
			sec.Title = name + " · connecting…"
		case hub.ConnDown:
			sec.Title = name + " · daemon not responding"
		default:
			sec.Title = fmt.Sprintf("%s · %d awake", name, n)
		}
		m.Sections = append(m.Sections, sec)
	}
	sort.SliceStable(m.Sections, func(i, j int) bool { return m.Sections[i].Title < m.Sections[j].Title })
	// The bat stands alone: no number beside it, ever (the owner's call).
	// What needs you is the red dot; the count is in the tooltip.
	if len(m.Sections) > 0 {
		m.Tooltip = fmt.Sprintf("doze · %d awake", awake)
	}
	return m
}
