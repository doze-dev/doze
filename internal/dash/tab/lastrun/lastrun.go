// Package lastrun is the tab of a task: how it last ran, what it printed, and
// the runs before it with their reasons.
package lastrun

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/core"
	"github.com/doze-dev/doze/internal/journal"
)

// Tab is the last-run tab of one task.
type Tab struct {
	name   string
	id     string
	output []control.LogFrame
	events []journal.Event
	err    string
}

// New returns the last-run tab for a task.
func New(name string) *Tab { return &Tab{name: name, id: "lastrun:" + name} }

// Title implements core.Tab.
func (t *Tab) Title() string { return "Last run" }

// Show implements core.Tab: read the last run's output, and follow the history.
func (t *Tab) Show(c *core.Context, _ control.InstanceView) tea.Cmd {
	d, name := c.Daemon, t.name
	read := func() tea.Msg {
		lines, err := d.Logs(context.Background(), control.LogQuery{Instance: name, Tail: 200})
		return core.Logs{Instance: name, Lines: lines, Err: err}
	}
	follow := func() tea.Msg { return core.Follow{ID: t.id, Topic: "journal", Names: []string{name}} }
	return tea.Batch(read, follow)
}

// Hide implements core.Tab.
func (t *Tab) Hide() tea.Cmd { return func() tea.Msg { return core.Unfollow{ID: t.id} } }

// Keys implements core.Tab.
func (t *Tab) Keys() []core.Key { return []core.Key{{Keys: "t", Label: "run now"}} }

// Update implements core.Tab.
func (t *Tab) Update(msg tea.Msg, c *core.Context, _ control.InstanceView) (core.Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case core.Logs:
		if msg.Instance == t.name {
			if msg.Err != nil {
				t.err = msg.Err.Error()
			} else {
				t.err, t.output = "", msg.Lines
			}
		}
	case core.EventLine:
		if msg.ID == t.id && msg.Event.Instance == t.name {
			t.events = append(t.events, msg.Event)
		}
	case tea.KeyPressMsg:
		if msg.String() == "t" {
			d, name := c.Daemon, t.name
			return t, func() tea.Msg {
				err := d.Do(context.Background(), control.Verb{Name: control.VerbTask, Target: name})
				return core.Done{Verb: control.VerbTask, Target: name, Err: err}
			}
		}
	}
	return t, nil
}

// View implements core.Tab.
func (t *Tab) View(c *core.Context, inst control.InstanceView, w, h int) string {
	st := c.Styles
	var out []string
	switch {
	case inst.State == "booting":
		out = append(out, st.Cool.Render("◌ running now"))
	case inst.Task == nil:
		out = append(out, st.Dim.Render("it has not run yet"))
	default:
		k := inst.Task
		took := (time.Duration(k.TookMS) * time.Millisecond).Round(time.Millisecond)
		ago := c.Now.Sub(k.RanAt).Round(time.Second)
		if k.Result == "failed" {
			out = append(out, st.Bad.Render(fmt.Sprintf("✕ failed with exit %d", k.ExitCode))+
				st.Dim.Render(fmt.Sprintf(" · %s ago, took %s", ago, took)))
		} else {
			out = append(out, st.Good.Render("✓ done")+st.Dim.Render(fmt.Sprintf(" · %s ago, took %s", ago, took)))
		}
		if k.Why != "" {
			out = append(out, st.Dim.Render("it ran because: ")+st.Text.Render(k.Why))
		}
	}

	out = append(out, "", st.Dim.Render("output"))
	switch {
	case t.err != "":
		out = append(out, "  "+st.Bad.Render(t.err))
	case len(t.output) == 0:
		out = append(out, "  "+st.Dim.Render("nothing printed"))
	}
	histRoom := 0
	if n := len(t.runs()); n > 0 {
		histRoom = min(n, 6) + 2
	}
	room := h - len(out) - histRoom
	if room < 1 {
		room = 1
	}
	lines := t.output
	if len(lines) > room {
		lines = lines[len(lines)-room:]
	}
	for _, l := range lines {
		style := st.Text
		if strings.HasPrefix(l.Stream, "e") {
			style = st.Warn
		}
		out = append(out, "  "+style.Render(l.Line))
	}

	if runs := t.runs(); len(runs) > 0 {
		out = append(out, "", st.Dim.Render("history"))
		if len(runs) > 6 {
			runs = runs[len(runs)-6:]
		}
		for _, r := range runs {
			out = append(out, "  "+st.Dim.Render(r.at.In(c.Loc).Format("Jan 02 15:04"))+"  "+r.text)
		}
	}
	for i := range out {
		out[i] = ansi.Truncate(out[i], w, "…")
	}
	for len(out) < h {
		out = append(out, "")
	}
	return strings.Join(out[:h], "\n")
}

type run struct {
	at   time.Time
	text string
}

// runs pairs each wake with how it ended: the reason it ran and the result.
func (t *Tab) runs() []run {
	var out []run
	var why string
	var at time.Time
	for _, e := range t.events {
		switch e.Kind {
		case journal.Wake:
			why, at = e.Detail, e.Time
		case journal.Ready:
			out = append(out, run{at, fmt.Sprintf("done in %s — %s", e.Took.Round(time.Millisecond), why)})
		case journal.Failed:
			out = append(out, run{at, fmt.Sprintf("failed in %s — %s", e.Took.Round(time.Millisecond), why)})
		}
	}
	return out
}
