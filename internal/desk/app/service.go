//go:build purego

package app

import (
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/doze-dev/doze/internal/desk/serve"
	"github.com/doze-dev/doze/internal/desk/view"
)

// API is the service the page calls, bound to Wails. Its TypeScript side is
// generated into frontend/src/bindings (make bindings); a test keeps the two
// in step. Every method is a thin call into serve.Service, which is also
// what the browser reaches over HTTP, so the page behaves the same in both.
type API struct {
	s       *serve.Service
	painted func(ms int)
}

// Stacks is every stack, for the tab strip.
func (a *API) Stacks() []serve.StackSummary { return a.s.Stacks() }

// Pending is where the page should start: the stack doze open asked for.
func (a *API) Pending() serve.Nav { return a.s.Pending() }

// Version is the doze this app is, for the sidebar's foot.
func (a *API) Version() string { return a.s.Version() }

// Home is a stack's home page.
func (a *API) Home(key string) view.Home { return a.s.Home(key) }

// Instance is an instance's page.
func (a *API) Instance(key, name string) view.Instance { return a.s.Instance(key, name) }

// Logs is a window of an instance's lines, or of every instance's.
func (a *API) Logs(key string, q view.LogsQuery) view.Logs { return a.s.Logs(key, q) }

// Focus says what the page shows; the hub and the pushes follow it.
func (a *API) Focus(f serve.Focus) { a.s.SetFocus(f) }

// Blur says the page is hidden.
func (a *API) Blur() { a.s.Blur() }

// Do runs a verb on a stack.
func (a *API) Do(key, verb, target string) error { return a.s.Do(key, verb, target) }

// Painted reports the page's first paint, for the desk bench.
func (a *API) Painted(ms int) {
	if a.painted != nil {
		a.painted(ms)
	}
}

// Log is the page's line to the app's stderr: what it chose, what failed.
// The web view's own console is not visible from outside the window.
func (a *API) Log(line string) {
	fmt.Fprintln(os.Stderr, "page: "+line)
}

func newService(s *serve.Service, painted func(int)) application.Service {
	return application.NewService(&API{s: s, painted: painted})
}
