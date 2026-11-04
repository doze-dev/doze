// Package devserve runs the app's page and API over plain HTTP, with no
// Wails: `doze open --serve`. It is what Playwright tests against, what the
// Vite dev server proxies to, and a way to see the app in a browser on a
// machine with no desktop. With a fixture it needs no daemon at all.
package devserve

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/control/fixture"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/serve"
	"github.com/doze-dev/doze/internal/desk/web"
)

// Options says what to serve and where.
type Options struct {
	// Addr is the address to listen on; "127.0.0.1:0" picks a port.
	Addr string
	// Fixture names a scripted stack (shop, crash, modules) to serve instead
	// of the real daemons; "" serves the real ones.
	Fixture string
	// Out is where the URL is printed once listening.
	Out *os.File
}

// Fixtures are the scripted stacks a fixture run can serve.
var Fixtures = map[string]func(time.Time) *fakedaemon.Fake{
	"shop":    fixture.Shop,
	"crash":   fixture.Crash,
	"modules": fixture.Modules,
}

// Run serves until ctx ends.
func Run(ctx context.Context, o Options) error {
	h, cleanup, err := hubFor(o.Fixture)
	if err != nil {
		return err
	}
	defer cleanup()
	svc := serve.New(h, nil)
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

// hubFor is a hub over the real doze home, or over a fixture laid out in a
// temporary home with one control socket the hub finds by name.
func hubFor(name string) (*hub.Hub, func(), error) {
	if name == "" {
		return hub.New(hub.Options{}), func() {}, nil
	}
	make, ok := Fixtures[name]
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
	f := make(time.Now())
	h := hub.New(hub.Options{Home: home, Dial: func(string) control.Daemon { return f }})
	return h, func() { _ = os.RemoveAll(home) }, nil
}
