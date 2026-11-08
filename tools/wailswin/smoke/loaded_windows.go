//go:build wails && windows

package main

import "github.com/wailsapp/wails/v3/pkg/events"

// pageLoaded is the window event for a finished page load.
var pageLoaded = events.Windows.WebViewNavigationCompleted
