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
	"fmt"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/doze-dev/doze/internal/desk/hub"
)

// Options says what the app should open and how it should report.
type Options struct {
	// Open is the project directory to show first; empty means the tray only.
	Open string
	// Measure prints timing lines to stderr for the desk bench.
	Measure bool
}

// Run starts the app and blocks until it quits. A second launch hands its
// arguments to the running one and exits; Wails does that under
// SingleInstance, so the CLI never has to.
func Run(o Options) error {
	began := time.Now()
	d := &desk{o: o, hub: hub.New(hub.Options{})}
	d.app = application.New(application.Options{
		Name:        "doze",
		Description: "doze runs your stack, and understands it",
		Mac: application.MacOptions{
			// A menu bar app: no Dock icon, and closing the window does not quit.
			ActivationPolicy: application.ActivationPolicyAccessory,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "dev.doze.app",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				d.open(dirFrom(data))
			},
		},
	})
	d.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		d.measure("tray ready %s after start", time.Since(began).Round(time.Millisecond))
		d.startTray()
		d.hub.OnChange(d.refreshTray)
		go d.hub.Run(d.app.Context())
		if o.Open != "" {
			d.open(o.Open)
		}
	})
	return d.app.Run()
}

// desk is the running app.
type desk struct {
	o   Options
	app *application.App
	hub *hub.Hub
	bar
}

// open brings the window forward at a project. The window arrives in P1;
// until then the tray is the app.
func (d *desk) open(dir string) {
	d.measure("open %s", dir)
}

// dirFrom is the project folder a second launch asked for: its argument,
// else the folder it ran in.
func dirFrom(data application.SecondInstanceData) string {
	for i, a := range data.Args {
		if a == "open" && i+1 < len(data.Args) {
			return data.Args[i+1]
		}
	}
	return data.WorkingDir
}

func (d *desk) measure(format string, args ...any) {
	if d.o.Measure {
		fmt.Fprintf(os.Stderr, "desk: "+format+"\n", args...)
	}
}
