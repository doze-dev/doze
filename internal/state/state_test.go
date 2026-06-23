package state

import (
	"path/filepath"
	"testing"

	"github.com/nerdmenot/doze/internal/engine"
)

func obj(kind, name, hash string) engine.Object {
	return engine.Object{Kind: kind, Name: name, Hash: hash}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".doze", "state.json")
	s := New()
	s.Set("app", []engine.Object{obj("role", "a", "1"), obj("database", "app", "2")})
	s.Outputs = map[string]string{"url": "postgres://x"}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Objects("app")) != 2 || got.Objects("app")[0].Name != "a" {
		t.Errorf("round-trip objects = %+v", got.Objects("app"))
	}
	if got.Outputs["url"] != "postgres://x" {
		t.Errorf("round-trip outputs = %+v", got.Outputs)
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Instances) != 0 {
		t.Errorf("missing state should be empty, got %+v", s.Instances)
	}
}

func TestDiffCreateUpdateDelete(t *testing.T) {
	prior := []engine.Object{obj("role", "a", "1"), obj("database", "x", "1")}
	desired := []engine.Object{obj("role", "a", "2"), obj("schema", "s", "1")}
	changes := diff(prior, desired)

	var creates, updates, deletes int
	for _, c := range changes {
		switch c.Kind {
		case Create:
			if c.Object.Name != "s" {
				t.Errorf("unexpected create %q", c.Object.Name)
			}
			creates++
		case Update:
			if c.Object.Name != "a" {
				t.Errorf("unexpected update %q", c.Object.Name)
			}
			updates++
		case Delete:
			if c.Object.Name != "x" {
				t.Errorf("unexpected delete %q", c.Object.Name)
			}
			deletes++
		}
	}
	if creates != 1 || updates != 1 || deletes != 1 {
		t.Errorf("got %d creates, %d updates, %d deletes; want 1 each", creates, updates, deletes)
	}
}

func TestRemovedIsReverseOrder(t *testing.T) {
	prior := []engine.Object{obj("role", "a", "1"), obj("database", "x", "1"), obj("schema", "s", "1")}
	desired := []engine.Object{obj("role", "a", "1")} // keep role, drop db + schema
	removed := Removed(prior, desired)
	if len(removed) != 2 {
		t.Fatalf("removed = %+v, want 2", removed)
	}
	// Reverse prior order: schema (last) before database.
	if removed[0].Name != "s" || removed[1].Name != "x" {
		t.Errorf("removed order = %v, want [s x] (reverse)", []string{removed[0].Name, removed[1].Name})
	}
}

func TestSetEmptyClearsInstance(t *testing.T) {
	s := New()
	s.Set("a", []engine.Object{obj("role", "r", "1")})
	s.Set("a", nil)
	if _, ok := s.Instances["a"]; ok {
		t.Error("Set with no objects should clear the instance")
	}
}
