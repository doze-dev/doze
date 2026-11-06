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

// menu is the menu bar, rethought (2026-11-06): what needs you, at the top
// and only when something is red; one line per stack, each a submenu of
// what fits its state; then Open doze, Settings… and Quit. The window is
// for everything else.
func (d *desk) menu(m tray.Model, a view.Attention) *application.Menu {
	menu := d.app.NewMenu()
	d.needsYou(menu, a)
	if len(m.Lines) == 0 {
		menu.Add("No stacks yet · open a folder in doze").SetEnabled(false)
	} else {
		menu.AddSectionHeader("Stacks")
	}
	shown, more := tray.Split(m.Lines)
	for _, l := range shown {
		d.stackItem(menu, l)
	}
	if len(more) > 0 {
		// The rest, a submenu away: it scrolls on its own if it must.
		rest := menu.AddSubmenu(fmt.Sprintf("More stacks · %d", len(more)))
		for _, l := range more {
			d.stackItem(rest, l)
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

// stackItem is one stack's line and its submenu of what fits its state.
func (d *desk) stackItem(menu *application.Menu, l tray.Line) {
	mark := "○  "
	if l.Running {
		mark = "●  "
	}
	if l.Trouble {
		mark = "⚠︎  "
	}
	title := mark + l.Name
	if l.Sub != "" {
		title += "   ·   " + l.Sub
	}
	sub := menu.AddSubmenu(title)
	line := l
	sub.Add("Open in doze").OnClick(func(*application.Context) { d.open(line.Dir) })
	if l.Running {
		if url := d.svc.WebURL(l.Key); url != "" {
			sub.Add("Open " + url).OnClick(func(*application.Context) { _ = d.app.Browser.OpenURL(url) })
		}
		sub.AddSeparator()
		sub.Add("Sleep everything").OnClick(func(*application.Context) {
			d.report(d.svc.Do(line.Key, "sleep", ""), "sleep everything in "+line.Name)
		})
		sub.Add("Restart").OnClick(func(*application.Context) {
			d.report(d.svc.Stack(line.Key, "restart", nil), "restart "+line.Name)
		})
		sub.Add("Down").OnClick(func(*application.Context) {
			d.report(d.svc.Stack(line.Key, "down", nil), "bring "+line.Name+" down")
		})
		return
	}
	sub.AddSeparator()
	sub.Add("Up as last time").OnClick(func(*application.Context) { d.up(line, "up-last") })
	sub.Add("Up everything").OnClick(func(*application.Context) { d.up(line, "up-all") })
}

// up starts a stopped stack from the menu bar. Up looks first (D19): with
// something in the way the window opens on the stack, where the checks'
// sheet asks; otherwise it just starts.
func (d *desk) up(l tray.Line, action string) {
	if d.svc.Preflight(l.Key, action, nil).Blocked {
		d.svc.AskUp(l.Key, action)
		d.open(l.Dir)
		return
	}
	d.report(d.svc.Stack(l.Key, action, nil), "start "+l.Name)
}

// report says a menu bar action that failed: the window may be closed, so
// it is said as the app says things (a notice, and a notification when it
// is not on screen is the notifier's call).
func (d *desk) report(err error, what string) {
	if err != nil {
		d.svc.Say("could not "+what+": "+err.Error(), true)
	}
}

// needsYou renders tray.NeedsOf above the stacks: each red item a submenu
// with its detail, its fix and Show; the yellow line opens the bell.
func (d *desk) needsYou(menu *application.Menu, a view.Attention) {
	n := tray.NeedsOf(a)
	if n.Header != "" {
		menu.Add(n.Header).SetEnabled(false)
	}
	for _, l := range n.Lines {
		sub := menu.AddSubmenu(l.Title)
		if l.Detail != "" {
			sub.Add(l.Detail).SetEnabled(false)
			sub.AddSeparator()
		}
		item := l.Item
		if l.Fix != nil {
			fix := *l.Fix
			sub.Add(fix.Label).OnClick(func(*application.Context) {
				if err := d.svc.FixItem(item, fix); err != nil {
					d.showItem(item)
				}
			})
		}
		sub.Add("Show in doze").OnClick(func(*application.Context) { d.showItem(item) })
	}
	if n.Worth != "" {
		menu.Add(n.Worth).OnClick(func(*application.Context) {
			d.svc.OpenBell()
			d.open("")
		})
	}
	if !n.Empty() {
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
