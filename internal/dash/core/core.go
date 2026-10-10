// Package core is what a dash page and the dash that hosts it agree on. It is
// a package of its own so that pages live in sub-packages and still do not
// import the root.
//
// A page is a value: it is given a message and the shared Context, and returns
// the page to show next. It reaches the daemon only through control.Daemon,
// the interface, never a socket.
package core

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/theme"
)

// Context is what every page shares and none owns.
type Context struct {
	// Daemon is the only way a page reaches the stack.
	Daemon control.Daemon
	// Snap is the stack as of the last frame.
	Snap control.Snapshot
	// Now is the time to draw against; tests fix it.
	Now time.Time
	// Styles is the active theme in the active variant.
	Styles theme.Styles
	// Loc is the zone times are shown in; tests fix it.
	Loc *time.Location
}

// Key is one entry of the key bar: what to press and what it does.
type Key struct{ Keys, Label string }

// Page is one screen.
type Page interface {
	// Update handles a message. The returned page replaces this one.
	Update(msg tea.Msg, c *Context) (Page, tea.Cmd)
	// View draws the page into w columns and h rows.
	View(c *Context, w, h int) string
	// Keys are the keys the footer advertises on this page.
	Keys() []Key
}

// Messages the host sends to a page, beyond Bubble Tea's own.
type (
	// Resized says how many cells the page has.
	Resized struct{ W, H int }
	// Refreshed says Context.Snap changed.
	Refreshed struct{}
	// Logs is the answer to a log request a page made.
	Logs struct {
		Instance string
		Lines    []control.LogFrame
		Err      error
	}
	// Done is a verb finishing: what ran, on whom, and how it went.
	Done struct {
		Verb, Target string
		Err          error
	}
)

// Messages a page returns, as the message of a command, to ask the host for
// something. The host handles them and does not pass them on.
type (
	// Navigate shows another page, keeping this one to come back to.
	Navigate struct{ To Page }
	// Back returns to the page before.
	Back struct{}
	// Follow starts streaming the log lines of Names, tagged with ID, after a
	// backlog of recent lines. A second Follow with the same ID replaces the
	// first.
	Follow struct {
		ID      string
		Names   []string
		Backlog int
	}
	// Unfollow stops a Follow.
	Unfollow struct{ ID string }
)

// LogLine is one followed line, sent to the page on top.
type LogLine struct {
	ID    string
	Frame control.LogFrame
}

// Tab is one view inside a frame: the logs of an instance, its overview, its
// last run. A frame shows one tab at a time and routes the keys.
type Tab interface {
	// Title is the name on the tab bar.
	Title() string
	// Show is called when the tab becomes the visible one, and Hide when it
	// stops being. They return the commands that start and stop what the tab
	// follows.
	Show(c *Context, inst control.InstanceView) tea.Cmd
	Hide() tea.Cmd
	Update(msg tea.Msg, c *Context, inst control.InstanceView) (Tab, tea.Cmd)
	View(c *Context, inst control.InstanceView, w, h int) string
	Keys() []Key
}
