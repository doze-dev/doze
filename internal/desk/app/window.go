//go:build purego

package app

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// The one window. It is created when asked for and destroyed when closed, so
// the menu bar alone carries no web view (design D2, D14). A second request
// while it is open brings it forward.
type window struct {
	win      *application.WebviewWindow
	openedAt time.Time
}

// open brings the window forward at a project. The tab for the project
// arrives with the hub's views in P1; for now the window is the shell.
func (d *desk) open(dir string) {
	d.measure("open %s", dir)
	if key := d.svc.KeyFor(dir); key != "" {
		d.svc.Navigate(key)
	}
	application.InvokeSync(func() {
		if d.win != nil {
			d.win.Show()
			d.win.Focus()
			return
		}
		d.openedAt = time.Now()
		// A menu bar app is not brought forward by a new window on its own:
		// it becomes a regular app (Dock icon, ⌘Tab) while the window is open.
		activate(true)
		d.win = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:            "doze",
			Width:            960,
			Height:           640,
			MinWidth:         640,
			MinHeight:        400,
			URL:              "/",
			BackgroundType:   application.BackgroundTypeSolid,
			BackgroundColour: application.NewRGB(0x14, 0x16, 0x1a),
			Mac: application.MacWindow{
				TitleBar:                application.MacTitleBarHiddenInset,
				InvisibleTitleBarHeight: 38,
			},
		})
		// Window event listeners run on their own goroutine; d.win and the
		// activation policy belong to the main thread.
		d.win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
			application.InvokeSync(func() {
				d.measure("window closed")
				d.win = nil
				activate(false)
			})
		})
		d.win.Show()
		d.win.Focus()
		d.measure("window shown %s after open", time.Since(d.openedAt).Round(time.Millisecond))
	})
	// The policy change lands on the next run-loop turn; activate after it.
	time.AfterFunc(50*time.Millisecond, func() {
		application.InvokeSync(func() {
			bringForward()
			if d.win != nil {
				d.win.Focus()
			}
		})
	})
}
