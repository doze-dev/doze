// Package tui is doze's live control room: an mprocs-style split view with an
// instance sidebar on the left and, on the right, the selected instance's
// telemetry (state, CPU, a RAM/connection trace, a reap countdown) above its
// streaming logs. It refreshes continuously so the picture is always live.
package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/ui"
)

// ── palette / themes ────────────────────────────────────────────────────────
// The look is a dark "console": a near-neutral slate canvas lifted by one vivid
// accent, with bright state marks on small glyphs/badges and a faint accent tint
// in the chrome (borders, selection). Color stays off the large fills so it never
// reads as noisy. Themes vary the accent + chrome; cycle them with `t` (persisted
// under the doze home). The state colors (active/idle/booting/error) stay constant
// across themes so status always reads the same.
type theme struct {
	name                                        string
	accent, text, dim, faint, panel, sel, selFg lipgloss.Color
	green, gold, cyan, red                      lipgloss.Color
}

var themes = []theme{
	{"violet", "#BD93F9", "#E2E4EE", "#7C8290", "#454C5A", "#3B3550", "#2A2440", "#1C1726", "#7EE787", "#F2C879", "#82AAFF", "#FF7A93"},
	{"emerald", "#5EE0A0", "#E1EBE6", "#7B877F", "#46524C", "#2E4A40", "#1F3A30", "#0E2018", "#7EE787", "#F2C879", "#82AAFF", "#FF7A93"},
	{"amber", "#F2B765", "#ECE7DF", "#888076", "#4F4A40", "#4A3F2C", "#3A2F1E", "#241B0E", "#7EE787", "#F2C879", "#82AAFF", "#FF7A93"},
	{"cyan", "#56D4E0", "#DEEBEC", "#78878A", "#45525A", "#2E474C", "#1F373C", "#0E2024", "#7EE787", "#F2C879", "#82AAFF", "#FF7A93"},
	{"rose", "#FF8FB0", "#EFE4E8", "#8A8088", "#4F454C", "#4A2E3A", "#3A1F2C", "#24101A", "#7EE787", "#F2C879", "#82AAFF", "#FF7A93"},
}

var (
	cAccent, cText, cDim, cFaint, cPanel, cSel, cSelFg lipgloss.Color
	cGreen, cGold, cCyan, cRed                         lipgloss.Color

	stTitle, stDim, stFaint, stText, stLabel, stErr, stAccent, stGreen lipgloss.Style
)

var activeTheme int

// applyTheme makes themes[i] (wrapped to range) the active palette and rebuilds
// every derived style.
func applyTheme(i int) {
	activeTheme = ((i % len(themes)) + len(themes)) % len(themes)
	t := themes[activeTheme]
	cAccent, cText, cDim, cFaint = t.accent, t.text, t.dim, t.faint
	cPanel, cSel, cSelFg = t.panel, t.sel, t.selFg
	cGreen, cGold, cCyan, cRed = t.green, t.gold, t.cyan, t.red
	stTitle = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	stDim = lipgloss.NewStyle().Foreground(cDim)
	stFaint = lipgloss.NewStyle().Foreground(cFaint)
	stText = lipgloss.NewStyle().Foreground(cText)
	stLabel = lipgloss.NewStyle().Foreground(cDim)
	stErr = lipgloss.NewStyle().Foreground(cRed)
	stAccent = lipgloss.NewStyle().Foreground(cAccent)
	stGreen = lipgloss.NewStyle().Foreground(cGreen)
}

func init() { applyTheme(0) }

// themeFilePath is where the chosen theme name is remembered, under the doze home.
func themeFilePath() string {
	home := os.Getenv("DOZE_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		home = filepath.Join(h, ".doze")
	}
	return filepath.Join(home, "tui.theme")
}

// loadTheme applies the persisted theme, if any.
func loadTheme() {
	p := themeFilePath()
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	name := strings.TrimSpace(string(b))
	for i, t := range themes {
		if t.name == name {
			applyTheme(i)
			return
		}
	}
}

// saveTheme remembers the active theme for next time (best-effort).
func saveTheme() {
	if p := themeFilePath(); p != "" {
		_ = os.WriteFile(p, []byte(themes[activeTheme].name), 0o644)
	}
}

func stateColor(state string) lipgloss.Color {
	switch state {
	case "active":
		return cGreen
	case "idle":
		return cGold
	case "booting":
		return cCyan
	case "error", "tainted":
		return cRed
	default:
		return cDim
	}
}

const (
	histLen   = 300 // ~2.5 min of memory history at refreshMS (was 32s — too short/flat)
	detailH   = 14  // detail card height (fits the 5-row memory trace)
	refreshMS = 500 * time.Millisecond
	logsMS    = 400 * time.Millisecond
	spinMS    = 110 * time.Millisecond
)

var spinner = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

// history holds rolling samples for an instance's sparklines.
type history struct {
	ram   []float64
	conns []float64
}

func (h *history) push(ram, conns float64) {
	h.ram = pushCap(h.ram, ram)
	h.conns = pushCap(h.conns, conns)
}

func pushCap(s []float64, v float64) []float64 {
	s = append(s, v)
	if len(s) > histLen {
		s = s[len(s)-histLen:]
	}
	return s
}

// ── messages ──────────────────────────────────────────────────────────────
type (
	tickMsg     time.Time
	logsTickMsg time.Time
	spinMsg     time.Time
	statusMsg   struct {
		resp control.Response
		err  error
	}
	logsMsg struct {
		name  string
		lines []string
		err   error
	}
	actionMsg struct {
		verb, name string
		err        error
	}
	resourcesMsg struct {
		name string
		res  []control.ResourceView
		acts []control.ActionView
		err  error
	}
	adminResultMsg struct {
		action, resource, result string
		err                      error
	}
	itemsMsg struct {
		name, resource, kind string
		items                []inspItem
		err                  error
	}
)

// fetchItems loads the selected resource's contents as navigable items (the
// engine returns JSON for the listMarker input).
func fetchItems(c *control.Client, name, action, resource, kind string) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.Do(control.Request{Op: "admin", DB: name, Action: action, Resource: resource, Input: listMarker})
		if err != nil {
			return itemsMsg{name: name, resource: resource, kind: kind, err: err}
		}
		items, perr := parseItems(kind, resp.Result)
		return itemsMsg{name: name, resource: resource, kind: kind, items: items, err: perr}
	}
}

// parseItems turns an engine's JSON item list into display rows for the kind.
func parseItems(kind, jsonStr string) ([]inspItem, error) {
	var raw []map[string]any
	if strings.TrimSpace(jsonStr) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, err
	}
	out := make([]inspItem, 0, len(raw))
	for _, r := range raw {
		switch kind {
		case "queue":
			body := jstr(r["body"])
			var meta []string
			if g := jstr(r["group"]); g != "" {
				meta = append(meta, "group "+g)
			}
			if rc := jstr(r["received"]); rc != "" && rc != "1" {
				meta = append(meta, "recv×"+rc)
			}
			detail := prettyJSON(body)
			if a, ok := r["attrs"].(map[string]any); ok && len(a) > 0 {
				detail += "\n\nattributes"
				for _, k := range sortedAnyKeys(a) {
					meta = append(meta, k+"="+jstr(a[k]))
					detail += "\n  " + k + " = " + jstr(a[k])
				}
			}
			out = append(out, inspItem{title: oneLine(body), meta: strings.Join(meta, "  ·  "), detail: detail, delArg: jstr(r["handle"])})
		case "bucket":
			key := jstr(r["key"])
			out = append(out, inspItem{
				title: key, meta: ui.HumanBytes(jint(r["size"])) + "  ·  " + jstr(r["modified"]),
				detail: key, delArg: key,
			})
		case "topic":
			proto, ep := jstr(r["protocol"]), jstr(r["endpoint"])
			filt := jstr(r["filter"])
			title := proto + " → " + ep
			meta := "filter: " + orNoneStr(filt)
			if b, _ := r["raw"].(bool); b {
				meta += "  ·  raw"
			}
			if c, _ := r["confirmed"].(bool); !c {
				meta += "  ·  pending"
			}
			detail := "endpoint: " + ep + "\nfilter: " + orNoneStr(filt)
			// After a test publish each subscription is annotated with whether this
			// event's attributes matched its filter policy — the routing made visible.
			if mv, ok := r["matched"]; ok {
				if b, _ := mv.(bool); b {
					title = "✓ " + title
					meta = "MATCHED — receives this event  ·  " + meta
				} else {
					title = "✗ " + title
					meta = "filtered out — " + meta
				}
			}
			out = append(out, inspItem{title: title, meta: meta, detail: detail})
		}
	}
	return out, nil
}

func jstr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}

func jint(v any) int64 {
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	return 0
}

func sortedAnyKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// prettyJSON indents a JSON body for the expanded detail pane (developers send
// JSON messages — a one-line blob is unreadable). Non-JSON is returned verbatim.
func prettyJSON(s string) string {
	t := strings.TrimSpace(s)
	if t == "" || (t[0] != '{' && t[0] != '[') {
		return s
	}
	var v any
	if json.Unmarshal([]byte(t), &v) != nil {
		return s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(b)
}

func oneLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + " ↵"
	}
	return s
}

func orNoneStr(s string) string {
	if strings.TrimSpace(s) == "" {
		return "none (all messages)"
	}
	return s
}

// cleanErr strips the gRPC envelope ("rpc error: code = … desc = ") from an admin
// error so the inspector shows the engine's actual message.
func cleanErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if i := strings.LastIndex(s, "desc = "); i >= 0 {
		s = s[i+len("desc = "):]
	}
	return s
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// builtinAdmin reports whether an engine exposes dash-manageable resources.
func builtinAdmin(eng string) bool {
	switch eng {
	case "s3", "sqs", "sns":
		return true
	}
	return false
}

func tick() tea.Cmd     { return tea.Tick(refreshMS, func(t time.Time) tea.Msg { return tickMsg(t) }) }
func logsTick() tea.Cmd { return tea.Tick(logsMS, func(t time.Time) tea.Msg { return logsTickMsg(t) }) }
func spin() tea.Cmd     { return tea.Tick(spinMS, func(t time.Time) tea.Msg { return spinMsg(t) }) }

func refresh(c *control.Client) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.Do(control.Request{Op: "status"})
		return statusMsg{resp: resp, err: err}
	}
}

func fetchLogs(c *control.Client, name string) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.Do(control.Request{Op: "logs", DB: name})
		return logsMsg{name: name, lines: resp.Lines, err: err}
	}
}

func do(c *control.Client, verb, name string) tea.Cmd {
	return func() tea.Msg {
		_, err := c.Do(control.Request{Op: verb, DB: name})
		return actionMsg{verb: verb, name: name, err: err}
	}
}

func fetchResources(c *control.Client, name string) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.Do(control.Request{Op: "resources", DB: name})
		return resourcesMsg{name: name, res: resp.Resources, acts: resp.Actions, err: err}
	}
}

func runAdmin(c *control.Client, name, action, resource, input string) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.Do(control.Request{Op: "admin", DB: name, Action: action, Resource: resource, Input: input})
		return adminResultMsg{action: action, resource: resource, result: resp.Result, err: err}
	}
}

// richPrefix marks an Admin input string as a structured (JSON) payload — for
// multi-field actions (publish/send/put) composed via the form or parsed from
// inline `k=v` syntax. Kept in sync with each engine's admin.richPrefix.
const richPrefix = "\x01"

// listMarker, as the Admin input, asks a read action for the JSON item list that
// drives the inspector. Kept in sync with each engine's admin.listMarker.
const listMarker = "\x01list"

// inspItem is one row in the inspector's navigable list (a message, object, or
// subscription), already rendered to display strings.
type inspItem struct {
	title  string // primary line (body preview / key / "proto → endpoint")
	meta   string // dim secondary line (attrs / size·modified / filter)
	detail string // full content for the expanded pane
	delArg string // argument for the per-item delete action; "" = not deletable
}

// composerField is one labeled field of the multi-field composer.
type composerField struct {
	key   string // payload key (message/subject/attributes/body/group/key)
	label string
	hint  string
	value string
}

// richVerbs take a structured payload (a composer form, or inline `k=v` parsing).
var richVerbs = map[string]bool{"publish": true, "send": true, "put": true}

// composerSchema is the field set the composer presents for a rich verb.
func composerSchema(verb string) []composerField {
	switch verb {
	case "publish":
		return []composerField{
			{key: "message", label: "message", hint: "the notification body"},
			{key: "subject", label: "subject", hint: "optional"},
			{key: "attributes", label: "attributes", hint: "key=value, space-separated — routed by subscription filter policies"},
		}
	case "send":
		return []composerField{
			{key: "body", label: "body", hint: "the message body"},
			{key: "group", label: "group", hint: "FIFO MessageGroupId (optional)"},
			{key: "attributes", label: "attributes", hint: "key=value, space-separated"},
		}
	case "put":
		return []composerField{
			{key: "key", label: "key", hint: "object key"},
			{key: "body", label: "body", hint: "object contents"},
		}
	}
	return nil
}

// ── model ─────────────────────────────────────────────────────────────────
type model struct {
	client *control.Client
	resp   control.Response
	err    error
	width  int
	height int

	cursor   int
	follow   bool
	logVP    viewport.Model
	logErr   string
	logLines []string // raw log lines of the selected instance (for copy mode)

	// copy mode: a frozen, keyboard-navigable selection over the logs. Keyboard
	// granularity is whole LINES by default; Tab (or any word motion) toggles to
	// WORD. The MOUSE uses character granularity (copyCharMode) so a drag selects an
	// exact span — including within a single line — like a normal terminal.
	copyMode      bool
	copyWordMode  bool // false = line granularity (default), true = word
	copyCharMode  bool // mouse drag: character-precise span (overrides line/word)
	copyLines     []string
	copyCursor    int
	copyCol       int // word index (word mode)
	copyAnchor    int // selection start line; -1 = no range
	copyAnchorCol int // selection start word (word mode only)
	copyColCh     int // rune index on the cursor line (char mode)
	copyAnchorColCh int // rune index of the anchor (char mode)

	filtering bool
	filter    textinput.Model
	showHelp  bool

	// admin workspace: a full-screen management view for a builtin instance — a
	// resource list (queues/buckets/topics) on the left, an inspector (the result of
	// the engine's read/"viewer" action) on the right, and a labelled action bar
	// below. Menu-driven: keys map to the engine's actions (Browse/Peek/Send/Purge…);
	// destructive ones confirm first, input ones (send/publish) prompt via cmd.
	adminMode    bool
	adminName    string // instance the workspace belongs to
	adminRes     []control.ResourceView
	adminActs    []control.ActionView
	adminErr     string // resource-fetch error (shown in the inspector header)
	adminCursor  int    // active resource
	adminLoaded  bool   // resources fetched at least once (vs still loading)
	adminPending string // a destructive action ("purge"/"empty"/"del:<arg>") awaiting y/n

	// inspector: the selected resource's live contents as a navigable item list
	// (messages / objects / subscriptions), direct-manipulation rather than typed.
	inspItems    []inspItem
	inspCursor   int  // selected item
	inspExpanded bool // detail pane open for the selected item
	inspErr      string
	inspRouting  bool // topic: showing a test publish's routing — pause live reload

	// composer: a multi-field form for create actions (send/publish/put). The
	// active field is edited in cmd; Tab cycles fields, Enter submits.
	composerMode bool
	composerVerb string
	composerFlds []composerField
	composerAt   int

	cmd    textinput.Model // composer field editor
	itemVP viewport.Model  // the inspector item list, scrollable

	hist       map[string]*history
	frame      int
	flash      string
	flashFrame int
}

// setFlash records a transient status message (auto-cleared after ~2.5s).
func (m *model) setFlash(s string) { m.flash = s; m.flashFrame = m.frame }

// Run validates a daemon is up and launches the dashboard.
func Run(socketPath string) error {
	c := control.NewClient(socketPath)
	if !c.Available() {
		return fmt.Errorf("no daemon is running (boot an instance with `doze start <name>`)")
	}
	loadTheme() // restore the last-used theme
	fi := textinput.New()
	fi.Prompt = "/"
	fi.Placeholder = "filter"
	fi.CharLimit = 32
	ci := textinput.New()
	ci.Prompt = "❯ "
	ci.CharLimit = 512
	ci.ShowSuggestions = true // fish-style inline ghost completion (Tab/→ accepts)
	m := model{
		client:  c,
		follow:  true,
		filter:  fi,
		cmd:     ci,
		hist:    map[string]*history{},
		logVP:   viewport.New(0, 0),
		itemVP:  viewport.New(0, 0),
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(refresh(m.client), tick(), logsTick(), spin())
}

func (m model) bodyH() int {
	if h := m.height - 3; h > 4 { // header (2) + footer (1)
		return h
	}
	return 4
}

func (m model) sidebarW() int {
	sw := 32
	if m.width < 96 {
		sw = 26
	}
	if sw > m.width/2 {
		sw = m.width / 2
	}
	if sw < 12 {
		sw = 12
	}
	return sw
}

func (m model) rightW() int {
	if w := m.width - m.sidebarW() - 3; w > 12 { // sidebar border (1) + gap (2)
		return w
	}
	return 12
}

// visible returns instance indices in display order (name-sorted, filtered).
func (m model) visible() []int {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	idx := make([]int, 0, len(m.resp.Instances))
	for i, in := range m.resp.Instances {
		if q != "" && !strings.Contains(strings.ToLower(in.Name+" "+in.Engine), q) {
			continue
		}
		idx = append(idx, i)
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ia, ib := m.resp.Instances[idx[a]], m.resp.Instances[idx[b]]
		if ra, rb := groupRank(groupOf(ia)), groupRank(groupOf(ib)); ra != rb {
			return ra < rb
		}
		return ia.Name < ib.Name
	})
	return idx
}

// groupOf is the display heading an instance falls under: just two divisions —
// your own supervised apps are "processes", everything else (the engine modules:
// databases, caches, queues, buckets, topics) is "modules".
func groupOf(in control.InstanceView) string {
	if in.Engine == "process" {
		return "processes"
	}
	return "modules"
}

// groupRank orders the two divisions: modules first, processes last.
func groupRank(cat string) int {
	if cat == "processes" {
		return 1
	}
	return 0
}

// sbLine is one rendered sidebar line: a group header, or an instance at display
// index di (into visible()). The cursor only ever lands on instance lines.
type sbLine struct {
	header string
	di     int
}

// sidebarLines lays out the visible instances with a header line inserted wherever
// the group changes. Both the renderer and the click handler use it so headers and
// selection stay in sync.
func (m model) sidebarLines() []sbLine {
	vis := m.visible()
	out := make([]sbLine, 0, len(vis)+4)
	prev := ""
	for di, i := range vis {
		if g := groupOf(m.resp.Instances[i]); g != prev {
			out = append(out, sbLine{header: g})
			prev = g
		}
		out = append(out, sbLine{di: di})
	}
	return out
}

func (m model) selected() (control.InstanceView, bool) {
	vis := m.visible()
	if len(vis) == 0 || m.cursor < 0 || m.cursor >= len(vis) {
		return control.InstanceView{}, false
	}
	return m.resp.Instances[vis[m.cursor]], true
}

func (m *model) layout() {
	// logs box: rightW minus its rounded border (2) and horizontal padding (2×2).
	m.logVP.Width = max(4, m.rightW()-6)
	m.logVP.Height = max(3, m.bodyH()-detailH-6)
	// inspector item list: full width, the body height between the tab strip and
	// the footer (header, rule, tabs, rule … rule, footer = 6 chrome rows).
	m.itemVP.Width = max(10, m.width-2)
	m.itemVP.Height = max(3, m.height-6)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{refresh(m.client), tick()}
		if m.adminMode && m.adminName != "" { // keep the inspector live while managing
			cmds = append(cmds, fetchResources(m.client, m.adminName))
			if !m.inspRouting { // a routing snapshot is held until the user moves on
				if c := m.loadItems(); c != nil {
					cmds = append(cmds, c)
				}
			}
		}
		return m, tea.Batch(cmds...)

	case spinMsg:
		m.frame++
		if m.flash != "" && m.frame-m.flashFrame > 24 { // ~2.5s at 110ms
			m.flash = ""
		}
		return m, spin()

	case logsTickMsg:
		var cmd tea.Cmd
		if v, ok := m.selected(); ok && v.PID != 0 {
			cmd = fetchLogs(m.client, v.Name)
		}
		return m, tea.Batch(cmd, logsTick())

	case statusMsg:
		m.err = msg.err
		if msg.err == nil {
			m.resp = msg.resp
			for _, in := range m.resp.Instances {
				h := m.hist[in.Name]
				if h == nil {
					h = &history{}
					m.hist[in.Name] = h
				}
				h.push(float64(in.RAM), float64(in.Conns))
			}
		}
		if vis := m.visible(); m.cursor >= len(vis) {
			m.cursor = max(0, len(vis)-1)
		}
		return m, nil

	case logsMsg:
		if v, ok := m.selected(); ok && msg.name == v.Name {
			if msg.err != nil {
				m.logErr = msg.err.Error()
				if !m.copyMode {
					m.logVP.SetContent("")
				}
			} else {
				m.logErr = ""
				m.logLines = msg.lines
				if !m.copyMode { // freeze the view while copying
					m.logVP.SetContent(renderLogs(msg.lines))
					if m.follow {
						m.logVP.GotoBottom()
					}
				}
			}
		}
		return m, nil

	case actionMsg:
		if msg.err != nil {
			m.setFlash(stErr.Render("✗ " + msg.verb + " " + msg.name + ": " + msg.err.Error()))
		} else {
			m.setFlash(stGreen.Render("✓ " + msg.verb + " " + msg.name))
		}
		return m, refresh(m.client)

	case resourcesMsg:
		// Only adopt resources for the instance we're currently focused on.
		if v, ok := m.selected(); ok && msg.name == v.Name {
			m.adminName = msg.name
			if msg.err != nil {
				m.adminErr = msg.err.Error()
			} else {
				m.adminErr, m.adminRes, m.adminActs = "", msg.res, msg.acts
			}
			if m.adminMode {
				wasLoaded := m.adminLoaded
				m.adminLoaded = true
				if m.adminCursor >= len(m.adminRes) {
					m.adminCursor = max(0, len(m.adminRes)-1)
				}
				if !wasLoaded { // first load → fetch the selected resource's items
					return m, m.loadItems()
				}
			}
		}
		return m, nil

	case itemsMsg:
		// Adopt items only if they're for the resource we're still on.
		if r, ok := m.selectedResource(); ok && m.adminMode && msg.name == m.adminName && msg.resource == r.Name {
			if msg.err != nil {
				m.inspErr = cleanErr(msg.err)
			} else {
				m.inspErr = ""
				m.inspItems = msg.items
				if m.inspCursor >= len(m.inspItems) {
					m.inspCursor = max(0, len(m.inspItems)-1)
				}
			}
			m.refreshItemView()
		}
		return m, nil

	case adminResultMsg:
		// An item action (send/publish/put/del/purge/…): flash the outcome and
		// reload the live list + resource counts.
		if msg.err != nil {
			m.setFlash(stErr.Render("✗ " + cleanErr(msg.err)))
		} else if msg.action == "publish" && m.adminMode && strings.HasPrefix(strings.TrimSpace(msg.result), "[") {
			// A test publish returns per-subscription routing — show which subscriptions
			// the event reached, and hold that snapshot (pause live reload) so it can be
			// read and iterated on.
			if items, perr := parseItems("topic", msg.result); perr == nil {
				matched := 0
				for _, it := range items {
					if strings.HasPrefix(it.title, "✓") {
						matched++
					}
				}
				m.inspItems, m.inspCursor, m.inspExpanded, m.inspRouting = items, 0, false, true
				m.refreshItemView()
				m.setFlash(stGreen.Render(fmt.Sprintf("✓ routed to %d of %d subscription(s)", matched, len(items))))
				return m, fetchResources(m.client, m.adminName)
			}
		} else if head := firstLine(msg.result); head != "" {
			m.setFlash(stGreen.Render("✓ " + head))
		}
		if m.adminMode && m.adminName != "" {
			return m, tea.Batch(m.loadItems(), fetchResources(m.client, m.adminName))
		}
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.logVP, cmd = m.logVP.Update(msg)
	return m, cmd
}

// handleMouse routes the wheel and clicks like mprocs: wheel over the sidebar
// moves the selection, wheel over the right pane scrolls the logs, and a click
// in the sidebar selects that instance.
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	const headerRows = 2 // title + rule above the body
	if m.adminMode {     // the inspector owns the mouse so clicks never leak to the dash
		if m.composerMode {
			return m, nil
		}
		const tabRow = 2     // header(0) rule(1) tabs(2) rule(3) list(4…)
		const listTop = 4    // first row of the item list
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.inspCursor > 0 {
				m.inspCursor--
				m.refreshItemView()
			}
			return m, nil
		case tea.MouseButtonWheelDown:
			if m.inspCursor < len(m.inspItems)-1 {
				m.inspCursor++
				m.refreshItemView()
			}
			return m, nil
		case tea.MouseButtonLeft:
			if msg.Action != tea.MouseActionPress {
				return m, nil
			}
			if msg.Y == tabRow { // click a resource tab to switch
				if i := m.tabAt(msg.X); i >= 0 {
					if m.selectResourceByIndex(i) {
						return m, m.loadItems()
					}
				}
				return m, nil
			}
			// The item list: ~2 rows per item from the list top, offset by scroll.
			row := msg.Y - listTop + m.itemVP.YOffset
			if idx := row / 2; idx >= 0 && idx < len(m.inspItems) {
				if idx == m.inspCursor {
					m.inspExpanded = !m.inspExpanded
				} else {
					m.inspCursor, m.inspExpanded = idx, false
				}
				m.refreshItemView()
			}
			return m, nil
		}
		return m, nil
	}
	if m.copyMode { // scroll the frozen logs; drag to extend the selection
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.logVP.LineUp(2)
		case tea.MouseButtonWheelDown:
			m.logVP.LineDown(2)
		case tea.MouseButtonLeft:
			switch msg.Action {
			case tea.MouseActionPress: // re-anchor a fresh character-precise selection
				ln := m.logLineAt(msg.Y)
				m.copyCharMode, m.copyWordMode = true, false
				m.copyCursor, m.copyColCh = ln, m.logRuneColAt(ln, msg.X)
				m.copyAnchor, m.copyAnchorColCh = m.copyCursor, m.copyColCh
				m.refreshCopyView()
			case tea.MouseActionMotion: // drag → extend the character span
				ln := m.logLineAt(msg.Y)
				m.copyCursor = ln
				if m.copyCharMode {
					m.copyColCh = m.logRuneColAt(ln, msg.X)
				} else if m.copyWordMode {
					m.copyCol = m.logColAt(ln, msg.X)
				}
				m.refreshCopyView()
			case tea.MouseActionRelease:
				dragged := m.copyAnchor != m.copyCursor ||
					(m.copyCharMode && m.copyAnchorColCh != m.copyColCh) ||
					(m.copyWordMode && m.copyAnchorCol != m.copyCol)
				if dragged {
					return m.copySelection() // dragged → copy exactly what was spanned
				}
				m.copyAnchor, m.copyAnchorCol, m.copyAnchorColCh = -1, 0, 0 // plain click → position only
				m.refreshCopyView()
			}
		}
		return m, nil
	}
	overSidebar := msg.X < m.sidebarW()
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if overSidebar {
			if m.cursor > 0 {
				m.cursor--
			}
			return m, m.onSelect()
		}
		m.follow = false
		m.logVP.LineUp(3)
		return m, nil
	case tea.MouseButtonWheelDown:
		if overSidebar {
			if m.cursor < len(m.visible())-1 {
				m.cursor++
			}
			return m, m.onSelect()
		}
		m.logVP.LineDown(3)
		if m.logVP.AtBottom() { // caught up — resume tailing
			m.follow = true
		}
		return m, nil
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		if overSidebar {
			// Map the clicked body row to a sidebar line (headers interspersed); only
			// instance lines are selectable.
			lines := m.sidebarLines()
			if row := msg.Y - headerRows; row >= 0 && row < len(lines) {
				if ln := lines[row]; ln.header == "" {
					m.cursor = ln.di
					return m, m.onSelect()
				}
			}
		} else if m.logsRegion(msg.Y) && len(m.logLines) > 0 {
			// Enter copy mode with a character-precise anchor at the click, so a drag
			// selects an exact span (including within one line), like a terminal.
			m.copyMode, m.copyWordMode, m.copyCharMode = true, false, true
			m.copyLines = m.logLines
			ln := m.logLineAt(msg.Y)
			m.copyCursor, m.copyColCh = ln, m.logRuneColAt(ln, msg.X)
			m.copyAnchor, m.copyAnchorColCh = m.copyCursor, m.copyColCh
			m.refreshCopyView()
		}
	}
	return m, nil
}

// logsTop is the screen row of the first visible log line (header + detail box +
// the logs box's top border/title/rule).
func (m model) logsTop() int { return 2 + (detailH + 2) + 3 }

// logsRegion reports whether screen row y falls inside the log viewport.
func (m model) logsRegion(y int) bool {
	return y >= m.logsTop() && y < m.logsTop()+m.logVP.Height
}

// logLineAt maps a screen row to a log line index (clamped).
func (m model) logLineAt(y int) int {
	return clampi(m.logVP.YOffset+(y-m.logsTop()), 0, max(0, len(m.copyLines)-1))
}

// logColAt maps a screen X to a word index on the given log line. The logs
// content starts after the sidebar (sidebarW), the 2-col gap, the box's left
// border, and its 2-col padding.
func (m model) logColAt(line, x int) int {
	contentX := x - (m.sidebarW() + 5)
	if contentX < 0 {
		contentX = 0
	}
	ws := m.wordsAt(line)
	for wi, sp := range ws {
		if contentX < sp.end {
			return wi
		}
	}
	return max(0, len(ws)-1)
}

// logRuneColAt maps a screen X to a rune index on the given log line (char
// granularity for mouse selection). The end position (len) is allowed so a drag
// can include the last character. Same content offset as logColAt.
func (m model) logRuneColAt(line, x int) int {
	contentX := x - (m.sidebarW() + 5)
	if contentX < 0 {
		contentX = 0
	}
	n := 0
	if line >= 0 && line < len(m.copyLines) {
		n = len([]rune(m.copyLines[line]))
	}
	return clampi(contentX, 0, n)
}

// copySelection writes the selected text to the clipboard and leaves copy mode.
func (m model) copySelection() (tea.Model, tea.Cmd) {
	var text, what string
	switch {
	case m.copyCharMode: // character-precise span (mouse) — exactly what was dragged
		text, what = m.selectedCharText(), "selection"
	case !m.copyWordMode: // whole line(s) — the keyboard default
		lo, hi := m.copyRange()
		text = strings.Join(m.copyLines[lo:hi+1], "\n")
		what = fmt.Sprintf("%d line(s)", hi-lo+1)
	case m.copyAnchor >= 0: // word-wise span
		text, what = m.selectedText(), "selection"
	default: // the single word under the cursor
		if s, e := m.curWordRange(); s >= 0 {
			r := []rune(m.copyLines[m.copyCursor])
			text = string(r[s:clampi(e, 0, len(r))])
		}
		what = "word"
	}
	err := clipboard.WriteAll(text)
	m.copyMode, m.copyCharMode = false, false
	m.copyAnchor, m.copyAnchorCol, m.copyAnchorColCh = -1, 0, 0
	m.logVP.SetContent(renderLogs(m.logLines))
	if m.follow {
		m.logVP.GotoBottom()
	}
	if err != nil {
		m.setFlash(stErr.Render("✗ copy failed: " + err.Error()))
	} else {
		m.setFlash(stGreen.Render("✓ copied " + what + " to clipboard"))
	}
	return m, nil
}

// selectedText extracts the word-wise selected span, covering whole middle lines.
func (m model) selectedText() string {
	lL, lW, hL, hW := m.wordSel()
	loW, hiW := m.wordsAt(lL), m.wordsAt(hL)
	if len(loW) == 0 || len(hiW) == 0 {
		return ""
	}
	s := loW[clampi(lW, 0, len(loW)-1)].start
	e := hiW[clampi(hW, 0, len(hiW)-1)].end
	if lL == hL {
		r := []rune(m.copyLines[lL])
		return string(r[clampi(s, 0, len(r)):clampi(e, 0, len(r))])
	}
	rFirst := []rune(m.copyLines[lL])
	parts := []string{string(rFirst[clampi(s, 0, len(rFirst)):])}
	for i := lL + 1; i < hL; i++ {
		parts = append(parts, m.copyLines[i])
	}
	rLast := []rune(m.copyLines[hL])
	parts = append(parts, string(rLast[:clampi(e, 0, len(rLast))]))
	return strings.Join(parts, "\n")
}

// ── character-precise selection (mouse drag) ────────────────────────────────

// charSel returns the ordered span (loLine, loCol, hiLine, hiCol) of the
// character selection (anchor → cursor), in rune coordinates.
func (m model) charSel() (lL, lC, hL, hC int) {
	if m.copyAnchor > m.copyCursor || (m.copyAnchor == m.copyCursor && m.copyAnchorColCh > m.copyColCh) {
		return m.copyCursor, m.copyColCh, m.copyAnchor, m.copyAnchorColCh
	}
	return m.copyAnchor, m.copyAnchorColCh, m.copyCursor, m.copyColCh
}

// selCharRange is the selected rune span [start,end) on line i for the active
// character selection (whole intermediate lines are fully covered). ok is false
// when line i is outside the selection.
func (m model) selCharRange(i int) (int, int, bool) {
	if !m.copyCharMode || m.copyAnchor < 0 {
		return 0, 0, false
	}
	lL, lC, hL, hC := m.charSel()
	if i < lL || i > hL {
		return 0, 0, false
	}
	n := len([]rune(m.copyLines[i]))
	start, end := 0, n
	if i == lL {
		start = clampi(lC, 0, n)
	}
	if i == hL {
		end = clampi(hC, 0, n)
	}
	return start, end, true
}

// selectedCharText extracts the character-precise selected span across lines.
func (m model) selectedCharText() string {
	lL, lC, hL, hC := m.charSel()
	if lL < 0 || hL >= len(m.copyLines) {
		return ""
	}
	if lL == hL {
		r := []rune(m.copyLines[lL])
		return string(r[clampi(lC, 0, len(r)):clampi(hC, 0, len(r))])
	}
	rFirst := []rune(m.copyLines[lL])
	parts := []string{string(rFirst[clampi(lC, 0, len(rFirst)):])}
	for i := lL + 1; i < hL; i++ {
		parts = append(parts, m.copyLines[i])
	}
	rLast := []rune(m.copyLines[hL])
	parts = append(parts, string(rLast[:clampi(hC, 0, len(rLast))]))
	return strings.Join(parts, "\n")
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.showHelp { // any key dismisses the help overlay
		m.showHelp = false
		return m, nil
	}
	if m.copyMode {
		return m.handleCopyKey(msg)
	}
	if m.adminMode {
		return m.handleAdminKey(msg)
	}
	if m.filtering {
		switch msg.String() {
		case "enter", "esc":
			m.filtering = false
			m.filter.Blur()
			if msg.String() == "esc" {
				m.filter.SetValue("")
			}
			m.cursor = 0
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.cursor = 0
		return m, cmd
	}

	vis := m.visible()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, m.onSelect()
	case "down", "j":
		if m.cursor < len(vis)-1 {
			m.cursor++
		}
		return m, m.onSelect()
	case "g", "home":
		m.cursor = 0
		return m, m.onSelect()
	case "G", "end":
		m.cursor = max(0, len(vis)-1)
		return m, m.onSelect()
	case "/":
		m.filtering = true
		m.filter.Focus()
		return m, textinput.Blink
	case "f":
		m.follow = !m.follow
		if m.follow {
			m.logVP.GotoBottom()
		}
		return m, nil
	case "pgup", "ctrl+u":
		m.follow = false
		m.logVP.HalfViewUp()
		return m, nil
	case "pgdown", "ctrl+d":
		m.logVP.HalfViewDown()
		return m, nil
	case "c":
		if len(m.logLines) > 0 { // enter copy mode (line granularity by default)
			m.copyMode, m.copyWordMode, m.copyCharMode = true, false, false
			m.copyLines = m.logLines
			m.copyCursor = len(m.copyLines) - 1
			m.copyCol = 0
			m.copyAnchor, m.copyAnchorCol, m.copyAnchorColCh = -1, 0, 0
			m.refreshCopyView()
		}
		return m, nil
	case "t":
		applyTheme(activeTheme + 1)
		saveTheme()
		m.setFlash(stAccent.Render("theme · " + themes[activeTheme].name))
		return m, nil
	case "?":
		m.showHelp = true
		return m, nil
	case "r":
		return m, refresh(m.client)
	}

	if v, ok := m.selected(); ok {
		switch msg.String() {
		case "enter":
			// Enter "opens" a running builtin into its console (s3/sqs/sns); for
			// anything else (or a sleeping one) it boots, like b.
			if builtinAdmin(v.Engine) && v.PID != 0 {
				return m.openConsole(v)
			}
			m.setFlash(stDim.Render("booting " + v.Name + "…"))
			return m, do(m.client, "boot", v.Name)
		case "b":
			m.setFlash(stDim.Render("booting " + v.Name + "…"))
			return m, do(m.client, "boot", v.Name)
		case "d":
			m.setFlash(stDim.Render("reaping " + v.Name + "…"))
			return m, do(m.client, "down", v.Name)
		case "R":
			m.setFlash(stDim.Render("restarting " + v.Name + "…"))
			return m, do(m.client, "restart", v.Name)
		case "p": // pin: toggle the idle-reaper exemption (keep awake)
			_, _ = m.client.Do(control.Request{Op: "keepawake", DB: v.Name})
			if v.KeepAwake { // was pinned → now auto-sleeps again
				m.setFlash(stDim.Render("○ " + v.Name + " will auto-sleep again"))
			} else {
				m.setFlash(stAccent.Render("▲ keeping " + v.Name + " awake"))
			}
			return m, refresh(m.client)
		case "a": // legacy shortcut → open the console (now the Enter affordance)
			if builtinAdmin(v.Engine) && v.PID != 0 {
				return m.openConsole(v)
			}
			return m, nil
		}
	}
	return m, nil
}

// openConsole drops into the resource console for a running builtin: a REPL scoped
// to the instance's sub-resources. Seeds the transcript with a greeting and focuses
// the prompt.
// openConsole enters the inspector for a running builtin: it fetches the
// resources and the first one's contents, which render as a live, navigable list.
func (m model) openConsole(v control.InstanceView) (tea.Model, tea.Cmd) {
	m.adminMode, m.adminErr, m.adminLoaded = true, "", false
	m.adminCursor, m.adminPending = 0, ""
	m.inspItems, m.inspCursor, m.inspExpanded, m.inspErr, m.inspRouting = nil, 0, false, "", false
	m.itemVP.SetContent(stFaint.Render("loading…"))
	m.itemVP.GotoTop()
	return m, fetchResources(m.client, v.Name)
}

// handleAdminKey drives the inspector — a direct-manipulation browser. ↑↓ move
// between items, Enter expands the selected one, n composes a new item, d deletes
// it, the destructive bulk ops confirm, Tab switches resource, Esc backs out.
func (m model) handleAdminKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.composerMode { // multi-field create form
		return m.handleComposerKey(msg)
	}
	if m.adminPending != "" { // confirm a delete / purge / empty
		switch msg.String() {
		case "y", "Y", "enter":
			act, arg := m.adminPending, ""
			if i := strings.IndexByte(act, ':'); i >= 0 {
				act, arg = act[:i], act[i+1:]
			}
			m.adminPending = ""
			if r, ok := m.selectedResource(); ok {
				return m, runAdmin(m.client, m.adminName, act, r.Name, arg)
			}
			return m, nil
		default:
			m.adminPending = ""
			m.setFlash(stDim.Render("cancelled"))
			return m, nil
		}
	}

	kind := m.resKind()
	switch msg.String() {
	case "esc", "q":
		if m.inspExpanded { // collapse the open item first
			m.inspExpanded = false
			m.refreshItemView()
			return m, nil
		}
		m.adminMode = false
		return m, nil
	case "up", "k": // move the item list
		if m.inspCursor > 0 {
			m.inspCursor--
		}
		m.refreshItemView()
		return m, nil
	case "down", "j":
		if m.inspCursor < len(m.inspItems)-1 {
			m.inspCursor++
		}
		m.refreshItemView()
		return m, nil
	case "left", "h", "shift+tab": // previous resource tab
		m.cycleResource(-1)
		return m, m.loadItems()
	case "right", "l", "tab": // next resource tab
		m.cycleResource(1)
		return m, m.loadItems()
	case "g", "home":
		m.inspCursor, m.inspExpanded = 0, false
		m.refreshItemView()
		return m, nil
	case "G", "end":
		m.inspCursor, m.inspExpanded = max(0, len(m.inspItems)-1), false
		m.refreshItemView()
		return m, nil
	case "pgup", "ctrl+u":
		m.itemVP.HalfViewUp()
		return m, nil
	case "pgdown", "ctrl+d":
		m.itemVP.HalfViewDown()
		return m, nil
	case "enter", " ":
		m.inspExpanded = !m.inspExpanded
		m.refreshItemView()
		return m, nil
	case "r":
		m.inspRouting = false // a manual refresh drops the routing snapshot
		return m, m.loadItems()
	case "n": // compose a new item (send/publish/put)
		if v := composeVerb(kind); v != "" {
			return m.openComposer(v)
		}
		return m, nil
	case "d", "x": // delete the selected item
		if it, ok := m.selectedItem(); ok && it.delArg != "" {
			if da := deleteAction(kind); da != "" {
				m.adminPending = da + ":" + it.delArg
				m.setFlash(stErr.Render("delete this " + itemNoun(kind) + "? — y to confirm"))
			}
		}
		return m, nil
	case "P": // purge (queue) / empty (bucket)
		switch kind {
		case "queue":
			m.adminPending = "purge"
			m.setFlash(stErr.Render("purge every message? — y to confirm"))
		case "bucket":
			m.adminPending = "empty"
			m.setFlash(stErr.Render("empty the bucket? — y to confirm"))
		}
		return m, nil
	case "R": // redrive (queue dead-letter → source)
		if kind == "queue" {
			if r, ok := m.selectedResource(); ok {
				return m, runAdmin(m.client, m.adminName, "redrive", r.Name, "")
			}
		}
		return m, nil
	}
	return m, nil
}

// refreshItemView re-renders the item list into the viewport with the cursor row
// emphasized and (when expanded) the selected item's detail inline, keeping the
// cursor on screen.
func (m *model) refreshItemView() {
	w := m.itemVP.Width
	if w < 4 {
		w = 4
	}
	switch {
	case m.inspErr != "":
		m.itemVP.SetContent(stErr.Render("✕ " + truncate(m.inspErr, w)))
		return
	case !m.adminLoaded:
		m.itemVP.SetContent(stFaint.Render("loading…"))
		return
	case len(m.inspItems) == 0:
		m.itemVP.SetContent(stFaint.Render(emptyMsg(m.resKind())))
		return
	}
	var b strings.Builder
	cursorTop, line := 0, 0
	for i, it := range m.inspItems {
		sel := i == m.inspCursor
		if sel {
			cursorTop = line
			b.WriteString(stAccent.Render("▸ ") + stAccent.Bold(true).Render(truncate(it.title, w-2)) + "\n")
		} else {
			b.WriteString("  " + stText.Render(truncate(it.title, w-2)) + "\n")
		}
		line++
		if it.meta != "" {
			b.WriteString("    " + stFaint.Render(truncate(it.meta, w-4)) + "\n")
			line++
		}
		if sel && m.inspExpanded && it.detail != "" {
			b.WriteString("    " + stFaint.Render(strings.Repeat("┄", max(1, w-6))) + "\n")
			line++
			for _, ln := range strings.Split(it.detail, "\n") {
				b.WriteString("    " + stDim.Render(truncate(ln, w-4)) + "\n")
				line++
			}
		}
	}
	m.itemVP.SetContent(strings.TrimRight(b.String(), "\n"))
	off, h := m.itemVP.YOffset, m.itemVP.Height
	if cursorTop < off {
		m.itemVP.SetYOffset(cursorTop)
	} else if cursorTop >= off+h {
		m.itemVP.SetYOffset(cursorTop - h + 1)
	}
}

func emptyMsg(kind string) string {
	switch kind {
	case "queue":
		return "no messages — the queue is empty (press n to send one)"
	case "bucket":
		return "no objects — the bucket is empty (press n to upload one)"
	case "topic":
		return "no subscriptions — nothing receives this topic"
	}
	return "(empty)"
}

func itemNoun(kind string) string {
	switch kind {
	case "queue":
		return "message"
	case "bucket":
		return "object"
	case "topic":
		return "subscription"
	}
	return "item"
}

// inlinePayload turns inline `<body> k=v k2=v2` args into the JSON payload an
// engine expects (the body is every non-`k=v` token; `subject=`/`group=` map to
// their fields, the rest are attributes). `put` is `<key> <body…>`.
func inlinePayload(verb, args string) string {
	if verb == "put" {
		parts := strings.SplitN(args, " ", 2)
		p := map[string]string{"key": parts[0]}
		if len(parts) == 2 {
			p["body"] = parts[1]
		}
		return mustJSON(p)
	}
	var body []string
	attrs := map[string]string{}
	payload := map[string]any{}
	for _, tok := range strings.Fields(args) {
		if k, v, ok := strings.Cut(tok, "="); ok && k != "" && !strings.ContainsAny(k, `"'`) {
			switch k {
			case "subject", "group":
				payload[k] = v
			default:
				attrs[k] = v
			}
			continue
		}
		body = append(body, tok)
	}
	if verb == "send" {
		payload["body"] = strings.Join(body, " ")
	} else {
		payload["message"] = strings.Join(body, " ")
	}
	if len(attrs) > 0 {
		payload["attributes"] = attrs
	}
	return mustJSON(payload)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// parseAttrs turns "k=v k2=v2" into a map (composer attributes field).
func parseAttrs(s string) map[string]string {
	out := map[string]string{}
	for _, tok := range strings.Fields(s) {
		if k, v, ok := strings.Cut(tok, "="); ok && k != "" {
			out[k] = v
		}
	}
	return out
}

// ── composer (multi-field forms for publish/send/put) ───────────────────────

func (m model) openComposer(verb string) (tea.Model, tea.Cmd) {
	m.composerMode, m.composerVerb = true, verb
	m.composerFlds = m.composerFieldsFor(verb)
	m.composerAt = 0
	m.cmd.SetValue(m.composerFlds[0].value)
	m.cmd.CursorEnd()
	m.cmd.SetSuggestions(nil)
	return m, m.cmd.Focus()
}

// composerFieldsFor adapts the schema to the selected resource. For a FIFO queue
// the message group is required, so it is pre-filled (with a sensible default) and
// labelled as such; for a standard queue the group field is dropped entirely
// because SQS ignores it there.
func (m model) composerFieldsFor(verb string) []composerField {
	flds := composerSchema(verb)
	if verb != "send" {
		return flds
	}
	if m.selectedIsFIFO() {
		for i := range flds {
			if flds[i].key == "group" {
				flds[i].label = "group (required for FIFO)"
				flds[i].hint = "MessageGroupId — messages in a group stay ordered"
				flds[i].value = "default"
			}
		}
		return flds
	}
	out := flds[:0]
	for _, f := range flds {
		if f.key != "group" {
			out = append(out, f)
		}
	}
	return out
}

// selectedIsFIFO reports whether the active queue is a FIFO queue.
func (m model) selectedIsFIFO() bool {
	r, ok := m.selectedResource()
	if !ok {
		return false
	}
	return r.Info["fifo"] == "true" || strings.HasSuffix(r.Name, ".fifo")
}

func (m *model) composerSave() {
	if m.composerAt >= 0 && m.composerAt < len(m.composerFlds) {
		m.composerFlds[m.composerAt].value = m.cmd.Value()
	}
}

func (m model) handleComposerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.composerMode = false
		m.cmd.SetValue("")
		m.setFlash(stDim.Render("cancelled"))
		return m, nil
	case "tab", "down":
		m.composerSave()
		m.composerAt = (m.composerAt + 1) % len(m.composerFlds)
		m.cmd.SetValue(m.composerFlds[m.composerAt].value)
		m.cmd.CursorEnd()
		return m, nil
	case "shift+tab", "up":
		m.composerSave()
		m.composerAt = (m.composerAt - 1 + len(m.composerFlds)) % len(m.composerFlds)
		m.cmd.SetValue(m.composerFlds[m.composerAt].value)
		m.cmd.CursorEnd()
		return m, nil
	case "enter":
		m.composerSave()
		return m.composerSubmit()
	}
	var cmd tea.Cmd
	m.cmd, cmd = m.cmd.Update(msg)
	return m, cmd
}

// composerSubmit assembles the form into the engine payload and dispatches it.
func (m model) composerSubmit() (tea.Model, tea.Cmd) {
	vals := map[string]string{}
	for _, f := range m.composerFlds {
		vals[f.key] = f.value
	}
	payload := map[string]any{}
	switch m.composerVerb {
	case "publish":
		payload["message"] = vals["message"]
		if vals["subject"] != "" {
			payload["subject"] = vals["subject"]
		}
		if a := parseAttrs(vals["attributes"]); len(a) > 0 {
			payload["attributes"] = a
		}
	case "send":
		payload["body"] = vals["body"]
		grp := vals["group"]
		if grp == "" && m.selectedIsFIFO() {
			grp = "default" // FIFO requires a group — never let a send dead-end
		}
		if grp != "" {
			payload["group"] = grp
		}
		if a := parseAttrs(vals["attributes"]); len(a) > 0 {
			payload["attributes"] = a
		}
	case "put":
		payload["key"] = vals["key"]
		payload["body"] = vals["body"]
	}
	verb := m.composerVerb
	m.composerMode = false
	m.cmd.SetValue("")
	r, ok := m.selectedResource()
	if !ok {
		return m, nil
	}
	return m, runAdmin(m.client, m.adminName, verb, r.Name, richPrefix+mustJSON(payload))
}

// cycleResource moves the active resource and resets the item cursor.
func (m *model) cycleResource(dir int) {
	if len(m.adminRes) == 0 {
		return
	}
	m.adminCursor = (m.adminCursor + dir + len(m.adminRes)) % len(m.adminRes)
	m.inspCursor, m.inspExpanded, m.inspItems, m.inspRouting = 0, false, nil, false
	m.itemVP.SetContent(stFaint.Render("loading…"))
	m.itemVP.GotoTop()
}

// selectResourceByIndex sets the active resource (mouse rail click).
func (m *model) selectResourceByIndex(i int) bool {
	if i < 0 || i >= len(m.adminRes) || i == m.adminCursor {
		return false
	}
	m.adminCursor = i
	m.inspCursor, m.inspExpanded, m.inspItems, m.inspRouting = 0, false, nil, false
	m.itemVP.SetContent(stFaint.Render("loading…"))
	m.itemVP.GotoTop()
	return true
}

// selectedResource is the resource the inspector cursor is on.
func (m model) selectedResource() (control.ResourceView, bool) {
	if m.adminCursor < 0 || m.adminCursor >= len(m.adminRes) {
		return control.ResourceView{}, false
	}
	return m.adminRes[m.adminCursor], true
}

func (m model) selectedItem() (inspItem, bool) {
	if m.inspCursor < 0 || m.inspCursor >= len(m.inspItems) {
		return inspItem{}, false
	}
	return m.inspItems[m.inspCursor], true
}

// kind of the active resource ("queue"/"bucket"/"topic").
func (m model) resKind() string {
	if r, ok := m.selectedResource(); ok {
		return r.Kind
	}
	return ""
}

// readAction is the engine action that lists a kind's contents for the inspector.
func readAction(kind string) string {
	switch kind {
	case "queue":
		return "peek"
	case "bucket":
		return "browse"
	case "topic":
		return "subs"
	}
	return ""
}

// composeVerb is the create action for a kind (opens the composer).
func composeVerb(kind string) string {
	switch kind {
	case "queue":
		return "send"
	case "bucket":
		return "put"
	case "topic":
		return "publish"
	}
	return ""
}

// deleteAction is the per-item delete action for a kind ("" = not deletable here).
func deleteAction(kind string) string {
	switch kind {
	case "queue":
		return "del"
	case "bucket":
		return "rm"
	}
	return ""
}

// loadItems re-fetches the active resource's items into the inspector.
func (m model) loadItems() tea.Cmd {
	r, ok := m.selectedResource()
	if !ok {
		return nil
	}
	act := readAction(r.Kind)
	if act == "" {
		return nil
	}
	return fetchItems(m.client, m.adminName, act, r.Name, r.Kind)
}

// sortedInfoKeys returns a resource's Info keys in stable order.
func sortedInfoKeys(info map[string]string) []string {
	ks := make([]string, 0, len(info))
	for k := range info {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// ── word-precise selection over the frozen logs ─────────────────────────────
type wordSpan struct{ start, end int } // rune indices [start,end) on a line

// lineWords splits a line into whitespace-delimited words with their rune spans.
func lineWords(s string) []wordSpan {
	r := []rune(s)
	var ws []wordSpan
	for i := 0; i < len(r); {
		for i < len(r) && unicode.IsSpace(r[i]) {
			i++
		}
		if i >= len(r) {
			break
		}
		st := i
		for i < len(r) && !unicode.IsSpace(r[i]) {
			i++
		}
		ws = append(ws, wordSpan{st, i})
	}
	return ws
}

func (m model) wordsAt(line int) []wordSpan {
	if line < 0 || line >= len(m.copyLines) {
		return nil
	}
	return lineWords(m.copyLines[line])
}

func (m model) lastWord(line int) int { return max(0, len(m.wordsAt(line))-1) }

// wordSel returns the ordered span (loLine, loWord, hiLine, hiWord) of a
// word-wise selection (anchor → cursor).
func (m model) wordSel() (lL, lW, hL, hW int) {
	if m.copyAnchor > m.copyCursor || (m.copyAnchor == m.copyCursor && m.copyAnchorCol > m.copyCol) {
		return m.copyCursor, m.copyCol, m.copyAnchor, m.copyAnchorCol
	}
	return m.copyAnchor, m.copyAnchorCol, m.copyCursor, m.copyCol
}

// curWordRange is the rune span of the word under the cursor (-1,-1 if none).
func (m model) curWordRange() (int, int) {
	ws := m.wordsAt(m.copyCursor)
	if len(ws) == 0 {
		return -1, -1
	}
	w := clampi(m.copyCol, 0, len(ws)-1)
	return ws[w].start, ws[w].end
}

// selWordRange is the selected rune span on line i for a word-wise selection
// (whole intermediate lines are fully covered).
func (m model) selWordRange(i int) (int, int, bool) {
	if !m.copyWordMode || m.copyAnchor < 0 {
		return 0, 0, false
	}
	lL, lW, hL, hW := m.wordSel()
	if i < lL || i > hL {
		return 0, 0, false
	}
	start, end := 0, len([]rune(m.copyLines[i]))
	if ws := m.wordsAt(i); len(ws) > 0 {
		if i == lL {
			start = ws[clampi(lW, 0, len(ws)-1)].start
		}
		if i == hL {
			end = ws[clampi(hW, 0, len(ws)-1)].end
		}
	}
	return start, end, true
}

// handleCopyKey drives copy mode. Granularity defaults to whole LINES; Tab (or
// any horizontal/word motion) flips to WORD. It moves the cursor, optionally
// anchors a range, then copies the selection (a line, lines, a word, or a span).
func (m model) handleCopyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	last := len(m.copyLines) - 1
	exit := func() {
		m.copyMode, m.copyCharMode = false, false
		m.copyAnchor, m.copyAnchorCol, m.copyAnchorColCh = -1, 0, 0
		m.logVP.SetContent(renderLogs(m.logLines))
		if m.follow {
			m.logVP.GotoBottom()
		}
	}
	// The keyboard works in line/word granularity; any keyboard interaction leaves
	// the mouse's character mode (copy/exit keys keep it so the span still copies).
	switch msg.String() {
	case "c", "y", "enter", "esc", "q", "ctrl+c":
	default:
		m.copyCharMode = false
	}
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		exit()
		return m, nil
	case "tab": // toggle line ↔ word granularity
		m.copyWordMode = !m.copyWordMode
		m.copyAnchor = -1
	case "up", "k":
		m.copyCursor--
	case "down", "j":
		m.copyCursor++
	case "left", "h", "b": // previous word (flips to word granularity)
		m.copyWordMode = true
		if m.copyCol > 0 {
			m.copyCol--
		} else if m.copyCursor > 0 {
			m.copyCursor--
			m.copyCol = m.lastWord(m.copyCursor)
		}
	case "right", "l", "w": // next word (flips to word granularity)
		m.copyWordMode = true
		if m.copyCol < m.lastWord(m.copyCursor) {
			m.copyCol++
		} else if m.copyCursor < last {
			m.copyCursor++
			m.copyCol = 0
		}
	case "0", "^":
		m.copyWordMode, m.copyCol = true, 0
	case "$":
		m.copyWordMode, m.copyCol = true, m.lastWord(m.copyCursor)
	case "pgup", "ctrl+u":
		m.copyCursor -= 10
	case "pgdown", "ctrl+d":
		m.copyCursor += 10
	case "g", "home":
		m.copyCursor, m.copyCol = 0, 0
	case "G", "end":
		m.copyCursor = last
	case "v", " ": // toggle a selection range (line range or word span per mode)
		if m.copyAnchor < 0 {
			m.copyAnchor, m.copyAnchorCol = m.copyCursor, m.copyCol
		} else {
			m.copyAnchor, m.copyAnchorCol = -1, 0
		}
	case "a": // select all lines
		m.copyWordMode = false
		m.copyAnchor, m.copyAnchorCol, m.copyCursor = 0, 0, last
	case "c", "y", "enter":
		return m.copySelection()
	default:
		return m, nil
	}
	m.copyCursor = clampi(m.copyCursor, 0, last)
	m.copyCol = clampi(m.copyCol, 0, m.lastWord(m.copyCursor))
	m.refreshCopyView()
	return m, nil
}

// copyRange is the inclusive selected line range (just the cursor if no anchor).
func (m model) copyRange() (int, int) {
	if m.copyAnchor < 0 {
		return m.copyCursor, m.copyCursor
	}
	lo, hi := m.copyAnchor, m.copyCursor
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

// refreshCopyView re-renders the frozen logs with the active selection
// highlighted — whole lines when line-wise, an inline span when word- or
// character-wise (the latter for mouse drags) — and keeps the cursor in view.
func (m *model) refreshCopyView() {
	w := m.logVP.Width
	loL, hiL := m.copyRange()
	curFull := lipgloss.NewStyle().Background(cAccent).Foreground(cSelFg).Width(w)
	selFull := lipgloss.NewStyle().Background(cSel).Foreground(cText).Width(w)
	curSeg := lipgloss.NewStyle().Background(cAccent).Foreground(cSelFg)
	selSeg := lipgloss.NewStyle().Background(cSel).Foreground(cText)
	ccs, cce := m.curWordRange()

	var b strings.Builder
	for i, ln := range m.copyLines {
		disp := truncate(ln, w)
		if m.copyCharMode { // character-precise span (mouse drag)
			dr := []rune(disp)
			cs, ce, has := m.selCharRange(i)
			if !has {
				b.WriteString(stText.Render(disp))
				b.WriteByte('\n')
				continue
			}
			cs, ce = clampi(cs, 0, len(dr)), clampi(ce, 0, len(dr))
			b.WriteString(stText.Render(string(dr[:cs])))
			b.WriteString(selSeg.Render(string(dr[cs:ce])))
			b.WriteString(stText.Render(string(dr[ce:])))
			b.WriteByte('\n')
			continue
		}
		if !m.copyWordMode { // line granularity — highlight whole lines
			switch {
			case i == m.copyCursor:
				b.WriteString(curFull.Render(disp))
			case m.copyAnchor >= 0 && i >= loL && i <= hiL:
				b.WriteString(selFull.Render(disp))
			default:
				b.WriteString(stText.Render(disp))
			}
			b.WriteByte('\n')
			continue
		}
		dr := []rune(disp)
		if len(dr) == 0 { // empty line — still show the cursor if it's here
			if i == m.copyCursor {
				b.WriteString(curSeg.Render(" "))
			}
			b.WriteByte('\n')
			continue
		}
		ss, se, hasSel := m.selWordRange(i)
		// Walk runes, coalescing runs of the same style: 2 = cursor word (on top),
		// 1 = selection span, 0 = plain.
		idAt := func(j int) int {
			if i == m.copyCursor && ccs >= 0 && j >= ccs && j < cce {
				return 2
			}
			if hasSel && j >= ss && j < se {
				return 1
			}
			return 0
		}
		for j := 0; j < len(dr); {
			id := idAt(j)
			k := j + 1
			for k < len(dr) && idAt(k) == id {
				k++
			}
			seg := string(dr[j:k])
			switch id {
			case 2:
				b.WriteString(curSeg.Render(seg))
			case 1:
				b.WriteString(selSeg.Render(seg))
			default:
				b.WriteString(stText.Render(seg))
			}
			j = k
		}
		b.WriteByte('\n')
	}
	m.logVP.SetContent(strings.TrimRight(b.String(), "\n"))
	// Keep the cursor in view without re-centering on every move (so dragging
	// and arrow-stepping stay smooth); only scroll when it leaves the window.
	off, h := m.logVP.YOffset, m.logVP.Height
	if m.copyCursor < off {
		off = m.copyCursor
	} else if m.copyCursor >= off+h {
		off = m.copyCursor - h + 1
	}
	m.logVP.SetYOffset(max(0, off))
}

// onSelect refetches logs immediately when the selection moves, and (for a
// running builtin) its resources so the detail hint and admin panel are ready.
func (m *model) onSelect() tea.Cmd {
	m.logVP.SetContent("")
	m.logErr = ""
	// Moving off the previous instance invalidates its cached resources/items.
	m.adminRes, m.adminActs, m.adminName, m.adminErr = nil, nil, "", ""
	m.adminCursor, m.inspItems, m.inspCursor, m.inspExpanded = 0, nil, 0, false
	v, ok := m.selected()
	if !ok || v.PID == 0 {
		return nil
	}
	cmds := []tea.Cmd{fetchLogs(m.client, v.Name)}
	if builtinAdmin(v.Engine) {
		cmds = append(cmds, fetchResources(m.client, v.Name))
	}
	return tea.Batch(cmds...)
}

// viewHelp is the centered keybinding overlay (toggled with `?`). It also
// documents the mouse and the state glyphs — the things the footer can't fit.
func (m model) viewHelp() string {
	k := func(keys, desc string) string {
		return stAccent.Render(fmt.Sprintf("%-11s", keys)) + " " + stDim.Render(desc)
	}
	sec := func(t string) string { return stLabel.Bold(true).Render(t) }
	col1 := strings.Join([]string{
		sec("Navigate"),
		k("↑↓ / j k", "move selection"),
		k("g / G", "first / last"),
		k("/", "filter by name"),
		"",
		sec("Instance"),
		k("b / enter", "boot (wake it)"),
		k("d", "reap — sleep, keeps data"),
		k("R", "restart"),
		k("p", "keep awake (no auto-sleep)"),
		k("enter", "console (s3/sqs/sns)"),
	}, "\n")
	col2 := strings.Join([]string{
		sec("Logs"),
		k("f", "follow / pause"),
		k("c", "copy mode"),
		k("pgup/pgdn", "scroll"),
		"",
		sec("Display"),
		k("t", "cycle theme"),
		k("? / q", "help / quit"),
	}, "\n")
	mouse := strings.Join([]string{
		sec("Mouse"),
		k("scroll", "sidebar move · logs scroll"),
		k("click", "select · drag selects logs"),
	}, "\n")
	states := sec("States") + "   " +
		stGreen.Render("● active") + "  " +
		lipgloss.NewStyle().Foreground(cGold).Render("○ idle") + "  " +
		stFaint.Render("· asleep") + "  " +
		lipgloss.NewStyle().Foreground(cCyan).Render("⠿ booting") + "  " +
		stErr.Render("✕ error")

	body := stTitle.Render("doze dash") + stDim.Render("  ·  keys") + "\n\n" +
		lipgloss.JoinHorizontal(lipgloss.Top, col1, "      ", col2) + "\n\n" +
		mouse + "\n\n" + states + "\n\n" +
		stFaint.Render("press any key to close")
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).
		Padding(1, 3).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// viewConsole is the resource console: a breadcrumb header, a tab strip of the
// instance's resources (queues/buckets/topics), the selected resource's contents
// as a single navigable list, and a context action footer. One list to move
// through, ←→ to switch resource — no focus juggling.
func (m model) viewConsole() string {
	bodyH := max(3, m.height-6)

	v, _ := m.selected()
	title := stTitle.Render("◆ "+m.adminName) + stDim.Render("  "+v.Engine)
	back := stFaint.Render("esc exit")
	gap := max(1, m.width-lipgloss.Width(title)-lipgloss.Width(back))
	header := title + strings.Repeat(" ", gap) + back
	rule := stFaint.Render(strings.Repeat("─", max(1, m.width)))

	return lipgloss.JoinVertical(lipgloss.Left,
		header, rule, m.consoleTabs(), rule,
		m.inspectorMain(m.width, bodyH),
		rule, m.inspectorFooter())
}

// tabLabels are the resource tab captions — name + a live count + any badge — used
// by both the rendered strip and mouse hit-testing.
func (m model) tabLabels() []string {
	out := make([]string, len(m.adminRes))
	for i, r := range m.adminRes {
		label := r.Name
		if n := resCount(r); n != "" {
			label += "(" + n + ")"
		}
		if b := resBadges(r); b != "" {
			label += " " + b
		}
		if r.Info["dlq"] == "true" { // a dead-letter companion of the primary queue
			label = "↳ " + label
		}
		out[i] = label
	}
	return out
}

// consoleTabs renders the resource tab strip, the active resource bracketed and
// bright.
func (m model) consoleTabs() string {
	switch {
	case m.adminErr != "" && len(m.adminRes) == 0:
		return " " + stErr.Render("✕ "+truncate(m.adminErr, m.width-3))
	case !m.adminLoaded:
		return " " + stFaint.Render("loading…")
	case len(m.adminRes) == 0:
		return " " + stFaint.Render("(no resources)")
	}
	labels := m.tabLabels()
	tabs := make([]string, len(labels))
	for i, label := range labels {
		switch {
		case i == m.adminCursor:
			tabs[i] = stAccent.Bold(true).Render("‹" + label + "›")
		case m.adminRes[i].Info["dlq"] == "true": // dead-letter companion — de-emphasized
			tabs[i] = stFaint.Render(" " + label + " ")
		default:
			tabs[i] = stDim.Render(" " + label + " ")
		}
	}
	return " " + truncate(strings.Join(tabs, stFaint.Render("  ")), m.width-2)
}

// tabAt maps an x column on the tab row to a resource index, or -1.
func (m model) tabAt(x int) int {
	pos := 1 // leading space
	for i, label := range m.tabLabels() {
		segW := lipgloss.Width(" " + label + " ")
		if i == m.adminCursor {
			segW = lipgloss.Width("‹" + label + "›")
		}
		if x >= pos && x < pos+segW {
			return i
		}
		pos += segW + 2 // "  " separator
	}
	return -1
}

// resCount extracts the leading message/object count from a resource's status
// line ("4 msgs" → "4") for the compact tab caption.
func resCount(r control.ResourceView) string {
	if f := strings.Fields(r.Status); len(f) > 0 {
		if _, err := strconv.Atoi(f[0]); err == nil {
			return f[0]
		}
	}
	return ""
}

// resBadges renders a resource's salient traits (FIFO, dead-letter protection)
// from its engine Info, so the queue's nature is visible at a glance.
func resBadges(r control.ResourceView) string {
	var bs []string
	if r.Info["fifo"] == "true" {
		bs = append(bs, "FIFO")
	}
	if r.Info["redrive"] != "" {
		bs = append(bs, "DLQ↩")
	}
	return strings.Join(bs, " ")
}

// inspectorMain is the right column: the live item list, or — while composing — a
// labeled create form.
func (m model) inspectorMain(w, h int) string {
	if m.composerMode {
		return lipgloss.NewStyle().Width(w).Height(h).Render(m.composerForm(w, h))
	}
	return lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h).Render(m.itemVP.View())
}

// composerForm renders the multi-field create form (the active field shows the
// live editor; others show their entered value).
func (m model) composerForm(w, h int) string {
	r, _ := m.selectedResource()
	lines := []string{stTitle.Render("new " + itemNoun(m.resKind())) + stDim.Render("  → "+r.Name), ""}
	for i, f := range m.composerFlds {
		label := "  " + f.label
		if i == m.composerAt {
			label = stAccent.Render("▸ ") + stAccent.Bold(true).Render(f.label)
		} else {
			label = stFaint.Render(label)
		}
		lines = append(lines, label)
		if i == m.composerAt {
			lines = append(lines, "    "+m.cmd.View())
			if f.hint != "" {
				lines = append(lines, "    "+stFaint.Render(f.hint))
			}
		} else if f.value != "" {
			lines = append(lines, "    "+stText.Render(truncate(f.value, w-4)))
		} else {
			lines = append(lines, "    "+stFaint.Render("(empty)"))
		}
		lines = append(lines, "")
	}
	lines = append(lines, stFaint.Render("⇥ next field   ↵ "+m.composerVerb+"   esc cancel"))
	return strings.Join(lines, "\n")
}

// inspectorFooter is the context action bar — the keys for the current kind plus
// the item count, or a confirm prompt.
func (m model) inspectorFooter() string {
	if m.adminPending != "" {
		act := m.adminPending
		if i := strings.IndexByte(act, ':'); i >= 0 {
			act = act[:i]
		}
		return " " + stErr.Render("⚠ "+act+" — ") + stAccent.Render("y") + stDim.Render(" confirm") + stFaint.Render("  ·  any other key cancels")
	}
	if m.composerMode {
		return " " + stDim.Render("composing a new "+itemNoun(m.resKind())+" — fill the form, ↵ to "+m.composerVerb)
	}
	kind := m.resKind()
	key := func(k, l string) string { return stAccent.Render(k) + stDim.Render(" "+l) }
	sep := stFaint.Render("  ·  ")
	parts := []string{key("↑↓", "move"), key("↵", "expand")}
	switch kind {
	case "queue":
		parts = append(parts, key("n", "send"), key("d", "delete"), key("P", "purge"), key("R", "redrive"))
	case "bucket":
		parts = append(parts, key("n", "put"), key("d", "delete"), key("P", "empty"))
	case "topic":
		parts = append(parts, key("n", "test publish"))
	}
	if len(m.adminRes) > 1 {
		parts = append(parts, key("←→", kind))
	}
	parts = append(parts, key("r", "refresh"), key("esc", "exit"))
	left := strings.Join(parts, sep)
	var right string
	if m.inspRouting {
		right = stGreen.Render("routing of last publish — ") + stDim.Render("r resets")
	} else {
		right = stFaint.Render(fmt.Sprintf("%d %s", len(m.inspItems), itemNoun(kind)+"s"))
	}
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right)-2)
	return " " + left + strings.Repeat(" ", gap) + right
}

func (m model) View() string {
	if m.width == 0 {
		mm := m
		mm.width, mm.height = 110, 32
		mm.layout()
		return mm.render()
	}
	return m.render()
}

func (m model) render() string {
	if m.width < 64 || m.height < 18 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			stDim.Render("doze dash needs a larger window")+"\n"+stFaint.Render("at least 64 × 18"))
	}
	if m.showHelp {
		return m.viewHelp()
	}
	if m.adminMode {
		return m.viewConsole()
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.viewSidebar(), "  ", m.viewRight())
	return lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), body, m.viewFooter())
}

// ── header ────────────────────────────────────────────────────────────────
func (m model) viewHeader() string {
	live := stGreen // steady (a blink reads as a fault); the data updating shows it's live
	var up int
	for _, in := range m.resp.Instances {
		if in.PID != 0 {
			up++
		}
	}
	sub := stFaint.Render("mission control")
	if m.flash != "" {
		sub = m.flash
	}
	left := stTitle.Render("◆ doze") + "  " + sub
	listen := m.resp.Listen
	if listen == "" {
		listen = "—"
	}
	// Total RSS lives in the sidebar footer (with the fleet counts); keep the
	// header focused on endpoint / up-count / liveness so memory isn't shown twice.
	right := strings.Join([]string{
		stDim.Render(listen),
		stText.Render(fmt.Sprintf("%d up", up)) + stDim.Render("/"+fmt.Sprint(len(m.resp.Instances))),
		live.Render("●") + stDim.Render(" live"),
	}, stFaint.Render("  ·  "))
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	line := left + strings.Repeat(" ", gap) + right
	rule := stFaint.Render(strings.Repeat("─", max(1, m.width)))
	return line + "\n" + rule
}

// ── sidebar ───────────────────────────────────────────────────────────────
func (m model) viewSidebar() string {
	w := m.sidebarW()
	bodyH := m.bodyH()
	vis := m.visible()

	var maxRAM int64 = 1
	for _, i := range vis {
		if r := m.resp.Instances[i].RAM; r > maxRAM {
			maxRAM = r
		}
	}
	var rows []string
	for _, ln := range m.sidebarLines() {
		if ln.header != "" {
			rows = append(rows, m.sidebarHeader(ln.header, w))
			continue
		}
		rows = append(rows, m.sidebarRow(m.resp.Instances[vis[ln.di]], ln.di == m.cursor, w, maxRAM))
	}
	if len(rows) == 0 {
		rows = append(rows, stDim.Render("  (no instances)"))
	}
	footer := m.sidebarTotals(w)
	avail := max(0, bodyH-len(footer))
	for len(rows) < avail {
		rows = append(rows, "")
	}
	all := append(rows[:avail], footer...)
	return lipgloss.NewStyle().Width(w).
		Border(lipgloss.NormalBorder(), false, true, false, false). // right edge only
		BorderForeground(cPanel).
		Render(strings.Join(all, "\n"))
}

// sidebarHeader renders a group section label (a faint, upper-case heading that
// reads distinctly from the indented instance rows below it).
func (m model) sidebarHeader(label string, w int) string {
	return lipgloss.NewStyle().Width(w).Render(stFaint.Render(strings.ToUpper(label)))
}

func (m model) sidebarRow(in control.InstanceView, selected bool, w int, maxRAM int64) string {
	st := displayState(in)
	// The name is the primary element of the row: bold, and given most of the
	// width. The precise RAM figure lives in the detail pane and the footer total,
	// so the sidebar stays uncluttered and the names read large.
	nameStyle := stText.Bold(true)
	if selected {
		nameStyle = stAccent.Bold(true)
	}
	// A small bar proportional to the heaviest instance keeps relative memory
	// glanceable without spending a column on digits.
	const bw = 5
	var bar string
	if in.RAM > 0 {
		filled := clampi(int(float64(in.RAM)/float64(maxRAM)*float64(bw)+0.5), 0, bw)
		bar = lipgloss.NewStyle().Foreground(stateColor(st)).Render(strings.Repeat("▰", filled)) +
			stFaint.Render(strings.Repeat("▱", bw-filled))
	} else {
		bar = stFaint.Render(strings.Repeat("▱", bw))
	}
	nameMax := w - bw - 7
	if in.KeepAwake {
		nameMax -= 2 // room for the ▲ keep-awake marker
	}
	left := m.glyph(in) + " " + nameStyle.Render(truncate(in.Name, max(3, nameMax)))
	if in.KeepAwake {
		left += stAccent.Render(" ▲") // pinned: exempt from auto-sleep
	}
	// Reserve the 2-char row prefix AND a 1-col right margin so the bar sits just
	// inside the panel's right border instead of flush against it.
	gap := max(1, w-lipgloss.Width(left)-bw-3)
	inner := left + strings.Repeat(" ", gap) + bar + " "
	if selected {
		return lipgloss.NewStyle().Background(cSel).Width(w).Render(stAccent.Render("▌") + " " + inner)
	}
	return lipgloss.NewStyle().Width(w).Render("  " + inner)
}

// sidebarTotals is the at-a-glance resource summary pinned to the bottom.
func (m model) sidebarTotals(w int) []string {
	var act, idle, asleep, errc int
	var total int64
	var cpuTotal float64
	for _, in := range m.resp.Instances {
		switch displayState(in) {
		case "active", "booting":
			act++
		case "idle":
			idle++
		case "error":
			errc++
		default:
			asleep++
		}
		if in.PID != 0 {
			total += in.RAM
			cpuTotal += in.CPU
		}
	}
	counts := stGreen.Render(fmt.Sprintf("●%d", act)) + "  " +
		lipgloss.NewStyle().Foreground(cGold).Render(fmt.Sprintf("○%d", idle)) + "  " +
		stFaint.Render(fmt.Sprintf("·%d", asleep))
	if errc > 0 {
		counts += "  " + stErr.Render(fmt.Sprintf("✕%d", errc))
	}
	mem := stLabel.Render("cpu ") + stAccent.Render(orDash(ui.CPUStr(cpuTotal))) +
		stFaint.Render("  ") + stLabel.Render("mem ") + stAccent.Render(orDash(memStr(total)))
	return []string{
		stFaint.Render(strings.Repeat("─", max(1, w))),
		" " + counts,
		" " + mem,
	}
}

func (m model) glyph(in control.InstanceView) string {
	st := displayState(in)
	s := lipgloss.NewStyle().Foreground(stateColor(st))
	switch st {
	case "booting":
		return s.Render(string(spinner[m.frame%len(spinner)]))
	case "active":
		return s.Render("●") // filled
	case "idle":
		return s.Render("○") // hollow
	case "error":
		return s.Render("✕")
	default:
		return stFaint.Render("·") // asleep — small + faint
	}
}

// ── right pane ────────────────────────────────────────────────────────────
func (m model) viewRight() string {
	w := m.rightW()
	v, ok := m.selected()
	if !ok {
		return lipgloss.NewStyle().Width(w).Height(m.bodyH()).
			Align(lipgloss.Center, lipgloss.Center).Foreground(cDim).
			Render("nothing selected")
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.viewDetail(v, w), m.viewLogs(v, w))
}

func (m model) viewDetail(v control.InstanceView, w int) string {
	st := displayState(v)
	title := stTitle.Render(v.Name) + stDim.Render("  "+v.Engine)
	if v.Version != "" {
		title += stDim.Render(" " + v.Version)
	}
	if v.KeepAwake {
		title += stAccent.Render("   ▲ kept awake")
	}
	badge := lipgloss.NewStyle().Foreground(stateColor(st)).Bold(true).Render(displayLabel(st))

	vit := func(label, val string) string { return stLabel.Render(label) + " " + stText.Render(orDash(val)) }
	cpuVal := ""
	if v.PID != 0 {
		cpuVal = ui.CPUStr(v.CPU)
	}
	// A process is a supervised app (not a proxied backend), so its vitals lead with
	// a health badge and restart count rather than a doze connection count.
	isProc := v.Engine == "process"
	cells := []string{stLabel.Render("state") + " " + badge}
	if isProc {
		cells = append(cells, stLabel.Render("health")+" "+healthBadge(v.Healthy))
	} else {
		cells = append(cells, vit("conns", fmt.Sprint(v.Conns)))
	}
	cells = append(cells, vit("cpu", cpuVal), vit("pid", pidStr(v.PID)), vit("up", ui.Uptime(v.StartedAt)))
	if v.RestartCount > 0 {
		cells = append(cells, vit("restarts", fmt.Sprint(v.RestartCount)))
	}
	row1 := strings.Join(cells, stFaint.Render("   "))
	row2 := stLabel.Render("endpoint ") + stText.Render(orDash(v.Endpoint))
	urlLine := stLabel.Render(orEnv(v.EnvVar)+" ") + stAccent.Render(truncate(orDash(v.URL), w-len(orEnv(v.EnvVar))-7))

	dataLine := stLabel.Render("data ") + stDim.Render(truncate(abbrevHome(v.DataDir), w-8))

	// Memory: a filled braille area trace with a right-edge y-axis. When the
	// instance is asleep there's nothing to plot, so say so plainly rather than
	// drawing a dash-filled flatline that reads as "broken".
	const lbl = 7 // width of the "memory" gutter
	const rows = 5
	h := m.hist[v.Name]
	pad := strings.Repeat(" ", lbl)
	mem := make([]string, rows)
	if v.PID == 0 {
		mem[1] = stLabel.Render("memory ") + stFaint.Render("— asleep, no live trace")
	} else {
		// Keep the pixel count (gw*2) below the sample count so the trace is dense
		// (downsampled) rather than upsampled into long straight runs.
		gw := clampi(w-lbl-18, 16, 110)
		graph := lipgloss.NewStyle().Foreground(cAccent)
		varying := h != nil && varies(h.ram)
		var g []string
		if varying {
			g = brailleGraph(h.ram, gw, rows, true) // filled area
		} else {
			g = make([]string, rows)
			for i := range g {
				g[i] = strings.Repeat(" ", gw)
			}
			g[1] = strings.Repeat("⠒", gw) // running but flat — a steady midline
			graph = stFaint
		}
		// Right-edge y-axis: peak anchors the TOP, current (bright) the label row,
		// low + time span the BOTTOM. Bounds appear only with a varying trace.
		lo, hi := memBounds(h)
		peakLbl, lowLbl, span := "", "", ""
		if varying {
			peakLbl, lowLbl, span = memStr(hi), memStr(lo), memWindow(h)
		}
		for i := range g {
			gutter, suffix := pad, ""
			switch i {
			case 0:
				suffix = "  " + stDim.Render(orDash(peakLbl)) // peak (top of scale)
			case 1:
				gutter = stLabel.Render("memory ")
				suffix = "  " + stText.Render(orDash(memStr(v.RAM))) // current (bright)
			case rows - 1:
				tail := orDash(lowLbl) // low (bottom of scale)
				if span != "" {
					tail += stFaint.Render(" · " + span)
				}
				suffix = "  " + stFaint.Render(tail)
			}
			mem[i] = gutter + graph.Render(g[i]) + suffix
		}
	}

	var status string
	switch {
	case v.LastError != "":
		status = stErr.Render("✕ " + truncate(v.LastError, w-6))
	case v.Tainted:
		status = stErr.Render("✕ structure incomplete — run `doze apply` to re-converge")
	case isProc && (st == "active" || st == "idle"):
		// A process is a supervised app: report liveness/health, not connections.
		switch {
		case v.Healthy != nil && !*v.Healthy:
			status = stErr.Render("✕ running but health probe failing")
		case v.Healthy != nil && *v.Healthy:
			status = stGreen.Render("● running — healthy")
		default:
			status = lipgloss.NewStyle().Foreground(cCyan).Render("● running — waiting for health")
		}
	case st == "active":
		status = stGreen.Render("● serving " + fmt.Sprint(v.Conns) + " connection(s)")
	case st == "booting":
		status = lipgloss.NewStyle().Foreground(cCyan).Render(string(spinner[m.frame%len(spinner)]) + " booting…")
	case st == "idle":
		// Running, zero connections — not asleep. A subtle countdown to reap, unless
		// it's pinned awake.
		switch {
		case v.KeepAwake:
			status = stAccent.Render("▲ kept awake — won't auto-sleep")
		case !v.IdleSince.IsZero() && m.resp.IdleTimeout > 0:
			status = m.reapHint(v)
		default:
			status = lipgloss.NewStyle().Foreground(cGold).Render("○ idle — up, 0 connections")
		}
	default: // reaped
		status = stDim.Render("· asleep — connect to wake it")
	}

	// For a running builtin, the connection count is less interesting than its
	// resources — surface the count, the names (so each queue/bucket/topic is
	// visible without entering), and the console affordance.
	var resLine string
	if builtinAdmin(v.Engine) && v.PID != 0 {
		hint := stFaint.Render("press ") + stAccent.Render("enter") + stFaint.Render(" for the console")
		if m.adminName == v.Name && len(m.adminRes) > 0 {
			kind := m.adminRes[0].Kind + "s"
			status = stGreen.Render("● ") + stAccent.Render(fmt.Sprintf("%d %s", len(m.adminRes), kind)) +
				stDim.Render(" · ") + hint
			names := make([]string, 0, len(m.adminRes))
			for _, r := range m.adminRes {
				n := r.Name
				if b := resBadges(r); b != "" {
					n += " " + b
				}
				names = append(names, n)
			}
			resLine = stFaint.Render("  " + truncate(strings.Join(names, "  ·  "), w-6))
		} else {
			status = stGreen.Render("● serving") + stDim.Render(" · ") + hint
		}
	}

	lines := []string{
		title,
		stFaint.Render(strings.Repeat("╌", max(1, w-6))),
		row1, row2, urlLine, dataLine, "",
	}
	lines = append(lines, mem...)
	lines = append(lines, "", status)
	if resLine != "" {
		lines = append(lines, resLine)
	}
	card := strings.Join(lines, "\n")
	return lipgloss.NewStyle().Width(w).Height(detailH).
		Border(lipgloss.RoundedBorder()).BorderForeground(cPanel).
		Padding(0, 2).Render(card)
}

// reapHint is a quiet, compact idle countdown — a thin baseline that empties.
func (m model) reapHint(v control.InstanceView) string {
	remain := m.resp.IdleTimeout - time.Since(v.IdleSince)
	if remain < 0 {
		remain = 0
	}
	const track = 16
	filled := clampi(int(float64(remain)/float64(m.resp.IdleTimeout)*float64(track)+0.5), 0, track)
	bar := lipgloss.NewStyle().Foreground(cGold).Render(strings.Repeat("▔", filled)) +
		stFaint.Render(strings.Repeat("▔", track-filled))
	idle := lipgloss.NewStyle().Foreground(cGold).Render("○")
	return idle + stDim.Render(" idle · sleeps in "+compactDur(remain)+"  ") + bar
}

// memStr formats resident memory as MB (or GB at ≥1024 MB) with two decimals,
// e.g. "42.18 MB" / "1.25 GB". Empty for zero so callers can show a dash.
func memStr(b int64) string {
	if b <= 0 {
		return ""
	}
	const mb = 1024 * 1024
	if b < 1024*mb {
		return fmt.Sprintf("%.2f MB", float64(b)/mb)
	}
	return fmt.Sprintf("%.2f GB", float64(b)/(1024*mb))
}

// memBounds returns the min and max resident memory across the history window,
// which anchor the bottom and top of the auto-scaled trace (its y-axis).
func memBounds(h *history) (lo, hi int64) {
	if h == nil || len(h.ram) == 0 {
		return 0, 0
	}
	mn, mx := h.ram[0], h.ram[0]
	for _, v := range h.ram {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	return int64(mn), int64(mx)
}

// memWindow is the time span the history currently covers (its x-extent).
func memWindow(h *history) string {
	if h == nil || len(h.ram) == 0 {
		return ""
	}
	return compactDur(time.Duration(len(h.ram)) * refreshMS)
}

// varies reports whether a series has any movement worth plotting as a line.
func varies(vals []float64) bool {
	if len(vals) < 2 {
		return false
	}
	mn, mx := vals[0], vals[0]
	for _, v := range vals {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	return mx > 0
}

// brailleGraph plots vals using braille dots (each cell packs 2×4 dots). With
// fill, every column is shaded from the line down to the baseline — an area chart
// whose silhouette reads as a solid shape; otherwise it's a thin connected line.
// Returns hRows plain strings (top to bottom); the caller colors them. Scaled to
// the window's own min..max so small movements are visible.
func brailleGraph(vals []float64, w, hRows int, fill bool) []string {
	cols, rowsPx := w*2, hRows*4
	cell := make([][]uint8, hRows)
	for i := range cell {
		cell[i] = make([]uint8, w)
	}
	if len(vals) > 0 {
		mn, mx := vals[0], vals[0]
		for _, v := range vals {
			if v < mn {
				mn = v
			}
			if v > mx {
				mx = v
			}
		}
		span := mx - mn
		yOf := func(v float64) int {
			f := 0.5
			if span > 0 {
				f = (v - mn) / span
			}
			return clampi(int((1-f)*float64(rowsPx-1)+0.5), 0, rowsPx-1) // 0 = top
		}
		prev := -1
		for x := 0; x < cols; x++ {
			idx := 0
			if cols > 1 {
				idx = x * (len(vals) - 1) / (cols - 1)
			}
			y := yOf(vals[idx])
			lo, hi := y, y
			if fill {
				hi = rowsPx - 1 // shade from the line down to the baseline (area)
			} else if prev >= 0 { // bridge to the previous point so the line is continuous
				if prev < lo {
					lo = prev
				}
				if prev > hi {
					hi = prev
				}
			}
			for yy := lo; yy <= hi; yy++ {
				setDot(cell, x, yy)
			}
			prev = y
		}
	}
	out := make([]string, hRows)
	for r := 0; r < hRows; r++ {
		var b strings.Builder
		for c := 0; c < w; c++ {
			b.WriteRune(rune(0x2800 + int(cell[r][c])))
		}
		out[r] = b.String()
	}
	return out
}

// setDot lights the braille dot at pixel (x,y) within the cell grid.
func setDot(cell [][]uint8, x, y int) {
	// Braille bit layout per cell (2 cols × 4 rows of dots).
	bits := [4][2]uint8{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}
	cell[y/4][x/2] |= bits[y%4][x%2]
}

func (m model) viewLogs(v control.InstanceView, w int) string {
	mode := stDim.Render("paused")
	if m.follow {
		mode = stGreen.Render("following")
	}
	if m.copyMode {
		// A clear, always-visible toggle so the tab switch is never a mystery: the
		// active granularity is emphasized, the other dimmed, with the key spelled out.
		on, off := stAccent.Bold(true), stFaint
		lineLbl, wordLbl := on.Render("LINE"), off.Render("word")
		if m.copyWordMode {
			lineLbl, wordLbl = off.Render("line"), on.Render("WORD")
		}
		mode = stDim.Render("copy ") + lineLbl + stFaint.Render(" / ") + wordLbl + stDim.Render("  ·  tab switches")
	}
	title := stLabel.Render("logs ") + stFaint.Render("· "+v.Name)
	gap := max(1, w-6-lipgloss.Width(title)-lipgloss.Width(mode))
	head := title + strings.Repeat(" ", gap) + mode

	var bodyTxt string
	switch {
	case v.PID == 0:
		bodyTxt = stFaint.Render("(asleep — no live log stream)") + strings.Repeat("\n", max(0, m.logVP.Height-1))
	case m.logErr != "":
		bodyTxt = stDim.Render(m.logErr) + strings.Repeat("\n", max(0, m.logVP.Height-1))
	default:
		bodyTxt = m.logVP.View()
	}
	inner := head + "\n" + stFaint.Render(strings.Repeat("╌", max(1, w-6))) + "\n" + bodyTxt
	return lipgloss.NewStyle().Width(w).
		Border(lipgloss.RoundedBorder()).BorderForeground(cPanel).
		Padding(0, 2).Render(inner)
}

// ── footer ────────────────────────────────────────────────────────────────
func (m model) viewFooter() string {
	key := func(k, label string) string { return stAccent.Render(k) + stDim.Render(" "+label) }
	sep := stFaint.Render("  ·  ")
	if m.filtering {
		return stAccent.Render(m.filter.View()) + stFaint.Render("   enter/esc")
	}
	if m.copyMode {
		toggle := key("tab", "word mode")
		motion := key("↑↓", "line")
		if m.copyWordMode {
			toggle = key("tab", "line mode")
			motion = key("←→", "word") + sep + key("↑↓", "line")
		}
		return strings.Join([]string{
			toggle, motion, key("v", "select"), key("y", "copy"), key("esc", "exit"),
		}, sep)
	}
	parts := []string{key("↑↓", "select"), key("b", "boot"), key("d", "reap")}
	if v, ok := m.selected(); ok && builtinAdmin(v.Engine) && v.PID != 0 {
		parts = append(parts, key("↵", "console"))
	}
	parts = append(parts,
		key("f", "follow"), key("c", "copy"), key("/", "filter"),
		key("t", "theme"), key("?", "more"), key("q", "quit"))
	return strings.Join(parts, sep)
}

// ── helpers ───────────────────────────────────────────────────────────────

// displayState promotes a reaped instance carrying an error to "error", and a
// running-but-tainted instance (last converge failed) to "tainted" so it never
// reads as healthy.
func displayState(in control.InstanceView) string {
	if in.LastError != "" && (in.State == "reaped" || in.State == "") {
		return "error"
	}
	if in.Tainted {
		return "tainted"
	}
	if in.State == "" {
		return "reaped"
	}
	return in.State
}

// displayLabel is the user-facing name for a state. "reaped" is doze's internal
// term; users see "asleep" everywhere, so the badge says ASLEEP, not REAPED.
func displayLabel(state string) string {
	if state == "reaped" {
		return "ASLEEP"
	}
	return strings.ToUpper(state)
}

// healthBadge renders a supervised process's latest liveness probe result: nil
// (not yet probed) reads as "starting".
func healthBadge(h *bool) string {
	switch {
	case h == nil:
		return lipgloss.NewStyle().Foreground(cCyan).Render("starting")
	case *h:
		return stGreen.Render("healthy")
	default:
		return stErr.Render("unhealthy")
	}
}

func renderLogs(lines []string) string {
	if len(lines) == 0 {
		return stFaint.Render("(no output yet)")
	}
	var b strings.Builder
	for _, ln := range lines {
		b.WriteString(stText.Render(ln))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	if w <= 1 || len(r) == 0 {
		return "…"
	}
	if w-1 > len(r) {
		return s
	}
	return string(r[:w-1]) + "…"
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// abbrevHome shortens a path under the user's home dir to a leading ~.
func abbrevHome(p string) string {
	if p == "" {
		return "—"
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
func orEnv(s string) string {
	if s == "" {
		return "url"
	}
	return s
}
func pidStr(p int) string {
	if p == 0 {
		return "—"
	}
	return fmt.Sprint(p)
}
func compactDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d >= time.Minute {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
func clampi(v, lo, hi int) int { return max(lo, min(hi, v)) }
