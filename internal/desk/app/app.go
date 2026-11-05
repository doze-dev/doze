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
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/doze-dev/doze/internal/adminprompt"
	"github.com/doze-dev/doze/internal/desk/devserve"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/link"
	"github.com/doze-dev/doze/internal/desk/serve"
	"github.com/doze-dev/doze/internal/desk/web"
)

// Options says what the app should open and how it should report.
type Options struct {
	// Open is the project directory to show first; empty means the tray only.
	Open string
	// Link is a doze:// link to open at, from `doze open doze://shop/db`.
	Link string
	// Show is a service in that project to open the window at, as a
	// notification's Show does; empty opens the stack.
	Show string
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
	// Setup sets a folder with no doze.hcl up (init, add, empty) and starts
	// its daemon; the CLI provides it.
	Setup func(dir, action, arg string) error
}

// Run starts the app and blocks until it quits. A second launch hands its
// arguments to the running one and exits; Wails does that under
// SingleInstance, so the CLI never has to.
func Run(o Options) error {
	began := time.Now()
	h, cleanup, err := devserve.HubFor(o.Fixture)
	if err != nil {
		return err
	}
	defer cleanup()
	d := &desk{o: o, hub: h}
	d.svc = serve.New(d.hub, nil)
	d.svc.SetVersion(o.Version)
	d.svc.SetSetup(o.Setup)
	if adminprompt.Supported() {
		d.svc.SetRepair(adminprompt.Run)
	}
	note, noteSvc := newNotifier()
	d.svc.SetNotifier(note.send)
	services := []application.Service{newService(d.svc, func(ms int) {
		d.measure("home painted %dms after the page loaded", ms)
	})}
	services = append(services, noteSvc...)
	// A menu bar app: no Dock icon, and closing the window does not quit.
	// Asked for a project, it starts as a regular app so macOS lets it come
	// forward at launch; the window's close puts it back in the menu bar.
	policy := application.ActivationPolicyAccessory
	if o.Open != "" || o.Window {
		policy = application.ActivationPolicyRegular
	}
	d.app = application.New(application.Options{
		Name:        "doze",
		Description: "doze runs your stack, and understands it",
		Assets:      application.AssetOptions{Handler: web.Handler(), DisableLogging: true},
		Services:    services,
		Mac: application.MacOptions{
			ActivationPolicy: policy,
		},
		// Closing the window leaves the menu bar item; only Quit quits.
		Linux:   application.LinuxOptions{DisableQuitOnLastWindowClosed: true},
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		SingleInstance: &application.SingleInstanceOptions{
			// DOZE_DESK_INSTANCE lets the desk bench and tests run beside a
			// real app without being handed off to it.
			UniqueID: "dev.doze.app" + os.Getenv("DOZE_DESK_INSTANCE"),
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				if l := linkFrom(data.Args); l != "" {
					d.follow(l)
					return
				}
				dir := dirFrom(data)
				d.open(dir)
				if show := flagFrom(data.Args, "--show"); show != "" {
					d.svc.OpenAt(dir, show)
				}
			},
		},
	})
	// A doze:// link clicked anywhere on the system (macOS hands it here).
	d.app.Event.OnApplicationEvent(events.Common.ApplicationLaunchedWithUrl, func(e *application.ApplicationEvent) {
		u := e.Context().URL()
		time.AfterFunc(300*time.Millisecond, func() { d.follow(u) })
	})
	d.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		d.measure("tray ready %s after start", time.Since(began).Round(time.Millisecond))
		d.startTray()
		note.start(d)
		d.hub.OnChange(d.refreshTray)
		d.svc.SetEmitter(func(name string, data any) {
			d.app.Event.Emit(name, data)
		})
		go d.hub.Run(d.app.Context())
		go d.svc.Run(d.app.Context())
		if o.Link != "" {
			// The hub has to know the stacks before a name can be found.
			time.AfterFunc(300*time.Millisecond, func() { d.follow(o.Link) })
		}
		if o.Open != "" || o.Window {
			d.open(o.Open)
			if o.Show != "" {
				d.svc.OpenAt(o.Open, o.Show)
			}
			if o.CloseAfter > 0 {
				time.AfterFunc(o.CloseAfter, func() {
					application.InvokeSync(func() {
						if d.win != nil {
							d.win.Close()
						}
					})
				})
			}
		}
		if o.QuitAfter > 0 {
			time.AfterFunc(o.QuitAfter, d.app.Quit)
		}
	})
	return d.app.Run()
}

// desk is the running app.
type desk struct {
	o   Options
	app *application.App
	hub *hub.Hub
	svc *serve.Service
	bar
	window
}

// flagFrom is a flag's value in a second launch's arguments: "--show db" or
// "--show=db".
func flagFrom(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v
		}
	}
	return ""
}

// dirFrom is the project folder a second launch asked for: the argument
// after open, resolved against the folder it ran in, else the project
// that folder is inside, found the way the CLI finds doze.hcl: upward.
func dirFrom(data application.SecondInstanceData) string {
	for i, a := range data.Args {
		if a != "open" {
			continue
		}
		skip := false
		for _, arg := range data.Args[i+1:] {
			if skip {
				skip = false
				continue
			}
			if arg == "--show" {
				skip = true // its value is a service, not a folder
				continue
			}
			if strings.HasPrefix(arg, "-") {
				continue
			}
			if filepath.IsAbs(arg) {
				return arg
			}
			return filepath.Join(data.WorkingDir, arg)
		}
		break
	}
	return projectDir(data.WorkingDir)
}

// projectDir is the nearest folder at or above dir with a doze.hcl, or dir
// itself when none has one.
func projectDir(dir string) string {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "doze.hcl")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}

func (d *desk) measure(format string, args ...any) {
	if d.o.Measure {
		fmt.Fprintf(os.Stderr, "desk: "+format+"\n", args...)
	}
}

// follow opens the window where a doze:// link points. A stack named by its
// name is found among the stacks the hub knows; a link that points nowhere
// is said, and the window opens on the Library.
func (d *desk) follow(raw string) {
	l, err := link.Parse(raw)
	if err != nil {
		d.svc.Say(raw+": "+err.Error(), true)
		d.open("")
		return
	}
	switch l.Page {
	case "settings":
		d.svc.OpenSettings()
		d.open("")
		return
	case "library":
		d.open("")
		return
	}
	dir := l.Dir
	if dir == "" {
		for _, st := range d.hub.Stacks() {
			if strings.EqualFold(st.Name, l.Stack) {
				dir = st.Dir
				break
			}
		}
	}
	if dir == "" {
		d.svc.Say("no stack called "+l.Stack+" here; the Library lists the ones doze knows", true)
		d.open("")
		return
	}
	d.open(dir)
	if l.Show != "" {
		d.svc.OpenAt(dir, l.Show)
	}
}

// linkFrom is a doze:// link among a launch's arguments, or "".
func linkFrom(args []string) string {
	for _, a := range args {
		if link.Is(a) {
			return a
		}
	}
	return ""
}
