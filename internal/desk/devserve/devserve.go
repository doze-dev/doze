// Package devserve runs the app's page and API over plain HTTP, with no
// Wails: `doze open --serve`. It is what Playwright tests against, what the
// Vite dev server proxies to, and a way to see the app in a browser on a
// machine with no desktop. With a fixture it needs no daemon at all.
package devserve

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/control/fixture"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/serve"
	"github.com/doze-dev/doze/internal/desk/web"
	"github.com/doze-dev/doze/internal/display"
)

// Options says what to serve and where.
type Options struct {
	// Addr is the address to listen on; "127.0.0.1:0" picks a port.
	Addr string
	// Fixture names a scripted stack (shop, crash, modules, larder) to serve instead
	// of the real daemons; "" serves the real ones.
	Fixture string
	// Out is where the URL is printed once listening.
	Out *os.File
	// Version is the doze this is.
	Version string
	// Open is a folder to open first, as doze open dir does; with a fixture
	// it is for a folder with no doze.hcl, whose setup writes the starter
	// file only.
	Open string
	// Setup sets a folder up; nil with a fixture writes the starter file.
	Setup func(dir, action, arg string) error
}

// Fixtures are the scripted stacks a fixture run can serve.
var Fixtures = map[string]func(time.Time) *fakedaemon.Fake{
	"shop":      fixture.Shop,
	"crash":     fixture.Crash,
	"modules":   fixture.Modules,
	"long":      fixture.Long,
	"badconfig": fixture.Shop,
	"portclash": fixture.Crash,
	"changed":   fixture.Shop,
	"stale":     fixture.Shop,
	"larder":    fixture.Larder,
}

// Run serves until ctx ends.
func Run(ctx context.Context, o Options) error {
	h, cleanup, err := HubFor(o.Fixture)
	if err != nil {
		return err
	}
	defer cleanup()
	svc := serve.New(h, nil)
	// Stack actions are acted out, never run: a fixture has no real stacks,
	// and a test must not leave a daemon behind. Starting one fails, after
	// a moment, saying so, which is how the page's failure is tested.
	svc.FakeStack = func(st hub.StackView, action string) ([]byte, error) {
		time.Sleep(1500 * time.Millisecond)
		// A doze.hcl that does not load fails as doze up would, so the page's
		// reading of a config mistake can be seen.
		if _, err := os.Stat(filepath.Join(st.Dir, "doze.hcl")); err == nil && action != "down" {
			if _, err := config.LoadShape(filepath.Join(st.Dir, "doze.hcl"), nil); err != nil {
				return []byte("Error: " + err.Error()), errors.New("exit status 1")
			}
		}
		return []byte("doze: this is a fixture; " + st.Name + " cannot really " + map[string]string{"up-last": "start", "up-all": "start", "up": "start", "down": "stop", "restart": "restart"}[action] + " here"), errors.New("exit status 1")
	}
	svc.SetVersion(o.Version)
	setup := o.Setup
	if setup == nil && o.Fixture != "" {
		setup = starterOnly
	}
	svc.SetSetup(setup)
	if o.Open != "" {
		svc.Open(o.Open)
	}
	api := serve.Handler(svc) // sets the service's emitter to its event stream
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/events", api)
	mux.Handle("/", web.Handler())

	ln, err := net.Listen("tcp", o.Addr)
	if err != nil {
		return err
	}
	if o.Out != nil {
		fmt.Fprintf(o.Out, "doze app at http://%s\n", ln.Addr())
	}
	go h.Run(ctx)
	go svc.Run(ctx)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// HubFor is a hub over the real doze home, or over a fixture laid out in a
// temporary home with one control socket the hub finds by name.
func HubFor(name string) (*hub.Hub, func(), error) {
	if name == "" {
		return hub.New(hub.Options{}), func() {}, nil
	}
	build, ok := Fixtures[name]
	if !ok {
		var names []string
		for n := range Fixtures {
			names = append(names, n)
		}
		return nil, nil, fmt.Errorf("no fixture %q (have %s)", name, strings.Join(names, ", "))
	}
	home, err := os.MkdirTemp("", "doze-fixture-")
	if err != nil {
		return nil, nil, err
	}
	run := filepath.Join(home, "projects", name+"-1", "run")
	if err := os.MkdirAll(run, 0o755); err != nil {
		return nil, nil, err
	}
	sock := filepath.Join(run, "doze.admin.sock")
	if err := os.WriteFile(sock, nil, 0o644); err != nil {
		return nil, nil, err
	}
	if err := seedStopped(home, time.Now()); err != nil {
		return nil, nil, err
	}
	f := build(time.Now())
	if name == "portclash" {
		// The crash fixture's api, failing because port 7799 is taken; the
		// spec holds that port, so there is a real listener to name.
		f.Mutate(func(s *control.Snapshot) {
			for i := range s.Instances {
				if s.Instances[i].Name == "api" {
					s.Instances[i].LastError = "listen tcp 127.0.0.1:7799: bind: address already in use"
				}
			}
		})
	}
	if name == "badconfig" {
		// The shop, running, with a doze.hcl saved broken under it.
		dir := filepath.Join(home, "work", "shop")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, "doze.hcl"), []byte(brokenConfig), 0o644); err != nil {
			return nil, nil, err
		}
		f.Snap.Dir = dir
	}
	if name == "larder" {
		// The Larder example, up from its own doze.hcl: the map's calls
		// and the grid's tooltips are read from it.
		dir := filepath.Join(home, "work", "larder")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, "doze.hcl"), []byte(fixture.LarderHCL), 0o644); err != nil {
			return nil, nil, err
		}
		f.Snap.Dir, f.Snap.Loaded = dir, time.Now()
	}
	if name == "changed" {
		// The shop, running a doze.hcl it read ten minutes ago; the file was
		// saved since: the "doze.hcl changed" banner.
		dir := filepath.Join(home, "work", "shop")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, "doze.hcl"), []byte(changedConfig), 0o644); err != nil {
			return nil, nil, err
		}
		f.Snap.Dir, f.Snap.Loaded = dir, time.Now().Add(-10*time.Minute)
	}
	// The meters every second after the script, as a daemon sends them, so
	// a metered page is never told its picture is old.
	stop := make(chan struct{})
	if last := lastStats(f.Frames); last != nil {
		go func() {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					f.Push(*last)
				}
			}
		}()
	}
	var d control.Daemon = f
	if name == "stale" {
		// A daemon that answered once and then stopped: the page keeps the
		// last word heard and says the daemon is not answering.
		d = &quiet{Fake: f}
	}
	h := hub.New(hub.Options{Home: home, Dial: func(string) control.Daemon { return d }})
	return h, func() {
		close(stop)
		_ = os.RemoveAll(home)
	}, nil
}

// lastStats is the script's last meters frame, or nil when it has none.
func lastStats(frames []control.Frame) *control.Frame {
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i].Topic == control.TopicStats {
			fr := frames[i]
			return &fr
		}
	}
	return nil
}

// starterOnly is the fixture run's setup: it writes the starter file and
// can do nothing else, since there is no CLI behind it.
func starterOnly(dir, action, _ string) error {
	if action != "empty" {
		return errors.New("only the starter file can be written here")
	}
	return os.WriteFile(filepath.Join(dir, "doze.hcl"), []byte(display.StarterFile), 0o644)
}
