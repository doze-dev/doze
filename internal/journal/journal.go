// Package journal is the structured record of what the daemon did: every wake,
// sleep, crash and restart, with what caused it and how long it took. It is
// what a screen reads to say "woke in 38 ms, because worker asked".
//
// Events are JSON lines in a file. Nothing is kept in memory: a daemon at rest
// holds an open file and a sequence number.
package journal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sync"
	"time"
)

// The kinds of event.
const (
	Wake    = "wake"    // a start began
	Ready   = "ready"   // the start finished; Took is the time since its wake
	Sleep   = "sleep"   // stopped on purpose
	Crash   = "crash"   // exited by itself; Detail is how
	Restart = "restart" // a crashed instance is being started again
	GiveUp  = "give_up" // the restart budget is spent
	Failed  = "failed"  // a start did not complete; Detail is why
	Step    = "step"    // a setup or on_ready command ran; Detail names it
	Pin     = "pin"     // pinned awake, or unpinned; Detail is "on" or "off"
)

// The kinds of cause.
const (
	ByConnection = "connection" // a client connected
	ByDependency = "dependency" // an instance that depends on it started; By names it
	ByCommand    = "command"    // someone asked: the CLI, the dash, the API
	ByRestart    = "restart"    // the restart policy
	ByIdle       = "idle"       // nobody held it for the idle time
	ByShutdown   = "shutdown"   // the daemon is stopping
)

// Cause is why something happened.
type Cause struct {
	Kind string `json:"kind,omitempty"`
	// By names who: the dependant, for ByDependency.
	By string `json:"by,omitempty"`
}

// Event is one line of the journal.
type Event struct {
	Seq      uint64        `json:"seq"`
	Time     time.Time     `json:"time"`
	Kind     string        `json:"kind"`
	Instance string        `json:"instance"`
	Cause    Cause         `json:"cause,omitzero"`
	Took     time.Duration `json:"took,omitempty"` // nanoseconds
	Detail   string        `json:"detail,omitempty"`
}

// DefaultMaxBytes is the size at which the journal file is rotated. One
// rotated file is kept, so the journal holds at most about twice this.
const DefaultMaxBytes = 1 << 20

// Journal appends events to a file and hands them to subscribers.
type Journal struct {
	path     string
	maxBytes int64
	now      func() time.Time

	mu   sync.Mutex
	f    *os.File
	size int64
	seq  uint64
	subs map[*chan Event]struct{}
}

// Open opens (creating if needed) the journal at path. maxBytes <= 0 uses
// DefaultMaxBytes. The sequence continues from the last event on disk, so Seq
// is unique across daemon restarts.
func Open(path string, maxBytes int64) (*Journal, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	j := &Journal{path: path, maxBytes: maxBytes, now: time.Now}
	// The last event is in the live file, or in the rotated one if the live
	// file is new.
	for _, p := range []string{rotated(path), path} {
		events, err := readFile(p)
		if err != nil {
			return nil, err
		}
		if n := len(events); n > 0 {
			j.seq = events[n-1].Seq
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	j.f, j.size = f, st.Size()
	return j, nil
}

func rotated(path string) string { return path + ".1" }

// Append stamps e with the next sequence number and the time, writes it, and
// hands it to subscribers. It returns the stamped event. A nil Journal accepts
// and drops events, so callers need no guard.
func (j *Journal) Append(e Event) Event {
	if j == nil {
		return e
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.seq++
	e.Seq = j.seq
	if e.Time.IsZero() {
		e.Time = j.now()
	}
	if j.f != nil {
		line, err := json.Marshal(e)
		if err == nil {
			line = append(line, '\n')
			if j.size+int64(len(line)) > j.maxBytes && j.size > 0 {
				j.rotate()
			}
			if n, err := j.f.Write(line); err == nil {
				j.size += int64(n)
			}
		}
	}
	for chp := range j.subs {
		select {
		case *chp <- e:
		default: // a slow reader catches up from the file with Since
		}
	}
	return e
}

// rotate moves the live file aside, replacing the previous rotated file, and
// starts a new one. Caller holds j.mu.
func (j *Journal) rotate() {
	_ = j.f.Close()
	_ = os.Rename(j.path, rotated(j.path))
	f, err := os.OpenFile(j.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		j.f = nil // keep serving subscribers; the next Append retries nothing
		return
	}
	j.f, j.size = f, 0
}

// Since returns every event on disk with a sequence number above seq, oldest
// first. Since(0) is the whole journal.
func (j *Journal) Since(seq uint64) ([]Event, error) {
	if j == nil {
		return nil, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []Event
	for _, p := range []string{rotated(j.path), j.path} {
		events, err := readFile(p)
		if err != nil {
			return nil, err
		}
		for _, e := range events {
			if e.Seq > seq {
				out = append(out, e)
			}
		}
	}
	return out, nil
}

// Tail returns the last n events on disk, oldest first.
func (j *Journal) Tail(n int) ([]Event, error) {
	all, err := j.Since(0)
	if err != nil || len(all) <= n {
		return all, err
	}
	return all[len(all)-n:], nil
}

// Subscribe delivers every event appended from now on, until ctx is done. A
// reader that falls more than buf events behind misses some; it can tell from
// a gap in Seq, and fill it with Since.
func (j *Journal) Subscribe(ctx context.Context, buf int) <-chan Event {
	if buf < 1 {
		buf = 1
	}
	ch := make(chan Event, buf)
	if j == nil {
		close(ch)
		return ch
	}
	j.mu.Lock()
	if j.subs == nil {
		j.subs = map[*chan Event]struct{}{}
	}
	j.subs[&ch] = struct{}{}
	j.mu.Unlock()
	go func() {
		<-ctx.Done()
		j.mu.Lock()
		delete(j.subs, &ch)
		close(ch)
		j.mu.Unlock()
	}()
	return ch
}

// Close closes the file. Appends after Close still reach subscribers.
func (j *Journal) Close() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f == nil {
		return nil
	}
	err := j.f.Close()
	j.f = nil
	return err
}

// readFile reads every event in one journal file. A missing file is empty. A
// line that does not parse — a write cut short by a crash — is skipped.
func readFile(path string) ([]Event, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Seq != 0 {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// Read returns every event in the journal at path, oldest first, without
// opening it for writing: for a reader in another process.
func Read(path string) ([]Event, error) {
	var out []Event
	for _, p := range []string{rotated(path), path} {
		events, err := readFile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, events...)
	}
	return out, nil
}
