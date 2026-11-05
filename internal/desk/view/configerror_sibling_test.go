package view

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// An error in a sibling *.doze.hcl quotes that file, not doze.hcl.
func TestConfigErrorInASiblingFile(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "doze.hcl")
	extra := filepath.Join(dir, "extra.doze.hcl")
	if err := os.WriteFile(main, []byte("stack = \"shop\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extra, []byte("app \"worker\" {\n  run = \"y\"\n  port = \"eighty\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := "invalid config —\n\nError: Unsuitable value type\n\n  on extra.doze.hcl line 3, in app \"worker\":\n   3:   port = \"eighty\"\n\nUnsuitable value: a number is required"
	now := time.Now()
	ce := ConfigErrorOf(main, errors.New(msg), now, now)
	if ce.File != extra || ce.Line != 3 {
		t.Fatalf("file %q line %d, want %q line 3", ce.File, ce.Line, extra)
	}
	var bad string
	for _, l := range ce.Excerpt {
		if l.Bad {
			bad = l.Text
		}
	}
	if bad != `  port = "eighty"` {
		t.Fatalf("bad line %q", bad)
	}
}
