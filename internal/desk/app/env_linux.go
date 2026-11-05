//go:build linux && purego

package app

import "os"

// WebKitGTK renders through the GPU when it can. A VM with no GPU (the
// Lima desktop, a CI runner) makes it fail its EGL setup and fall back
// noisily, or worse; compositing off goes straight to the software path,
// which the window does not need to be quick. Set before GTK loads; a
// person who set either variable themselves is left alone.
func init() {
	for k, v := range map[string]string{
		"WEBKIT_DISABLE_COMPOSITING_MODE": "1",
		"GSK_RENDERER":                    "cairo",
	} {
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}
