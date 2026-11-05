// Package tray decides what the menu bar shows, as the MenuBar board draws
// it: the bat alone (never a number), the red dot when something needs
// you, and one line per stack with a quiet second line. It knows nothing
// about Wails; internal/desk/app turns a Model into native menu items.
// Being data, it has a table test, and the words come from
// internal/display so the menu bar, the CLI and the app say the same thing.
package tray

import (
	"fmt"
	"sort"
	"time"

	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/view"
	"github.com/doze-dev/doze/internal/display"
)

// Model is the menu bar, as data.
type Model struct {
	// NeedsYou turns the red dot on: something crashed, or a task failed.
	NeedsYou bool
	// Tooltip is one line for a hover: "doze · 4 awake".
	Tooltip string
	// Lines is one per stack, running ones first in name order, then the
	// stopped ones most recently up first. Empty means doze knows no stack.
	Lines []Line
}

// Line is one stack in the menu: its name, and a second line saying how it
// is. Clicking it opens the stack in the window.
type Line struct {
	Key, Name, Dir string
	Running        bool
	// Sub is the quiet line under the name: "4 awake · 551 MB ·
	// https://shop.acme.dev", "search is crashing · try 2 of 5", or for a
	// stopped stack "up 3 days ago".
	Sub string
	// Trouble says this stack is what the red dot is about.
	Trouble bool
}

// Build makes the menu bar's model from what the hub knows at now.
func Build(stacks []hub.StackView, now time.Time) Model {
	m := Model{Tooltip: "doze"}
	awake := 0
	var running, stopped []Line
	for _, st := range stacks {
		name := st.Name
		if name == "" {
			name = "a stack"
		}
		if !st.Running {
			l := Line{Key: st.Key, Name: name, Dir: st.Dir, Sub: "never up"}
			if !st.LastUp.IsZero() {
				l.Sub = "up " + display.Ago(now.Sub(st.LastUp)) + " ago"
			}
			stopped = append(stopped, l)
			continue
		}
		l := Line{Key: st.Key, Name: name, Dir: st.Dir, Running: true}
		n, mem := 0, int64(0)
		snap := view.Snapshot(st)
		for _, v := range st.Instances {
			if v.Kind == "task" {
				continue
			}
			if display.Awake(v.State) {
				n++
				mem += v.RAM
			}
			if display.Troubled(v) && !l.Trouble {
				// The first thing wrong is the line: what, and the try.
				l.Trouble, m.NeedsYou = true, true
				l.Sub = v.Name + " is " + display.LookOf(v).Word
				if b := display.Because(v, snap, now); b != "" {
					l.Sub += " · " + b
				}
			}
		}
		awake += n
		switch {
		case l.Trouble:
		case st.Conn == hub.ConnConnecting:
			l.Sub = "connecting…"
		case st.Conn == hub.ConnDown:
			l.Sub = "daemon not responding"
		default:
			l.Sub = fmt.Sprintf("%d awake", n)
			if mem > 0 {
				l.Sub += " · " + display.MB(mem)
			}
			if url := firstURL(st); url != "" {
				l.Sub += " · " + url
			}
		}
		running = append(running, l)
	}
	sort.SliceStable(running, func(i, j int) bool { return running[i].Name < running[j].Name })
	sort.SliceStable(stopped, func(i, j int) bool {
		a, b := lastUp(stacks, stopped[i].Key), lastUp(stacks, stopped[j].Key)
		if !a.Equal(b) {
			return a.After(b)
		}
		return stopped[i].Name < stopped[j].Name
	})
	m.Lines = append(running, stopped...)
	if len(running) > 0 {
		m.Tooltip = fmt.Sprintf("doze · %d awake", awake)
	}
	return m
}

// firstURL is the stack's own domain if an app declares one, else nothing:
// the menu has room for one address.
func firstURL(st hub.StackView) string {
	for _, v := range st.Instances {
		if v.HTTPS != "" {
			return v.HTTPS
		}
	}
	return ""
}

func lastUp(stacks []hub.StackView, key string) time.Time {
	for _, st := range stacks {
		if st.Key == key {
			return st.LastUp
		}
	}
	return time.Time{}
}
