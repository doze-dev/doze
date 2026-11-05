//go:build purego

package app

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/doze-dev/doze/internal/control"
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
	m := tray.Build(d.hub.Stacks())
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
		d.item.SetLabel(m.Label)
		d.item.SetTooltip(m.Tooltip)
		d.item.SetMenu(menu)
	})
}

func (d *desk) menu(m tray.Model) *application.Menu {
	menu := d.app.NewMenu()
	if len(m.Sections) == 0 {
		menu.Add("No stack is running").SetEnabled(false)
	}
	for _, sec := range m.Sections {
		menu.AddSectionHeader(sec.Title)
		for _, r := range sec.Rows {
			sub := menu.AddSubmenu(r.Label)
			key, name := sec.Key, r.Name
			sub.Add("Wake").SetEnabled(!r.Awake).OnClick(func(*application.Context) {
				d.run(key, control.Verb{Name: control.VerbWake, Target: name})
			})
			sub.Add("Sleep").SetEnabled(r.Awake).OnClick(func(*application.Context) {
				d.run(key, control.Verb{Name: control.VerbSleep, Target: name})
			})
		}
		dir, key := sec.Dir, sec.Key
		menu.Add("Open " + sec.Name).OnClick(func(*application.Context) { d.open(dir) })
		menu.Add("Down").OnClick(func(*application.Context) { d.stack(key, "down") })
		menu.AddSeparator()
	}
	for _, s := range m.Stopped {
		// A stopped stack, with the switch's two sides: open it, or bring it
		// up as last time without the window.
		sub := menu.AddSubmenu(s.Name + " · stopped")
		dir, key := s.Dir, s.Key
		sub.Add("Up as last time").OnClick(func(*application.Context) { d.stack(key, "up-last") })
		sub.Add("Open").OnClick(func(*application.Context) { d.open(dir) })
	}
	if len(m.Stopped) > 0 {
		menu.AddSeparator()
	}
	menu.Add("Open doze").OnClick(func(*application.Context) { d.open("") })
	menu.Add("Quit doze").OnClick(func(*application.Context) { d.app.Quit() })
	return menu
}

// stack acts on a whole stack from the menu, off the main thread.
func (d *desk) stack(key, action string) {
	go func() {
		if err := d.svc.Stack(key, action, nil); err != nil {
			fmt.Fprintf(os.Stderr, "doze: %s: %v\n", action, err)
		}
	}()
}

// run sends a verb off the main thread and refreshes once it is done, so a
// click's result shows without waiting for the next frame.
func (d *desk) run(key string, verb control.Verb) {
	go func() {
		if err := d.hub.Do(context.Background(), key, verb); err != nil {
			fmt.Fprintf(os.Stderr, "doze: %s %s: %v\n", verb.Name, verb.Target, err)
		}
		d.hub.Refresh()
		d.refreshTray()
	}()
}
