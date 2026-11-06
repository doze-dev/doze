package view

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
	"github.com/doze-dev/doze/internal/journal"
)

// Away is what happened while the window was closed (the WhileAway
// board): crashes, failed starts and task runs, with how each stands now.
// Routine wakes and sleeps are not news. Nil when nothing happened.
type Away struct {
	// For is how long the window was closed: "3 h 12 m".
	For    string      `json:"for"`
	Events []AwayEvent `json:"events"`
}

// AwayEvent is one line: who, what, when, and how it is now.
type AwayEvent struct {
	Key      string `json:"key"`
	Stack    string `json:"stack"`
	Instance string `json:"instance"`
	// At is the times, "13:02, 13:40"; What the rest of the line after the
	// name: "crashed twice in shop and started again".
	At   string `json:"at"`
	What string `json:"what"`
	// Now is how it stands, Tone its colour (bad, good, dim): "still
	// failing · in Needs you", "running", "done".
	Now  string `json:"now"`
	Tone string `json:"tone"`
	when time.Time
}

// AwayOf reads the stacks' journals and task records since the window
// closed.
func AwayOf(stacks []hub.StackView, since, now time.Time) *Away {
	if since.IsZero() || now.Sub(since) < 30*time.Second {
		return nil
	}
	var out []AwayEvent
	for _, st := range stacks {
		name := st.Name
		if name == "" {
			name = baseName(st.Dir)
		}
		byInst := map[string][]journal.Event{}
		for _, e := range st.Events {
			if e.Time.Before(since) {
				continue
			}
			switch e.Kind {
			case journal.Crash, journal.Failed, journal.GiveUp:
				byInst[e.Instance] = append(byInst[e.Instance], e)
			}
		}
		snap := Snapshot(st)
		for inst, es := range byInst {
			v, ok := display.Find(snap, inst)
			troubled := ok && display.Troubled(v)
			crashes, failed := 0, false
			var times []string
			for _, e := range es {
				switch e.Kind {
				case journal.Crash:
					crashes++
				default:
					failed = true
				}
				if len(times) < 3 {
					times = append(times, e.Time.Local().Format("15:04"))
				}
			}
			ev := AwayEvent{Key: st.Key, Stack: name, Instance: inst, At: strings.Join(times, ", "), when: es[len(es)-1].Time}
			switch {
			case failed && crashes == 0:
				ev.What = "did not start in " + name
			case crashes == 1:
				ev.What = "crashed in " + name
			case crashes == 2:
				ev.What = "crashed twice in " + name
			default:
				ev.What = fmt.Sprintf("crashed %d times in %s", crashes, name)
			}
			if troubled {
				ev.Now, ev.Tone = "still failing · in Needs you", "bad"
			} else {
				if crashes > 0 {
					ev.What += " and started again"
				}
				ev.Now, ev.Tone = "running", "good"
				if ok && !display.AwakeNow(v) {
					ev.Now, ev.Tone = display.LookOf(v).Word, "dim"
				}
			}
			out = append(out, ev)
		}
		for _, v := range st.Instances {
			if v.Kind != "task" || v.Task == nil || v.Task.RanAt.Before(since) {
				continue
			}
			ev := AwayEvent{Key: st.Key, Stack: name, Instance: v.Name, At: v.Task.RanAt.Local().Format("15:04"), when: v.Task.RanAt}
			took := (time.Duration(v.Task.TookMS) * time.Millisecond).Round(100 * time.Millisecond)
			if v.Task.Result == "failed" {
				ev.What = fmt.Sprintf("failed in %s · exit %d", name, v.Task.ExitCode)
				ev.Now, ev.Tone = "still failing · in Needs you", "bad"
			} else {
				ev.What = fmt.Sprintf("ran in %s · exit 0 in %s", name, took)
				ev.Now, ev.Tone = "done", "good"
			}
			out = append(out, ev)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].when.After(out[j].when) })
	if len(out) > 6 {
		out = out[:6]
	}
	return &Away{For: span(now.Sub(since)), Events: out}
}

// span is a duration as a person says it: "40 m", "3 h 12 m", "2 days".
func span(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d m", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) - h*60
		if m == 0 {
			return fmt.Sprintf("%d h", h)
		}
		return fmt.Sprintf("%d h %d m", h, m)
	case d < 48*time.Hour:
		return "a day"
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}
