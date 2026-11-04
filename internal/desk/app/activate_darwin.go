//go:build darwin && purego

package app

import (
	"github.com/ebitengine/purego/objc"
)

// macOS activation policies (NSApplicationActivationPolicy).
const (
	policyRegular   = 0 // a Dock icon and a menu bar: an app with a window
	policyAccessory = 1 // no Dock icon: a menu bar item
)

// activate is how a menu bar app gets a window in front. While the window is
// open the app is a regular one (Dock icon, ⌘Tab); when it closes the app goes
// back to being a menu bar item. The same shape as OrbStack's. Must run on
// the main thread.
func activate(window bool) {
	app := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
	if !window {
		app.Send(objc.RegisterName("setActivationPolicy:"), policyAccessory)
		return
	}
	app.Send(objc.RegisterName("setActivationPolicy:"), policyRegular)
}

// bringForward activates the app. A policy change takes effect on the next
// turn of the run loop, so this is called a moment after activate(true).
// Since macOS 14 activation is cooperative: ask through NSRunningApplication
// (all windows, ignoring other apps) as well as the older call.
func bringForward() {
	const ignoringOtherApps, allWindows = 1 << 1, 1 << 0
	self := objc.ID(objc.GetClass("NSRunningApplication")).Send(objc.RegisterName("currentApplication"))
	self.Send(objc.RegisterName("activateWithOptions:"), ignoringOtherApps|allWindows)
	app := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
	app.Send(objc.RegisterName("activateIgnoringOtherApps:"), true)
}
