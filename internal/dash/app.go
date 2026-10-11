package dash

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/journal"
)

// Options configure a dash.
type Options struct {
	// Plain turns colour off.
	Plain bool
	// Now is the clock; nil is time.Now. Loc is the zone times are shown in; nil
	// is the local zone. Tests fix both.
	Now func() time.Time
	Loc *time.Location
}

// samples is how many meter readings are kept per instance, for the
// sparklines.
const samples = 60

// Model is the dash: the stack as the daemon last said it, what is selected,
// and the page on top (only Home so far).
type Model struct {
	d    control.Daemon
	st   styles
	snap control.Snapshot
	now  time.Time
	nowf func() time.Time
	loc  *time.Location

	w, h  int
	sel   string
	page  int // pageHome or pageFocus
	tab   int // the focused instance's tab
	level int // the logs tab's minimum level

	logs   []control.LogFrame // the selected instance's, oldest first
	events []journal.Event    // the journal, newest last
	ram    map[string][]float64
	cpu    map[string][]float64
	conns  map[string][]float64
	notice string

	send     func(tea.Msg)
	followed string
	cancel   context.CancelFunc
}

// New builds the dash around a daemon. Init and Run start it listening.
func New(d control.Daemon, o Options) *Model {
	m := &Model{d: d, st: newStyles(o.Plain), nowf: o.Now, loc: o.Loc, ram: map[string][]float64{}, cpu: map[string][]float64{}, conns: map[string][]float64{}, level: lvInfo}
	if m.nowf == nil {
		m.nowf = time.Now
	}
	if m.loc == nil {
		m.loc = time.Local
	}
	m.now = m.nowf()
	return m
}

type (
	snapshotMsg struct {
		snap control.Snapshot
		err  error
	}
	frameMsg control.Frame
	logMsg   struct {
		name  string
		frame control.LogFrame
	}
	doneMsg struct {
		verb, target string
		err          error
	}
	tickMsg time.Time
)

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.snapshot(), tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }))
}

func (m *Model) snapshot() tea.Cmd {
	d := m.d
	return func() tea.Msg {
		s, err := d.Snapshot(context.Background())
		return snapshotMsg{s, err}
	}
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.now = m.nowf()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case snapshotMsg:
		if msg.err == nil {
			m.snap = msg.snap
		}
		m.settle()
	case tickMsg:
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
	case frameMsg:
		m.apply(control.Frame(msg))
		m.settle()
	case logMsg:
		if msg.name == m.sel {
			m.logs = append(m.logs, msg.frame)
			if len(m.logs) > 400 {
				m.logs = m.logs[len(m.logs)-400:]
			}
		}
	case noticeMsg:
		m.notice = string(msg)
	case doneMsg:
		if msg.err != nil {
			m.notice = fmt.Sprintf("%s %s: %v", msg.verb, msg.target, msg.err)
		} else {
			m.notice = ""
		}
		return m, m.snapshot()
	case tea.KeyPressMsg:
		return m.key(msg.String())
	}
	return m, m.follow()
}

// key handles one key on Home.
func (m *Model) key(k string) (tea.Model, tea.Cmd) {
	m.notice = ""
	if m.page == pageFocus {
		if cmd, handled := m.focusKey(k); handled {
			return m, cmd
		}
	}
	switch k {
	case "enter", "l", "right":
		m.open()
	case "ctrl+c", "q":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "home", "g":
		if rs := rows(m.snap); len(rs) > 0 {
			m.sel = rs[0].v.Name
		}
	case "end", "G":
		if rs := rows(m.snap); len(rs) > 0 {
			m.sel = rs[len(rs)-1].v.Name
		}
	case "w":
		return m, m.verb(control.VerbWake)
	case "r":
		return m, m.verb(control.VerbRestart)
	case "s":
		v, ok := m.selected()
		if !ok {
			break
		}
		if users := inUse(v); users != "" {
			// The confirm screen (Confirm-Sleep.dc.html) is not built yet; until it
			// is, nothing is slept out from under what is using it.
			m.notice = v.Name + " is in use by " + users + ": the confirm screen is not built yet"
			break
		}
		return m, m.verb(control.VerbSleep)
	}
	return m, m.follow()
}

// inUse names what is holding v, or "".
func inUse(v control.InstanceView) string {
	var by []string
	for _, h := range v.Holders {
		switch h.Kind {
		case "dependant":
			by = append(by, h.Name)
		case "connection":
			by = append(by, fmt.Sprintf("%d connection(s)", h.Count))
		}
	}
	return strings.Join(by, ", ")
}

func (m *Model) verb(name string) tea.Cmd {
	v, ok := m.selected()
	if !ok {
		return nil
	}
	d, target := m.d, v.Name
	return func() tea.Msg {
		return doneMsg{name, target, d.Do(context.Background(), control.Verb{Name: name, Target: target})}
	}
}

func (m *Model) move(by int) {
	rs := rows(m.snap)
	for i, r := range rs {
		if r.v.Name == m.sel {
			if j := i + by; j >= 0 && j < len(rs) {
				m.sel = rs[j].v.Name
			}
			return
		}
	}
}

// settle keeps a selection that exists.
func (m *Model) settle() {
	if _, ok := m.selected(); ok {
		return
	}
	if rs := rows(m.snap); len(rs) > 0 {
		m.sel = rs[0].v.Name
	}
}

func (m *Model) selected() (control.InstanceView, bool) { return m.find(m.sel) }

func (m *Model) find(name string) (control.InstanceView, bool) {
	for _, v := range m.snap.Instances {
		if v.Name == name {
			return v, true
		}
	}
	return control.InstanceView{}, false
}

// follow streams the selected instance's log lines when the selection changes,
// and stops following the one before.
func (m *Model) follow() tea.Cmd {
	if m.sel == "" || m.sel == m.followed || m.send == nil {
		if m.sel != m.followed {
			m.followed, m.logs = m.sel, nil
		}
		return nil
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.followed, m.logs = m.sel, nil
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	d, send, name := m.d, m.send, m.sel
	go func() {
		_ = d.Subscribe(ctx, control.Subscription{Topics: []string{control.TopicLogs}, Names: []string{name}, Backlog: 60}, func(f control.Frame) error {
			if f.Log != nil {
				send(logMsg{name, *f.Log})
			}
			return nil
		})
	}()
	return nil
}

// apply folds a topic frame into what the dash knows.
func (m *Model) apply(f control.Frame) {
	switch f.Topic {
	case control.TopicInstances:
		if f.Snapshot {
			m.snap.Instances = append([]control.InstanceView(nil), f.Instances...)
		} else {
			for _, v := range f.Instances {
				replaced := false
				for i := range m.snap.Instances {
					if m.snap.Instances[i].Name == v.Name {
						m.snap.Instances[i], replaced = v, true
					}
				}
				if !replaced {
					m.snap.Instances = append(m.snap.Instances, v)
				}
			}
		}
		for _, name := range f.Removed {
			out := m.snap.Instances[:0]
			for _, v := range m.snap.Instances {
				if v.Name != name {
					out = append(out, v)
				}
			}
			m.snap.Instances = out
		}
	case control.TopicStats:
		for _, s := range f.Stats {
			for i := range m.snap.Instances {
				if m.snap.Instances[i].Name == s.Name {
					m.snap.Instances[i].RAM, m.snap.Instances[i].CPU, m.snap.Instances[i].Conns = s.RAM, s.CPU, s.Conns
				}
			}
			m.ram[s.Name] = push(m.ram[s.Name], float64(s.RAM))
			m.cpu[s.Name] = push(m.cpu[s.Name], s.CPU)
			m.conns[s.Name] = push(m.conns[s.Name], float64(s.Conns))
		}
	case control.TopicJournal:
		if f.Event != nil {
			m.events = append(m.events, *f.Event)
			if len(m.events) > 300 {
				m.events = m.events[len(m.events)-300:]
			}
		}
	}
}

func push(vs []float64, v float64) []float64 {
	vs = append(vs, v)
	if len(vs) > samples {
		vs = vs[len(vs)-samples:]
	}
	return vs
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	v := tea.NewView(m.Render(m.w, m.h))
	v.AltScreen = true
	return v
}

// Render draws the dash at w x h: what View draws, and what the tests look at.
func (m *Model) Render(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	if m.page == pageFocus {
		return strings.Join(m.focus(w, h), "\n")
	}
	return strings.Join(m.home(w, h), "\n")
}

// Run opens the dash on a terminal and returns when it is closed.
func Run(ctx context.Context, d control.Daemon, o Options) error {
	m := New(d, o)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	m.send = p.Send
	sctx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		_ = d.Subscribe(sctx, control.Subscription{
			Topics: []string{control.TopicInstances, control.TopicStats, control.TopicJournal},
			Since:  control.JournalFromStart,
		}, func(f control.Frame) error {
			p.Send(frameMsg(f))
			return nil
		})
	}()
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("the dash: %w", err)
	}
	return nil
}

// Plain says colour should be off, from the environment.
func Plain() bool { return os.Getenv("NO_COLOR") != "" }
