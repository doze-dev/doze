// Package logstore keeps what every instance prints, on disk.
//
// Each start of an instance is a run. A run is one or more segment files of
// plain text, one line per line printed:
//
//	2026-10-10T09:15:02.418305Z o listening on :8080
//
// a timestamp, the stream, and the text. Nothing is held in memory beyond a
// partial line per writer, so a daemon's footprint does not grow with what its
// services print, and the logs of a stopped or crashed instance stay readable.
//
// Readers address a position by Cursor and can follow: a follower is woken by
// the writer, not by a timer, and reads from the file, so it never misses a
// line however far behind it falls.
package logstore

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// The streams a line can come from. A step's stream is StepPrefix plus its
// name: "s:setup".
const (
	Stdout     = "o"
	Stderr     = "e"
	StepPrefix = "s:"
)

// Defaults for Options.
const (
	DefaultMaxRuns  = 5
	DefaultMaxBytes = 10 << 20
)

const timeLayout = "2006-01-02T15:04:05.000000Z"

// Options bound what is kept, per instance.
type Options struct {
	MaxRuns  int   // runs kept, the live one included; <= 0 is DefaultMaxRuns
	MaxBytes int64 // bytes kept across all runs; <= 0 is DefaultMaxBytes
}

// Store is the log store for one stack.
type Store struct {
	dir  string
	opts Options
	now  func() time.Time

	mu      sync.Mutex
	live    map[string]*Run          // the run being written, per instance
	changed map[string]chan struct{} // closed, then replaced, when an instance gets a line
}

// Open returns the store rooted at dir. Nothing is created until something is
// written.
func Open(dir string, opts Options) *Store {
	if opts.MaxRuns <= 0 {
		opts.MaxRuns = DefaultMaxRuns
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	return &Store{dir: dir, opts: opts, now: time.Now, live: map[string]*Run{}, changed: map[string]chan struct{}{}}
}

// segmentBytes is the size at which a run moves to a new segment. Old segments
// are what pruning deletes, so a quarter of the cap keeps most of it in use.
func (s *Store) segmentBytes() int64 { return s.opts.MaxBytes / 4 }

// Line is one line an instance printed.
type Line struct {
	Instance string
	Run      int
	Time     time.Time
	Stream   string
	Text     string
}

// Cursor is a position in an instance's log: the start of a line, or the end
// of the newest segment. The zero Cursor is the start of the oldest line kept.
type Cursor struct {
	Run    int
	Seg    int
	Offset int64
}

func (c Cursor) before(run, seg int) bool {
	return c.Run < run || (c.Run == run && c.Seg < seg)
}

// RunInfo describes one kept run.
type RunInfo struct {
	ID      int
	Started time.Time // the time of its first line; zero if it printed nothing
	Bytes   int64
	Live    bool // still being written
}

// segment is one file of a run.
type segment struct {
	run, seg int
	path     string
	size     int64
}

func segmentName(run, seg int) string { return fmt.Sprintf("r%06d.s%03d.log", run, seg) }

// segments lists an instance's segment files, oldest first.
func (s *Store) segments(instance string) ([]segment, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, instance))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []segment
	for _, e := range entries {
		var run, seg int
		if n, _ := fmt.Sscanf(e.Name(), "r%06d.s%03d.log", &run, &seg); n != 2 || e.Name() != segmentName(run, seg) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, segment{run: run, seg: seg, path: filepath.Join(s.dir, instance, e.Name()), size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].run != out[j].run {
			return out[i].run < out[j].run
		}
		return out[i].seg < out[j].seg
	})
	return out, nil
}

// Run is one start of an instance, open for writing.
type Run struct {
	s        *Store
	instance string
	id       int

	mu      sync.Mutex
	f       *os.File
	seg     int
	size    int64
	writers []*lineWriter
	closed  bool
	buf     []byte // the line being formatted; reused, so a line costs no allocation
}

// Begin starts a new run for instance and makes it the one being written. A
// run still open for the same instance is closed first.
func (s *Store) Begin(instance string) (*Run, error) {
	s.mu.Lock()
	old := s.live[instance]
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	if err := os.MkdirAll(filepath.Join(s.dir, instance), 0o755); err != nil {
		return nil, err
	}
	segs, err := s.segments(instance)
	if err != nil {
		return nil, err
	}
	id := 1
	if n := len(segs); n > 0 {
		id = segs[n-1].run + 1
	}
	r := &Run{s: s, instance: instance, id: id}
	if err := r.open(0); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.live[instance] = r
	s.mu.Unlock()
	s.prune(instance, id, 0)
	return r, nil
}

// ID is the run's number: 1 for an instance's first start, counting up.
func (r *Run) ID() int { return r.id }

func (r *Run) open(seg int) error {
	f, err := os.OpenFile(filepath.Join(r.s.dir, r.instance, segmentName(r.id, seg)), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	r.f, r.seg, r.size = f, seg, 0
	return nil
}

// Writer returns a writer whose complete lines are recorded on stream. It is
// what a process's stdout or stderr is connected to.
func (r *Run) Writer(stream string) io.Writer {
	w := &lineWriter{run: r, stream: stream}
	r.mu.Lock()
	r.writers = append(r.writers, w)
	r.mu.Unlock()
	return w
}

// Line records one line on stream.
func (r *Run) Line(stream, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeLocked(stream, []byte(text))
}

func (r *Run) writeLocked(stream string, text []byte) {
	if r.closed || r.f == nil {
		return
	}
	line := r.s.now().UTC().AppendFormat(r.buf[:0], timeLayout)
	line = append(line, ' ')
	line = append(line, stream...)
	line = append(line, ' ')
	line = append(line, text...)
	line = append(line, '\n')
	r.buf = line
	if r.size > 0 && r.size+int64(len(line)) > r.s.segmentBytes() {
		_ = r.f.Close()
		if err := r.open(r.seg + 1); err != nil {
			r.f = nil
			return
		}
		r.s.prune(r.instance, r.id, r.seg)
	}
	n, _ := r.f.Write(line)
	r.size += int64(n)
	r.s.wake(r.instance)
}

// Close ends the run: a line still being assembled is recorded as it stands,
// and the file is closed. The run stays readable.
func (r *Run) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	for _, w := range r.writers {
		if w.partial.Len() > 0 {
			r.writeLocked(w.stream, w.partial.Bytes())
			w.partial.Reset()
		}
	}
	r.closed = true
	if r.f != nil {
		_ = r.f.Close()
		r.f = nil
	}
	r.mu.Unlock()

	r.s.mu.Lock()
	if r.s.live[r.instance] == r {
		delete(r.s.live, r.instance)
	}
	r.s.mu.Unlock()
	r.s.wake(r.instance)
}

// lineWriter assembles the bytes a process writes into lines.
type lineWriter struct {
	run     *Run
	stream  string
	partial bytes.Buffer
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.run.mu.Lock()
	defer w.run.mu.Unlock()
	rest := p
	for {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			w.partial.Write(rest)
			break
		}
		line := rest[:i]
		if w.partial.Len() > 0 { // the line began in an earlier Write
			w.partial.Write(line)
			line = w.partial.Bytes()
		}
		w.run.writeLocked(w.stream, bytes.TrimSuffix(line, []byte("\r")))
		w.partial.Reset()
		rest = rest[i+1:]
	}
	return len(p), nil
}

// prune deletes what is over the limits for instance: whole runs beyond
// MaxRuns, then the oldest segments while the total is over MaxBytes. The
// segment being written (liveRun, liveSeg) is never deleted.
func (s *Store) prune(instance string, liveRun, liveSeg int) {
	segs, err := s.segments(instance)
	if err != nil {
		return
	}
	runs := map[int]bool{}
	for _, sg := range segs {
		runs[sg.run] = true
	}
	oldestKept := 0
	if len(runs) > s.opts.MaxRuns {
		ids := make([]int, 0, len(runs))
		for id := range runs {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		oldestKept = ids[len(ids)-s.opts.MaxRuns]
	}
	var total int64
	for _, sg := range segs {
		total += sg.size
	}
	for _, sg := range segs {
		live := sg.run == liveRun && sg.seg == liveSeg
		if live {
			continue
		}
		if sg.run < oldestKept || total > s.opts.MaxBytes {
			if os.Remove(sg.path) == nil {
				total -= sg.size
			}
		}
	}
}

// wake tells followers of instance that there is something new to read.
func (s *Store) wake(instance string) {
	s.mu.Lock()
	if ch, ok := s.changed[instance]; ok {
		close(ch)
		delete(s.changed, instance)
	}
	s.mu.Unlock()
}

// waiter returns a channel closed at the next write to instance.
func (s *Store) waiter(instance string) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.changed[instance]
	if !ok {
		ch = make(chan struct{})
		s.changed[instance] = ch
	}
	return ch
}

// Runs lists the runs kept for instance, oldest first.
func (s *Store) Runs(instance string) ([]RunInfo, error) {
	segs, err := s.segments(instance)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	live := s.live[instance]
	s.mu.Unlock()
	var out []RunInfo
	for _, sg := range segs {
		if n := len(out); n == 0 || out[n-1].ID != sg.run {
			info := RunInfo{ID: sg.run, Live: live != nil && live.id == sg.run}
			if lines, _, _ := readSegment(instance, sg, 0, 1); len(lines) == 1 {
				info.Started = lines[0].Time
			}
			out = append(out, info)
		}
		out[len(out)-1].Bytes += sg.size
	}
	return out, nil
}

// Read returns up to max complete lines of instance from cursor on, across
// runs, and the cursor to continue from. A cursor that points into something
// since deleted continues from the oldest line kept after it.
func (s *Store) Read(instance string, from Cursor, max int) ([]Line, Cursor, error) {
	segs, err := s.segments(instance)
	if err != nil {
		return nil, from, err
	}
	var out []Line
	next := from
	for _, sg := range segs {
		if from.before(sg.run, sg.seg) {
			next = Cursor{Run: sg.run, Seg: sg.seg}
		} else if from.Run != sg.run || from.Seg != sg.seg {
			continue // before the cursor
		}
		lines, end, err := readSegment(instance, sg, next.Offset, max-len(out))
		if err != nil {
			return out, next, err
		}
		out = append(out, lines...)
		next.Offset = end
		from = next
		if len(out) >= max {
			break
		}
	}
	return out, next, nil
}

// Tail returns the last n lines of one run of instance, and the cursor just
// past them. run 0 is the newest run; a negative run counts back from it (-1
// is the one before).
func (s *Store) Tail(instance string, run, n int) ([]Line, Cursor, error) {
	segs, err := s.segments(instance)
	if err != nil || len(segs) == 0 {
		return nil, Cursor{}, err
	}
	if run <= 0 {
		var ids []int
		for _, sg := range segs {
			if len(ids) == 0 || ids[len(ids)-1] != sg.run {
				ids = append(ids, sg.run)
			}
		}
		if -run >= len(ids) {
			return nil, Cursor{}, fmt.Errorf("%s has only %d run(s) kept", instance, len(ids))
		}
		run = ids[len(ids)-1+run]
	}
	var out []Line
	end := Cursor{}
	found := false
	for i := len(segs) - 1; i >= 0; i-- {
		sg := segs[i]
		if sg.run != run {
			continue
		}
		lines, off, err := readSegment(instance, sg, 0, int(^uint(0)>>1))
		if err != nil {
			return nil, Cursor{}, err
		}
		if !found {
			end, found = Cursor{Run: sg.run, Seg: sg.seg, Offset: off}, true
		}
		out = append(lines, out...)
		if len(out) >= n {
			break
		}
	}
	if !found {
		return nil, Cursor{}, fmt.Errorf("%s has no run %d", instance, run)
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out, end, nil
}

// Follow calls emit for every line of instance from cursor on, and keeps going
// as lines are written — into new runs too — until ctx is done or emit fails.
func (s *Store) Follow(ctx context.Context, instance string, from Cursor, emit func(Line) error) error {
	for {
		// Take the waiter before reading, so a line written between the read
		// and the wait still wakes this follower.
		wait := s.waiter(instance)
		for {
			lines, next, err := s.Read(instance, from, 512)
			if err != nil {
				return err
			}
			for _, l := range lines {
				if err := emit(l); err != nil {
					return err
				}
			}
			from = next
			if len(lines) < 512 {
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-wait:
		}
	}
}

// Remove deletes everything kept for instance.
func (s *Store) Remove(instance string) error {
	s.mu.Lock()
	live := s.live[instance]
	s.mu.Unlock()
	if live != nil {
		live.Close()
	}
	return os.RemoveAll(filepath.Join(s.dir, instance))
}

// readSegment reads up to max complete lines of one segment from offset, and
// returns the offset just past the last one read.
func readSegment(instance string, sg segment, offset int64, max int) ([]Line, int64, error) {
	if max <= 0 {
		return nil, offset, nil
	}
	f, err := os.Open(sg.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, offset, nil // pruned between the listing and the read
	}
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	var out []Line
	br := bufio.NewReaderSize(f, 64*1024)
	for len(out) < max {
		raw, err := br.ReadBytes('\n')
		if err != nil {
			break // EOF, or a line still being written: stop before it
		}
		offset += int64(len(raw))
		if l, ok := parseLine(raw[:len(raw)-1]); ok {
			l.Instance, l.Run = instance, sg.run
			out = append(out, l)
		}
	}
	return out, offset, nil
}

func parseLine(raw []byte) (Line, bool) {
	stamp, rest, ok := bytes.Cut(raw, []byte(" "))
	if !ok {
		return Line{}, false
	}
	stream, text, _ := bytes.Cut(rest, []byte(" "))
	t, err := time.Parse(timeLayout, string(stamp))
	if err != nil {
		return Line{}, false
	}
	return Line{Time: t, Stream: string(stream), Text: string(text)}, true
}
