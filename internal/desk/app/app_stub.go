//go:build !purego

// Package app is the desktop face of doze. Without the purego build tag the
// app is not compiled in: this file keeps the CLI and the daemon building, and
// says so when someone asks for the app anyway.
package app

import (
	"errors"
	"time"
)

// Options says what the app should open and how it should report.
type Options struct {
	// Open is the project directory to show first; empty means the tray only.
	Open string
	// Measure prints timing lines to stderr for the desk bench.
	Measure bool
	// Fixture names a scripted stack to run over instead of the real daemons,
	// for the desk bench; Window opens the window on it.
	Fixture string
	Window  bool
	// CloseAfter closes the window that long after it opens, and QuitAfter
	// quits the app that long after it starts; zero means never. For the
	// desk bench.
	CloseAfter, QuitAfter time.Duration
	// Version is the doze this is, shown in the sidebar.
	Version string
}

// ErrNotBuilt is returned when this binary was built without the app.
var ErrNotBuilt = errors.New("this doze was built without the app (build with -tags purego)")

// Run reports that the app is not in this binary.
func Run(Options) error { return ErrNotBuilt }
