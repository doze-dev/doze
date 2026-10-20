package dotdoze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureIgnoresEverythingInDotDoze(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".doze")
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != ignore {
		t.Fatalf(".gitignore = %q, want %q", got, ignore)
	}
}

func TestEnsureLeavesAnExistingIgnoreAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".doze")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := "*\n!notes.md\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(got) != mine {
		t.Fatalf(".gitignore = %q, want the person's %q", got, mine)
	}
}

func TestEnsureWritesNoIgnoreElsewhere(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("a .gitignore was written outside .doze: %v", err)
	}
}
