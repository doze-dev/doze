package display

import (
	"fmt"
	"time"

	"github.com/doze-dev/doze/internal/control"
)

// What a surface tells the person about a task they asked for, in its words.

// RunningNote is the header's count of tasks running: "◌ 2 running", or ""
// when none is.
func RunningNote(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("◌ %d running", n)
}

// TaskStarted is said at once when a task is asked for: the daemon runs it
// before it answers, so for a while nothing else on the screen would say so.
func TaskStarted(target string) string { return "running " + target + "…" }

// TaskResult says how a task that has just finished went, from the record the
// snapshot carries, and whether it went badly. "" when there is no record.
func TaskResult(name string, t *control.TaskView) (text string, bad bool) {
	if t == nil {
		return "", false
	}
	took := (time.Duration(t.TookMS) * time.Millisecond).Round(100 * time.Millisecond)
	if t.Result == "failed" {
		return fmt.Sprintf("%s failed (exit %d) after %s", name, t.ExitCode, took), true
	}
	return fmt.Sprintf("%s done in %s", name, took), false
}
