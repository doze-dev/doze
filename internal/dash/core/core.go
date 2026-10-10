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
