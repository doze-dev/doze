package journal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T, path string, max int64) *Journal {
	t.Helper()
	j, err := Open(path, max)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j
}

func TestAppendAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j := open(t, path, 0)

	wake := j.Append(Event{Kind: Wake, Instance: "db", Cause: Cause{Kind: ByDependency, By: "api"}})
	ready := j.Append(Event{Kind: Ready, Instance: "db", Took: 38 * time.Millisecond})
	if wake.Seq != 1 || ready.Seq != 2 || wake.Time.IsZero() {
		t.Fatalf("stamped %+v then %+v", wake, ready)
	}

	all, err := j.Since(0)
	if err != nil || len(all) != 2 {
		t.Fatalf("Since(0) = %+v, %v", all, err)
	}
	if all[0].Cause != (Cause{Kind: ByDependency, By: "api"}) || all[1].Took != 38*time.Millisecond {
		t.Fatalf("read back %+v", all)
	}
	if after, _ := j.Since(1); len(after) != 1 || after[0].Kind != Ready {
		t.Fatalf("Since(1) = %+v", after)
	}
}

// The sequence must not restart with the daemon, or a reader holding a cursor
// would see old numbers again.
func TestSequenceSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j := open(t, path, 0)
	j.Append(Event{Kind: Wake, Instance: "db"})
	j.Append(Event{Kind: Ready, Instance: "db"})
	j.Close()

	j2 := open(t, path, 0)
	if e := j2.Append(Event{Kind: Sleep, Instance: "db"}); e.Seq != 3 {
		t.Fatalf("after reopen the next event is seq %d, want 3", e.Seq)
	}
}

func TestRotationKeepsOneOldFileAndTheSequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j := open(t, path, 400) // a few events per file
	for i := 0; i < 40; i++ {
		j.Append(Event{Kind: Wake, Instance: "db", Detail: strings.Repeat("x", 40)})
	}
	for _, p := range []string{path, rotated(path)} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if st.Size() > 400 {
			t.Errorf("%s is %d bytes, over the 400 byte cap", filepath.Base(p), st.Size())
		}
	}
	all, err := j.Since(0)
	if err != nil || len(all) == 0 {
		t.Fatalf("Since(0) = %d events, %v", len(all), err)
	}
	if last := all[len(all)-1].Seq; last != 40 {
		t.Fatalf("last seq = %d, want 40", last)
	}
	for i := 1; i < len(all); i++ {
		if all[i].Seq != all[i-1].Seq+1 {
			t.Fatalf("what is kept must be contiguous, got %d after %d", all[i].Seq, all[i-1].Seq)
		}
	}
	// And a reopen after rotation still continues the sequence.
	j.Close()
	if e := open(t, path, 400).Append(Event{Kind: Sleep}); e.Seq != 41 {
		t.Fatalf("after rotation and reopen, seq = %d, want 41", e.Seq)
	}
}

func TestSubscribeAndTail(t *testing.T) {
	j := open(t, filepath.Join(t.TempDir(), "journal.jsonl"), 0)
	j.Append(Event{Kind: Wake, Instance: "old"})

	ctx, cancel := context.WithCancel(context.Background())
	ch := j.Subscribe(ctx, 4)
	j.Append(Event{Kind: Wake, Instance: "db"})
	select {
	case e := <-ch:
		if e.Instance != "db" || e.Seq != 2 {
			t.Fatalf("subscriber got %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber got nothing")
	}
	cancel()
	if _, open := <-ch; open {
		t.Fatal("the channel must close when the subscription ends")
	}
	j.Append(Event{Kind: Sleep, Instance: "db"}) // must not panic on the closed subscriber

	if tail, _ := j.Tail(2); len(tail) != 2 || tail[1].Kind != Sleep {
		t.Fatalf("Tail(2) = %+v", tail)
	}
}

func TestTornLastLineIsSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j := open(t, path, 0)
	j.Append(Event{Kind: Wake, Instance: "db"})
	j.Close()
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"seq":2,"kind":"rea`)
	f.Close()

	j2 := open(t, path, 0)
	if e := j2.Append(Event{Kind: Ready, Instance: "db"}); e.Seq != 2 {
		t.Fatalf("seq after a torn line = %d, want 2", e.Seq)
	}
}

func TestNilJournalIsANoOp(t *testing.T) {
	var j *Journal
	j.Append(Event{Kind: Wake})
	if all, err := j.Since(0); all != nil || err != nil {
		t.Fatal("a nil journal reads as empty")
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
}
