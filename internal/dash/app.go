package dash

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

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
	// Glyphs is the set of characters drawn with: GlyphsUnicode (the default),
	// GlyphsNarrow or GlyphsASCII. DetectGlyphs chooses from the environment.
	Glyphs string
}

// samples is how many meter readings are kept per instance, for the
// sparklines.
const samples = 240

// logsKept is how many lines of each instance the dash holds, and how many it
// asks the daemon for when it opens.
const logsKept = 400

// holdFor is how long Home's order stays still after a key or a click.
const holdFor = 3 * time.Second

// Model is the dash: the stack as the daemon last said it, what is selected,
// and the page on top (only Home so far).
type Model struct {
	d    control.Daemon
	st   styles
	snap control.Snapshot
	now  time.Time
	nowf func() time.Time
	loc  *time.Location

	w, h         int
	sel          string
	page         int                // pageHome or pageFocus
	hit          []string           // the instance on each line of the stack, "" for a heading
	listTop      int                // the first line of the stack shown, when it is longer than its box
	tableNote    string             // what the table's scrolling has put out of sight
	sortBy       int                // o: how the table orders its rows within a group
	tree         bool               // t: the table as a dependency tree
	vitalsOn     bool               // the vitals are drawn across the top of this page
	pinned       bool               // the logs are scrolled back and hold still
	pinTop       int                // the first line shown, when pinned
	page1        int                // how many lines the logs showed last, for paging
	search       string             // /: on a logs tab, only the lines that hold it
	all          []control.LogFrame // the stack's lines, for the logs page
	allCancel    context.CancelFunc
	paused       bool     // the logs page holds still
	timeRel      bool     // t: log times as how long ago, not the clock
	shown        []string // the lines of the logs the last frame drew
	selecting    bool     // v: some of them are selected, to copy
	dragging     bool
	selA, selB   int
	frozen       []string // what was shown when the selection began
	searching    bool     // the search is being typed
	runSel       int      // [ ]: the run of the logs tab, 0 for all of them
	query        string   // the search while the logs tab is drawing
	filter       string   // /: only the rows that match
	filtering    bool     // the filter is being typed
	hitY0, hitX1 int      // where the stack's lines begin, and the column it ends at
	clickName    string   // the last row clicked, and when: a second click opens it
	clickAt      time.Time
	modal        *modal        // the palette or a confirmation, over the page
	con          *consoleState // the focused instance's console, when it has one
	currentTabID string        // the tab being drawn, for blocks that look up another
	tab          int           // the focused instance's tab
	level        int           // the logs tab's minimum level

	logs      []control.LogFrame // the selected instance's, oldest first
	events    []journal.Event    // the journal, newest last
	ram       map[string][]float64
	cpu       map[string][]float64
	conns     map[string][]float64
	notice    string
	noticeBad bool
	glyphs    glyphSet

	// holdUntil keeps Home's order still for a moment after the person acted, so
	// a row sorted by memory does not move out from under a click; held is the
	// order it was in, and heldBy the sort it was made for.
	holdUntil time.Time
	held      []string
	heldBy    int

	// known are the instances the dash has seen, so one added while it is open
	// can have its logs asked for; resubLogs asks again.
	known     map[string]bool
	resubLogs func()
	atTop     bool // the logs are scrolled to the oldest line the dash holds
	mouseTold bool // the shift-drag hint has been said
	noticeAt  time.Time
	history   []message

	conn      connState // whether the daemon is being heard
	waiting   bool      // no instances frame or snapshot yet: the stack is not known
	lastFrame time.Time
	downErr   string
	retryAt   time.Time

	send      func(tea.Msg)
	followed  string
	statAt    map[string][]time.Time        // when each instance's readings were taken, when the daemon said
	chartInst string                        // whose readings the charts being drawn are, to find their times
	cache     map[string][]control.LogFrame // every instance's kept lines, by name
	cancel    context.CancelFunc
}

// New builds the dash around a daemon. Init and Run start it listening.
func New(d control.Daemon, o Options) *Model {
	m := &Model{d: d, st: newStyles(o.Plain), nowf: o.Now, loc: o.Loc, ram: map[string][]float64{}, cpu: map[string][]float64{}, conns: map[string][]float64{}, level: lvInfo, glyphs: parseGlyphs(o.Glyphs)}
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
	defer m.syncConsole()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case connMsg:
		if msg.up {
			was := m.conn
			m.conn, m.lastFrame, m.downErr = connLive, m.now, ""
			if was == connDown {
				// what happened while it was gone is not known: ask again, and
				// have the console follow again
				m.subscribeConsole()
				return m, m.snapshot()
			}
		} else {
			m.conn, m.downErr, m.retryAt = connDown, humanise(msg.err), m.now.Add(msg.retry)
		}
	case snapshotMsg:
		if msg.err == nil {
			m.waiting = false
			m.snap = msg.snap
			for i := range m.snap.Instances {
				m.snap.Instances[i] = cleanView(m.snap.Instances[i])
			}
		}
		m.settle()
	case tickMsg:
		m.fadeNotice()
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
	case frameMsg:
		m.lastFrame = m.now
		if msg.Topic == control.TopicInstances {
			m.waiting = false
		}
		m.apply(control.Frame(msg))
		m.settle()
	case logMsg:
		msg.frame.Line = clean(msg.frame.Line)
		if m.cache == nil {
			m.cache = map[string][]control.LogFrame{}
		}
		if m.seenLine(msg.name, msg.frame) {
			break // the daemon said it again after a reconnect
		}
		c := append(m.cache[msg.name], msg.frame)
		if len(c) > logsKept {
			c = c[len(c)-logsKept:]
		}
		m.cache[msg.name] = c
		if msg.name == m.sel {
			m.logs = c
		}
		if m.page == pageLogs && !m.paused {
			m.all = append(m.all, msg.frame)
			if len(m.all) > allKept {
				m.all = m.all[len(m.all)-allKept:]
			}
		}
	case noticeMsg:
		m.say(string(msg))
	case failMsg:
		m.fail(string(msg))
	case consoleMsg:
		m.applyConsole(msg)
	case doneMsg:
		if msg.err != nil {
			m.fail(fmt.Sprintf("%s %s failed: %s", msg.verb, msg.target, humanise(msg.err)))
		} else {
			m.clearNotice()
		}
		return m, m.snapshot()
	case tea.KeyPressMsg:
		return m.keyText(msg.String(), msg.Text)
	case tea.MouseClickMsg:
		m.holdUntil = m.now.Add(holdFor)
		m.tellMouse()
		if cmd, ok := m.logMouse(msg); ok {
			return m, cmd
		}
		m.click(tea.Mouse(msg))
	case tea.MouseMotionMsg, tea.MouseReleaseMsg:
		if cmd, ok := m.logMouse(msg); ok {
			return m, cmd
		}
	case tea.MouseWheelMsg:
		m.tellMouse()
		if m.modal == nil {
			btn := tea.Mouse(msg).Button
			switch {
			case m.page == pageHome && btn == tea.MouseWheelUp:
				m.move(-1)
			case m.page == pageHome && btn == tea.MouseWheelDown:
				m.move(1)
			case m.scrollable() && btn == tea.MouseWheelUp:
				m.scrollBy(-3)
			case m.scrollable() && btn == tea.MouseWheelDown:
				m.scrollBy(3)
			}
		}
	}
	return m, m.follow()
}

// key handles one key on Home.
func (m *Model) key(k string) (tea.Model, tea.Cmd) { return m.keyText(k, "") }

// keyText handles a key; text is what it types, for the palette and the reset
// confirmation.
func (m *Model) keyText(k, text string) (tea.Model, tea.Cmd) {
	m.holdUntil = m.now.Add(holdFor)
	if m.noticeBad {
		if k == "esc" && m.modal == nil {
			m.clearNotice()
			return m, nil
		}
	} else {
		m.clearNotice()
	}
	if m.modal != nil {
		return m, m.modalKey(k, text)
	}
	if m.selecting && (m.page == pageFocus || m.page == pageLogs) {
		return m, m.selectKey(k)
	}
	if m.page == pageLogs {
		if m.searching || m.search != "" && k == "esc" {
			switch k {
			case "esc":
				m.search, m.searching = "", false
			case "enter":
				m.searching = false
			case "backspace":
				if r := []rune(m.search); len(r) > 0 {
					m.search = string(r[:len(r)-1])
				}
			default:
				if text != "" && k != "ctrl+c" {
					m.search += text
				}
			}
			return m, nil
		}
		if k == "ctrl+c" {
			return m, tea.Quit
		}
		cmd, _ := m.stackLogsKey(k)
		return m, cmd
	}
	if m.page == pageFocus && m.searching {
		switch k {
		case "esc":
			m.search, m.searching = "", false
		case "enter":
			m.searching = false
		case "backspace":
			if r := []rune(m.search); len(r) > 0 {
				m.search = string(r[:len(r)-1])
			}
		default:
			if text != "" && k != "ctrl+c" {
				m.search += text
			}
		}
		return m, m.follow()
	}
	if m.page == pageFocus && k == "esc" && m.search != "" {
		m.search = ""
		return m, m.follow()
	}
	if m.filtering && m.page == pageHome {
		switch k {
		case "esc":
			m.filter, m.filtering = "", false
		case "enter":
			m.filtering = false
		case "backspace":
			if r := []rune(m.filter); len(r) > 0 {
				m.filter = string(r[:len(r)-1])
			}
		case "up", "down":
			if k == "up" {
				m.move(-1)
			} else {
				m.move(1)
			}
		default:
			if text != "" && k != "ctrl+c" {
				m.filter += text
			}
		}
		return m, m.follow()
	}
	if m.page == pageHome && k == "esc" && m.filter != "" {
		m.filter = ""
		return m, m.follow()
	}
	if m.page == pageFocus {
		if cmd, handled := m.focusKey(k); handled {
			return m, cmd
		}
	}
	switch k {
	case "o":
		m.sortBy = (m.sortBy + 1) % sortModes
	case "y", "Y":
		if v, ok := m.selected(); ok {
			return m, m.yankURL(v, k == "Y")
		}
	case "t":
		m.tree = !m.tree
	case "/":
		m.filtering = true
	case "enter", "right":
		m.open()
	case "l":
		m.openInstanceLogs()
	case "L":
		m.openLogs()
	case "?":
		m.modal = &modal{kind: modalHelp}
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "home", "g":
		if rs := m.ordered(); len(rs) > 0 {
			m.sel = rs[0].v.Name
		}
	case "end", "G":
		if rs := m.ordered(); len(rs) > 0 {
			m.sel = rs[len(rs)-1].v.Name
		}
	case "w":
		return m, m.verb(control.VerbWake)
	case "r":
		if v, ok := m.selected(); ok {
			return m, m.askRestart(v.Name)
		}
	case "s":
		if v, ok := m.selected(); ok {
			return m, m.askSleep(v.Name)
		}
	case ":":
		m.modal = &modal{kind: modalPalette}
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
	if c := m.refuse(); c != nil {
		return c
	}
	d, target := m.d, v.Name
	return func() tea.Msg {
		return doneMsg{name, target, d.Do(context.Background(), control.Verb{Name: name, Target: target})}
	}
}

func (m *Model) move(by int) {
	rs := m.ordered()
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
	if m.sel != m.followed {
		// the lines of the instance chosen are the ones already kept: nothing is
		// asked for again, and nothing is lost by looking at another
		m.followed = m.sel
		m.logs = m.cache[m.sel]
	}
	return nil
}

// apply folds a topic frame into what the dash knows.
func (m *Model) apply(f control.Frame) {
	switch f.Topic {
	case control.TopicInstances:
		if f.Snapshot {
			m.snap.Instances = append([]control.InstanceView(nil), f.Instances...)
			for i := range m.snap.Instances {
				m.snap.Instances[i] = cleanView(m.snap.Instances[i])
			}
		} else {
			for _, v := range f.Instances {
				v = cleanView(v)
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
		m.noteNew()
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
			if at := m.statAt[s.Name]; !f.At.IsZero() && len(at) > 0 && !f.At.After(at[len(at)-1]) {
				continue // a reading already held: the history is sent again on a reconnect
			}
			for i := range m.snap.Instances {
				if m.snap.Instances[i].Name == s.Name {
					m.snap.Instances[i].RAM, m.snap.Instances[i].CPU, m.snap.Instances[i].Conns = s.RAM, s.CPU, s.Conns
				}
			}
			if f.At.IsZero() {
				delete(m.statAt, s.Name) // not stamped: the charts assume a second apart
			} else {
				if m.statAt == nil {
					m.statAt = map[string][]time.Time{}
				}
				at := append(m.statAt[s.Name], f.At)
				if len(at) > samples {
					at = at[len(at)-samples:]
				}
				m.statAt[s.Name] = at
			}
			m.ram[s.Name] = push(m.ram[s.Name], float64(s.RAM))
			m.cpu[s.Name] = push(m.cpu[s.Name], s.CPU)
			m.conns[s.Name] = push(m.conns[s.Name], float64(s.Conns))
		}
	case control.TopicJournal:
		if f.Event != nil {
			m.events = append(m.events, cleanEvent(*f.Event))
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
	v.MouseMode = tea.MouseModeCellMotion
	if !m.st.plain {
		// The palette is drawn for this ground. Without it the dash is the board's
		// colours on whatever the terminal's theme is, and the faint and the
		// selection read wrongly.
		v.BackgroundColor = lipgloss.Color(hexBg)
		v.ForegroundColor = lipgloss.Color(hexText)
	}
	return v
}

// Render draws the dash at w x h: what View draws, and what the tests look at.
func (m *Model) Render(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	page := m.home
	switch m.page {
	case pageFocus:
		page = m.focus
	case pageLogs:
		page = m.stackLogs
	}
	if w < minW || h < minH {
		return substitute(tooSmall(m.st, w, h), m.glyphs)
	}
	base := page(w, h)
	if m.modal != nil {
		saved := m.st
		m.st = fadedStyles(saved.plain)
		base = page(w, h)
		m.st = saved
	}
	if ok, banner := m.health(); !ok {
		// the picture is old or not yet there: faded, and said at the top right
		if m.modal == nil {
			saved := m.st
			m.st = fadedStyles(saved.plain)
			base = page(w, h)
			m.st = saved
		}
		if len(base) > 0 {
			b := m.st.pillY.Render(banner)
			if m.conn == connConnecting {
				b = m.st.pillC.Render(banner)
			}
			left := ansi.Truncate(base[0], max(w-width(b)-1, 0), "")
			base[0] = left + strings.Repeat(" ", max(w-width(left)-width(b), 1)) + b
		}
	}
	lines := m.withModal(base, w, h)
	if len(lines) > h {
		// a page that came out too tall loses its middle, not its header and keys
		keep := h - 1
		lines = append(lines[:max(keep, 0)], lines[len(lines)-1])
	}
	for i, l := range lines {
		if width(l) > w {
			lines[i] = ansi.Truncate(l, w, "…")
		}
	}
	return substitute(strings.Join(lines, "\n"), m.glyphs)
}

// Run opens the dash on a terminal and returns when it is closed.
func Run(ctx context.Context, d control.Daemon, o Options) error {
	m := New(d, o)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	m.send = p.Send
	sctx, stop := context.WithCancel(ctx)
	defer stop()
	m.conn, m.waiting = connConnecting, true
	var lastSeq uint64
	go keepSubscribed(sctx, d, func() control.Subscription {
		// the journal resumes where it was, not from the start again
		since := control.JournalFromStart
		if lastSeq > 0 {
			since = lastSeq
		}
		return control.Subscription{
			Topics:   []string{control.TopicInstances, control.TopicStats, control.TopicJournal},
			Interval: time.Second, // a reading a second: the charts' time axis is built on it
			Since:    since,
		}
	}, p.Send, func(f control.Frame) {
		if f.Event != nil && f.Event.Seq > lastSeq {
			lastSeq = f.Event.Seq
		}
		p.Send(frameMsg(f))
	}, true)
	// Every instance's lines, once, for as long as the dash is open: moving from
	// one instance to another and back finds what it left, and the stack's logs
	// are these same lines merged.
	// The daemon names the instances when a logs subscription opens, so one added
	// later needs it opened again; what is replayed is dropped as already held.
	var logMu sync.Mutex
	var stopLogs context.CancelFunc
	startLogs := func() {
		logMu.Lock()
		defer logMu.Unlock()
		if stopLogs != nil {
			stopLogs()
		}
		var lctx context.Context
		lctx, stopLogs = context.WithCancel(sctx)
		go keepSubscribed(lctx, d, func() control.Subscription {
			return control.Subscription{Topics: []string{control.TopicLogs}, Backlog: logsKept}
		}, p.Send, func(f control.Frame) {
			if f.Log != nil {
				p.Send(logMsg{f.Log.Instance, *f.Log})
			}
		}, false)
	}
	m.resubLogs = startLogs
	startLogs()
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("the dash: %w", err)
	}
	return nil
}

// Plain says colour should be off, from the environment.
func Plain() bool { return os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" }

func contextBackground() context.Context { return context.Background() }
