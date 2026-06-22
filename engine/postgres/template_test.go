package postgres

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCloneTemplate(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "tmpl")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "PG_VERSION"), []byte("14\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "x"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(root, "dest")
	if err := (Driver{}).CloneTemplate(context.Background(), src, dst); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "PG_VERSION")); string(b) != "14\n" {
		t.Errorf("PG_VERSION = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "sub", "x")); string(b) != "hello" {
		t.Errorf("sub/x = %q", b)
	}
	if !provisioned(dst) {
		t.Error("clone should look provisioned (PG_VERSION present)")
	}
}
