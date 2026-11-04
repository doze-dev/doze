//go:build purego

// Package app is the desktop face of doze: the menu bar, the window and the
// native glue around them. It is the only package in doze that imports Wails,
// and it is compiled only with the purego tag, so a build without the tag is
// the CLI and the daemon alone.
//
// The daemon and every CLI command link this package without ever calling
// into it. That must stay free: nothing here may run at init time beyond what
// Wails itself does, and the bench (test/bench) enforces the daemon's idle
// memory with the package linked.
package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Options says what the app should open and how it should report.
type Options struct {
	// Open is the project directory to show first; empty means the tray only.
	Open string
	// Measure prints timing lines to stderr for the desk bench.
	Measure bool
}

// Run starts the app and blocks until it quits.
func Run(o Options) error {
	a := application.New(application.Options{
		Name:        "doze",
		Description: "doze runs your stack, and understands it",
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory,
		},
	})
	return a.Run()
}
