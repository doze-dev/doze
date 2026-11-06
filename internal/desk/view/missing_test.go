package view

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingFiles(t *testing.T) {
	dir := t.TempDir()
	if got := missingFiles("~/code/shop"); got != "" {
		t.Errorf("a path that is not absolute is not judged, got %q", got)
	}
	if got := missingFiles(dir); got != "doze.hcl is gone" {
		t.Errorf("no doze.hcl: got %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "doze.hcl"), []byte("stack = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := missingFiles(dir); got != "" {
		t.Errorf("all there: got %q", got)
	}
	if got := missingFiles(filepath.Join(dir, "nope")); got != "the folder is gone" {
		t.Errorf("no folder: got %q", got)
	}
}
