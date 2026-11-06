// Package notify decides when the app says something outside its window
// (design D8): only for a crash, a failed task, or an instance that did not
// become ready, once per episode, and never for what is on screen. It is
// pure: the app hands it what the hub shows and gets back what to say.
package notify

import (
	"fmt"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// Note is one notification: what to say, and where its Show should go.
type Note struct {
	// ID is one per episode, so a repeat replaces rather than piles up.
	ID       string
	Key      string // the stack
	Dir      string
	Instance string
	Title    string // "api crashed · shop"
	Body     string // the error, or the task's result
	// Sub is the line under the body: "try 3 of 5 · next in 4s".
	Sub string
}

// Rules holds which episodes were already said.
type Rules struct {
	crashed map[string]bool   // stack/instance → in a crash episode
	ran     map[string]string // stack/task → the last run reported
	// primed marks the stacks looked at once: what a stack already shows
	// when it is first seen is not news, whenever it appears.
	primed map[string]bool
}

// New makes rules that say nothing about what is already the case when
// they first look at a stack: a crash that was there before the app opened
// is on screen, not news.
func New() *Rules {
	return &Rules{crashed: map[string]bool{}, ran: map[string]string{}, primed: map[string]bool{}}
}

// episodeOver says a crash episode has ended: the instance is up, dozing
// or asleep. Between the tries of a crash loop it boots again, which is
// the same episode, as the Crash board's "try 3 of 5" says.
func episodeOver(v control.InstanceView) bool {
	switch display.LookOf(v).Word {
	case "awake", "dozing", "asleep", "off", "paused":
		return true
	}
	return false
}

// Next looks at the stacks and returns what to say now. visible says an
// instance is on screen, so nothing is said about it; the episode is still
// marked as seen. now is for the words.
func (r *Rules) Next(stacks []hub.StackView, visible func(key, instance string) bool, now time.Time) []Note {
	var out []Note
	seen := map[string]bool{}
	for _, st := range stacks {
		primed := r.primed[st.Key]
		for _, v := range st.Instances {
			id := st.Key + "/" + v.Name
			seen[id] = true
			if v.Kind == "task" {
				r.task(&out, st, v, id, visible, primed)
				continue
			}
			if episodeOver(v) {
				delete(r.crashed, id)
				continue
			}
			if !display.Troubled(v) || r.crashed[id] {
				continue
			}
			r.crashed[id] = true
			if !primed || visible(st.Key, v.Name) {
				continue
			}
			n := Note{ID: id + "/crash", Key: st.Key, Dir: st.Dir, Instance: v.Name, Body: display.FirstLine(v.LastError)}
			switch {
			case strings.Contains(v.LastError, "not become ready"):
				// The third of D8's three: it started but never answered.
				n.Title = v.Name + " " + display.FirstLine(v.LastError) + " · " + st.Name
				n.Body = "its logs usually say why"
				n.Sub = "ready check in doze.hcl"
			case v.State == "crashing":
				n.Title = v.Name + " crashed · " + st.Name
				if v.RestartMax > 0 {
					n.Sub = fmt.Sprintf("try %d of %d", v.RestartCount, v.RestartMax)
					if !v.NextRestart.IsZero() && v.NextRestart.After(now) {
						n.Sub += fmt.Sprintf(" · next in %ds", int(v.NextRestart.Sub(now).Seconds()+0.5))
					}
				}
			default:
				n.Title = v.Name + " failed · " + st.Name
				n.Sub = "it stopped and waits for you"
			}
			if n.Body == "" {
				n.Body = "no error text; its logs usually say why"
			}
			out = append(out, n)
		}
		r.primed[st.Key] = true
	}
	// Instances that are gone end their episodes.
	for id := range r.crashed {
		if !seen[id] {
			delete(r.crashed, id)
		}
	}
	return out
}

func (r *Rules) task(out *[]Note, st hub.StackView, v control.InstanceView, id string, visible func(string, string) bool, primed bool) {
	if v.Task == nil {
		return
	}
	run := v.Task.RanAt.UTC().Format(time.RFC3339Nano) + "/" + v.Task.Result
	prev, known := r.ran[id]
	r.ran[id] = run
	if !primed || !known || prev == run || v.Task.Result != "failed" || visible(st.Key, v.Name) {
		return
	}
	result, _ := display.TaskResult(v.Name, v.Task)
	title := result + " · " + st.Name
	*out = append(*out, Note{
		ID: id + "/" + run, Key: st.Key, Dir: st.Dir, Instance: v.Name,
		Title: title,
		Body:  fmt.Sprintf("task %q failed: exit %d; its last lines are on its page", v.Name, v.Task.ExitCode),
		Sub:   "a task failed; nothing retries it",
	})
}
