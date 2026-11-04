//go:build !darwin && purego

package app

// activate is macOS-only: elsewhere a shown window is in front already.
func activate(bool) {}

func bringForward() {}
