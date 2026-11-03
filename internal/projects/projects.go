// Package projects remembers each stack between runs: where its config is,
// when it last went up or down, and which of its services were started, so
// `doze up` can offer the same set again and the app can list stopped stacks.
//
// Each project keeps one small file, project.json, in its own state folder
// (<home>/projects/<slug>/). A slug can't be turned back into a path, so the
// file says where the config lives, and listing is a scan of those files.
package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// FileName is the record's name inside a project's state folder.
const FileName = "project.json"

// schema is the record's version. A reader keeps fields it doesn't know.
const schema = 1

// Project is what is remembered about one stack.
type Project struct {
	Schema int    `json:"schema"`
	Config string `json:"config"`         // absolute path of its doze.hcl
	Name   string `json:"name,omitempty"` // the stack's name
	// LastUp and LastDown are when doze up and doze down last ran for it.
	LastUp   time.Time `json:"last_up,omitzero"`
	LastDown time.Time `json:"last_down,omitzero"`
	// LastSet is what was started last time, as the names asked for (their
	// dependencies follow). Empty means everything.
	LastSet []string `json:"last_set,omitempty"`

	dir   string                     // the state folder it was read from
	extra map[string]json.RawMessage // fields a newer doze wrote, kept on save
}

// Dir is the project's state folder.
func (p Project) Dir() string { return p.dir }

// Load reads the record in dir. A project never recorded gives ok false.
func Load(dir string) (p Project, ok bool, err error) {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return Project{Schema: schema, dir: dir}, false, nil
	}
	if err != nil {
		return Project{}, false, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return Project{}, false, fmt.Errorf("%s: %w", filepath.Join(dir, FileName), err)
	}
	var all map[string]json.RawMessage
	_ = json.Unmarshal(b, &all)
	for _, known := range []string{"schema", "config", "name", "last_up", "last_down", "last_set"} {
		delete(all, known)
	}
	if len(all) > 0 {
		p.extra = all
	}
	p.dir = dir
	return p, true, nil
}

// Save writes the record atomically, so a reader never sees half of it.
func Save(dir string, p Project) error {
	p.Schema = schema
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if len(p.extra) > 0 {
		var m map[string]json.RawMessage
		_ = json.Unmarshal(b, &m)
		for k, v := range p.extra {
			if _, ours := m[k]; !ours {
				m[k] = v
			}
		}
		if b, err = json.Marshal(m); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, FileName+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, FileName))
}

// Update loads the record in dir, applies fn, and saves it.
func Update(dir string, fn func(*Project)) error {
	p, _, err := Load(dir)
	if err != nil {
		return err
	}
	fn(&p)
	return Save(dir, p)
}

// List is every recorded project under home, most recently used first. A
// record that can't be read is skipped: one bad file must not hide the rest.
func List(home string) ([]Project, error) {
	dirs, err := filepath.Glob(filepath.Join(home, "projects", "*", FileName))
	if err != nil {
		return nil, err
	}
	var out []Project
	for _, f := range dirs {
		if p, ok, err := Load(filepath.Dir(f)); err == nil && ok {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].lastUsed().After(out[j].lastUsed()) })
	return out, nil
}

func (p Project) lastUsed() time.Time {
	if p.LastDown.After(p.LastUp) {
		return p.LastDown
	}
	return p.LastUp
}
