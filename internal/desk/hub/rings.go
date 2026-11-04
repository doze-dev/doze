package hub

import (
	"time"

	"github.com/doze-dev/doze/internal/control"
)

// What a stack keeps of what its daemon streams, bounded as the dash bounded
// it: four minutes of meters at a second each, and the last lines each
// instance printed. A hidden page costs nothing because the topics that feed
// these are subscribed only while a page wants them (Hub.Watch).
const (
	// samplesKept is how many meter readings an instance keeps.
	samplesKept = 240
	// linesKept is how many log lines an instance keeps, and how many a new
	// subscription asks for as backlog.
	linesKept = 400
	// allLinesKept is how many lines the merged all-logs page keeps.
	allLinesKept = 800
)

// Series is one instance's meters over time, oldest first. At has the time
// of each reading when the daemon stamped it.
type Series struct {
	RAM, CPU, Conns []float64
	At              []time.Time
}

func (s *Series) push(v control.StatView, at time.Time) {
	// A reading already held is sent again on a reconnect; skip it.
	if !at.IsZero() && len(s.At) > 0 && !at.After(s.At[len(s.At)-1]) {
		return
	}
	s.RAM = cap240(append(s.RAM, float64(v.RAM)))
	s.CPU = cap240(append(s.CPU, v.CPU))
	s.Conns = cap240(append(s.Conns, float64(v.Conns)))
	if !at.IsZero() {
		s.At = append(s.At, at)
		if len(s.At) > samplesKept {
			s.At = s.At[len(s.At)-samplesKept:]
		}
	} else {
		s.At = nil // not stamped: a second apart is assumed
	}
}

func cap240(vs []float64) []float64 {
	if len(vs) > samplesKept {
		return vs[len(vs)-samplesKept:]
	}
	return vs
}

func (s Series) clone() Series {
	return Series{
		RAM: append([]float64(nil), s.RAM...), CPU: append([]float64(nil), s.CPU...),
		Conns: append([]float64(nil), s.Conns...), At: append([]time.Time(nil), s.At...),
	}
}

// lines is a bounded ring of what an instance printed, in time order.
type lines []control.LogFrame

// add appends a line unless it is one already held, and says whether it
// did: a reconnect's backlog repeats lines. Lines arrive in time order, so
// only those as new as it are compared, as the dash's seenLine did.
func (l lines) add(f control.LogFrame, keep int) (lines, bool) {
	for i := len(l) - 1; i >= 0 && !l[i].Time.Before(f.Time); i-- {
		if l[i].Time.Equal(f.Time) && l[i].Line == f.Line && l[i].Run == f.Run {
			return l, false
		}
	}
	l = append(l, f)
	if len(l) > keep {
		l = l[len(l)-keep:]
	}
	return l, true
}
