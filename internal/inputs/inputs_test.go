package inputs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		pat, name string
		want      bool
	}{
		{"migrations/*.sql", "migrations/001.sql", true},
		{"migrations/*.sql", "migrations/sub/001.sql", false},
		{"migrations/*.sql", "other/001.sql", false},
		{"**/*.go", "main.go", true},
		{"**/*.go", "cmd/api/main.go", true},
		{"**/*.go", "cmd/api/main.js", false},
		{"cmd/**", "cmd/api/main.go", true},
		{"cmd/**", "cmd", true},
		{"cmd/**/main.go", "cmd/main.go", true},
		{"cmd/**/main.go", "cmd/a/b/main.go", true},
		{"go.mod", "go.mod", true},
		{"go.mod", "sub/go.mod", false},
		{"file?.txt", "file1.txt", true},
		{"file?.txt", "file10.txt", false},
	}
	for _, c := range cases {
		if got := Match(c.pat, c.name); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pat, c.name, got, c.want)
		}
	}
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// lifecycle.md measure: touch a file outside inputs, no change; change one
// inside, a different hash.
func TestHashChangesOnlyWithAnInput(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "migrations/001.sql", "create table a;")
	write(t, dir, "migrations/002.sql", "create table b;")
	write(t, dir, "README.md", "hello")
	pats := []string{"migrations/*.sql"}

	h0, n, err := Hash(dir, pats)
	if err != nil || n != 2 {
		t.Fatalf("hash = %q, %d, %v", h0, n, err)
	}
	write(t, dir, "README.md", "changed outside the inputs")
	if h, _, _ := Hash(dir, pats); h != h0 {
		t.Fatal("a file outside the inputs changed the hash")
	}
	write(t, dir, "migrations/002.sql", "create table b2;")
	h1, _, _ := Hash(dir, pats)
	if h1 == h0 {
		t.Fatal("a changed input did not change the hash")
	}
	write(t, dir, "migrations/003.sql", "create table c;")
	if h2, n, _ := Hash(dir, pats); h2 == h1 || n != 3 {
		t.Fatalf("a new input: hash same=%v, files=%d", h2 == h1, n)
	}
	if err := os.Remove(filepath.Join(dir, "migrations/001.sql")); err != nil {
		t.Fatal(err)
	}
	if h3, n, _ := Hash(dir, pats); n != 2 || h3 == h1 {
		t.Fatalf("a removed input: files=%d", n)
	}
}

func TestNothingMatchedIsAStableHash(t *testing.T) {
	dir := t.TempDir()
	a, n, err := Hash(dir, []string{"nope/**/*.x"})
	b, _, _ := Hash(dir, []string{"nope/**/*.x"})
	if err != nil || n != 0 || a != b {
		t.Fatalf("a = %q b = %q n = %d err = %v", a, b, n, err)
	}
}

func TestHeavyFoldersAreSkippedUnlessNamed(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "src/a.js", "1")
	write(t, dir, "node_modules/pkg/index.js", "2")
	files, _ := Files(dir, []string{"**/*.js"})
	if len(files) != 1 || files[0] != "src/a.js" {
		t.Fatalf("files = %v", files)
	}
	files, _ = Files(dir, []string{"node_modules/**/*.js"})
	if len(files) != 1 {
		t.Fatalf("a pattern that names node_modules = %v", files)
	}
}
