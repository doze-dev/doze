package fixture

import (
	"sync"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/journal"
)

// The site's recordings (make site-media) are of Larder acted out the way the
// daemon acts: wake starts what a service needs first, each one waking and
// then awake, and the service itself last; a restart brings a crashing
// service back. The plain fixtures change state at once, which a test wants
// and a video does not.

// LarderLive is Larder, acted out.
func LarderLive(now time.Time) *fakedaemon.Fake {
	f := Larder(now)
	act(f, sizes(f), now)
	return f
}

// LarderAsleep is Larder up with every service asleep, as after a night with
// nothing calling: the recording wakes it.
func LarderAsleep(now time.Time) *fakedaemon.Fake {
	f := Larder(now)
	ram := sizes(f)
	for i := range f.Snap.Instances {
		if v := &f.Snap.Instances[i]; v.Kind != "task" {
			v.State, v.RAM, v.CPU, v.Conns, v.Holders, v.IdleSince = "reaped", 0, 0, 0, nil, time.Time{}
		}
	}
	// No meters: nothing has been running to measure.
	frames := f.Frames[:0]
	for _, fr := range f.Frames {
		if fr.Topic != control.TopicStats {
			frames = append(frames, fr)
		}
	}
	f.Frames = frames
	act(f, ram, now)
	return f
}

// LarderLiveCrash is LarderCrash, acted out: Restart brings orders back.
func LarderLiveCrash(now time.Time) *fakedaemon.Fake {
	f := LarderCrash(now)
	act(f, sizes(Larder(now)), now)
	return f
}

// sizes is each instance's memory when awake.
func sizes(f *fakedaemon.Fake) map[string]int64 {
	ram := map[string]int64{}
	for _, v := range f.Snap.Instances {
		ram[v.Name] = v.RAM
	}
	return ram
}

// act sets f's OnDo to act wake and restart out over a few seconds.
func act(f *fakedaemon.Fake, ram map[string]int64, now time.Time) {
	// The journal says each wake and ready, as the daemon's does, at the
	// fixture's now: the page reads a wake with no word as stuck.
	say := func(kind, name string, c journal.Cause, took time.Duration) {
		f.Push(control.Frame{Topic: control.TopicJournal, Event: &journal.Event{Time: now, Kind: kind, Instance: name, Cause: c, Took: took}})
	}
	var mu sync.Mutex // one act at a time, in order
	set := func(name, state string) {
		f.Mutate(func(s *control.Snapshot) {
			for i := range s.Instances {
				if v := &s.Instances[i]; v.Name == name {
					v.State = state
					if state == "active" {
						v.RAM, v.LastError, v.RestartCount, v.NextRestart = ram[name], "", 0, time.Time{}
					}
				}
			}
		})
	}
	f.OnDo = func(verb control.Verb) {
		switch verb.Name {
		case control.VerbWake:
			order := wakeOrder(f.Snap.Instances, verb.Target)
			set(verb.Target, "booting")
			say(journal.Wake, verb.Target, journal.Cause{Kind: journal.ByCommand}, 0)
			go func() {
				mu.Lock()
				defer mu.Unlock()
				for _, n := range order {
					say(journal.Wake, n, journal.Cause{Kind: journal.ByDependency, By: verb.Target}, 0)
					set(n, "booting")
					time.Sleep(450 * time.Millisecond)
					say(journal.Ready, n, journal.Cause{}, 450*time.Millisecond)
					set(n, "active")
				}
				time.Sleep(600 * time.Millisecond)
				say(journal.Ready, verb.Target, journal.Cause{}, 600*time.Millisecond)
				set(verb.Target, "active")
			}()
		case control.VerbRestart:
			say(journal.Wake, verb.Target, journal.Cause{Kind: journal.ByCommand}, 0)
			f.Mutate(func(s *control.Snapshot) {
				for i := range s.Instances {
					if v := &s.Instances[i]; v.Name == verb.Target {
						v.State, v.LastError, v.RestartCount, v.NextRestart = "booting", "", 0, time.Time{}
					}
				}
			})
			go func() {
				time.Sleep(1500 * time.Millisecond)
				say(journal.Ready, verb.Target, journal.Cause{}, 1500*time.Millisecond)
				set(verb.Target, "active")
			}()
		}
	}
}

// wakeOrder is what target needs that is not awake, what each needs before
// it: the order the daemon starts them in. Tasks have run already.
func wakeOrder(in []control.InstanceView, target string) []string {
	by := map[string]control.InstanceView{}
	for _, v := range in {
		by[v.Name] = v
	}
	var order []string
	seen := map[string]bool{target: true}
	var visit func(string)
	visit = func(n string) {
		for _, d := range by[n].Needs {
			v, ok := by[d]
			if !ok || seen[d] || v.Kind == "task" {
				continue
			}
			seen[d] = true
			visit(d)
			if v.State != "active" && v.State != "idle" {
				order = append(order, d)
			}
		}
	}
	visit(target)
	return order
}
