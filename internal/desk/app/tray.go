//go:build purego

package app

import (
	_ "embed"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/doze-dev/doze/internal/desk/tray"
	"github.com/doze-dev/doze/internal/desk/view"
)

// The menu bar bat, black on transparent, as a template image so the system
// draws it for light and dark menu bars. 44 px is the 2x image the bar wants.
//
//go:embed icons/bat-44.png
var batIcon []byte

// alertIcon is the bat with a dot: something crashed or a task failed.
//
//go:embed icons/bat-alert-44.png
var alertIcon []byte

// bar turns the tray package's model into the native menu bar item, and
// rebuilds it only when the model changes: the hub reports every frame, and
// most frames change nothing a menu shows.
type bar struct {
	item *application.SystemTray

	mu      sync.Mutex
	alert   bool   // the dot is showing
	built   uint64 // models built, in order
	applied uint64 // the newest model the menu shows
	shown   string
}

func (d *desk) startTray() {
	d.item = d.app.SystemTray.New()
	d.item.SetTemplateIcon(batIcon)
	d.item.SetTooltip("doze")
	d.svc.OnAttention(d.refreshTray)
	if runtime.GOOS == "linux" {
		// A StatusNotifier item says it is not a menu, so a desktop opens the
		// menu on the secondary button and sends the primary one here: the
		// primary click opens the window, as it does for most tray apps there.
		// macOS shows the menu on the click itself and never calls this.
		d.item.OnClick(func() { d.open("") })
	}
	d.refreshTray()
}

// refreshTray is called from the hub's goroutine and from a verb's; the
// menu is built there and handed to the main thread whole. Calls can
// overtake each other, so each model carries its sequence number and the
// main thread applies only what is newer than the menu it shows.
func (d *desk) refreshTray() {
	m := tray.Build(d.hub.Stacks(), time.Now())
	// The list as the last push built it; a change to it calls here again
	// (OnAttention), at the push's pace. The dot is what is red and not
	// put aside, as the bell counts it.
	a := d.svc.LastAttention()
	m.NeedsYou = a.Red > 0
	sig := fmt.Sprintf("%+v|%s", m, attnSig(a))
	d.mu.Lock()
	d.built++
	seq := d.built
	same := sig == d.shown
	d.mu.Unlock()
	if same {
		return
	}
	menu := d.menu(m, a)
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
		if m.NeedsYou != d.alert {
			d.alert = m.NeedsYou
			if m.NeedsYou {
				d.item.SetTemplateIcon(alertIcon)
			} else {
				d.item.SetTemplateIcon(batIcon)
			}
		}
	})
}

// menu is the MenuBar board: one line per stack, running first, with how
// it is under the name; then Open doze, Settings…, Quit. A line opens its
// stack in the window; a stopped stack's line opens its page, where the
// switch is. Native menus take one line of text, so the second line of the
// board follows the name after a dot.
func (d *desk) menu(m tray.Model, a view.Attention) *application.Menu {
	menu := d.app.NewMenu()
	d.needsYou(menu, a)
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

// needsYou is the MenuBarNeeds board's section above the stacks: up to
// three red items, each a submenu with its first fix and Show, then one
// line for the things worth doing, which opens the window's bell. With
// nothing red and nothing worth doing there is no section.
func (d *desk) needsYou(menu *application.Menu, a view.Attention) {
	shown := 0
	for _, it := range a.Items {
		if !it.Red || it.Fixed {
			continue
		}
		if shown == 0 {
			menu.Add(fmt.Sprintf("Needs you · %d", a.Red)).SetEnabled(false)
		}
		if shown == 3 {
			break
		}
		shown++
		title := it.Title
		if it.Stack != "" {
			title += "   ·   " + it.Stack
		}
		sub := menu.AddSubmenu(title)
		if it.Detail != "" {
			sub.Add(it.Detail).SetEnabled(false)
			sub.AddSeparator()
		}
		item := it
		for _, f := range it.Fixes {
			if f.Kind == "show" {
				continue
			}
			fix := f
			sub.Add(fix.Label).OnClick(func(*application.Context) {
				if err := d.svc.FixItem(item, fix); err != nil {
					d.showItem(item)
				}
			})
			break
		}
		sub.Add("Show in doze").OnClick(func(*application.Context) { d.showItem(item) })
	}
	if a.Yellow > 0 {
		label := fmt.Sprintf("%d thing%s worth doing", a.Yellow, map[bool]string{true: "", false: "s"}[a.Yellow == 1])
		if shown > 0 {
			label = "and " + label
		}
		menu.Add(label + "   ·   Open").OnClick(func(*application.Context) {
			d.svc.OpenBell()
			d.open("")
		})
	}
	if shown > 0 || a.Yellow > 0 {
		menu.AddSeparator()
	}
}

// showItem opens the window on the page that explains an item.
func (d *desk) showItem(it view.Item) {
	dir := ""
	for _, st := range d.hub.Stacks() {
		if st.Key == it.Key {
			dir = st.Dir
		}
	}
	if dir == "" {
		d.svc.OpenBell()
		d.open("")
		return
	}
	if it.Instance != "" {
		d.svc.OpenAt(dir, it.Instance)
		d.open("")
		return
	}
	d.open(dir)
}

// attnSig is what of the list the menu shows, so a change to it rebuilds the menu.
func attnSig(a view.Attention) string {
	s := fmt.Sprint(a.Red, a.Yellow)
	for _, it := range a.Items {
		if it.Red && !it.Fixed {
			// Not the detail: its countdown ("next in 4s") would rebuild
			// the menu every second, maybe while it is open.
			s += "|" + it.ID + it.Title + it.Print
		}
	}
	return s
}
