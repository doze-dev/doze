// Package web serves the app's page. The React frontend is built by bun into
// frontend/dist and embedded here, so the binary carries it; the same handler
// serves the web view in the app and the browser under `doze open --serve`.
// Nothing here knows a stack: views come from internal/desk/view through the
// service and the push channel.
package web

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/doze-dev/doze/internal/desk/frontend"
)

// NotBuilt is the page served when the frontend was not built into this
// binary: a dev checkout before `make frontend`.
const NotBuilt = `<!doctype html><meta charset="utf-8"><title>doze</title>
<body style="font: 14px -apple-system, system-ui, sans-serif; padding: 2em">
<p><b>doze</b>: this binary was built without the app's page.</p>
<p>Run <code>make frontend</code>, then build again.</p>`

// Dist and Built are the page's files and whether they are in this binary.
func Dist() fs.FS { return frontend.Dist() }
func Built() bool { return frontend.Built() }

// Handler serves the page and its assets. Anything that is not a file is the
// page: the frontend routes within itself.
func Handler() http.Handler {
	dist := Dist()
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Built() {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(NotBuilt))
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if _, err := fs.Stat(dist, p); err == nil {
				if strings.HasPrefix(p, "assets/") {
					// Vite hashes asset names, so they can be cached for good.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/"
		w.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(w, r)
	})
}
