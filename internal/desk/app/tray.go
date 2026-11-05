//go:build purego

package app

import (
	_ "embed"
	"fmt"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/doze-dev/doze/internal/desk/tray"
)

// The menu bar bat, black on transparent, as a template image so the system
// draws it for light and dark menu bars. 44 px is the 2x image the bar wants.
//
//go:embed icons/bat-44.png
var batIcon []byte

// bar turns the tray package's model into the native menu bar item, and
// rebuilds it only when the model changes: the hub reports every frame, and
// most frames change nothing a menu shows.
type bar struct {
	item *application.SystemTray

	mu      sync.Mutex
	built   uint64 // models built, in order
	applied uint64 // the newest model the menu shows
	shown   string
}

func (d *desk) startTray() {
	d.item = d.app.SystemTray.New()
	d.item.SetTemplateIcon(batIcon)
	d.item.SetTooltip("doze")
	d.refreshTray()
}

// refreshTray is called from the hub's goroutine and from a verb's; the
// menu is built there and handed to the main thread whole. Calls can
// overtake each other, so each model carries its sequence number and the
// main thread applies only what is newer than the menu it shows.
func (d *desk) refreshTray() {
	m := tray.Build(d.hub.Stacks(), time.Now())
	sig := fmt.Sprintf("%+v", m)
	d.mu.Lock()
	d.built++
	seq := d.built
	same := sig == d.shown
	d.mu.Unlock()
	if same {
		return
	}
	menu := d.menu(m)
	application.InvokeSync(func() {
		d.mu.Lock()
		stale := seq < d.applied
		if !stale {
			d.applied, d.shown = seq, sig
		}
		d.mu.Unlock()
		if stale {
			return
		}
		d.item.SetTooltip(m.Tooltip)
		d.item.SetMenu(menu)
	})
}

// menu is the MenuBar board: one line per stack, running first, with how
// it is under the name; then Open doze, Settings…, Quit. A line opens its
// stack in the window; a stopped stack's line opens its page, where the
// switch is. Native menus take one line of text, so the second line of the
// board follows the name after a dot.
func (d *desk) menu(m tray.Model) *application.Menu {
	menu := d.app.NewMenu()
	if len(m.Lines) == 0 {
		menu.Add("No stacks yet · open a folder in doze").SetEnabled(false)
	}
	stoppedStarted := false
	for _, l := range m.Lines {
		if !l.Running && !stoppedStarted {
			stoppedStarted = true
			if l != m.Lines[0] {
				menu.AddSeparator()
			}
		}
		dir := l.Dir
		title := l.Name
		if l.Sub != "" {
			title += "   ·   " + l.Sub
		}
		item := menu.Add(title).OnClick(func(*application.Context) { d.open(dir) })
		if l.Trouble {
			item.SetTooltip("needs you")
		}
	}
	menu.AddSeparator()
	menu.Add("Open doze").SetAccelerator("CmdOrCtrl+Shift+D").OnClick(func(*application.Context) { d.open("") })
	menu.Add("Settings…").SetAccelerator("CmdOrCtrl+,").OnClick(func(*application.Context) {
		d.svc.OpenSettings()
		d.open("")
	})
	menu.Add("Quit doze   ·   stacks keep running").OnClick(func(*application.Context) { d.app.Quit() })
	return menu
}
