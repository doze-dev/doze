// Package overview is the tab that says what an instance is and why it is
// awake: where to reach it, what holds it, what it costs, and what happened to
// it lately.
package overview

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/core"
	"github.com/doze-dev/doze/internal/display"
	"github.com/doze-dev/doze/internal/journal"
)

// samples is how much memory history the sparkline holds.
const samples = 40

// Tab is the overview of one instance.
type Tab struct {
	name   string
	id     string
	events []journal.Event
	mem    []int64
}

// New returns the overview tab for an instance.
func New(name string) *Tab { return &Tab{name: name, id: "overview:" + name} }

// Title implements core.Tab.
func (t *Tab) Title() string { return "Overview" }

// Show implements core.Tab: follow the instance's journal.
func (t *Tab) Show(*core.Context, control.InstanceView) tea.Cmd {
	return func() tea.Msg { return core.Follow{ID: t.id, Topic: "journal", Names: []string{t.name}} }
}

// Hide implements core.Tab.
func (t *Tab) Hide() tea.Cmd { return func() tea.Msg { return core.Unfollow{ID: t.id} } }

// Keys implements core.Tab.
func (t *Tab) Keys() []core.Key { return nil }

// Update implements core.Tab.
func (t *Tab) Update(msg tea.Msg, c *core.Context, inst control.InstanceView) (core.Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case core.EventLine:
		if msg.ID == t.id && msg.Event.Instance == t.name {
			t.events = append(t.events, msg.Event)
			if len(t.events) > 200 {
				t.events = t.events[len(t.events)-200:]
			}
		}
	case core.Refreshed:
		if inst.RAM > 0 {
			t.mem = append(t.mem, inst.RAM)
			if len(t.mem) > samples {
				t.mem = t.mem[len(t.mem)-samples:]
			}
		}
	}
	return t, nil
}

// spark draws values as a row of block glyphs, tallest the largest.
func spark(vs []int64) string {
	if len(vs) == 0 {
		return ""
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	lo, hi := vs[0], vs[0]
	for _, v := range vs {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	var b strings.Builder
	for _, v := range vs {
		i := 0
		if hi > lo {
			i = int((v - lo) * int64(len(blocks)-1) / (hi - lo))
		}
		b.WriteRune(blocks[i])
	}
	return b.String()
}

// View implements core.Tab.
func (t *Tab) View(c *core.Context, inst control.InstanceView, w, h int) string {
	st := c.Styles
	var out []string
	row := func(label, value string) {
		out = append(out, st.Dim.Render(fmt.Sprintf("%-10s", label))+value)
	}

	if inst.Domain != "" {
		row("name", st.Text.Render(inst.Domain))
	}
	if inst.Endpoint != "" {
		row("address", st.Text.Render(inst.Endpoint))
	}
	if inst.Bind != "" && inst.Bind != inst.Endpoint {
		row("bound on", st.Text.Render(inst.Bind))
	}
	if inst.URL != "" {
		row("connect", st.Text.Render(inst.URL))
	}
	if inst.EnvVar != "" {
		row("env", st.Text.Render(inst.EnvVar))
	}
	if inst.Console != "" {
		row("console", st.Text.Render(inst.Console))
	}
	if inst.DataDir != "" {
		row("data", st.Dim.Render(inst.DataDir))
	}
	if inst.Version != "" && inst.Version != "builtin" {
		row("version", st.Text.Render(inst.Version))
	}

	out = append(out, "")
	if inst.State == "active" || inst.State == "idle" {
		out = append(out, st.Dim.Render("awake because"))
		because := false
		for _, hd := range inst.Holders {
			because = true
			switch hd.Kind {
			case "dependant":
				out = append(out, "  "+st.Text.Render("held by "+hd.Name))
			case "connection":
				out = append(out, "  "+st.Text.Render(fmt.Sprintf("%d connection%s", hd.Count, plural(hd.Count))))
			case "pin":
				out = append(out, "  "+st.Text.Render("pinned"))
			default:
				out = append(out, "  "+st.Text.Render(hd.Kind))
			}
		}
		if !because {
			if text, _ := display.Note(inst, c.Snap.IdleTimeout, c.Now); text != "" {
				out = append(out, "  "+st.Text.Render(text))
			} else {
				out = append(out, "  "+st.Dim.Render("nothing: it sleeps when the idle time is up"))
			}
		}
		out = append(out, "")
		vit := fmt.Sprintf("%s  cpu %.0f%%  %d hold%s", humanBytes(inst.RAM), inst.CPU, inst.Conns, plural(inst.Conns))
		out = append(out, st.Dim.Render(fmt.Sprintf("%-10s", "vitals"))+st.Text.Render(vit))
		if len(t.mem) > 1 {
			out = append(out, st.Dim.Render(fmt.Sprintf("%-10s", "memory"))+st.Accent.Render(spark(t.mem)))
		}
	} else {
		out = append(out, st.Dim.Render("asleep — it costs no memory until something wakes it"))
	}

	out = append(out, "", st.Dim.Render("recent"))
	if len(t.events) == 0 {
		out = append(out, "  "+st.Dim.Render("nothing yet"))
	}
	room := h - len(out)
	if room < 1 {
		room = 1
	}
	from := 0
	if len(t.events) > room {
		from = len(t.events) - room
	}
	for _, e := range t.events[from:] {
		line := st.Dim.Render(e.Time.In(c.Loc).Format("15:04:05")) + "  " + st.Text.Render(display.Event(e))
		out = append(out, "  "+line)
	}
	for i := range out {
		out[i] = ansi.Truncate(out[i], w, "…")
	}
	for len(out) < h {
		out = append(out, "")
	}
	return strings.Join(out[:h], "\n")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func humanBytes(b int64) string {
	const mb = 1 << 20
	switch {
	case b <= 0:
		return "0"
	case b >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(b)/(1<<30))
	case b >= mb:
		return fmt.Sprintf("%dM", (b+mb/2)/mb)
	}
	return fmt.Sprintf("%dK", (b+512)/1024)
}

var _ = time.Second
