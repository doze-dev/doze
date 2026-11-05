package view

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigErrorFindsTheLineAndTheWords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doze.hcl")
	src := "stack = \"bad\"\n\napp \"web\" {\n  run  = \"pnpm dev\"\n  port = \"3000x\"\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	err := errors.New("invalid config —\n\nError: Unsuitable value type\n\n  on " + path + " line 5, in app \"web\":\n   5:   port = \"3000x\"\n\nUnsuitable value: a number is required")
	now := time.Date(2026, 11, 6, 1, 0, 0, 0, time.UTC)
	ce := ConfigErrorOf(path, err, now.Add(-20*time.Second), now)
	if ce.Text != "doze.hcl changed 20s ago and does not load" {
		t.Errorf("text = %q", ce.Text)
	}
	if ce.Summary != "Unsuitable value type" || ce.Detail != "Unsuitable value: a number is required" || ce.Line != 5 {
		t.Errorf("summary %q detail %q line %d", ce.Summary, ce.Detail, ce.Line)
	}
	if len(ce.Excerpt) != 6 || ce.Excerpt[0].N != 2 || !ce.Excerpt[3].Bad || ce.Excerpt[3].Text != "  port = \"3000x\"" {
		t.Errorf("excerpt = %+v", ce.Excerpt)
	}
	if ConfigErrorOf(path, nil, now, now) != nil {
		t.Error("no error, no page")
	}
}
