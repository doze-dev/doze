// Package autostart starts the app at login, in the menu bar only: a
// LaunchAgent on macOS, an XDG autostart entry on Linux. It writes and
// removes one file in the person's own folders and needs no privilege.
package autostart

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Label names the login item on every platform.
const Label = "dev.doze.app"

// ErrUnsupported is a platform with no login items doze knows how to write.
var ErrUnsupported = errors.New("starting at login is not supported here")

// Enabled says the login item is in place.
func Enabled() bool {
	p, err := path()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Set puts the login item in place, starting exe with "open" and no folder
// (the menu bar only), or removes it.
func Set(on bool, exe string) error {
	p, err := path()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(entry(exe)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Stable is the path to write into the login item: exe itself when it is
// inside an app bundle, else the doze on the PATH when that is the same
// file, so a Homebrew upgrade (a new Cellar folder) does not leave the
// item pointing at a version that is gone.
func Stable(exe string) string {
	if strings.Contains(exe, ".app/Contents/MacOS/") {
		return exe
	}
	onPath, err := exec.LookPath("doze")
	if err != nil {
		return exe
	}
	a, errA := filepath.EvalSymlinks(onPath)
	b, errB := filepath.EvalSymlinks(exe)
	if errA != nil || errB != nil || a != b {
		return exe
	}
	if abs, err := filepath.Abs(onPath); err == nil {
		return abs
	}
	return exe
}
