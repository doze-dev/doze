package logstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func texts(lines []Line) string {
	var out []string
	for _, l := range lines {
		out = append(out, l.Stream+" "+l.Text)
	}
	return strings.Join(out, " | ")
}

func begin(t *testing.T, s *Store, instance string) *Run {
	t.Helper()
	r, err := s.Begin(instance)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestLinesAreAssembledAndTagged(t *testing.T) {
	s := Open(t.TempDir(), Options{})
	r := begin(t, s, "api")
	out, errw := r.Writer(Stdout), r.Writer(Stderr)

	fmt.Fprint(out, "listening on ")
	fmt.Fprint(errw, "warn: slow\n")
	fmt.Fprint(out, ":8080\nready\r\n")
	r.Line(StepPrefix+"setup", "migrated 3")
	fmt.Fprint(out, "no newline yet")

	lines, _, err := s.Read("api", Cursor{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := texts(lines), "e warn: slow | o listening on :8080 | o ready | s:setup migrated 3"; got != want {
		t.Fatalf("read:\n  %s\nwant:\n  %s", got, want)
	}
	if lines[0].Instance != "api" || lines[0].Run != 1 || lines[0].Time.IsZero() {
		t.Fatalf("line = %+v", lines[0])
	}

	// Closing records the line that never got its newline.
	r.Close()
	all, _, _ := s.Read("api", Cursor{}, 100)
	if last := all[len(all)-1]; last.Text != "no newline yet" {
		t.Fatalf("last line after close = %q", last.Text)
	}
}

// The logs of a stopped instance stay readable, run by run.
func TestRunsOutliveTheProcess(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir, Options{})
	first := begin(t, s, "api")
	first.Line(Stdout, "first run")
	first.Close()
	second := begin(t, s, "api")
	second.Line(Stdout, "second run")
	second.Close()

	// A new store over the same directory: a daemon restart.
	s = Open(dir, Options{})
	runs, err := s.Runs("api")
	if err != nil || len(runs) != 2 || runs[0].ID != 1 || runs[1].ID != 2 || runs[1].Live {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
	if runs[0].Started.IsZero() || runs[0].Bytes == 0 {
		t.Fatalf("run info = %+v", runs[0])
	}
	latest, _, _ := s.Tail("api", 0, 10)
	previous, _, _ := s.Tail("api", -1, 10)
	byID, _, _ := s.Tail("api", 1, 10)
	if texts(latest) != "o second run" || texts(previous) != "o first run" || texts(byID) != "o first run" {
		t.Fatalf("latest %q, previous %q, run 1 %q", texts(latest), texts(previous), texts(byID))
	}
	if _, _, err := s.Tail("api", -2, 10); err == nil {
		t.Fatal("asking for a run that is not kept must be an error")
	}
	if third := begin(t, s, "api"); third.ID() != 3 {
		t.Fatalf("the next run after a restart is %d, want 3", third.ID())
	}
	if lines, _, err := s.Tail("never-ran", 0, 10); lines != nil || err != nil {
		t.Fatalf("an instance that never ran has no lines and no error, got %v, %v", lines, err)
	}
}

func TestCursorResumesAcrossRuns(t *testing.T) {
	s := Open(t.TempDir(), Options{})
	r := begin(t, s, "api")
	r.Line(Stdout, "a")
	r.Line(Stdout, "b")

	lines, cur, _ := s.Read("api", Cursor{}, 1)
	if texts(lines) != "o a" {
		t.Fatalf("first read = %q", texts(lines))
	}
	r.Line(Stdout, "c")
	r.Close()
	r2 := begin(t, s, "api")
	r2.Line(Stdout, "d")

	lines, cur, _ = s.Read("api", cur, 100)
	if texts(lines) != "o b | o c | o d" || lines[2].Run != 2 {
		t.Fatalf("resumed read = %q", texts(lines))
	}
	if more, _, _ := s.Read("api", cur, 100); len(more) != 0 {
		t.Fatalf("nothing new, but read %q", texts(more))
	}
}

func TestOldRunsArePruned(t *testing.T) {
	s := Open(t.TempDir(), Options{MaxRuns: 2})
	for i := 1; i <= 4; i++ {
		r := begin(t, s, "api")
		r.Line(Stdout, fmt.Sprintf("run %d", i))
		r.Close()
	}
	runs, _ := s.Runs("api")
	if len(runs) != 2 || runs[0].ID != 3 || runs[1].ID != 4 {
		t.Fatalf("kept %+v, want runs 3 and 4", runs)
	}
}

// One run that prints without end must stay under the cap, and what is kept
// must be its newest lines.
func TestALongRunStaysUnderTheCap(t *testing.T) {
	dir := t.TempDir()
	const cap = 64 * 1024
	s := Open(dir, Options{MaxBytes: cap})
	r := begin(t, s, "api")
	line := strings.Repeat("x", 100)
	for i := 0; i < 5000; i++ { // about 650 kB, ten times the cap
		r.Line(Stdout, fmt.Sprintf("%05d %s", i, line))
	}
	var total int64
	entries, _ := os.ReadDir(filepath.Join(dir, "api"))
	for _, e := range entries {
		info, _ := e.Info()
		total += info.Size()
	}
	if total > cap+cap/4 {
		t.Fatalf("%d bytes on disk for a %d byte cap", total, cap)
	}
	if len(entries) < 2 {
		t.Fatalf("a long run should be split into segments, found %d file(s)", len(entries))
	}
	tail, _, _ := s.Tail("api", 0, 1)
	if len(tail) != 1 || !strings.HasPrefix(tail[0].Text, "04999 ") {
		t.Fatalf("the newest line must survive, tail = %q", texts(tail))
	}
	// A reader whose cursor fell into the deleted part resumes from what is left.
	lines, _, err := s.Read("api", Cursor{Run: 1, Seg: 0, Offset: 10}, 1)
	if err != nil || len(lines) != 1 || lines[0].Text[:5] == "00000" {
		t.Fatalf("resume past a pruned segment: %q, %v", texts(lines), err)
	}
}

// A follower is woken by writes, sees every line in order, and carries on into
// the next run.
func TestFollow(t *testing.T) {
	s := Open(t.TempDir(), Options{})
	r := begin(t, s, "api")
	r.Line(Stdout, "before")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var got []string
	done := make(chan error, 1)
	go func() {
		done <- s.Follow(ctx, "api", Cursor{}, func(l Line) error {
			mu.Lock()
			got = append(got, fmt.Sprintf("%d:%s", l.Run, l.Text))
			mu.Unlock()
			return nil
		})
	}()
	seen := func() string { mu.Lock(); defer mu.Unlock(); return strings.Join(got, " ") }
	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if seen() == want {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatalf("followed %q, want %q", seen(), want)
	}

	waitFor("1:before")
	for i := 0; i < 50; i++ {
		r.Line(Stdout, fmt.Sprintf("l%d", i))
	}
	r.Close()
	r2 := begin(t, s, "api")
	r2.Line(Stderr, "second")

	var want []string
	want = append(want, "1:before")
	for i := 0; i < 50; i++ {
		want = append(want, fmt.Sprintf("1:l%d", i))
	}
	want = append(want, "2:second")
	waitFor(strings.Join(want, " "))

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Follow: %v", err)
	}
}

// A follower of an instance that has not started yet waits for it.
func TestFollowBeforeTheFirstRun(t *testing.T) {
	s := Open(t.TempDir(), Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan string, 1)
	go s.Follow(ctx, "api", Cursor{}, func(l Line) error { got <- l.Text; return nil })

	time.Sleep(20 * time.Millisecond)
	begin(t, s, "api").Line(Stdout, "hello")
	select {
	case text := <-got:
		if text != "hello" {
			t.Fatalf("got %q", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the follower never saw the first line")
	}
}

func TestConcurrentWriters(t *testing.T) {
	s := Open(t.TempDir(), Options{})
	r := begin(t, s, "api")
	var wg sync.WaitGroup
	for _, stream := range []string{Stdout, Stderr} {
		w := r.Writer(stream)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				fmt.Fprintf(w, "line %d of a writer\n", i)
			}
		}()
	}
	wg.Wait()
	lines, _, err := s.Read("api", Cursor{}, 5000)
	if err != nil || len(lines) != 1000 {
		t.Fatalf("read %d lines, %v; want 1000", len(lines), err)
	}
	for _, l := range lines {
		if !strings.HasSuffix(l.Text, "of a writer") {
			t.Fatalf("a line was torn: %q", l.Text)
		}
	}
}

// Tail must return the same lines whether the run is a few bytes or many
// blocks long, and whatever n is.
func TestTailReadsFromTheEnd(t *testing.T) {
	s := Open(t.TempDir(), Options{MaxBytes: 64 << 20})
	r := begin(t, s, "api")
	const total = 4000 // about 480 kB: several 64 kB blocks
	for i := 0; i < total; i++ {
		r.Line(Stdout, fmt.Sprintf("%05d %s", i, strings.Repeat("y", 100)))
	}
	for _, n := range []int{0, 1, 2, 199, 200, 1000, total, total + 50} {
		lines, end, err := s.Tail("api", 0, n)
		if err != nil {
			t.Fatal(err)
		}
		want := n
		if want > total {
			want = total
		}
		if len(lines) != want {
			t.Fatalf("Tail(%d) returned %d lines, want %d", n, len(lines), want)
		}
		for i, l := range lines {
			if got, wantNo := l.Text[:5], fmt.Sprintf("%05d", total-want+i); got != wantNo {
				t.Fatalf("Tail(%d) line %d is %s, want %s", n, i, got, wantNo)
			}
		}
		// The cursor is the end of the run: nothing new to read from it.
		if more, _, _ := s.Read("api", end, 10); len(more) != 0 {
			t.Fatalf("Tail(%d) cursor is not at the end: read %d more", n, len(more))
		}
	}
	// A line still being written is not returned, and the cursor stops before it.
	fmt.Fprint(r.Writer(Stdout), "half a li")
	lines, end, _ := s.Tail("api", 0, 1)
	if len(lines) != 1 || lines[0].Text[:5] != fmt.Sprintf("%05d", total-1) {
		t.Fatalf("tail with a partial line pending = %q", texts(lines))
	}
	r.Close()
	if more, _, _ := s.Read("api", end, 10); len(more) != 1 || more[0].Text != "half a li" {
		t.Fatalf("after close the pending line follows the cursor, got %q", texts(more))
	}
}
