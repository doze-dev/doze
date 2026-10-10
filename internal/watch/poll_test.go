package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAChangeIsReportedOnceAfterItSettles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "main.go"), "package main")
	write(t, filepath.Join(dir, "notes.txt"), "not watched")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan string, 8)
	go Poll(ctx, []Set{{Dir: dir, Globs: []string{"**/*.go"}}}, 20*time.Millisecond, func(p string) { got <- p })

	time.Sleep(60 * time.Millisecond)
	write(t, filepath.Join(dir, "notes.txt"), "edited, but not matched")
	select {
	case p := <-got:
		t.Fatalf("a file outside the globs was reported: %s", p)
	case <-time.After(150 * time.Millisecond):
	}

	// A burst of writes is one event.
	for i := 0; i < 5; i++ {
		write(t, filepath.Join(dir, "main.go"), "package main // "+string(rune('a'+i)))
		write(t, filepath.Join(dir, "sub", "x.go"), "package sub")
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case p := <-got:
		if filepath.Base(p) != "main.go" && filepath.Base(p) != "x.go" {
			t.Fatalf("reported %s", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a change was not reported")
	}
	select {
	case p := <-got:
		t.Fatalf("one burst was reported twice: %s", p)
	case <-time.After(200 * time.Millisecond):
	}

	// Removing a watched file is a change too.
	if err := os.Remove(filepath.Join(dir, "main.go")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("a removal was not reported")
	}
}
