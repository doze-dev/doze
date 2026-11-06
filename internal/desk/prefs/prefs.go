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
	"time"
)

// Prefs is what is remembered.
type Prefs struct {
	// Window is the frame the window had when it was last closed; zero
	// means it has never been placed.
	Window Frame `json:"window"`
	// LastDir is the project the window last showed, for the next launch
	// with nothing asked for.
	LastDir string `json:"lastDir,omitempty"`
	// Hints is the Library's help strip: whether it was dismissed, and
	// whether a connection to a service was ever seen, which is its third
	// step.
	Hints Hints `json:"hints,omitzero"`
	// Theme is the colour theme chosen in Settings (violet, teal, …);
	// empty is violet. Mode is dark, light, or empty to follow the system.
	Theme string `json:"theme,omitempty"`
	Mode  string `json:"mode,omitempty"`
	// Views is each stack's overview view (tiles, list, map), by key.
	Views map[string]string `json:"views,omitempty"`
	// Recent is the services whose pages were opened last, newest first,
	// for ⌘K's Recent section.
	Recent []Recent `json:"recent,omitempty"`
	// NotNow is the Needs you items put aside, by ID, with what each said
	// then: one that says something new shows again.
	NotNow map[string]string `json:"notNow,omitempty"`
	// WindowClosed is when the window last closed, for "while the window
	// was closed".
	WindowClosed time.Time `json:"windowClosed,omitzero"`
}

// Recent is one service opened: the stack's key and the service's name.
type Recent struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// recentKept is how many recent services are remembered.
const recentKept = 8

// Opened puts a service at the front of Recent, once.
func (p *Prefs) Opened(key, name string) {
	out := []Recent{{Key: key, Name: name}}
	for _, r := range p.Recent {
		if r.Key != key || r.Name != name {
			out = append(out, r)
		}
	}
	if len(out) > recentKept {
		out = out[:recentKept]
	}
	p.Recent = out
}

// Hints is the help strip's state.
type Hints struct {
	Dismissed bool `json:"dismissed,omitempty"`
	Connected bool `json:"connected,omitempty"`
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
