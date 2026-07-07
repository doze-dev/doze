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
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/actions"
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

// defaultAdaptive is the default (violet) theme as light/dark pairs, so the dash
// stays readable on light terminals. The Dark halves are the violet theme's tuned
// values (the source of truth internal/ui aligns with); the named themes keep
// their dark-only palettes. adaptive wraps a theme color with its light variant.
func adaptive(light string, dark lipgloss.Color) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: string(dark)}
}

// noColor mirrors internal/ui's NO_COLOR gating: when set, lipgloss strips all
// styling, so selections must be indicated with reverse video / markers instead.
var noColor = os.Getenv("NO_COLOR") != ""

// reverseVideo wraps s in a raw reverse-video escape — the NO_COLOR selection
// indicator (NO_COLOR bans color, not emphasis; lipgloss drops everything).
func reverseVideo(s string) string { return "\x1b[7m" + s + "\x1b[27m" }

// selStyled renders a selection span: reverse video under NO_COLOR (so it stays
// visible), the given background style otherwise.
func selStyled(st lipgloss.Style, s string) string {
	if noColor {
		return reverseVideo(s)
	}
	return st.Render(s)
}

var (
	cAccent, cText, cDim, cFaint, cPanel, cSel, cSelFg lipgloss.TerminalColor
	cGreen, cGold, cCyan, cRed                         lipgloss.TerminalColor

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
	if activeTheme == 0 { // the default theme adapts to light terminals
		cAccent, cText = adaptive("#6F42C1", t.accent), adaptive("#2A2D36", t.text)
		cDim, cFaint = adaptive("#666C78", t.dim), adaptive("#B8BEC9", t.faint)
		cPanel, cSel = adaptive("#D8D2E8", t.panel), adaptive("#E4DEF5", t.sel)
		cSelFg = adaptive("#F8F6FE", t.selFg)
		cGreen, cGold = adaptive("#1A7F37", t.green), adaptive("#9A6700", t.gold)
		cCyan, cRed = adaptive("#0969DA", t.cyan), adaptive("#CF222E", t.red)
	}
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

func stateColor(state string) lipgloss.TerminalColor {
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

// itemID is a stable identity for an item across refreshes: the delete handle /
// key where the engine provides one, else the rendered content itself.
func itemID(it inspItem) string {
	if it.delArg != "" {
		return "h:" + it.delArg
	}
	return "t:" + it.title + "\x00" + it.meta
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
			{key: "file", label: "file", hint: "path to a local file — its contents become the object (leave blank to type inline)"},
			{key: "key", label: "key", hint: "object key (defaults to the file name)"},
			{key: "body", label: "body", hint: "inline contents, when no file is given"},
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
	logVP    viewport.Model
	logErr   string
	logLines []string // raw log lines of the selected instance (for copy mode)

	// copy mode: a frozen, keyboard-navigable selection over the logs. The
	// keyboard selects whole LINES; the MOUSE uses character granularity
	// (copyCharMode) so a drag selects an exact span — including within a single
	// line — like a normal terminal.
	copyMode        bool
	copyCharMode    bool // mouse drag: character-precise span (overrides lines)
	copyLines       []string
	copyCursor      int
	copyAnchor      int // selection start line; -1 = no range
	copyColCh       int // rune index on the cursor line (char mode)
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
	flash      string         // flash text (raw; styled + width-fitted at render time)
	flashStyle lipgloss.Style // how to paint the flash
	flashErr   bool           // an action error: persists until dismissed (esc / next action)
	flashFrame int

	// dashPending is a destructive dash action ("down:<name>" / "restart:<name>")
	// awaiting y/n confirmation in the footer, mirroring the console's pattern.
	dashPending string

	// connLost is set when the last status poll failed: the daemon is unreachable
	// (the tick keeps retrying). lastOK timestamps the newest good data so the
	// header can say how stale the picture is.
	connLost bool
	lastOK   time.Time

	// palette: the k9s-style `:` command prompt. The input is a plain buffer
	// (append/backspace only — no cursor), with a suggestion list overlaid above
	// the footer; palSel is the highlighted suggestion.
	paletteMode bool
	palInput    string
	palSel      int

	// management view (:console): the manageable builtins (s3/sqs/sns) in a left
	// switcher rail, the selected one's live console on the right. There is no
	// rail focus — the console is always active; `[`/`]` switch which service.
	mgmtMode   bool
	mgmtCursor int
}

// setFlash records a transient status message (auto-cleared after ~2.5s).
func (m *model) setFlash(st lipgloss.Style, s string) {
	m.flash, m.flashStyle, m.flashErr = s, st, false
	m.flashFrame = m.frame
}

// setFlashErr records an action failure that stays visible until dismissed
// (esc, or any subsequent action replacing it) — errors must not evaporate.
func (m *model) setFlashErr(s string) {
	m.flash, m.flashStyle, m.flashErr = s, stErr, true
	m.flashFrame = m.frame
}

// clearFlash drops the current flash (esc dismisses a persistent error).
func (m *model) clearFlash() { m.flash, m.flashErr = "", false }

// Run validates a daemon is up and launches the dashboard.
func Run(socketPath string) error {
	c := control.NewClient(socketPath)
	if !c.Available() {
		return fmt.Errorf("no daemon is running (boot an instance with `doze wake <name>`)")
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
		client: c,
		filter: fi,
		cmd:    ci,
		hist:   map[string]*history{},
		logVP:  viewport.New(0, 0),
		itemVP: viewport.New(0, 0),
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
// your own supervised apps are "processes", everything else (databases, caches,
// queues, buckets, topics) is "services". Engines arrive as plugin modules, but
// that's plumbing — users think in services (matches the CLI status table).
func groupOf(in control.InstanceView) string {
	if in.Engine == "process" {
		return "processes"
	}
	return "services"
}

// groupRank orders the two divisions: services first, processes last.
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
	// The detail card is content-sized, so the logs pane takes whatever the body
	// has left under it (border 2 + title + rule = 4 rows of logs-box chrome).
	m.logVP.Width = max(4, m.rightW()-6)
	m.logVP.Height = max(3, m.bodyH()-m.detailBoxH()-4)
	// inspector item list: full width (minus the management rail when that view
	// hosts the console), the body height between the tab strip and the footer
	// (header, rule, tabs, rule … rule, footer = 6 chrome rows).
	consoleW := m.width
	if m.mgmtMode {
		consoleW = max(20, m.width-mgmtRailW-1)
	}
	m.itemVP.Width = max(10, consoleW-2)
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
			// A routing snapshot — or an item the user is reading expanded — is held
			// until they move on, so a queue shift can't swap it out from under them.
			if !m.inspRouting && !m.inspExpanded {
				if c := m.loadItems(); c != nil {
					cmds = append(cmds, c)
				}
			}
		}
		return m, tea.Batch(cmds...)

	case spinMsg:
		m.frame++
		// Successes fade; errors persist until dismissed (esc or the next action).
		if m.flash != "" && !m.flashErr && m.frame-m.flashFrame > 24 { // ~2.5s at 110ms
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
		m.connLost = msg.err != nil // shown in the header; the tick keeps retrying
		if msg.err == nil {
			m.lastOK = time.Now()
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
		m.layout() // the content-sized detail card may have grown/shrunk
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
					// Auto-tail: keep pinned to the newest line when already at the
					// bottom; if the user has scrolled up to read, leave them there.
					wasBottom := m.logVP.AtBottom()
					m.logVP.SetContent(renderLogs(msg.lines, m.logVP.Width))
					if wasBottom {
						m.logVP.GotoBottom()
					}
				}
			}
		}
		return m, nil

	case actionMsg:
		if msg.err != nil {
			m.setFlashErr("✗ " + msg.verb + " " + msg.name + ": " + msg.err.Error())
		} else if msg.verb != "pin" { // pin already flashed its direction
			m.setFlash(stGreen, "✓ "+msg.verb+" "+msg.name)
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
			// Resources feed the detail card's status strip, so its height just
			// changed; re-layout now to resize the logs pane in the same frame —
			// otherwise the taller card overflows the viewport for one tick and
			// the whole screen scrolls (the header jumps off, then returns).
			m.layout()
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
				// Re-pin the selection by item identity, not position — when the queue
				// shifts under a refresh the cursor follows its item instead of silently
				// landing on a different one.
				prevID := ""
				if it, ok := m.selectedItem(); ok {
					prevID = itemID(it)
				}
				m.inspItems = msg.items
				if prevID != "" {
					for i, it := range m.inspItems {
						if itemID(it) == prevID {
							m.inspCursor = i
							break
						}
					}
				}
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
			m.setFlashErr("✗ " + cleanErr(msg.err))
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
				m.setFlash(stGreen, fmt.Sprintf("✓ routed to %d of %d subscription(s)", matched, len(items)))
				return m, fetchResources(m.client, m.adminName)
			}
		} else if head := firstLine(msg.result); head != "" {
			m.setFlash(stGreen, "✓ "+head)
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
	if m.paletteMode || m.mgmtMode || m.dashPending != "" {
		return m, nil // keyboard-driven overlays/views own the input
	}
	if m.adminMode { // the inspector owns the mouse so clicks never leak to the dash
		if m.composerMode {
			return m, nil
		}
		const tabRow = 2  // header(0) rule(1) tabs(2) rule(3) list(4…)
		const listTop = 4 // first row of the item list
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
			// The item list: walk each item's actual rendered height (1 row without
			// meta, 2 with, more when expanded), offset by scroll.
			row := msg.Y - listTop + m.itemVP.YOffset
			if idx := m.itemIndexAtRow(row); idx >= 0 {
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
				m.copyCharMode = true
				m.copyCursor, m.copyColCh = ln, m.logRuneColAt(ln, msg.X)
				m.copyAnchor, m.copyAnchorColCh = m.copyCursor, m.copyColCh
				m.refreshCopyView()
			case tea.MouseActionMotion: // drag → extend the character span
				ln := m.logLineAt(msg.Y)
				m.copyCursor = ln
				if m.copyCharMode {
					m.copyColCh = m.logRuneColAt(ln, msg.X)
				}
				m.refreshCopyView()
			case tea.MouseActionRelease:
				dragged := m.copyAnchor != m.copyCursor ||
					(m.copyCharMode && m.copyAnchorColCh != m.copyColCh)
				if dragged {
					return m.copySelection() // dragged → copy exactly what was spanned
				}
				m.copyAnchor, m.copyAnchorColCh = -1, 0 // plain click → position only
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
			m.copyMode, m.copyCharMode = true, true
			m.copyLines = m.logLines
			ln := m.logLineAt(msg.Y)
			m.copyCursor, m.copyColCh = ln, m.logRuneColAt(ln, msg.X)
			m.copyAnchor, m.copyAnchorColCh = m.copyCursor, m.copyColCh
			m.refreshCopyView()
		}
	}
	return m, nil
}

// detailBoxH is the rendered height of the detail card including its border.
// The card is content-sized, so the logs pane and the mouse math derive from
// the same builder the renderer uses.
func (m model) detailBoxH() int {
	v, ok := m.selected()
	if !ok {
		return 2
	}
	return len(m.detailLines(v, m.rightW())) + 2
}

// logsTop is the screen row of the first visible log line (header + detail box +
// the logs box's top border/title/rule).
func (m model) logsTop() int { return 2 + m.detailBoxH() + 3 }

// logsRegion reports whether screen row y falls inside the log viewport.
func (m model) logsRegion(y int) bool {
	return y >= m.logsTop() && y < m.logsTop()+m.logVP.Height
}

// logLineAt maps a screen row to a log line index (clamped).
func (m model) logLineAt(y int) int {
	return clampi(m.logVP.YOffset+(y-m.logsTop()), 0, max(0, len(m.copyLines)-1))
}

// logRuneColAt maps a screen X to a rune index on the given log line (char
// granularity for mouse selection). The end position (len) is allowed so a drag
// can include the last character. The logs content starts after the sidebar
// (sidebarW), the 2-col gap, the box's left border, and its 2-col padding.
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

// copySelection writes the selected text to the clipboard and leaves copy mode,
// flashing exactly what was copied ("✓ copied 3 lines" / "✓ copied 27 chars").
func (m model) copySelection() (tea.Model, tea.Cmd) {
	var text, what string
	if m.copyCharMode && m.copyAnchor >= 0 { // character-precise span (mouse drag)
		text = m.selectedCharText()
		what = plural(len([]rune(text)), "char")
	} else { // whole line(s) — the keyboard default
		lo, hi := m.copyRange()
		text = strings.Join(m.copyLines[lo:hi+1], "\n")
		what = plural(hi-lo+1, "line")
	}
	err := clipboard.WriteAll(text)
	m.copyMode, m.copyCharMode = false, false
	m.copyAnchor, m.copyAnchorColCh = -1, 0
	m.logVP.SetContent(renderLogs(m.logLines, m.logVP.Width))
	m.logVP.GotoBottom() // leaving copy mode returns to the live tail
	if err != nil {
		m.setFlashErr("✗ copy failed: " + err.Error())
	} else {
		m.setFlash(stGreen, "✓ copied "+what)
	}
	return m, nil
}

// plural is "1 line" / "3 lines" — copy feedback reads like a sentence.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
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
	if m.paletteMode {
		return m.handlePaletteKey(msg)
	}
	if m.copyMode {
		return m.handleCopyKey(msg)
	}
	if m.mgmtMode {
		return m.handleMgmtKey(msg)
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
	if m.dashPending != "" { // confirm a staged reap / restart (footer prompt)
		verb, name, _ := strings.Cut(m.dashPending, ":")
		m.dashPending = ""
		switch msg.String() {
		case "y", "Y", "enter":
			m.setFlash(stDim, dashVerbLabel(verb)+" "+orAllServices(name)+"…")
			return m, do(m.client, verb, name)
		default:
			m.setFlash(stDim, "cancelled")
			return m, nil
		}
	}

	vis := m.visible()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc": // dismiss a persistent error flash
		m.clearFlash()
		return m, nil
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
	case "pgup", "ctrl+u":
		m.logVP.HalfViewUp()
		return m, nil
	case "pgdown", "ctrl+d":
		m.logVP.HalfViewDown()
		return m, nil
	case "c":
		if len(m.logLines) > 0 { // enter copy mode (line selection)
			m.copyMode, m.copyCharMode = true, false
			m.copyLines = m.logLines
			m.copyCursor = len(m.copyLines) - 1
			m.copyAnchor, m.copyAnchorColCh = -1, 0
			m.refreshCopyView()
		}
		return m, nil
	case "t":
		applyTheme(activeTheme + 1)
		saveTheme()
		m.setFlash(stAccent, "theme · "+themes[activeTheme].name)
		return m, nil
	case "?":
		m.showHelp = true
		return m, nil
	case ":", "ctrl+k": // open the command palette (Spotlight-style)
		m.paletteMode, m.palInput, m.palSel = true, "", 0
		return m, nil
	case "r":
		return m, refresh(m.client)
	}

	if v, ok := m.selected(); ok {
		switch msg.String() {
		case "enter", "b": // boot (wake) — management lives in :console
			m.setFlash(stDim, "booting "+v.Name+"…")
			return m, do(m.client, "boot", v.Name)
		case "d": // destructive — stage a y/n confirm in the footer
			m.dashPending = "down:" + v.Name
			return m, nil
		case "R":
			m.dashPending = "restart:" + v.Name
			return m, nil
		case "p": // pin: toggle the idle-reaper exemption (keep awake)
			if v.KeepAwake { // was pinned → now auto-sleeps again
				m.setFlash(stDim, "○ "+v.Name+" will auto-sleep again")
			} else {
				m.setFlash(stAccent, "▲ keeping "+v.Name+" awake")
			}
			name := v.Name
			client := m.client
			return m, func() tea.Msg { // async like every other action; errors surface
				_, err := client.Do(control.Request{Op: "keepawake", DB: name})
				return actionMsg{verb: "pin", name: name, err: err}
			}
		case "y": // copy the connection URL — the name-based address, never a raw IP
			url := copyableURL(v)
			if url == "" {
				m.setFlash(stDim, "no url for "+v.Name)
				return m, nil
			}
			if err := clipboard.WriteAll(url); err != nil {
				m.setFlashErr("✗ copy failed: " + err.Error())
			} else {
				m.setFlash(stGreen, "✓ copied "+v.Name+" url")
			}
			return m, nil
		}
	}
	return m, nil
}

// copyableURL is the address a user pastes to reach a service: the connection
// string, else the shared-endpoint resource path, else the DNS-name endpoint —
// never a raw 127.0.0.x:port (which is an internal bind detail, not routable by
// intent). Falls back to whatever Endpoint holds (a unix socket path) last.
func copyableURL(v control.InstanceView) string {
	switch {
	case v.URL != "":
		return v.URL
	case v.Resource != "":
		return v.Resource
	case v.Domain != "" && v.Endpoint != "":
		if _, port, ok := strings.Cut(v.Endpoint, ":"); ok {
			return v.Domain + ":" + port
		}
		return v.Domain
	default:
		return v.Endpoint
	}
}

// dashVerbLabel is the user-facing name of a staged dash action's control verb.
func dashVerbLabel(verb string) string {
	switch verb {
	case "down":
		return "reap"
	case "destroy", "reset":
		return "reset"
	}
	return verb
}

// orAllServices names an empty instance argument in prompts ("reap all services?").
func orAllServices(name string) string {
	if name == "" {
		return "all services"
	}
	return name
}

// ── command palette (`:`) ────────────────────────────────────────────────────

// palMaxRows caps the suggestion list overlaid above the prompt.
const palMaxRows = 8

// paletteLocals are the dash-only view verbs merged into the `:` suggestions —
// they act on the view, not the daemon, so they live here, not in the registry.
var paletteLocals = []struct {
	name, summary string
	aliases       []string
	arg           bool // takes an (optional) argument → completing appends a space
}{
	{"console", "browse & act on buckets, queues, topics — put files, send, publish, purge", []string{"manage", "resources"}, true},
	{"theme", "cycle the color theme, or switch to one by name", nil, true},
	{"filter", "filter the instance list; bare `filter` clears it", nil, true},
	{"help", "show the key & legend overlay", nil, false},
	{"quit", "leave the dash", nil, false},
}

// palSuggestion is one completable row of the palette's suggestion list.
type palSuggestion struct {
	insert  string // what Tab completes into the input
	label   string // primary column (verb or instance name)
	summary string // dim column (action summary or engine type)
	space   bool   // completing appends a trailing space (verb taking an arg)
}

// paletteSuggestions computes the rows for the current input: registry actions
// (matched by name or alias) plus the local view verbs while typing the verb,
// then instance-name completions once a verb and a space are in place.
func (m model) paletteSuggestions() []palSuggestion {
	verb, argPrefix, hasArg := strings.Cut(m.palInput, " ")
	if !hasArg { // verb position
		var out []palSuggestion
		seen := map[string]bool{}
		// View verbs (console, theme, …) lead the list so the console is always
		// visible when the palette opens — it's the headline command, not a
		// registry action buried past the row cap.
		p := strings.ToLower(verb)
		for _, lv := range paletteLocals {
			match := strings.HasPrefix(lv.name, p)
			for _, al := range lv.aliases {
				match = match || strings.HasPrefix(al, p)
			}
			if match {
				seen[lv.name] = true
				out = append(out, palSuggestion{insert: lv.name, label: lv.name, summary: lv.summary, space: lv.arg})
			}
		}
		for _, a := range actions.Match(verb) {
			if seen[a.Name] {
				continue
			}
			out = append(out, palSuggestion{
				insert: a.Name, label: a.Name, summary: a.Summary,
				space: a.Arg != actions.ArgNone,
			})
		}
		return out
	}

	// Argument position.
	lp := strings.ToLower(argPrefix)
	var engineOK func(string) bool // narrow instance rows to engines the verb works on
	switch strings.ToLower(verb) {
	case "theme":
		var out []palSuggestion
		for _, t := range themes {
			if strings.HasPrefix(t.name, lp) {
				out = append(out, palSuggestion{insert: t.name, label: t.name, summary: "theme"})
			}
		}
		return out
	case "filter", "help", "quit", "q":
		return nil // free text / no argument
	case "console", "manage", "resources":
		engineOK = builtinAdmin
	default:
		act, ok := actions.Lookup(verb)
		if !ok || act.Arg == actions.ArgNone {
			return nil
		}
		if act.Name == "console" {
			engineOK = builtinAdmin
		}
	}
	var out []palSuggestion
	for _, in := range m.resp.Instances {
		if !strings.HasPrefix(strings.ToLower(in.Name), lp) {
			continue
		}
		if engineOK != nil && !engineOK(in.Engine) {
			continue
		}
		out = append(out, palSuggestion{insert: in.Name, label: in.Name, summary: in.Engine})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].label < out[b].label })
	return out
}

// cappedSuggestions is paletteSuggestions cut to the rendered window.
func (m model) cappedSuggestions() []palSuggestion {
	s := m.paletteSuggestions()
	if len(s) > palMaxRows {
		s = s[:palMaxRows]
	}
	return s
}

// handlePaletteKey drives the prompt: printable characters edit, Tab/→ completes
// the highlighted suggestion, ↑↓ move it, Enter executes, Esc closes.
func (m model) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sugs := m.cappedSuggestions()
	switch msg.String() {
	case "esc", "ctrl+c":
		m.paletteMode, m.palInput, m.palSel = false, "", 0
		return m, nil
	case "enter":
		// Enter runs the HIGHLIGHTED row whenever the typed text doesn't already
		// resolve on its own — arrowing to a suggestion and pressing Enter must
		// run that suggestion (requiring Tab first is not discoverable).
		if len(sugs) > 0 {
			s := sugs[clampi(m.palSel, 0, len(sugs)-1)]
			verb, arg, hasArg := strings.Cut(m.palInput, " ")
			if !hasArg {
				if !paletteVerbResolves(strings.TrimSpace(verb)) {
					m.palInput = s.insert
				}
			} else if a := strings.TrimSpace(arg); a != "" && strings.HasPrefix(strings.ToLower(s.insert), strings.ToLower(a)) {
				m.palInput = verb + " " + s.insert
			}
		}
		return m.paletteExec()
	case "up":
		if m.palSel > 0 {
			m.palSel--
		}
		return m, nil
	case "down":
		if m.palSel < len(sugs)-1 {
			m.palSel++
		}
		return m, nil
	case "tab", "right":
		if len(sugs) > 0 {
			s := sugs[clampi(m.palSel, 0, len(sugs)-1)]
			if verb, _, hasArg := strings.Cut(m.palInput, " "); hasArg {
				m.palInput = verb + " " + s.insert
			} else {
				m.palInput = s.insert
				if s.space {
					m.palInput += " "
				}
			}
			m.palSel = 0
		}
		return m, nil
	case "backspace":
		if r := []rune(m.palInput); len(r) > 0 {
			m.palInput = string(r[:len(r)-1])
		}
		m.palSel = 0
		return m, nil
	}
	switch {
	case msg.Type == tea.KeyRunes:
		m.palInput += string(msg.Runes)
		m.palSel = 0
	case msg.String() == " ": // the space key arrives as its own key type
		m.palInput += " "
		m.palSel = 0
	}
	return m, nil
}

// paletteVerbResolves reports whether typed text already names a runnable verb
// on its own — a local view verb or a registry action/alias. When it doesn't,
// Enter adopts the highlighted suggestion instead of failing on a prefix.
func paletteVerbResolves(v string) bool {
	if v == "" {
		return false
	}
	lv := strings.ToLower(v)
	if lv == "q" {
		return true
	}
	for _, l := range paletteLocals {
		if l.name == lv {
			return true
		}
		for _, al := range l.aliases {
			if al == lv {
				return true
			}
		}
	}
	_, ok := actions.Lookup(lv)
	return ok
}

// paletteExec parses and runs the typed command: local view verbs first, then
// the action registry (aliases like :boot / :reap / :destroy resolve there).
func (m model) paletteExec() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.palInput)
	m.paletteMode, m.palInput, m.palSel = false, "", 0
	if input == "" {
		return m, nil
	}
	verb, arg, _ := strings.Cut(input, " ")
	arg = strings.TrimSpace(arg)

	switch strings.ToLower(verb) {
	case "theme":
		if arg == "" {
			applyTheme(activeTheme + 1)
		} else {
			found := -1
			for i, t := range themes {
				if t.name == strings.ToLower(arg) {
					found = i
				}
			}
			if found < 0 {
				m.setFlashErr("✗ no theme " + arg + " — " + themeNames())
				return m, nil
			}
			applyTheme(found)
		}
		saveTheme()
		m.setFlash(stAccent, "theme · "+themes[activeTheme].name)
		return m, nil
	case "filter":
		m.filter.SetValue(arg)
		m.cursor = 0
		if arg == "" {
			m.setFlash(stDim, "filter cleared")
		} else {
			m.setFlash(stDim, "filter · "+arg)
		}
		return m, nil
	case "console", "manage", "resources":
		return m.openMgmt(arg)
	case "help":
		m.showHelp = true
		return m, nil
	case "quit", "q":
		return m, tea.Quit
	}

	act, ok := actions.Lookup(verb)
	if !ok {
		m.setFlashErr("✗ unknown command :" + verb + " — try :help")
		return m, nil
	}
	name := arg
	switch {
	case name != "" && act.Arg != actions.ArgNone:
		if _, ok := m.instanceByName(name); !ok {
			m.setFlashErr("✗ unknown service " + name)
			return m, nil
		}
	case name == "" && act.Arg == actions.ArgInstanceRequired:
		v, ok := m.selected() // default to the selection, like the keybindings
		if !ok {
			m.setFlashErr("✗ :" + act.Name + " needs a service name")
			return m, nil
		}
		name = v.Name
	}
	if act.Confirm { // destructive → the same y/n footer confirm as d / R
		m.dashPending = act.Op + ":" + name
		return m, nil
	}
	return m.runAction(act, name)
}

// runAction executes a resolved registry action (after any confirm).
func (m model) runAction(act actions.Action, name string) (tea.Model, tea.Cmd) {
	if act.Kind == actions.KindOp {
		if name == "" && !act.OpAcceptsAll {
			// The registry says this op's handler has no empty-means-all (boot,
			// today) — fan out over the fleet client-side like the CLI does.
			var cmds []tea.Cmd
			for _, in := range m.resp.Instances {
				if in.Disabled {
					continue
				}
				cmds = append(cmds, do(m.client, act.Op, in.Name))
			}
			if len(cmds) == 0 {
				m.setFlash(stDim, "nothing to "+act.Name)
				return m, nil
			}
			m.setFlash(stDim, fmt.Sprintf("%s: %d service(s)…", act.Name, len(cmds)))
			return m, tea.Batch(cmds...)
		}
		m.setFlash(stDim, act.Name+" "+orAllServices(name)+"…")
		return m, do(m.client, act.Op, name)
	}
	// KindLocal — the dash's own plumbing.
	switch act.Name {
	case "console": // the registry may carry the console verb too — same view
		return m.openMgmt(name)
	case "url":
		v, _ := m.instanceByName(name)
		url := v.URL
		if url == "" {
			url = v.Endpoint
		}
		if url == "" {
			m.setFlash(stDim, "no url for "+name)
			return m, nil
		}
		if err := clipboard.WriteAll(url); err != nil {
			m.setFlashErr("✗ copy failed: " + err.Error())
		} else {
			m.setFlash(stGreen, "✓ copied "+name+" url")
		}
		return m, nil
	case "logs":
		if name != "" {
			if !m.selectInstance(name) {
				m.setFlashErr("✗ unknown service " + name)
				return m, nil
			}
			return m, m.onSelect()
		}
		m.logVP.GotoBottom()
		return m, nil
	}
	m.setFlashErr("✗ :" + act.Name + " isn't available in the dash")
	return m, nil
}

// themeNames lists the switchable themes for the :theme error hint.
func themeNames() string {
	names := make([]string, len(themes))
	for i, t := range themes {
		names[i] = t.name
	}
	return strings.Join(names, "/")
}

// instanceByName finds an instance in the current status snapshot.
func (m model) instanceByName(name string) (control.InstanceView, bool) {
	for _, in := range m.resp.Instances {
		if in.Name == name {
			return in, true
		}
	}
	return control.InstanceView{}, false
}

// selectInstance moves the sidebar cursor to the named instance, clearing the
// filter when it hides it; false when the name isn't in the fleet.
func (m *model) selectInstance(name string) bool {
	find := func() bool {
		for di, i := range m.visible() {
			if m.resp.Instances[i].Name == name {
				m.cursor = di
				return true
			}
		}
		return false
	}
	if find() {
		return true
	}
	if m.filter.Value() != "" {
		m.filter.SetValue("")
		return find()
	}
	return false
}

// openConsole enters the inspector for a builtin: it fetches the resources and
// the first one's contents, which render as a live, navigable list.
func (m model) openConsole(v control.InstanceView) (tea.Model, tea.Cmd) {
	m.adminMode, m.adminErr, m.adminLoaded = true, "", false
	m.adminCursor, m.adminPending = 0, ""
	m.inspItems, m.inspCursor, m.inspExpanded, m.inspErr, m.inspRouting = nil, 0, false, "", false
	m.itemVP.SetContent(stFaint.Render("loading…"))
	m.itemVP.GotoTop()
	return m, fetchResources(m.client, v.Name)
}

// ── management view (:console) ──────────────────────────────────────────────

// mgmtRailW is the width of the management view's instance rail.
const mgmtRailW = 22

// builtinInstances is the manageable fleet: instances whose engine has a
// console (s3/sqs/sns), name-sorted.
func (m model) builtinInstances() []control.InstanceView {
	var out []control.InstanceView
	for _, in := range m.resp.Instances {
		if builtinAdmin(in.Engine) {
			out = append(out, in)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// openMgmt enters the management view. A named instance jumps straight into its
// console; bare `:console` lands on the rail with the first builtin loaded.
func (m model) openMgmt(name string) (tea.Model, tea.Cmd) {
	bs := m.builtinInstances()
	if len(bs) == 0 {
		m.setFlashErr("✗ nothing to manage — the console drives s3/sqs/sns services")
		return m, nil
	}
	idx := 0
	if name != "" {
		found := -1
		for i, b := range bs {
			if b.Name == name {
				found = i
			}
		}
		if found < 0 {
			m.setFlashErr("✗ " + name + " isn't a manageable service (s3/sqs/sns)")
			return m, nil
		}
		idx = found
	}
	// Land straight in the console — no rail-focus step to tab through. The rail
	// is a switcher (`[`/`]`), not a place you navigate into.
	m.mgmtMode, m.mgmtCursor = true, idx
	m.selectInstance(bs[idx].Name) // resource messages are adopted via selection
	m.layout()
	return m.openConsole(bs[idx])
}

// handleMgmtKey drives the console. There is no rail-vs-pane focus: the item
// browser is always live, `[`/`]` switch which service you're managing, `w`
// wakes a reaped one, and esc/q leaves straight for the dash. Everything else
// falls through to the inspector keys.
func (m model) handleMgmtKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Service switch / wake are mgmt-level, but must not steal keys from an open
	// composer or a pending confirm (which own all input).
	if !m.composerMode && m.adminPending == "" {
		bs := m.builtinInstances()
		switch msg.String() {
		case "[", "{": // previous service
			if m.mgmtCursor > 0 {
				m.mgmtCursor--
				return m.switchMgmt(bs)
			}
			return m, nil
		case "]", "}": // next service
			if m.mgmtCursor < len(bs)-1 {
				m.mgmtCursor++
				return m.switchMgmt(bs)
			}
			return m, nil
		case "w": // wake a reaped service so its contents load
			if m.mgmtCursor >= 0 && m.mgmtCursor < len(bs) && bs[m.mgmtCursor].PID == 0 {
				name := bs[m.mgmtCursor].Name
				m.setFlash(stDim, "waking "+name+"…")
				return m, do(m.client, "boot", name)
			}
			return m, nil
		}
	}
	nm, cmd := m.handleAdminKey(msg)
	mm := nm.(model)
	if !mm.adminMode { // esc/q at the top of the console → back to the dash
		mm.mgmtMode = false
		mm.layout()
	}
	return mm, cmd
}

// switchMgmt loads the console of the instance under the rail cursor.
func (m model) switchMgmt(bs []control.InstanceView) (tea.Model, tea.Cmd) {
	if m.mgmtCursor < 0 || m.mgmtCursor >= len(bs) {
		return m, nil
	}
	b := bs[m.mgmtCursor]
	m.selectInstance(b.Name)
	return m.openConsole(b)
}

// viewMgmt renders the split management view: the builtin rail on the left,
// the active instance's console (the existing inspector UI) on the right.
func (m model) viewMgmt() string {
	bs := m.builtinInstances()
	rows := []string{" " + stLabel.Bold(true).Render("SERVICES"), ""}
	for i, b := range bs {
		name := truncate(b.Name, mgmtRailW-6)
		// A reaped service shows a hollow dot; selecting + `w` (or any action) wakes it.
		asleep := b.PID == 0
		switch {
		case i == m.mgmtCursor:
			rows = append(rows, stAccent.Render("▸ ")+m.glyph(b)+" "+stAccent.Bold(true).Render(name))
		default:
			rows = append(rows, "  "+m.glyph(b)+" "+stText.Render(name))
		}
		sub := b.Engine
		if asleep {
			sub += " · asleep"
		}
		rows = append(rows, "    "+stFaint.Render(truncate(sub, mgmtRailW-6)))
	}
	for len(rows) < m.height-3 {
		rows = append(rows, "")
	}
	rows = append(rows[:m.height-3],
		stFaint.Render(truncate("[ ] switch service", mgmtRailW-1)),
		stFaint.Render(truncate("w wake · esc dash", mgmtRailW-1)),
		"")
	rail := lipgloss.NewStyle().Width(mgmtRailW).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(cPanel).
		Render(strings.Join(rows, "\n"))
	// The console pane reuses the full console renderer at the pane's width — a
	// width-shifted copy keeps all its internal math consistent.
	mc := m
	mc.width = max(20, m.width-mgmtRailW-1)
	pane := mc.viewConsole()
	return lipgloss.JoinHorizontal(lipgloss.Top, rail, pane)
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
			m.setFlash(stDim, "cancelled")
			return m, nil
		}
	}

	kind := m.resKind()
	switch msg.String() {
	case "esc", "q":
		if m.flashErr { // dismiss a persistent error first
			m.clearFlash()
			return m, nil
		}
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
				m.setFlash(stErr, "delete this "+itemNoun(kind)+"? — y to confirm")
			}
		}
		return m, nil
	case "P": // purge (queue) / empty (bucket)
		switch kind {
		case "queue":
			m.adminPending = "purge"
			m.setFlash(stErr, "purge every message? — y to confirm")
		case "bucket":
			m.adminPending = "empty"
			m.setFlash(stErr, "empty the bucket? — y to confirm")
		}
		return m, nil
	case "R": // redrive (queue dead-letter → source)
		if kind == "queue" {
			if r, ok := m.selectedResource(); ok {
				return m, runAdmin(m.client, m.adminName, "redrive", r.Name, "")
			}
		}
		return m, nil
	case "y": // copy this service's resource URL/ARN
		if v, ok := m.selected(); ok {
			if url := copyableURL(v); url != "" {
				if err := clipboard.WriteAll(url); err != nil {
					m.setFlashErr("✗ copy failed: " + err.Error())
				} else {
					m.setFlash(stGreen, "✓ copied "+v.Name+" url")
				}
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

// itemIndexAtRow maps a content row (in item-list coordinates) to the item
// rendered there, mirroring refreshItemView's per-item heights: 1 row for the
// title, +1 when it has a meta line, and — for the expanded selected item —
// +1 for the rule plus one per detail line. -1 when the row is past the list.
func (m model) itemIndexAtRow(row int) int {
	if row < 0 {
		return -1
	}
	at := 0
	for i, it := range m.inspItems {
		h := 1
		if it.meta != "" {
			h++
		}
		if i == m.inspCursor && m.inspExpanded && it.detail != "" {
			h += 1 + len(strings.Split(it.detail, "\n"))
		}
		if row < at+h {
			return i
		}
		at += h
	}
	return -1
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
		m.setFlash(stDim, "cancelled")
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

// expandUser resolves a leading ~ to the user's home directory (a bare "~" or
// "~/…"); other paths pass through untouched.
func expandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
		}
	}
	return p
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
		key, body := vals["key"], vals["body"]
		if path := strings.TrimSpace(expandUser(vals["file"])); path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				m.setFlashErr("✗ " + err.Error())
				return m, nil
			}
			if !utf8.Valid(data) {
				m.setFlashErr("✗ " + filepath.Base(path) + " looks binary — only text objects are supported for now")
				return m, nil
			}
			body = string(data)
			if key == "" {
				key = filepath.Base(path)
			}
		}
		if key == "" {
			m.setFlashErr("✗ an object key (or a file to name it) is required")
			return m, nil
		}
		payload["key"] = key
		payload["body"] = body
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

// handleCopyKey drives copy mode: ↑↓ move the line cursor, v anchors a line
// range, a selects everything, y/c/enter copy, esc leaves. (The mouse keeps its
// own character-precise drag selection.)
func (m model) handleCopyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	last := len(m.copyLines) - 1
	exit := func() {
		m.copyMode, m.copyCharMode = false, false
		m.copyAnchor, m.copyAnchorColCh = -1, 0
		m.logVP.SetContent(renderLogs(m.logLines, m.logVP.Width))
		m.logVP.GotoBottom() // leaving copy mode returns to the live tail
	}
	// Any keyboard motion leaves the mouse's character mode (the copy/exit keys
	// keep it so a dragged span still copies).
	switch msg.String() {
	case "c", "y", "enter", "esc", "q", "ctrl+c":
	default:
		m.copyCharMode = false
	}
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		exit()
		return m, nil
	case "up", "k":
		m.copyCursor--
	case "down", "j":
		m.copyCursor++
	case "pgup", "ctrl+u":
		m.copyCursor -= 10
	case "pgdown", "ctrl+d":
		m.copyCursor += 10
	case "g", "home":
		m.copyCursor = 0
	case "G", "end":
		m.copyCursor = last
	case "v", " ": // anchor / drop a line range
		if m.copyAnchor < 0 {
			m.copyAnchor = m.copyCursor
		} else {
			m.copyAnchor = -1
		}
	case "a": // select all lines
		m.copyAnchor, m.copyCursor = 0, last
	case "c", "y", "enter":
		return m.copySelection()
	default:
		return m, nil
	}
	m.copyCursor = clampi(m.copyCursor, 0, last)
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
// highlighted — whole lines for the keyboard, an inline span for a mouse drag —
// and keeps the cursor in view.
func (m *model) refreshCopyView() {
	w := m.logVP.Width
	loL, hiL := m.copyRange()
	curFull := lipgloss.NewStyle().Background(cAccent).Foreground(cSelFg).Width(w)
	selFull := lipgloss.NewStyle().Background(cSel).Foreground(cText).Width(w)
	selSeg := lipgloss.NewStyle().Background(cSel).Foreground(cText)

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
			b.WriteString(selStyled(selSeg, string(dr[cs:ce])))
			b.WriteString(stText.Render(string(dr[ce:])))
			b.WriteByte('\n')
			continue
		}
		// Line granularity — highlight whole lines.
		switch {
		case i == m.copyCursor:
			b.WriteString(selStyled(curFull, disp))
		case m.copyAnchor >= 0 && i >= loL && i <= hiL:
			b.WriteString(selStyled(selFull, disp))
		default:
			b.WriteString(stText.Render(disp))
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
	m.layout() // the new selection's card height sizes the logs pane
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
		k("r", "refresh now"),
		"",
		sec("Instance"),
		k("b / enter", "boot (wake it)"),
		k("d", "reap — sleep, keeps data (confirms)"),
		k("R", "restart (confirms)"),
		k("p", "keep awake (no auto-sleep)"),
		k("y", "copy connection url"),
	}, "\n")
	col2 := strings.Join([]string{
		sec("Logs"),
		k("c", "copy mode (lines; drag for chars)"),
		k("pgup/pgdn", "scroll"),
		"",
		sec("Palette"),
		k(": / ctrl+k", "command palette — :wake, :sleep,"),
		k("", ":restart, :logs, :console, …"),
		k(":console", "manage s3/sqs/sns resources"),
		"",
		sec("Display"),
		k("t", "cycle theme"),
		k("esc", "dismiss error message"),
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
		stErr.Render("✕ error") + "  " +
		stErr.Render("! tainted")

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

// tabSegments renders each resource tab (the active one bracketed and bright) —
// the single source of segment widths for both the strip and mouse hit-testing.
func (m model) tabSegments() []string {
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
	return tabs
}

// consoleTabs renders the resource tab strip, truncated to the window width.
func (m model) consoleTabs() string {
	switch {
	case m.adminErr != "" && len(m.adminRes) == 0:
		return " " + stErr.Render("✕ "+truncate(m.adminErr, m.width-3))
	case !m.adminLoaded:
		return " " + stFaint.Render("loading…")
	case len(m.adminRes) == 0:
		return " " + stFaint.Render("(no resources)")
	}
	return " " + truncate(strings.Join(m.tabSegments(), stFaint.Render("  ")), m.width-2)
}

// tabAt maps an x column on the tab row to a resource index, or -1. It walks the
// same rendered segments the strip draws, and refuses clicks past the strip's
// truncation point so a hidden tab can't be hit.
func (m model) tabAt(x int) int {
	limit := 1 + m.width - 2 // the strip is truncated to width-2, starting at col 1
	pos := 1                 // leading space
	for i, seg := range m.tabSegments() {
		segW := lipgloss.Width(seg)
		if pos+segW > limit {
			return -1 // this tab is (at least partly) truncated away
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
	// A reaped service can't list its contents until it boots — offer the wake
	// right here rather than showing an empty/errored list.
	if v, ok := m.selected(); ok && v.PID == 0 {
		body := stFaint.Render("○ "+v.Name+" is asleep") + "\n\n" +
			stDim.Render("press ") + stAccent.Render("w") + stDim.Render(" to wake it and load its contents")
		return lipgloss.NewStyle().Width(w).Height(h).Padding(1, 2).Render(body)
	}
	return lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h).Render(m.itemVP.View())
}

// composerForm renders the multi-field create form (the active field shows the
// live editor; others show their entered value).
func (m model) composerForm(w, h int) string {
	r, _ := m.selectedResource()
	lines := []string{stTitle.Render("new "+itemNoun(m.resKind())) + stDim.Render("  → "+r.Name), ""}
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
	parts = append(parts, key("y", "copy url"))
	if v, ok := m.selected(); ok && v.PID == 0 {
		parts = append(parts, key("w", "wake"))
	}
	if len(m.builtinInstances()) > 1 {
		parts = append(parts, key("[ ]", "service"))
	}
	parts = append(parts, key("esc", "exit"))
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
	var out string
	switch {
	case m.mgmtMode:
		out = m.viewMgmt()
	case m.adminMode:
		out = m.viewConsole()
	default:
		body := lipgloss.JoinHorizontal(lipgloss.Top, m.viewSidebar(), "  ", m.viewRight())
		out = lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), body, m.viewFooter())
	}
	if m.paletteMode { // Spotlight-style floating panel in the upper third
		out = m.overlayAt(out, m.paletteView(), max(1, m.height/6))
	}
	if m.dashPending != "" { // destructive confirm — a centered modal, unmissable
		modal := m.confirmView()
		top := max(1, (m.height-lipgloss.Height(modal))/2)
		out = m.overlayAt(out, modal, top)
	}
	return out
}

// overlayAt floats block over the frame with its first row at top; every block
// row is horizontally centered and padded to the full width, so the rows it
// occupies are cleanly covered while the dash stays visible above and below.
// overlayAt composites a floating block (palette, modal) centered over the base
// frame starting at row `top`, splicing each block line INTO the base row so the
// content on either side — crucially the sidebar — stays visible rather than
// being wiped by a full-width blank line.
func (m model) overlayAt(frame, block string, top int) string {
	lines := strings.Split(frame, "\n")
	blockLines := strings.Split(block, "\n")
	blockW := 0
	for _, b := range blockLines {
		if w := ansi.StringWidth(b); w > blockW {
			blockW = w
		}
	}
	left := max(0, (m.width-blockW)/2)
	for i, b := range blockLines {
		r := top + i
		if r < 0 || r >= len(lines) {
			continue
		}
		base := lines[r]
		// Keep the base's first `left` cells (the sidebar lives here), padded out
		// if the row is short, then the block, then whatever base sits to its right.
		lseg := ansi.Truncate(base, left, "")
		if pad := left - ansi.StringWidth(lseg); pad > 0 {
			lseg += strings.Repeat(" ", pad)
		}
		if bw := ansi.StringWidth(b); bw < blockW {
			b += strings.Repeat(" ", blockW-bw)
		}
		rseg := ansi.TruncateLeft(base, left+blockW, "")
		lines[r] = lseg + "\x1b[0m" + b + "\x1b[0m" + rseg
	}
	return strings.Join(lines, "\n")
}

// paletteView is the Spotlight panel: a bordered box, ~60% of the window (min
// 46 cols), input on top, the suggestion rows beneath.
func (m model) paletteView() string {
	w := clampi(m.width*3/5, 46, max(46, m.width-4))
	inner := w - 6 // border (2) + padding (2×2)
	sugs := m.cappedSuggestions()
	sel := clampi(m.palSel, 0, max(0, len(sugs)-1))

	prompt := stAccent.Bold(true).Render(": ") + stText.Render(m.palInput) + stAccent.Render("▌")
	lines := []string{truncate(prompt, inner)}
	if len(sugs) > 0 {
		lines = append(lines, stFaint.Render(strings.Repeat("╌", max(1, inner))))
		nameW := 0
		for _, s := range sugs {
			if lw := lipgloss.Width(s.label); lw > nameW {
				nameW = lw
			}
		}
		hl := lipgloss.NewStyle().Background(cSel).Foreground(cText)
		for i, s := range sugs {
			pad := strings.Repeat(" ", nameW-lipgloss.Width(s.label)+2)
			if i == sel { // reverse video under NO_COLOR, tinted row otherwise
				lines = append(lines, selStyled(hl, truncate("▸ "+s.label+pad+s.summary, inner)))
				continue
			}
			row := "  " + stAccent.Render(s.label) + pad +
				stDim.Render(truncate(s.summary, max(4, inner-nameW-4)))
			lines = append(lines, truncate(row, inner))
		}
	} else if strings.TrimSpace(m.palInput) != "" {
		lines = append(lines, stFaint.Render("no matches"))
	}
	lines = append(lines, stFaint.Render(truncate("↵ run · ⇥ complete · esc close", inner)))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).
		Padding(0, 2).Width(w - 2).
		Render(strings.Join(lines, "\n"))
}

// confirmView is the centered modal for a staged destructive action: the verb
// and target read large, with a one-line consequence underneath.
func (m model) confirmView() string {
	verb, name, _ := strings.Cut(m.dashPending, ":")
	target := name
	if target == "" {
		target = "ALL services"
	}
	head := stErr.Bold(true).Render("⚠ " + dashVerbLabel(verb) + " " + target + "?")
	var why string
	switch verb {
	case "down":
		why = "stops it now; data is kept and a connection re-wakes it"
		if name == "" {
			why = "sleeps every awake service; connections re-wake them"
		}
	case "restart":
		why = "stops and re-boots the backend in place"
	case "destroy":
		why = "wipes its data; the next boot re-provisions fresh"
	}
	keys := stAccent.Render("y") + stDim.Render(" confirm") +
		stFaint.Render("   ·   ") + stAccent.Render("esc") + stDim.Render(" cancel")
	body := head + "\n" + stDim.Render(why) + "\n\n" + keys
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(cRed).
		Padding(1, 3).Render(body)
}

// ── header ────────────────────────────────────────────────────────────────
func (m model) viewHeader() string {
	var up int
	for _, in := range m.resp.Instances {
		if in.PID != 0 {
			up++
		}
	}
	listen := m.resp.Listen
	if listen == "" {
		listen = "—"
	}
	// Liveness is truthful: green "live" only while status polls succeed; a failed
	// poll flips the whole header state to a red "lost" until a poll lands again.
	liveDot := stGreen.Render("●") + stDim.Render(" live")
	if m.connLost {
		liveDot = stErr.Render("● lost")
	}
	// Total RSS lives in the sidebar footer (with the fleet counts); keep the
	// header focused on endpoint / up-count / liveness so memory isn't shown twice.
	right := strings.Join([]string{
		stDim.Render(listen),
		stText.Render(fmt.Sprintf("%d up", up)) + stDim.Render("/"+fmt.Sprint(len(m.resp.Instances))),
		liveDot,
	}, stFaint.Render("  ·  "))

	title := stTitle.Render("◆ doze")
	// The sub slot next to the title: flash > connection-lost banner > tagline.
	// Whatever it is, it is fitted to the width left over so a long message can
	// never blow the header line out of the terminal.
	avail := max(8, m.width-lipgloss.Width(title)-lipgloss.Width(right)-3)
	var sub string
	switch {
	case m.flash != "":
		sub = m.flashStyle.Render(truncate(m.flash, avail))
	case m.connLost:
		banner := "● lost — daemon unreachable, retrying…"
		age := ""
		if !m.lastOK.IsZero() { // how stale the picture on screen is
			age = "  data " + compactDur(time.Since(m.lastOK)) + " old"
		}
		sub = stErr.Render(truncate(banner, max(8, avail-lipgloss.Width(age)))) + stFaint.Render(age)
	default:
		sub = stFaint.Render("mission control")
	}
	left := title + "  " + sub
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
		if len(m.resp.Instances) == 0 { // truly empty — point at the way forward
			rows = append(rows,
				stDim.Render("  no instances yet"),
				stFaint.Render("  declare some in doze.hcl,"),
				stFaint.Render("  then run `doze up`"))
		} else { // instances exist, the filter hides them all
			rows = append(rows, stDim.Render("  (no matches)"))
		}
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
		if noColor { // the tinted row is invisible without color — reverse it instead
			return lipgloss.NewStyle().Width(w).Render(reverseVideo("▌ " + inner))
		}
		return lipgloss.NewStyle().Background(cSel).Width(w).Render(stAccent.Render("▌") + " " + inner)
	}
	return lipgloss.NewStyle().Width(w).Render("  " + inner)
}

// sidebarTotals is the at-a-glance resource summary pinned to the bottom.
func (m model) sidebarTotals(w int) []string {
	var act, idle, asleep, errc, taint int
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
		case "tainted":
			taint++
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
	if taint > 0 {
		counts += "  " + stErr.Render(fmt.Sprintf("!%d", taint))
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
	case "tainted":
		return s.Bold(true).Render("!") // converge failed — must not read as asleep
	default:
		return stFaint.Render("·") // asleep — small + faint
	}
}

// ── right pane ────────────────────────────────────────────────────────────
func (m model) viewRight() string {
	w := m.rightW()
	v, ok := m.selected()
	if !ok {
		msg := "nothing selected"
		if len(m.resp.Instances) == 0 {
			msg = "no instances yet\n\ndeclare some in doze.hcl, then run `doze up`\nto boot the fleet"
		}
		return lipgloss.NewStyle().Width(w).Height(m.bodyH()).
			Align(lipgloss.Center, lipgloss.Center).Foreground(cDim).
			Render(msg)
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.viewDetail(v, w), m.viewLogs(v, w))
}

// viewDetail is the instance card. It is content-sized: a sleeping instance
// yields a shorter card (no padding), and the logs pane below takes the rest.
func (m model) viewDetail(v control.InstanceView, w int) string {
	return lipgloss.NewStyle().Width(w).
		Border(lipgloss.RoundedBorder()).BorderForeground(cPanel).
		Padding(0, 2).Render(strings.Join(m.detailLines(v, w), "\n"))
}

// detailLines builds the card's content rows — the single source for rendering
// AND the layout math (logs pane height, mouse hit-testing). The design rule:
// one blank row between sections, facts consolidated into section-title rows,
// charts carry a left axis gutter and keep their right side empty.
func (m model) detailLines(v control.InstanceView, w int) []string {
	st := displayState(v)
	inner := max(24, w-6) // border (2) + padding (2×2)
	isProc := v.Engine == "process"

	// Title row: identity on the left, the live facts cluster right-aligned.
	// "builtin"/"0" are versionless pseudo-versions (a process, an AWS built-in) —
	// hide them, matching `doze status`; a process is not a "builtin".
	name := stTitle.Render(v.Name) + stDim.Render("  "+v.Engine)
	if v.Version != "" && v.Version != "builtin" && v.Version != "0" {
		name += stDim.Render(" " + v.Version)
	}
	if v.KeepAwake {
		name += stAccent.Render("  ▲ kept awake")
	}
	facts := m.titleFacts(v, st)
	gap := max(1, inner-lipgloss.Width(name)-lipgloss.Width(facts))
	titleRow := truncate(name+strings.Repeat(" ", gap)+facts, inner)

	// Connection facts: a fixed label column; the domain leads the endpoint with
	// the raw address in parens; env is the ready-to-paste VAR=URL; pid rides
	// dim on the data row (deep detail — :console territory).
	lbl := func(s string) string { return stLabel.Render(fmt.Sprintf("%-8s  ", s)) }
	endpoint := lbl("endpoint")
	switch {
	case v.Resource != "":
		// A shared front door (AWS built-in, ingress process): show the full,
		// directly-addressable path — the queue/bucket/topic or the :80 URL.
		endpoint += stText.Bold(true).Render(v.Resource)
	case domainAddr(v) != "":
		endpoint += stText.Bold(true).Render(domainAddr(v)) + stDim.Render("   ("+orDash(v.Endpoint)+")")
	default:
		endpoint += stText.Render(orDash(v.Endpoint))
	}
	// The bare connection URL — the value an app drops into its own config under
	// whatever name it uses; doze doesn't presume DATABASE_URL-style var names.
	// A process has none (it's reached at its endpoint), so the row is dropped
	// rather than shown as a bare dash.
	var urlRows []string
	if v.URL != "" {
		urlAvail := max(12, inner-10)
		urlChunks := wrapWidth(v.URL, urlAvail)
		urlRows = []string{lbl("url") + stAccent.Render(urlChunks[0])}
		if len(urlChunks) > 1 { // 2 lines max; anything longer ends in an ellipsis
			rest := strings.Join(urlChunks[1:], "")
			urlRows = append(urlRows, strings.Repeat(" ", 10)+stAccent.Render(truncate(rest, urlAvail)))
		}
	}
	dataRow := lbl("data") + stDim.Render(truncate(abbrevHome(v.DataDir), max(8, inner-28)))
	if v.PID != 0 {
		dataRow += stFaint.Render("   pid " + fmt.Sprint(v.PID))
	}

	lines := []string{titleRow, "", truncate(endpoint, inner)}
	lines = append(lines, urlRows...)
	lines = append(lines, truncate(dataRow, inner))

	// Charts — only while running; a sleeping card simply ends here (short is fine).
	if v.PID != 0 {
		h := m.hist[v.Name]
		lines = append(lines, "")
		lines = append(lines, memorySection(h, v.RAM, inner)...)
		if !isProc {
			if cs := connsSection(h, v.Conns, inner); len(cs) > 0 {
				lines = append(lines, "")
				lines = append(lines, cs...)
			}
		}
	}

	// Status strip: only what the title row can't say — failures, and the
	// builtin's resources + console affordance. Always a blank row above.
	var status, resLine string
	switch {
	case v.LastError != "":
		status = stErr.Render("✕ " + truncate(v.LastError, inner-2))
	case v.Tainted:
		status = stErr.Render("✕ structure incomplete — run `doze sync` to re-converge")
	case isProc && v.PID != 0 && v.Healthy != nil && !*v.Healthy:
		status = stErr.Render("✕ running but health probe failing")
	}
	if status == "" && builtinAdmin(v.Engine) && v.PID != 0 {
		hint := stAccent.Render(":console") + stFaint.Render(" manages it")
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
			resLine = stFaint.Render(truncate(strings.Join(names, "  ·  "), inner-2))
		} else {
			status = stGreen.Render("● serving") + stDim.Render(" · ") + hint
		}
	}
	if status != "" {
		lines = append(lines, "", status)
		if resLine != "" {
			lines = append(lines, resLine)
		}
	}
	return lines
}

// titleFacts is the card title's right-aligned facts cluster: state badge,
// conns/health, cpu, uptime, and the reap countdown when one is running.
func (m model) titleFacts(v control.InstanceView, st string) string {
	badge := lipgloss.NewStyle().Foreground(stateColor(st)).Bold(true).
		Render(stateGlyph(st) + " " + displayLabel(st))
	sep := stDim.Render(" · ")
	parts := []string{badge}
	if v.PID == 0 {
		if st == "booting" {
			parts = append(parts, stDim.Render("booting…"))
		} else {
			parts = append(parts, stDim.Render("wakes on connect"))
		}
		return strings.Join(parts, sep)
	}
	if v.Engine == "process" {
		parts = append(parts, healthBadge(v.Healthy))
	} else {
		parts = append(parts, stText.Render(fmt.Sprintf("%d conns", v.Conns)))
	}
	if c := ui.CPUStr(v.CPU); c != "" {
		parts = append(parts, stDim.Render("cpu "+c))
	}
	parts = append(parts, stDim.Render("up "+ui.Uptime(v.StartedAt)))
	if v.RestartCount > 0 {
		parts = append(parts, stDim.Render(fmt.Sprintf("%d restarts", v.RestartCount)))
	}
	// The idle reaper exempts supervised processes (they boot eagerly and stay
	// up), so they never sleep on idle — don't tease a countdown that won't fire.
	if st == "idle" && v.Engine != "process" && !v.KeepAwake && !v.IdleSince.IsZero() && m.resp.IdleTimeout > 0 {
		remain := m.resp.IdleTimeout - time.Since(v.IdleSince)
		if remain < 0 {
			remain = 0
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(cGold).Render("sleeps in "+compactDur(remain)))
	}
	return strings.Join(parts, sep)
}

// stateGlyph is the one-character mark for a display state (matches the
// sidebar's vocabulary).
func stateGlyph(st string) string {
	switch st {
	case "active":
		return "●"
	case "idle":
		return "○"
	case "booting":
		return "⠿"
	case "error":
		return "✕"
	case "tainted":
		return "!"
	default:
		return "·"
	}
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

// ── charts ──────────────────────────────────────────────────────────────────
// The bar: a developer glances and understands the shape in one second. So:
// an asciigraph-style curved LINE (box-drawing glyphs, never block bricks), a
// left axis gutter, stats in the section title, an empty right side, and no
// autoscale-amplified jitter — a steady series says so in words.

// steadySeries reports whether a window has no movement worth charting: its
// range is under ~5% of its mean. Autoscaling such a series would amplify
// jitter into full-height noise, so callers render a flat line instead.
func steadySeries(vals []float64) bool {
	if len(vals) < 2 {
		return true
	}
	mn, mx, sum := vals[0], vals[0], 0.0
	for _, v := range vals {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
		sum += v
	}
	if mx == mn {
		return true
	}
	mean := sum / float64(len(vals))
	if mean <= 0 {
		return false
	}
	return (mx-mn)/mean < 0.05
}

// curveChart renders vals as an asciigraph-style box-drawing line: '─' runs,
// '╭ ╮ ╰ ╯' corners and '│' risers, one transition per column. The series is
// scaled to integer levels 0..rows-1 (bottom..top, round-to-nearest); a flat
// series draws a mid-height line. Returns rows plain strings, top row first
// (the caller colors them).
func curveChart(vals []float64, w, rows int) []string {
	grid := make([][]rune, max(1, rows))
	for i := range grid {
		grid[i] = []rune(strings.Repeat(" ", max(0, w)))
	}
	if len(vals) > 0 && w > 0 && rows > 0 {
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
		lvl := func(x int) int {
			idx := 0
			if w > 1 {
				idx = x * (len(vals) - 1) / (w - 1)
			}
			if span <= 0 {
				return (rows - 1) / 2 // flat — hold the middle
			}
			return clampi(int((vals[idx]-mn)/span*float64(rows-1)+0.5), 0, rows-1)
		}
		rowOf := func(l int) int { return rows - 1 - l }
		prev := lvl(0)
		grid[rowOf(prev)][0] = '─'
		for x := 1; x < w; x++ {
			cur := lvl(x)
			switch {
			case cur == prev:
				grid[rowOf(cur)][x] = '─'
			case cur > prev: // rising: arrive, turn up, exit right higher
				grid[rowOf(prev)][x] = '╯'
				grid[rowOf(cur)][x] = '╭'
				for l := prev + 1; l < cur; l++ {
					grid[rowOf(l)][x] = '│'
				}
			default: // falling: arrive, turn down, exit right lower
				grid[rowOf(prev)][x] = '╮'
				grid[rowOf(cur)][x] = '╰'
				for l := cur + 1; l < prev; l++ {
					grid[rowOf(l)][x] = '│'
				}
			}
			prev = cur
		}
	}
	out := make([]string, len(grid))
	for i, r := range grid {
		out[i] = string(r)
	}
	return out
}

// chartGutter builds the left axis column: the top row carries the peak label
// and '┤', the bottom row the low label and '┼', the rows between a bare '│'.
// Labels are right-aligned; pass "" to leave a row unlabelled (flat series
// label the bottom row only). All rows come back equal-width.
func chartGutter(rows int, top, bottom string) []string {
	lw := max(4, max(lipgloss.Width(top), lipgloss.Width(bottom)))
	out := make([]string, max(1, rows))
	for i := range out {
		lbl, axis := "", "│"
		switch i {
		case len(out) - 1:
			lbl, axis = bottom, "┼"
		case 0:
			lbl, axis = top, "┤"
		}
		out[i] = fmt.Sprintf("%*s %s", lw, lbl, axis)
	}
	return out
}

// memShort is the axis-gutter form of a memory value: one decimal, unit-less
// for MB (the section title carries the unit), "G"-suffixed above 1 GB.
func memShort(b int64) string {
	const mb = 1024 * 1024
	if b <= 0 {
		return "0"
	}
	if b < 1024*mb {
		return strconv.FormatFloat(float64(b)/mb, 'f', 1, 64)
	}
	return strconv.FormatFloat(float64(b)/(1024*mb), 'f', 1, 64) + "G"
}

// memorySection is the card's memory block: a section-title row carrying the
// stats ("memory · 10.58 MB now · peak 14.66 MB", the window right-aligned as
// "last 2m30s") above a curved line over a left axis gutter. The chart's right
// side stays empty. now==peak prints once; a steady series (<5% range of mean)
// is never autoscale-amplified — it draws flat with a "steady" title.
func memorySection(h *history, ramNow int64, inner int) []string {
	const rows = 4
	var series []float64
	if h != nil {
		series = h.ram
	}
	lo, hi := memBounds(h)
	cur := memStr(ramNow)
	steady := len(series) < 2 || steadySeries(series)

	title := stLabel.Render("memory") + stDim.Render(" · ")
	switch {
	case steady:
		title += stText.Render("steady") + stDim.Render(" · ") + stText.Bold(true).Render(orDash(cur))
	case memStr(hi) == cur:
		title += stText.Bold(true).Render(cur+" now") + stDim.Render(" (the peak)")
	default:
		title += stText.Bold(true).Render(cur+" now") + stDim.Render(" · peak "+memStr(hi))
	}
	if win := memWindow(h); win != "" {
		right := stFaint.Render("last " + win)
		if gap := inner - lipgloss.Width(title) - lipgloss.Width(right); gap > 0 {
			title += strings.Repeat(" ", gap) + right
		}
	}

	topLbl, botLbl := memShort(hi), memShort(lo)
	if steady || topLbl == botLbl {
		topLbl, botLbl = "", memShort(max(lo, ramNow)) // flat: one value, bottom row
	}
	gut := chartGutter(rows, topLbl, botLbl)
	gw := max(8, inner-lipgloss.Width(gut[0])-1)
	if steady {
		series = []float64{0, 0} // force the clean mid-height flat line
	}
	g := curveChart(series, gw, rows)
	line := lipgloss.NewStyle().Foreground(cAccent)
	out := []string{truncate(title, inner)}
	for i := 0; i < rows; i++ {
		out = append(out, stDim.Render(gut[i])+line.Render(g[i]))
	}
	return out
}

// connsSection is the card's connection block: a single quiet row while the
// count holds ("conns · 4 for 2m30s"), or — once it varied in the window — a
// title row plus a 2-row step line in the same gutter style (integer labels).
func connsSection(h *history, now, inner int) []string {
	var series []float64
	if h != nil {
		series = h.conns
	}
	if len(series) == 0 {
		return nil
	}
	mn, mx := series[0], series[0]
	for _, v := range series {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	if int(mn) == int(mx) { // constant — one row of words, no chart
		return []string{stLabel.Render("conns") + stDim.Render(" · ") +
			stText.Bold(true).Render(fmt.Sprint(now)) + stDim.Render(" for "+memWindow(h))}
	}
	const rows = 2
	title := stLabel.Render("conns") + stDim.Render(" · ") +
		stText.Bold(true).Render(fmt.Sprintf("now %d", now)) +
		stDim.Render(fmt.Sprintf(" · peak %d", int(mx)))
	gut := chartGutter(rows, strconv.Itoa(int(mx)), strconv.Itoa(int(mn)))
	gw := max(8, inner-lipgloss.Width(gut[0])-1)
	g := curveChart(series, gw, rows)
	line := lipgloss.NewStyle().Foreground(cCyan)
	out := []string{truncate(title, inner)}
	for i := 0; i < rows; i++ {
		out = append(out, stDim.Render(gut[i])+line.Render(g[i]))
	}
	return out
}

// domainAddr is an instance's friendly mDNS endpoint (domain + the endpoint's
// port), or "" when the daemon advertises no domain.
func domainAddr(v control.InstanceView) string {
	if v.Domain == "" {
		return ""
	}
	if i := strings.LastIndex(v.Endpoint, ":"); i >= 0 && !strings.HasPrefix(v.Endpoint, "unix:") {
		return v.Domain + v.Endpoint[i:]
	}
	return v.Domain
}

func (m model) viewLogs(v control.InstanceView, w int) string {
	mode := "" // logs auto-tail; the only header status is copy-mode selection
	if m.copyMode {
		// Live selection feedback: the header always says what a copy would take.
		var sel string
		switch {
		case m.copyCharMode && m.copyAnchor >= 0: // mouse drag
			sel = plural(len([]rune(m.selectedCharText())), "char")
		case m.copyAnchor >= 0:
			lo, hi := m.copyRange()
			sel = plural(hi-lo+1, "line") + " selected"
		default:
			sel = "line " + fmt.Sprint(m.copyCursor+1) + " of " + fmt.Sprint(len(m.copyLines))
		}
		mode = stAccent.Bold(true).Render("COPY") + stDim.Render(" · "+sel)
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
		return strings.Join([]string{
			key("↑↓", "move"), key("v", "select lines"), key("a", "all"),
			key("y", "copy"), key("esc", "exit"),
		}, sep)
	}
	// Every dash key, in display order, each with a drop priority (higher drops
	// first). The line is trimmed to the window: least-important hints go before
	// core actions ever do, and "? help" / "q quit" (prio 0) always survive.
	type hint struct {
		text string
		prio int
	}
	hints := []hint{
		{key("↑↓", "select"), 1},
		{key("b", "boot"), 2},
		{key("d", "reap"), 3},
		{key("R", "restart"), 4},
		{key("p", "pin"), 6},
	}
	hints = append(hints,
		hint{key("y", "url"), 7},
		hint{key(":console", "manage aws"), 5},
		hint{key("c", "copy"), 9},
		hint{key("/", "filter"), 10},
		hint{key("r", "refresh"), 11},
		hint{key("t", "theme"), 12},
		hint{key(":", "cmds"), 1}, // the palette reaches everything — keep it visible
		hint{key("?", "help"), 0},
		hint{key("q", "quit"), 0},
	)
	line := func(hs []hint) string {
		parts := make([]string, len(hs))
		for i, h := range hs {
			parts[i] = h.text
		}
		return strings.Join(parts, sep)
	}
	for lipgloss.Width(line(hints)) > m.width {
		drop, at := 0, -1
		for i, h := range hints {
			if h.prio > drop {
				drop, at = h.prio, i
			}
		}
		if at < 0 { // only prio-0 hints left — nothing more can go
			break
		}
		hints = append(hints[:at], hints[at+1:]...)
	}
	return line(hints)
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

// renderLogs paints log lines truncated to the pane width — an overlong line
// must never soft-wrap and grow the box past its height budget (which would
// desync the mouse math). Copy mode gives access to the full line.
func renderLogs(lines []string, w int) string {
	if len(lines) == 0 {
		return stFaint.Render("(no output yet)")
	}
	if w <= 0 {
		w = 1 << 20 // viewport not laid out yet — leave lines whole
	}
	var b strings.Builder
	for _, ln := range lines {
		b.WriteString(stText.Render(truncate(ln, w)))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// truncate fits s into w display columns, ellipsized. It cuts by display width
// (ANSI- and wide-character-aware), so CJK/emoji content can't overflow a column.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// wrapWidth splits a plain string into chunks of at most w display columns
// (wide-character-aware). Always returns at least one chunk.
func wrapWidth(s string, w int) []string {
	if w <= 0 || lipgloss.Width(s) <= w {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	curW := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if curW+rw > w && cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
			curW = 0
		}
		cur.WriteRune(r)
		curW += rw
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
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
func compactDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d >= time.Minute {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
func clampi(v, lo, hi int) int { return max(lo, min(hi, v)) }
