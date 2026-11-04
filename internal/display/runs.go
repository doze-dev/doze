package display

import (
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/journal"
)

// A task's runs, as the journal tells them: when, why, how long, how it ended.

// CauseWords says in a few words what made a run happen.
func CauseWords(c journal.Cause) string {
	switch c.Kind {
	case journal.ByDependency:
		return c.By + " needs it"
	case journal.ByCommand:
		return "you asked"
	case journal.ByWatch:
		return c.By + " changed"
	case journal.ByConnection:
		return "a connection"
	case journal.ByRestart:
		return "restart policy"
	}
	return "doze up"
}

// Run is one run of a task: when it started, why, how long it took and
// whether it ended well. Done is false while it is still running.
type Run struct {
	At   time.Time
	Why  string
	Took time.Duration
	OK   bool
	Done bool
}

// RunHistory is a task's runs from the journal, oldest first. A run is a wake
// and what follows it, a ready or a failure. When the journal has kept none,
// the task's own record of its last run (last) stands in; nil when there is
// nothing to say.
func RunHistory(events []journal.Event, name string, last *control.TaskView) []Run {
	var runs []Run
	for _, e := range events {
		if e.Instance != name {
			continue
		}
		switch e.Kind {
		case journal.Wake:
			runs = append(runs, Run{At: e.Time, Why: CauseWords(e.Cause)})
		case journal.Ready:
			if n := len(runs); n > 0 && !runs[n-1].Done {
				runs[n-1].Took, runs[n-1].OK, runs[n-1].Done = e.Took, true, true
			}
		case journal.Failed:
			if n := len(runs); n > 0 && !runs[n-1].Done {
				runs[n-1].Took, runs[n-1].Done = e.Took, true
			}
		}
	}
	if len(runs) == 0 && last != nil && !last.RanAt.IsZero() {
		why := last.Why
		if why == "" {
			why = "doze up"
		}
		runs = []Run{{At: last.RanAt, Why: why, Took: time.Duration(last.TookMS) * time.Millisecond, OK: last.Result == "done", Done: true}}
	}
	return runs
}
