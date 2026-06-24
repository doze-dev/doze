package supervisor

import (
	"bytes"
	"sync"
)

// ring is a thread-safe, line-oriented bounded buffer used to capture the most
// recent backend log output. It implements io.Writer so it can be wired
// directly to a command's stdout/stderr.
type ring struct {
	mu      sync.Mutex
	max     int
	buf     []string
	partial bytes.Buffer
	total   int // monotonic count of lines ever pushed, for incremental streaming
}

func newRing(max int) *ring {
	if max <= 0 {
		max = 100
	}
	return &ring{max: max}
}

func (r *ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range p {
		if c == '\n' {
			r.push(r.partial.String())
			r.partial.Reset()
			continue
		}
		r.partial.WriteByte(c)
	}
	return len(p), nil
}

// push appends a completed line, evicting the oldest if at capacity. Caller
// holds the lock.
func (r *ring) push(line string) {
	r.buf = append(r.buf, line)
	r.total++
	if len(r.buf) > r.max {
		r.buf = r.buf[len(r.buf)-r.max:]
	}
}

// lines returns a copy of the buffered lines.
func (r *ring) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.buf))
	copy(out, r.buf)
	return out
}

// since returns the lines pushed after absolute index n, plus the current total
// (the cursor to pass next time). Lines evicted before n are unavailable — a burst
// larger than the buffer between polls drops the overflow — so the caller resumes
// from the oldest line still buffered.
func (r *ring) since(n int) (lines []string, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n >= r.total {
		return nil, r.total
	}
	oldest := r.total - len(r.buf) // absolute index of buf[0]
	start := n - oldest
	if start < 0 {
		start = 0
	}
	out := make([]string, len(r.buf)-start)
	copy(out, r.buf[start:])
	return out, r.total
}
