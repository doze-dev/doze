// Package prefs remembers the app's own small choices between launches:
// where its window was, which project it last showed. They live in
// $DOZE_HOME/desk/prefs.json, written whole and atomically, as the
// projects record is. Named prefs because internal/state is taken.
package prefs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Prefs is what is remembered.
type Prefs struct {
	// Window is the frame the window had when it was last closed; zero
	// means it has never been placed.
	Window Frame `json:"window"`
	// LastDir is the project the window last showed, for the next launch
	// with nothing asked for.
	LastDir string `json:"lastDir,omitempty"`
}

// Frame is a window's place and size, in screen points.
type Frame struct {
	X, Y, W, H int
}

// Placed says the frame was ever saved.
func (f Frame) Placed() bool { return f.W > 0 && f.H > 0 }

// Path is where the file lives under doze's folder.
func Path(home string) string { return filepath.Join(home, "desk", "prefs.json") }

// Load reads the file; a missing file is empty prefs, and a broken one is
// too, since nothing in it is worth refusing to start over.
func Load(home string) Prefs {
	var p Prefs
	b, err := os.ReadFile(Path(home))
	if err != nil {
		return p
	}
	if json.Unmarshal(b, &p) != nil {
		return Prefs{}
	}
	return p
}

// Save writes the file whole: to a temporary name beside it, then renamed
// over, so a crash mid-write leaves the last one.
func Save(home string, p Prefs) error {
	path := Path(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return nil
}
