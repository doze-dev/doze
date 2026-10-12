package daemon

import (
	"context"
	"sync"
	"time"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/ui"
)

// Recent readings. A screen that opens should not start with an empty chart, so
// the daemon keeps the last few minutes of every running instance's meters and
// sends them first to anyone who subscribes to stats.
//
// A daemon with nobody watching must stay light, so it samples slowly then: one
// reading every unwatchedGap, and only while something is running. While a
// screen is subscribed it is that subscription's own sampling that fills the
// ring, at the rate asked for. The ring holds readings of mixed spacing, so each
// frame carries the time it was taken (Frame.At).
const (
	meterKept    = 240
	unwatchedGap = 5 * time.Second
	// readings closer than this are one reading: two screens watching at once
	// sample twice
	meterDedupe = 800 * time.Millisecond
)

type meterRing struct {
	mu       sync.Mutex
	frames   []control.Frame // oldest first, with no per-process children
	watchers int
}

// record keeps a stats frame, without the processes under each app, unless one
// was kept a moment ago.
func (r *meterRing) record(f control.Frame) {
	if len(f.Stats) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := len(r.frames); n > 0 && f.At.Sub(r.frames[n-1].At) < meterDedupe {
		return
	}
	kept := control.Frame{Topic: control.TopicStats, At: f.At, Stats: make([]control.StatView, len(f.Stats))}
	for i, sv := range f.Stats {
		sv.Children = nil
		kept.Stats[i] = sv
	}
	r.frames = append(r.frames, kept)
	if len(r.frames) > meterKept {
		r.frames = append([]control.Frame(nil), r.frames[len(r.frames)-meterKept:]...)
	}
}

func (r *meterRing) history() []control.Frame {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]control.Frame(nil), r.frames...)
}

func (r *meterRing) lastAt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := len(r.frames); n > 0 {
		return r.frames[n-1].At
	}
	return time.Time{}
}

func (r *meterRing) watch(delta int) {
	r.mu.Lock()
	r.watchers += delta
	r.mu.Unlock()
}

func (r *meterRing) watched() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.watchers > 0
}

// sampleStats reads the meters of every running instance now.
func (d *Daemon) sampleStats() control.Frame {
	var pids []int
	running := d.rt.Registry().Snapshot()
	for _, inst := range running {
		if inst.PID != 0 {
			pids = append(pids, inst.PID)
		}
	}
	frame := control.Frame{Topic: control.TopicStats, At: time.Now()}
	if len(pids) == 0 {
		return frame
	}
	stats := ui.ProcStats(pids)
	tree := ui.ProcTree(pids)
	for _, inst := range running {
		if inst.PID == 0 {
			continue
		}
		st := stats[inst.PID]
		sv := control.StatView{Name: inst.Name, PID: inst.PID, RAM: st.RSS, CPU: st.CPU, Conns: inst.Conns}
		if decl := d.cfg.Lookup(inst.Name); decl != nil && decl.Type == config.AppEngine {
			for _, n := range tree[inst.PID] {
				sv.Children = append(sv.Children, control.ProcView{PID: n.PID, RSS: n.RSS, CPU: n.CPU, Cmd: n.Cmd, Depth: n.Depth})
			}
		}
		frame.Stats = append(frame.Stats, sv)
	}
	return frame
}

// runMeters keeps the ring filled while nobody is subscribed: every unwatchedGap,
// when something is running. It stands down as soon as a screen subscribes, whose
// own sampling fills the ring faster.
func (d *Daemon) runMeters(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if d.meters.watched() || time.Since(d.meters.lastAt()) < unwatchedGap {
			continue
		}
		running := false
		for _, inst := range d.rt.Registry().Snapshot() {
			if inst.PID != 0 {
				running = true
				break
			}
		}
		if running {
			d.meters.record(d.sampleStats())
		}
	}
}
