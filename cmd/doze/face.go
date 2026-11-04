package main

import (
	"runtime"
	"strings"
)

// Face is which of the binary's three faces a run of it is: the CLI, the
// daemon, or the app. The same file is all three; what it was asked to do
// and where it is running decide.
type Face int

const (
	// faceCLI runs a command and exits.
	faceCLI Face = iota
	// faceDaemon is `doze __daemon`, the per-stack daemon.
	faceDaemon
	// faceApp is the desktop app: `doze open`, or the bundle launched.
	faceApp
	// faceStatus is bare `doze` with no display: the status table.
	faceStatus
)

// faceInput is everything the choice reads. It is a value, so the choice is
// a pure function with a table test, and nothing in it touches the OS.
type faceInput struct {
	// Args are the arguments after the program name.
	Args []string
	// Env is the process environment.
	Env map[string]string
	// Exe is the resolved path of the running binary.
	Exe string
	// StdinTTY and StdoutTTY say whether a person is at a terminal.
	StdinTTY, StdoutTTY bool
	// GOOS overrides runtime.GOOS in tests; empty means the real one.
	GOOS string
	// BareOpensApp is whether bare `doze` opens the app. Off until the app
	// matches the TUI (plan P4); until then bare `doze` is the dash or the
	// status table, as before.
	BareOpensApp bool
}

// face chooses. The rules, in order:
//   - `__daemon` is the daemon;
//   - any other subcommand is the CLI;
//   - `open`, or the binary launched from inside an app bundle with no
//     arguments, is the app when there is a display, else the status table;
//   - bare `doze` is the app only once BareOpensApp is set and a display
//     exists; otherwise the CLI decides (dash or status table) as it did.
func face(in faceInput) Face {
	if len(in.Args) > 0 {
		switch in.Args[0] {
		case "__daemon":
			return faceDaemon
		case "open":
			if hasDisplay(in) {
				return faceApp
			}
			return faceStatus
		}
		return faceCLI
	}
	if inBundle(in.Exe) {
		if hasDisplay(in) {
			return faceApp
		}
		return faceStatus
	}
	if in.BareOpensApp && hasDisplay(in) {
		return faceApp
	}
	return faceCLI
}

// hasDisplay reports whether a window could be shown. DOZE_NO_GUI turns it
// off anywhere; on macOS an SSH session is the only way to be without a
// screen; on Linux a display is a DISPLAY or WAYLAND_DISPLAY variable.
func hasDisplay(in faceInput) bool {
	if in.Env["DOZE_NO_GUI"] != "" {
		return false
	}
	goos := in.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	switch goos {
	case "darwin":
		return in.Env["SSH_CONNECTION"] == "" && in.Env["SSH_TTY"] == ""
	case "linux":
		return in.Env["DISPLAY"] != "" || in.Env["WAYLAND_DISPLAY"] != ""
	}
	return false
}

// inBundle reports whether exe sits inside a macOS app bundle, which is how
// the Dock and Launchpad start the app: no arguments, no terminal.
func inBundle(exe string) bool {
	return strings.Contains(exe, ".app/Contents/MacOS/")
}
