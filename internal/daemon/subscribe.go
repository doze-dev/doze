package daemon

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/logstore"
	"github.com/doze-dev/doze/internal/ui"
)

const (
	// instancesMinGap spaces instances frames out, so a burst of connections
	// becomes a few frames instead of one per connection.
	instancesMinGap = 50 * time.Millisecond
	defaultStatsGap = time.Second
	minStatsGap     = 250 * time.Millisecond
)

// Subscribe streams the topics sub names until ctx is done or emit fails. Each
// topic runs on its own goroutine and only for as long as the subscription: a
// daemon with no subscribers samples nothing and follows nothing.
func (h *handler) Subscribe(ctx context.Context, sub control.Subscription, emit func(control.Frame) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
	)
	send := func(f control.Frame) error {
		mu.Lock()
		defer mu.Unlock()
		if firstErr != nil {
			return firstErr
		}
		if err := emit(f); err != nil {
			firstErr = err
			cancel()
		}
		return firstErr
	}
	topics := map[string]func(){
		control.TopicInstances: func() { h.subInstances(ctx, send) },
		control.TopicStats:     func() { h.subStats(ctx, sub.Interval, send) },
		control.TopicJournal:   func() { h.subJournal(ctx, sub.Since, send) },
		control.TopicLogs:      func() { h.subLogs(ctx, sub.Names, sub.Backlog, send) },
	}
	for _, t := range sub.Topics {
		run, ok := topics[t]
		if !ok {
			return fmt.Errorf("subscribe: no topic called %q (want instances, stats, journal or logs)", t)
		}
		wg.Add(1)
		go func() { defer wg.Done(); run() }()
	}
	wg.Wait()
	return firstErr
}

// subInstances sends every instance, then each one again whenever it changes.
// It is woken by the registry and compares against what it last sent, so it
// cannot miss a change: a subscriber that falls behind gets the current state,
// not a backlog.
func (h *handler) subInstances(ctx context.Context, send func(control.Frame) error) {
	wake, stop := h.d.rt.Registry().Notify()
	defer stop()
	last := map[string]control.InstanceView{}
	first := true
	for {
		now := h.snapshot()
		frame := control.Frame{Topic: control.TopicInstances, Snapshot: first}
		present := map[string]bool{}
		for _, v := range now {
			present[v.Name] = true
			if prev, ok := last[v.Name]; first || !ok || !reflect.DeepEqual(prev, v) {
				frame.Instances = append(frame.Instances, v)
				last[v.Name] = v
			}
		}
		for name := range last {
			if !present[name] {
				frame.Removed = append(frame.Removed, name)
				delete(last, name)
			}
		}
		if first || len(frame.Instances) > 0 || len(frame.Removed) > 0 {
			if send(frame) != nil {
				return
			}
		}
		first = false
		select {
		case <-ctx.Done():
			return
		case <-wake:
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(instancesMinGap):
		}
	}
}

// subStats samples every running instance's CPU and memory on an interval.
// This is the only place the daemon runs ps on a timer, and it runs only here.
func (h *handler) subStats(ctx context.Context, gap time.Duration, send func(control.Frame) error) {
	if gap <= 0 {
		gap = defaultStatsGap
	}
	if gap < minStatsGap {
		gap = minStatsGap
	}
	ticker := time.NewTicker(gap)
	defer ticker.Stop()
	for {
		var pids []int
		running := h.d.rt.Registry().Snapshot()
		for _, inst := range running {
			if inst.PID != 0 {
				pids = append(pids, inst.PID)
			}
		}
		frame := control.Frame{Topic: control.TopicStats}
		if len(pids) > 0 {
			stats := ui.ProcStats(pids)
			tree := ui.ProcTree(pids)
			for _, inst := range running {
				if inst.PID == 0 {
					continue
				}
				st := stats[inst.PID]
				sv := control.StatView{Name: inst.Name, PID: inst.PID, RAM: st.RSS, CPU: st.CPU, Conns: inst.Conns}
				if decl := h.d.cfg.Lookup(inst.Name); decl != nil && decl.Type == config.AppEngine {
					for _, n := range tree[inst.PID] {
						sv.Children = append(sv.Children, control.ProcView{PID: n.PID, RSS: n.RSS, CPU: n.CPU, Cmd: n.Cmd, Depth: n.Depth})
					}
				}
				frame.Stats = append(frame.Stats, sv)
			}
		}
		if send(frame) != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// subJournal sends the journal from since on: what is on disk, then each event
// as it is recorded.
func (h *handler) subJournal(ctx context.Context, since uint64, send func(control.Frame) error) {
	j := h.d.rt.Journal()
	if j == nil {
		return
	}
	live := j.Subscribe(ctx, 256)
	var last uint64
	if since != 0 {
		if since == control.JournalFromStart {
			since = 0
		}
		past, err := j.Since(since)
		if err != nil {
			return
		}
		for i := range past {
			if send(control.Frame{Topic: control.TopicJournal, Event: &past[i]}) != nil {
				return
			}
			last = past[i].Seq
		}
	}
	for e := range live {
		if e.Seq <= last {
			continue // already sent from disk
		}
		if last != 0 && e.Seq > last+1 {
			// This subscriber fell behind the live feed; the gap is on disk.
			missed, err := j.Since(last)
			if err != nil {
				return
			}
			for i := range missed {
				if missed[i].Seq >= e.Seq {
					break
				}
				if send(control.Frame{Topic: control.TopicJournal, Event: &missed[i]}) != nil {
					return
				}
			}
		}
		if send(control.Frame{Topic: control.TopicJournal, Event: &e}) != nil {
			return
		}
		last = e.Seq
	}
}

// subLogs sends the recent lines of each named instance and then follows it.
func (h *handler) subLogs(ctx context.Context, names []string, backlog int, send func(control.Frame) error) {
	if len(names) == 0 {
		for _, d := range h.d.cfg.Instances {
			names = append(names, d.Name)
		}
	}
	if backlog == 0 {
		backlog = logBacklog
	}
	store := h.d.rt.Logs()
	emit := func(l logstore.Line) error {
		return send(control.Frame{Topic: control.TopicLogs, Log: &control.LogFrame{
			Instance: l.Instance, Line: l.Text, Stream: l.Stream, Run: l.Run, Time: l.Time,
		}})
	}
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var from logstore.Cursor
			if backlog > 0 {
				lines, end, err := store.Tail(name, 0, backlog)
				if err != nil {
					return
				}
				for _, l := range lines {
					if emit(l) != nil {
						return
					}
				}
				from = end
			} else if _, end, err := store.Tail(name, 0, 0); err == nil {
				from = end
			}
			_ = store.Follow(ctx, name, from, emit)
		}()
	}
	wg.Wait()
}
