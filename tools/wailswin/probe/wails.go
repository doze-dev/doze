//go:build windows && wails

package main

// Linked, never used: what a CLI sharing a binary with a Wails app carries.
import "github.com/wailsapp/wails/v3/pkg/application"

var _ = application.New
