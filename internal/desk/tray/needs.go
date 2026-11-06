package tray

import (
	"fmt"

	"github.com/doze-dev/doze/internal/desk/view"
)

// Needs is the menu's Needs you section (the MenuBarNeeds board), as data:
// a header, up to three red items each with its first fix, and one line
// for the things worth doing. Empty when nothing is red or worth doing.
type Needs struct {
	Header string     // "Needs you · 2"; "" when nothing is red
	Lines  []NeedLine // red items, newest first, at most three
	Worth  string     // "and 3 things worth doing   ·   Open", or ""
}

// NeedLine is one red item: its title with its stack, the detail as the
// first line of its submenu, and the fix the menu can press.
type NeedLine struct {
	Title  string
	Detail string
	Item   view.Item
	// Fix is the first fix pressable outside the window; nil when every
	// fix asks first (the submenu then has Show alone).
	Fix *view.Fix
}

// shownRed is how many red items the menu lists.
const shownRed = 3

// NeedsOf builds the section from the list as the bell has it.
func NeedsOf(a view.Attention) Needs {
	var n Needs
	for _, it := range a.Items {
		if !it.Red || it.Fixed {
			continue
		}
		if len(n.Lines) == shownRed {
			break
		}
		l := NeedLine{Title: it.Title, Detail: it.Detail, Item: it}
		if it.Stack != "" {
			l.Title += "   ·   " + it.Stack
		}
		for _, f := range it.Fixes {
			if f.Kind != "show" {
				fix := f
				l.Fix = &fix
				break
			}
		}
		n.Lines = append(n.Lines, l)
	}
	if len(n.Lines) > 0 {
		n.Header = fmt.Sprintf("Needs you · %d", a.Red)
	}
	if a.Yellow > 0 {
		n.Worth = fmt.Sprintf("%d thing%s worth doing   ·   Open", a.Yellow, map[bool]string{true: "", false: "s"}[a.Yellow == 1])
		if len(n.Lines) > 0 {
			n.Worth = "and " + n.Worth
		}
	}
	return n
}

// Empty says there is no section.
func (n Needs) Empty() bool { return len(n.Lines) == 0 && n.Worth == "" }
