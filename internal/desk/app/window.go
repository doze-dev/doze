//go:build purego

package app

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/doze-dev/doze/internal/desk/prefs"
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
	d.svc.Open(dir)
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
		// The window comes back where it was closed (D15).
		p := prefs.Load(d.hub.Home())
		opts := application.WebviewWindowOptions{
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
		}
		if p.Window.Placed() {
			opts.Width, opts.Height = p.Window.W, p.Window.H
			opts.X, opts.Y = p.Window.X, p.Window.Y
			opts.InitialPosition = application.WindowXY
		}
		d.win = d.app.Window.NewWithOptions(opts)
		// Window event listeners run on their own goroutine; d.win and the
		// activation policy belong to the main thread.
		d.win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
			application.InvokeSync(func() {
				if w := d.win; w != nil {
					// Remembered for the next open: the frame, and the project.
					p := prefs.Load(d.hub.Home())
					p.Window.W, p.Window.H = w.Size()
					p.Window.X, p.Window.Y = w.Position()
					if dir != "" {
						p.LastDir = dir
					}
					if err := prefs.Save(d.hub.Home(), p); err != nil {
						d.measure("prefs not saved: %v", err)
					}
				}
				d.measure("window closed")
				d.win = nil
				activate(false)
			})
			// Nothing streams for a page nobody is looking at (D14).
			d.svc.Blur()
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
