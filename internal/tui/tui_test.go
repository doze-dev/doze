package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func threeInstances() model {
	fi := textinput.New()
	fi.Prompt = "/"
	return model{
		width: 110, height: 30,
		filter: fi,
		hist:   map[string]*history{},
		logVP:  viewport.New(40, 8),
		resp: control.Response{
			Listen: "127.0.0.1:6432",
			Instances: []control.InstanceView{
				{Name: "app", Engine: "postgres", State: "active", Conns: 1},
				{Name: "cache", Engine: "valkey", State: "idle"},
				{Name: "media", Engine: "s3", State: "reaped", LastError: "boom"},
			},
		},
	}
}

func send(m model, msg tea.Msg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

func TestCursorNavigationClamps(t *testing.T) {
	m := threeInstances()
	m = send(m, key("j"))
	m = send(m, key("j"))
	m = send(m, key("j")) // clamp at last
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (clamped)", m.cursor)
	}
	m = send(m, key("k"))
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
}

func TestSelectionIsNameSorted(t *testing.T) {
	m := threeInstances()
	// cursor 0 should be the alphabetically-first instance regardless of input order.
	if v, _ := m.selected(); v.Name != "app" {
		t.Fatalf("first selection = %q, want app", v.Name)
	}
	m.cursor = 2
	if v, _ := m.selected(); v.Name != "media" {
		t.Fatalf("last selection = %q, want media", v.Name)
	}
}

func TestFilterToggleAndClear(t *testing.T) {
	m := threeInstances()
	m = send(m, key("/"))
	if !m.filtering {
		t.Fatal("'/' should enter filter mode")
	}
	m = send(m, key("z")) // matches nothing
	if len(m.visible()) != 0 {
		t.Fatalf("filter 'z' should hide all, got %d", len(m.visible()))
	}
	m = send(m, key("esc"))
	if m.filtering {
		t.Fatal("esc should leave filter mode")
	}
	if len(m.visible()) != 3 {
		t.Fatalf("esc should clear the filter, got %d visible", len(m.visible()))
	}
}

// sqsActs mirrors the sqs engine's published actions.
func sqsActs() []control.ActionView {
	return []control.ActionView{
		{ID: "peek", Label: "Peek", Kind: "queue"},
		{ID: "send", Label: "Send", Kind: "queue", InputHint: "message body"},
		{ID: "purge", Label: "Purge", Kind: "queue", Destructive: true},
		{ID: "redrive", Label: "Redrive", Kind: "queue"},
	}
}

// inspectorModel builds an inspector scoped to a sqs instance, queue selected,
// with two messages already loaded.
func inspectorModel() model {
	return model{
		width: 110, height: 30,
		cmd:         textinput.New(),
		adminMode:   true,
		adminLoaded: true,
		adminName:   "jobs_sqs",
		adminActs:   sqsActs(),
		adminRes:    []control.ResourceView{{Kind: "queue", Name: "emails", Status: "2 msgs"}, {Kind: "queue", Name: "orders.fifo"}},
		adminCursor: 0,
		itemVP:      viewport.New(70, 20),
		inspItems: []inspItem{
			{title: "hello", meta: "group a", detail: "hello", delArg: "h1"},
			{title: "world", detail: "world", delArg: "h2"},
		},
	}
}

func TestParseItems(t *testing.T) {
	// queue messages: body + group + receive count + attributes + delete handle.
	items, err := parseItems("queue", `[{"body":"hi","group":"g1","received":"2","attrs":{"tier":"gold"},"handle":"rh"}]`)
	if err != nil || len(items) != 1 {
		t.Fatalf("parse queue: err=%v n=%d", err, len(items))
	}
	if items[0].title != "hi" || items[0].delArg != "rh" ||
		!strings.Contains(items[0].meta, "tier=gold") || !strings.Contains(items[0].meta, "group g1") {
		t.Fatalf("queue item = %+v", items[0])
	}
	// bucket objects are deletable by key.
	items, _ = parseItems("bucket", `[{"key":"a.txt","size":1024,"modified":"2026"}]`)
	if len(items) != 1 || items[0].title != "a.txt" || items[0].delArg != "a.txt" {
		t.Fatalf("bucket item = %+v", items[0])
	}
	// topic subscriptions show protocol + filter.
	items, _ = parseItems("topic", `[{"protocol":"sqs","endpoint":"emails","filter":"eventType ∈ [created]","raw":true,"confirmed":true}]`)
	if len(items) != 1 || !strings.Contains(items[0].title, "emails") || !strings.Contains(items[0].meta, "eventType") {
		t.Fatalf("topic item = %+v", items[0])
	}
	// a test publish annotates each subscription with whether it matched.
	items, _ = parseItems("topic", `[{"protocol":"sqs","endpoint":"emails","filter":"eventType ∈ [created]","matched":true},{"protocol":"http","endpoint":"hook","filter":"eventType ∈ [deleted]","matched":false}]`)
	if len(items) != 2 || !strings.HasPrefix(items[0].title, "✓") || !strings.HasPrefix(items[1].title, "✗") {
		t.Fatalf("routing badges = %q / %q", items[0].title, items[1].title)
	}
	if !strings.Contains(items[0].meta, "MATCHED") || !strings.Contains(items[1].meta, "filtered out") {
		t.Fatalf("routing meta = %q / %q", items[0].meta, items[1].meta)
	}
}

func TestPrettyJSON(t *testing.T) {
	// a JSON body is indented for the detail pane.
	got := prettyJSON(`{"to":"a@x.com","tmpl":"welcome"}`)
	if !strings.Contains(got, "\n  \"to\": \"a@x.com\"") {
		t.Fatalf("prettyJSON did not indent: %q", got)
	}
	// non-JSON is returned verbatim.
	if got := prettyJSON("just a plain string"); got != "just a plain string" {
		t.Fatalf("prettyJSON mangled plain text: %q", got)
	}
}

func TestResBadges(t *testing.T) {
	if b := resBadges(control.ResourceView{Info: map[string]string{"fifo": "true"}}); b != "FIFO" {
		t.Fatalf("fifo badge = %q", b)
	}
	if b := resBadges(control.ResourceView{Info: map[string]string{"redrive": "→dlq"}}); b != "DLQ↩" {
		t.Fatalf("dlq badge = %q", b)
	}
	if b := resBadges(control.ResourceView{}); b != "" {
		t.Fatalf("no-info badge = %q, want empty", b)
	}
}

func TestInspectorNav(t *testing.T) {
	m := inspectorModel()
	m.refreshItemView()
	m = send(m, key("down"))
	if m.inspCursor != 1 {
		t.Fatalf("down → cursor %d, want 1", m.inspCursor)
	}
	m = send(m, key("enter"))
	if !m.inspExpanded {
		t.Fatal("enter should expand the selected item")
	}
	// d stages a delete confirm carrying the item's handle.
	m = send(m, key("d"))
	if m.adminPending != "del:h2" {
		t.Fatalf("d should stage del:h2, got %q", m.adminPending)
	}
	// a non-confirm key cancels.
	m = send(m, key("x"))
	if m.adminPending != "" {
		t.Fatalf("cancel should clear pending, got %q", m.adminPending)
	}
}

func TestInspectorTabs(t *testing.T) {
	m := inspectorModel()
	m.refreshItemView()
	// ↓ always moves the item list (no focus juggling).
	m = send(m, key("down"))
	if m.inspCursor != 1 {
		t.Fatalf("down → inspCursor %d, want 1", m.inspCursor)
	}
	// →/← switch the resource tab.
	m = send(m, key("right"))
	if m.adminCursor != 1 {
		t.Fatalf("right → adminCursor %d, want 1", m.adminCursor)
	}
	m = send(m, key("left"))
	if m.adminCursor != 0 {
		t.Fatalf("left → adminCursor %d, want 0", m.adminCursor)
	}
}

func TestTabHitTest(t *testing.T) {
	m := inspectorModel() // tabs: "emails(2 msgs→2)" active, "orders.fifo"
	// The first tab starts at column 1; clicking there selects index 0.
	if got := m.tabAt(1); got != 0 {
		t.Fatalf("tabAt(1) = %d, want 0", got)
	}
	// A column far to the right past both tabs is a miss.
	if got := m.tabAt(500); got != -1 {
		t.Fatalf("tabAt(500) = %d, want -1", got)
	}
}

func TestInspectorComposeKey(t *testing.T) {
	m := inspectorModel()
	m = send(m, key("n")) // new → the queue's send composer
	if !m.composerMode || m.composerVerb != "send" {
		t.Fatalf("n should open the send composer, got mode=%v verb=%q", m.composerMode, m.composerVerb)
	}
}

func TestComposerFIFOAware(t *testing.T) {
	m := inspectorModel()
	// cursor 0 = "emails" (standard) → the group field is dropped.
	flds := m.composerFieldsFor("send")
	for _, f := range flds {
		if f.key == "group" {
			t.Fatal("standard queue should not offer a group field")
		}
	}
	// cursor 1 = "orders.fifo" → group is present, required-labelled, pre-filled.
	m.adminCursor = 1
	if !m.selectedIsFIFO() {
		t.Fatal("orders.fifo should be detected as FIFO")
	}
	flds = m.composerFieldsFor("send")
	var grp *composerField
	for i := range flds {
		if flds[i].key == "group" {
			grp = &flds[i]
		}
	}
	if grp == nil || grp.value != "default" || !strings.Contains(grp.label, "required") {
		t.Fatalf("FIFO group field = %+v", grp)
	}
	// Opening the composer on a FIFO queue pre-fills the group so a send works
	// out of the box (no MessageGroupId dead-end).
	nm, _ := m.openComposer("send")
	m = nm.(model)
	got := ""
	for _, f := range m.composerFlds {
		if f.key == "group" {
			got = f.value
		}
	}
	if got != "default" {
		t.Fatalf("FIFO composer group pre-fill = %q, want default", got)
	}
}

func TestComposerSubmit(t *testing.T) {
	m := inspectorModel()
	nm, _ := m.openComposer("send")
	m = nm.(model)
	// emails is a standard queue → body + attributes (no FIFO group field).
	if len(m.composerFlds) != 2 {
		t.Fatalf("standard send composer should have 2 fields, got %d", len(m.composerFlds))
	}
	m.composerFlds[0].value = "hello"
	m.composerFlds[1].value = "tier=gold"
	next, _ := m.composerSubmit()
	m = next.(model)
	if m.composerMode {
		t.Fatal("submit should close the composer")
	}
}

func TestInlinePayloadParsing(t *testing.T) {
	// publish: body tokens + k=v attributes + subject.
	got := inlinePayload("publish", `Welcome aboard tier=gold subject=Hi`)
	if !strings.Contains(got, `"message":"Welcome aboard"`) ||
		!strings.Contains(got, `"tier":"gold"`) || !strings.Contains(got, `"subject":"Hi"`) {
		t.Fatalf("publish inline payload = %s", got)
	}
	// send: group= maps to the group field.
	got = inlinePayload("send", `hello group=orders`)
	if !strings.Contains(got, `"body":"hello"`) || !strings.Contains(got, `"group":"orders"`) {
		t.Fatalf("send inline payload = %s", got)
	}
	// put: first token key, rest body.
	got = inlinePayload("put", `report.txt the body here`)
	if !strings.Contains(got, `"key":"report.txt"`) || !strings.Contains(got, `"body":"the body here"`) {
		t.Fatalf("put inline payload = %s", got)
	}
}

func TestCharSelectionText(t *testing.T) {
	m := model{copyCharMode: true, copyLines: []string{"hello world", "second line"}}
	// single line: [0,0)→[0,5) = "hello"
	m.copyAnchor, m.copyAnchorColCh = 0, 0
	m.copyCursor, m.copyColCh = 0, 5
	if got := m.selectedCharText(); got != "hello" {
		t.Fatalf("single-line char select = %q, want hello", got)
	}
	// reversed drag (cursor before anchor) still orders correctly
	m.copyAnchor, m.copyAnchorColCh = 0, 5
	m.copyCursor, m.copyColCh = 0, 0
	if got := m.selectedCharText(); got != "hello" {
		t.Fatalf("reversed char select = %q, want hello", got)
	}
	// multi-line: from (0,6) to (1,6) = "world\nsecond"
	m.copyAnchor, m.copyAnchorColCh = 0, 6
	m.copyCursor, m.copyColCh = 1, 6
	if got := m.selectedCharText(); got != "world\nsecond" {
		t.Fatalf("multi-line char select = %q, want %q", got, "world\nsecond")
	}
}

func TestInspectorRenders(t *testing.T) {
	m := threeInstances()
	m.cursor = 2                   // media (s3)
	m.resp.Instances[2].PID = 4242 // awake, so its contents render (asleep shows a wake prompt)
	m.cmd = textinput.New()
	m.adminMode = true
	m.adminLoaded = true
	m.adminName = "media"
	m.adminActs = []control.ActionView{
		{ID: "browse", Label: "Browse", Kind: "bucket"},
		{ID: "put", Label: "Put object", Kind: "bucket", InputHint: "key"},
		{ID: "empty", Label: "Empty", Kind: "bucket", Destructive: true},
	}
	m.adminRes = []control.ResourceView{{Kind: "bucket", Name: "uploads", Status: "2 objects"}}
	m.itemVP = viewport.New(60, 12)
	m.inspItems = []inspItem{
		{title: "logo.png", meta: "12K · 2026", delArg: "logo.png"},
		{title: "data.json", meta: "1.2K · 2026", delArg: "data.json"},
	}
	m.refreshItemView()
	out := m.View()
	// header instance, the resource rail, the item list, and the action bar.
	for _, want := range []string{"media", "uploads", "logo.png", "put", "delete", "esc"} {
		if !strings.Contains(out, want) {
			t.Fatalf("inspector view missing %q:\n%s", want, out)
		}
	}
}

func TestViewRendersInstancesAndKeys(t *testing.T) {
	m := threeInstances()
	out := m.View()
	for _, want := range []string{"app", "cache", "media", "boot", "reap", "doze"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q:\n%s", want, out)
		}
	}
	// The full key set (incl. restart) and the mouse/state legend live in `?` help.
	m.showHelp = true
	help := m.View()
	for _, want := range []string{"restart", "Mouse", "asleep", "cycle theme"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help overlay missing %q:\n%s", want, help)
		}
	}
	m.showHelp = false
	// Selecting the failed instance surfaces its error detail.
	m.cursor = 2
	if !strings.Contains(m.View(), "boom") {
		t.Fatalf("view should surface the selected instance's error:\n%s", m.View())
	}
}

func TestDaemonLossHeader(t *testing.T) {
	m := threeInstances()
	// A failed status poll must flip the header out of "live" into a visible
	// lost state (red dot + banner) — never a green "live" over stale data.
	m = send(m, statusMsg{err: errors.New("connect: no such file")})
	head := m.viewHeader()
	if !strings.Contains(head, "lost") || !strings.Contains(head, "unreachable") {
		t.Fatalf("lost header missing the banner:\n%s", head)
	}
	if strings.Contains(head, "live") {
		t.Fatalf("header must not say live while the daemon is unreachable:\n%s", head)
	}
	// The next successful poll restores the live state.
	m = send(m, statusMsg{resp: m.resp})
	head = m.viewHeader()
	if !strings.Contains(head, "live") || strings.Contains(head, "unreachable") {
		t.Fatalf("recovered header should be live again:\n%s", head)
	}
	if !m.lastOK.IsZero() && m.connLost {
		t.Fatal("connLost should clear on a successful poll")
	}
}

func TestFooterTrimsToWidth(t *testing.T) {
	m := threeInstances()
	// At full width the core actions are all present.
	m.width = 110
	f := m.viewFooter()
	for _, want := range []string{"boot", "reap", "help", "quit"} {
		if !strings.Contains(f, want) {
			t.Fatalf("footer at 110 missing %q: %s", want, f)
		}
	}
	// Narrower windows drop the least-important hints first, never overflow,
	// and always keep "? help" and "q quit".
	for _, w := range []int{80, 64} {
		m.width = w
		f := m.viewFooter()
		if got := lipgloss.Width(f); got > w {
			t.Fatalf("footer at %d cols is %d wide: %s", w, got, f)
		}
		for _, want := range []string{"help", "quit"} {
			if !strings.Contains(f, want) {
				t.Fatalf("footer at %d missing %q: %s", w, want, f)
			}
		}
	}
}

func TestTaintedGlyphAndTotals(t *testing.T) {
	m := threeInstances()
	tainted := control.InstanceView{Name: "db", Engine: "postgres", State: "active", PID: 42, Tainted: true}
	if g := m.glyph(tainted); !strings.Contains(g, "!") {
		t.Fatalf("tainted glyph = %q, want a '!' marker", g)
	}
	m.resp.Instances = append(m.resp.Instances, tainted)
	totals := strings.Join(m.sidebarTotals(30), "\n")
	if !strings.Contains(totals, "!1") {
		t.Fatalf("sidebar totals missing the tainted count: %q", totals)
	}
	// The tainted instance must not be counted as asleep (media reads as error,
	// so with tainted tallied separately the asleep count stays zero).
	if !strings.Contains(totals, "·0") {
		t.Fatalf("asleep count off (tainted leaked into it?): %q", totals)
	}
}

func TestDashConfirmFlow(t *testing.T) {
	m := threeInstances()
	// d stages a reap confirm rather than executing.
	m = send(m, key("d"))
	if m.dashPending != "down:app" {
		t.Fatalf("d should stage down:app, got %q", m.dashPending)
	}
	// The confirm renders as a centered modal (not a lowkey footer line).
	if out := m.View(); !strings.Contains(out, "reap app?") || !strings.Contains(out, "data is kept") {
		t.Fatalf("confirm modal should show the verb, target and consequence:\n%s", out)
	}
	// y confirms: pending clears and the action command is dispatched.
	next, cmd := m.Update(key("y"))
	m = next.(model)
	if m.dashPending != "" || cmd == nil {
		t.Fatalf("y should execute the staged reap (pending=%q cmd=%v)", m.dashPending, cmd)
	}
	// R stages a restart; any other key cancels it.
	m = send(m, key("R"))
	if m.dashPending != "restart:app" {
		t.Fatalf("R should stage restart:app, got %q", m.dashPending)
	}
	m = send(m, key("n"))
	if m.dashPending != "" {
		t.Fatalf("a non-confirm key should cancel, got %q", m.dashPending)
	}
}

func TestErrorFlashPersistsUntilDismissed(t *testing.T) {
	m := threeInstances()
	m = send(m, actionMsg{verb: "boot", name: "app", err: errors.New("exec: not found")})
	if !m.flashErr || m.flash == "" {
		t.Fatalf("an action error should set a persistent flash, got %+v", m.flash)
	}
	// The ~2.5s spin expiry must not clear an error flash.
	m.frame += 100
	m = send(m, spinMsg{})
	if m.flash == "" {
		t.Fatal("error flash should survive the auto-clear window")
	}
	// esc dismisses it.
	m = send(m, key("esc"))
	if m.flash != "" || m.flashErr {
		t.Fatalf("esc should dismiss the error flash, got %q", m.flash)
	}
}

func TestTruncateWideChars(t *testing.T) {
	// CJK: each rune is 2 columns; the result must fit the column budget.
	if got := truncate("ハローワールド", 5); lipgloss.Width(got) > 5 || !strings.HasSuffix(got, "…") {
		t.Fatalf("CJK truncate = %q (width %d)", got, lipgloss.Width(got))
	}
	if got := truncate("🚀🚀🚀🚀🚀", 4); lipgloss.Width(got) > 4 {
		t.Fatalf("emoji truncate = %q (width %d)", got, lipgloss.Width(got))
	}
	// Plain ASCII within budget passes through untouched.
	if got := truncate("hello", 10); got != "hello" {
		t.Fatalf("short string mangled: %q", got)
	}
	if got := truncate("hello world", 6); lipgloss.Width(got) > 6 || !strings.HasSuffix(got, "…") {
		t.Fatalf("ascii truncate = %q", got)
	}
}

func TestConsoleRowMath(t *testing.T) {
	// inspectorModel: item 0 has a meta line (2 rows), item 1 does not (1 row).
	m := inspectorModel()
	for row, want := range map[int]int{0: 0, 1: 0, 2: 1, 3: -1} {
		if got := m.itemIndexAtRow(row); got != want {
			t.Fatalf("collapsed: itemIndexAtRow(%d) = %d, want %d", row, got, want)
		}
	}
	// Expanding item 0 (detail "hello" = rule + 1 line) grows it to 4 rows.
	m.inspCursor, m.inspExpanded = 0, true
	for row, want := range map[int]int{0: 0, 3: 0, 4: 1, 5: -1} {
		if got := m.itemIndexAtRow(row); got != want {
			t.Fatalf("expanded: itemIndexAtRow(%d) = %d, want %d", row, got, want)
		}
	}
}

func TestItemIdentityPinning(t *testing.T) {
	m := inspectorModel()
	m.inspCursor = 1 // "world" (handle h2)
	// A refresh where a new message lands at the head must keep the cursor on
	// the same message, not the same position.
	shifted := []inspItem{
		{title: "new arrival", delArg: "h0"},
		{title: "hello", meta: "group a", detail: "hello", delArg: "h1"},
		{title: "world", detail: "world", delArg: "h2"},
	}
	m = send(m, itemsMsg{name: "jobs_sqs", resource: "emails", kind: "queue", items: shifted})
	if it, _ := m.selectedItem(); it.delArg != "h2" {
		t.Fatalf("selection drifted to %q, want the h2 item", it.delArg)
	}
}

// ── command palette ─────────────────────────────────────────────────────────

func TestPaletteOpensAndDispatchesAliases(t *testing.T) {
	m := threeInstances()
	m = send(m, key(":"))
	if !m.paletteMode {
		t.Fatal("':' should open the command palette")
	}
	// :boot is an alias of wake — it resolves through the registry and fires
	// the control op against the named instance.
	m.palInput = "boot cache"
	next, cmd := m.Update(key("enter"))
	m = next.(model)
	if m.paletteMode {
		t.Fatal("enter should close the palette")
	}
	if cmd == nil {
		t.Fatal("':boot cache' should dispatch a command")
	}
	if !strings.Contains(m.flash, "wake cache") {
		t.Fatalf("dispatch flash = %q, want the primary verb + name", m.flash)
	}
	// Unknown verbs surface a persistent error, not silence.
	m = send(m, key(":"))
	m.palInput = "frobnicate"
	m = send(m, key("enter"))
	if !m.flashErr || !strings.Contains(m.flash, "unknown command") {
		t.Fatalf("unknown verb flash = %q (err=%v)", m.flash, m.flashErr)
	}
}

func TestPaletteRequiredArgDefaultsToSelection(t *testing.T) {
	m := threeInstances() // selection = app
	m = send(m, key(":"))
	m.palInput = "restart"
	m = send(m, key("enter"))
	if m.dashPending != "restart:app" {
		t.Fatalf("bare :restart should stage the selected instance, got %q", m.dashPending)
	}
	// With no instances at all there is nothing to default to.
	empty := model{width: 110, height: 30, filter: textinput.New(), paletteMode: true, palInput: "restart"}
	empty = send(empty, key("enter"))
	if !empty.flashErr || !strings.Contains(empty.flash, "needs a service name") {
		t.Fatalf("no-selection flash = %q (err=%v)", empty.flash, empty.flashErr)
	}
}

func TestPaletteSuggestions(t *testing.T) {
	m := threeInstances()
	// Verb position: registry matches (dash-visible only) merged with locals.
	m.palInput = "s"
	labels := map[string]bool{}
	for _, s := range m.paletteSuggestions() {
		labels[s.label] = true
	}
	for _, want := range []string{"sleep", "sync"} {
		if !labels[want] {
			t.Fatalf("verb suggestions for 's' missing %q: %v", want, labels)
		}
	}
	if labels["status"] {
		t.Fatal("CLI-only verbs must not appear in the palette")
	}
	// The console local verb matches by its aliases too.
	m.palInput = "man"
	found := false
	for _, s := range m.paletteSuggestions() {
		if s.label == "console" {
			found = true
		}
	}
	if !found {
		t.Fatal("alias prefix 'man' should surface the console verb")
	}
	// An alias prefix still surfaces the action under its primary name.
	m.palInput = "boo"
	sugs := m.paletteSuggestions()
	if len(sugs) == 0 || sugs[0].label != "wake" {
		t.Fatalf("alias prefix 'boo' should suggest wake, got %+v", sugs)
	}
	// Local view verbs are merged in.
	m.palInput = "the"
	sugs = m.paletteSuggestions()
	if len(sugs) != 1 || sugs[0].label != "theme" {
		t.Fatalf("'the' should suggest the local theme verb, got %+v", sugs)
	}
	// Argument position: instance names, prefix-filtered, engine as the summary.
	m.palInput = "wake a"
	sugs = m.paletteSuggestions()
	if len(sugs) != 1 || sugs[0].label != "app" || sugs[0].summary != "postgres" {
		t.Fatalf("arg suggestions for 'wake a' = %+v", sugs)
	}
	// :console narrows to manageable builtins — only the s3 instance qualifies.
	m.palInput = "console "
	sugs = m.paletteSuggestions()
	if len(sugs) != 1 || sugs[0].label != "media" {
		t.Fatalf(":console should suggest only builtins, got %+v", sugs)
	}
}

func TestPaletteTabCompletion(t *testing.T) {
	m := threeInstances()
	m = send(m, key(":"))
	m.palInput = "wa"
	m = send(m, key("tab"))
	if m.palInput != "wake " {
		t.Fatalf("tab should complete the verb (with a space for its arg), got %q", m.palInput)
	}
	// ↓ moves the highlight; tab completes the highlighted instance.
	m = send(m, key("down"))
	m = send(m, key("tab"))
	if m.palInput != "wake cache" {
		t.Fatalf("tab should complete the highlighted arg, got %q", m.palInput)
	}
	// Esc closes without acting.
	m = send(m, key("esc"))
	if m.paletteMode || m.palInput != "" {
		t.Fatalf("esc should close the palette, got mode=%v input=%q", m.paletteMode, m.palInput)
	}
}

func TestPaletteConfirmFlow(t *testing.T) {
	m := threeInstances()
	m = send(m, key(":"))
	m.palInput = "restart cache"
	m = send(m, key("enter"))
	if m.dashPending != "restart:cache" {
		t.Fatalf(":restart cache should stage a confirm, got %q", m.dashPending)
	}
	if out := m.View(); !strings.Contains(out, "restart cache?") {
		t.Fatalf("confirm modal should prompt for the staged restart:\n%s", out)
	}
	next, cmd := m.Update(key("y"))
	m = next.(model)
	if m.dashPending != "" || cmd == nil {
		t.Fatalf("y should execute the staged restart (pending=%q cmd=%v)", m.dashPending, cmd)
	}
	// :sleep with no arg means every awake service — the confirm says so.
	m = send(m, key(":"))
	m.palInput = "sleep"
	m = send(m, key("enter"))
	if m.dashPending != "down:" {
		t.Fatalf("bare :sleep should stage a fleet-wide reap, got %q", m.dashPending)
	}
	if out := m.View(); !strings.Contains(out, "reap ALL services?") {
		t.Fatalf("fleet-wide confirm modal missing:\n%s", out)
	}
	m = send(m, key("n")) // cancel
	if m.dashPending != "" {
		t.Fatalf("cancel should clear pending, got %q", m.dashPending)
	}
}

func TestPaletteDiscoverability(t *testing.T) {
	m := threeInstances()
	m.width = 80
	if f := m.viewFooter(); !strings.Contains(f, "cmds") {
		t.Fatalf("footer at 80 cols should keep the ':' hint: %s", f)
	}
	m.width = 110
	m.showHelp = true
	if help := m.View(); !strings.Contains(help, "palette") {
		t.Fatalf("help overlay should document the command palette:\n%s", help)
	}
}

func TestPaletteEnterRunsHighlightedSuggestion(t *testing.T) {
	// The reported bug: open ':', arrow to a row, press Enter → nothing.
	// Enter must adopt the highlighted suggestion when the typed text doesn't
	// resolve by itself.
	m := threeInstances()
	m = send(m, key(":"))
	sugs := m.cappedSuggestions()
	if len(sugs) < 2 {
		t.Fatalf("expected suggestions on an empty palette, got %d", len(sugs))
	}
	m = send(m, key("down")) // highlight the second row
	want := sugs[1].insert
	next, cmd := m.Update(key("enter"))
	m = next.(model)
	if m.paletteMode {
		t.Fatal("enter should close the palette")
	}
	// The highlighted verb must have executed: either a dispatched command, a
	// staged confirm, or at minimum a flash naming it — never dead silence.
	if cmd == nil && m.dashPending == "" && m.flash == "" {
		t.Fatalf("highlighting %q and pressing enter did nothing", want)
	}
	if m.dashPending != "" {
		m = send(m, key("n")) // resolve a staged confirm before the next block
	}

	// A typed prefix adopts the highlight too: "wa" + enter runs wake.
	m = send(m, key(":"))
	m.palInput, m.palSel = "wa", 0
	next, cmd = m.Update(key("enter"))
	m = next.(model)
	if cmd == nil || !strings.Contains(m.flash, "wake") {
		t.Fatalf("':wa' + enter should run wake via the highlight; flash=%q cmd=%v", m.flash, cmd)
	}

	// An exact arg still executes as typed (no highlight hijack): wake cache.
	m = send(m, key(":"))
	m.palInput, m.palSel = "wake cache", 0
	next, _ = m.Update(key("enter"))
	m = next.(model)
	if !strings.Contains(m.flash, "wake cache") {
		t.Fatalf("exact ':wake cache' flash = %q", m.flash)
	}
}

// ── round 2: spotlight palette, confirm modal, charts, copy, management ────

func TestPaletteRendersCentered(t *testing.T) {
	// A tall sidebar so services fall on the palette's rows — the overlay must
	// splice the box in, not blank the whole row and hide them.
	m := threeInstances()
	m.width, m.height = 100, 30
	insts := make([]control.InstanceView, 16)
	for i := range insts {
		insts[i] = control.InstanceView{Name: fmt.Sprintf("svc_%02d", i), Engine: "postgres", State: "active"}
	}
	m.resp.Instances = insts
	m = send(m, key(":"))
	lines := strings.Split(m.View(), "\n")
	boxRow, boxCol := -1, -1
	for i, ln := range lines {
		// The palette box's top-left corner sits at the centered VISUAL column, past
		// the sidebar; the row still carries the sidebar to its left (that's the fix).
		// Measure the column by display width, not byte offset — the sidebar's ANSI
		// styling inflates byte indices.
		idx := strings.Index(ln, "╭")
		if idx < 0 {
			continue
		}
		if c := ansi.StringWidth(ln[:idx]); c >= 15 && c <= 25 {
			boxRow, boxCol = i, c
			break
		}
	}
	if boxRow < 0 {
		t.Fatalf("palette box not found in view:\n%s", m.View())
	}
	// Upper third of a 30-row screen, horizontally centered (panel = 60 cols).
	if boxRow < 2 || boxRow > 10 {
		t.Fatalf("palette top row = %d, want the upper third (2..10)", boxRow)
	}
	if boxCol < 15 || boxCol > 25 {
		t.Fatalf("palette left col = %d, want ~20 (centered 60-col panel)", boxCol)
	}
	// The sidebar survives on the palette's own rows — every service still renders.
	out := m.View()
	for _, name := range []string{"svc_00", "svc_08", "svc_15"} {
		if !strings.Contains(out, name) {
			t.Fatalf("sidebar service %q hidden behind the palette overlay:\n%s", name, out)
		}
	}
	// ctrl+k opens it too.
	m2 := threeInstances()
	m2 = send(m2, tea.KeyMsg{Type: tea.KeyCtrlK})
	if !m2.paletteMode {
		t.Fatal("ctrl+k should open the palette")
	}
}

func TestConfirmModalCenteredAndKeys(t *testing.T) {
	m := threeInstances()
	m.width, m.height = 100, 30
	m = send(m, key("d")) // stage reap app
	lines := strings.Split(m.View(), "\n")
	promptRow := -1
	for i, ln := range lines {
		if strings.Contains(ln, "reap app?") {
			promptRow = i
			break
		}
	}
	if promptRow < 0 {
		t.Fatalf("confirm modal not rendered:\n%s", m.View())
	}
	if promptRow < 9 || promptRow > 20 {
		t.Fatalf("confirm modal row = %d, want vertically centered", promptRow)
	}
	// esc cancels.
	m = send(m, key("esc"))
	if m.dashPending != "" {
		t.Fatalf("esc should cancel the staged action, got %q", m.dashPending)
	}
	// y executes (staged again first).
	m = send(m, key("d"))
	next, cmd := m.Update(key("y"))
	m = next.(model)
	if m.dashPending != "" || cmd == nil {
		t.Fatal("y should execute the staged action")
	}
}

func TestSteadySeriesDetection(t *testing.T) {
	flat := []float64{100, 100.5, 100.2, 100.4, 100.1}
	if !steadySeries(flat) {
		t.Fatal("sub-5%-range series should read as steady")
	}
	moving := []float64{1, 4, 10, 6, 2}
	if steadySeries(moving) {
		t.Fatal("a clearly moving series is not steady")
	}
}

// blockRunes are the eighth-block glyphs banned from the redesigned charts.
const blockRunes = "▁▂▃▄▅▆▇█"

func containsAnyRune(s, set string) bool {
	return strings.ContainsAny(s, set)
}

func TestCurveChartGlyphs(t *testing.T) {
	// Rise then fall: levels 0,0,3,3,1,1 across 6 columns and 4 rows.
	vals := []float64{0, 0, 3, 3, 1, 1}
	rows := curveChart(vals, 6, 4)
	if len(rows) != 4 {
		t.Fatalf("curveChart rows = %d, want 4", len(rows))
	}
	out := strings.Join(rows, "\n")
	for _, want := range []string{"╭", "╯", "╮", "╰", "│", "─"} {
		if !strings.Contains(out, want) {
			t.Fatalf("curve chart missing %q:\n%s", want, out)
		}
	}
	if containsAnyRune(out, blockRunes) {
		t.Fatalf("curve chart must not use block glyphs:\n%s", out)
	}
	// Flat series → a single mid-height '─' run.
	flatRows := curveChart([]float64{5, 5, 5}, 6, 4)
	flat := strings.Join(flatRows, "\n")
	if strings.Count(flat, "─") != 6 || containsAnyRune(flat, "╭╮╰╯│") {
		t.Fatalf("flat series should be one plain run:\n%s", flat)
	}
}

func TestMemorySectionLayout(t *testing.T) {
	const mb = 1024 * 1024
	// Steady: chart still renders (flat), title carries "steady · value".
	h := &history{}
	for i := 0; i < 50; i++ {
		h.push(float64(10*mb), 1)
	}
	sec := memorySection(h, 10*mb, 80)
	if len(sec) != 5 { // title + 4 chart rows
		t.Fatalf("memory section rows = %d, want 5", len(sec))
	}
	if !strings.Contains(sec[0], "memory · steady · 10.00 MB") {
		t.Fatalf("steady title = %q", sec[0])
	}
	if !strings.Contains(sec[0], "last ") {
		t.Fatalf("title should right-carry the window: %q", sec[0])
	}
	chart := strings.Join(sec[1:], "\n")
	if containsAnyRune(chart, blockRunes) {
		t.Fatalf("memory chart must not use block glyphs:\n%s", chart)
	}
	// Gutter on the LEFT: top row ends the gutter with ┤, bottom with ┼, and a
	// flat series labels only the bottom row.
	if !strings.Contains(sec[1], "┤") || !strings.Contains(sec[4], "10.0 ┼") {
		t.Fatalf("gutter axis wrong:\n%s", chart)
	}
	// The right side of the chart rows is empty — no label stack.
	for _, row := range sec[1:] {
		trimmed := strings.TrimRight(row, " ")
		if strings.Contains(trimmed, "MB") || strings.Contains(trimmed, "peak") {
			t.Fatalf("chart row carries right-side labels: %q", row)
		}
	}

	// Varying, current == peak: the title prints the value once.
	h2 := &history{}
	for i := 0; i < 50; i++ {
		h2.push(float64(10*mb+i*mb/5), 1)
	}
	cur := int64(10*mb + 49*mb/5)
	sec2 := memorySection(h2, cur, 80)
	if got := strings.Count(sec2[0], memStr(cur)); got != 1 {
		t.Fatalf("now==peak should print once, got %d in %q", got, sec2[0])
	}
	if !strings.Contains(strings.Join(sec2[1:], "\n"), "╭") {
		t.Fatalf("varying memory should draw the curved line:\n%s", strings.Join(sec2[1:], "\n"))
	}
	// Distinct peak shows in the title, not beside the chart.
	sec3 := memorySection(h2, 10*mb+5*mb, 80)
	if !strings.Contains(sec3[0], "peak "+memStr(cur)) {
		t.Fatalf("distinct peak should live in the title: %q", sec3[0])
	}
}

func TestConnsSectionCollapsesAndSteps(t *testing.T) {
	// Constant count: one quiet row, no chart, no blocks.
	h := &history{}
	for i := 0; i < 60; i++ {
		h.push(1, 4)
	}
	sec := connsSection(h, 4, 80)
	if len(sec) != 1 || !strings.Contains(sec[0], "conns · 4 for ") {
		t.Fatalf("constant conns should be one text row, got %+v", sec)
	}
	// Varying count: title + 2-row step line with integer gutter labels.
	h2 := &history{}
	for i := 0; i < 60; i++ {
		h2.push(1, float64(i%5))
	}
	sec2 := connsSection(h2, 3, 80)
	if len(sec2) != 3 {
		t.Fatalf("varying conns rows = %d, want 3 (title + 2 chart)", len(sec2))
	}
	if !strings.Contains(sec2[0], "now 3") || !strings.Contains(sec2[0], "peak 4") {
		t.Fatalf("conns title = %q", sec2[0])
	}
	chart := strings.Join(sec2[1:], "\n")
	if containsAnyRune(chart, blockRunes) {
		t.Fatalf("conns chart must not use block glyphs:\n%s", chart)
	}
	if !strings.Contains(sec2[1], "4 ┤") || !strings.Contains(sec2[2], "0 ┼") {
		t.Fatalf("conns gutter labels wrong:\n%s", chart)
	}
}

func TestDetailCardLayout(t *testing.T) {
	m := threeInstances()
	m.resp.Instances[0].PID = 63710
	m.resp.Instances[0].Conns = 4
	m.resp.Instances[0].RAM = 10 * 1024 * 1024
	m.resp.Instances[0].Endpoint = "127.0.0.1:5432"
	m.resp.Instances[0].URL = "postgres://localhost:5432/app"
	m.resp.Instances[0].EnvVar = "DATABASE_URL"
	h := &history{}
	for i := 0; i < 30; i++ {
		h.push(10*1024*1024, 4)
	}
	m.hist["app"] = h
	v := m.resp.Instances[0]
	lines := m.detailLines(v, 100)

	// Title row consolidates the state facts; the old "state ACTIVE" row is gone.
	if !strings.Contains(lines[0], "app") || !strings.Contains(lines[0], "ACTIVE") ||
		!strings.Contains(lines[0], "4 conns") || !strings.Contains(lines[0], "up ") {
		t.Fatalf("title row should carry the facts cluster: %q", lines[0])
	}
	for _, ln := range lines[1:] {
		if strings.Contains(ln, "state ") {
			t.Fatalf("old state row survived: %q", ln)
		}
	}
	// Structure: blank / endpoint / url / data / blank / memory… (1-line URL, no
	// presumed var name — apps take the value under their own name).
	if lines[1] != "" {
		t.Fatalf("row 1 should be blank, got %q", lines[1])
	}
	if !strings.Contains(lines[2], "endpoint") || !strings.Contains(lines[3], "url") ||
		!strings.Contains(lines[3], "postgres://") || strings.Contains(lines[3], "DATABASE_URL") {
		t.Fatalf("connection rows off: %q / %q", lines[2], lines[3])
	}
	if !strings.Contains(lines[4], "data") || !strings.Contains(lines[4], "pid 63710") {
		t.Fatalf("data row should carry the dim pid: %q", lines[4])
	}
	if lines[5] != "" || !strings.Contains(lines[6], "memory · steady · 10.00 MB") {
		t.Fatalf("memory section should follow one blank row: %q / %q", lines[5], lines[6])
	}
	// Chart rows 7..10, then a blank, then the constant-conns row.
	if lines[11] != "" || !strings.Contains(lines[12], "conns · 4 for ") {
		t.Fatalf("conns section should follow one blank row: %q / %q", lines[11], lines[12])
	}
	if len(lines) != 13 {
		t.Fatalf("card rows = %d, want 13", len(lines))
	}

	// Asleep: a short card — no charts, facts say it wakes on connect.
	sleeping := m.resp.Instances[1] // cache, PID 0
	sl := m.detailLines(sleeping, 100)
	joined := strings.Join(sl, "\n")
	if strings.Contains(joined, "memory ·") || strings.Contains(joined, "conns ·") {
		t.Fatalf("asleep card must skip the chart sections:\n%s", joined)
	}
	if !strings.Contains(sl[0], "wakes on connect") {
		t.Fatalf("asleep title facts = %q", sl[0])
	}
	if len(sl) >= len(lines) {
		t.Fatalf("asleep card should be shorter (%d vs %d rows)", len(sl), len(lines))
	}
}

func TestBuiltinStatusStripSeparated(t *testing.T) {
	m := threeInstances()
	m.resp.Instances[2].PID = 9 // media (s3) running
	m.resp.Instances[2].State = "active"
	m.resp.Instances[2].LastError = "" // healthy — the strip shows resources
	m.adminName = "media"
	m.adminRes = []control.ResourceView{{Kind: "queue", Name: "emails"}, {Kind: "queue", Name: "jobs"}}
	h := &history{}
	for i := 0; i < 20; i++ {
		h.push(5*1024*1024, 0)
	}
	m.hist["media"] = h
	lines := m.detailLines(m.resp.Instances[2], 100)
	strip := -1
	for i, ln := range lines {
		if strings.Contains(ln, ":console") {
			strip = i
		}
	}
	if strip < 1 {
		t.Fatalf("builtin status strip missing:\n%s", strings.Join(lines, "\n"))
	}
	if lines[strip-1] != "" {
		t.Fatalf("status strip must keep a blank row above, got %q", lines[strip-1])
	}
	if !strings.Contains(lines[strip+1], "emails") {
		t.Fatalf("resource names should follow the strip: %q", lines[strip+1])
	}
}

func TestCopyModeFeedback(t *testing.T) {
	m := threeInstances()
	m.resp.Instances[0].PID = 7 // logs pane renders for a running instance
	v := m.resp.Instances[0]
	m.copyMode = true
	m.copyLines = []string{"alpha", "beta", "gamma"}
	m.copyCursor, m.copyAnchor = 2, 1
	if head := m.viewLogs(v, 60); !strings.Contains(head, "2 lines selected") {
		t.Fatalf("copy header should report the live selection: %s", head)
	}
	// A mouse drag reports characters instead.
	m.copyCharMode = true
	m.copyAnchor, m.copyAnchorColCh = 0, 0
	m.copyCursor, m.copyColCh = 0, 5
	if head := m.viewLogs(v, 60); !strings.Contains(head, "5 chars") {
		t.Fatalf("drag header should report chars: %s", head)
	}
	// After a copy, the flash says exactly what was taken.
	m.copyCharMode = false
	m.copyCursor, m.copyAnchor = 2, 1
	next, _ := m.copySelection()
	m = next.(model)
	if !m.flashErr && !strings.Contains(m.flash, "2 lines") {
		t.Fatalf("copy flash = %q, want the line count", m.flash)
	}
	if m.copyMode {
		t.Fatal("copy should leave copy mode")
	}
}

func TestEnterBootsInsteadOfConsole(t *testing.T) {
	m := threeInstances()
	m.resp.Instances[2].PID = 42 // media (s3) is running — old behavior opened the console
	m.cursor = 2
	next, cmd := m.Update(key("enter"))
	m = next.(model)
	if m.adminMode || m.mgmtMode {
		t.Fatal("enter must not open the console anymore")
	}
	if cmd == nil || !strings.Contains(m.flash, "booting media") {
		t.Fatalf("enter should boot (flash=%q)", m.flash)
	}
}

func TestManagementView(t *testing.T) {
	m := threeInstances()
	// The rail lists only the manageable builtins.
	bs := m.builtinInstances()
	if len(bs) != 1 || bs[0].Name != "media" {
		t.Fatalf("builtinInstances = %+v, want just media", bs)
	}
	// :console opens straight into the console — no rail-focus step to tab through.
	m = send(m, key(":"))
	m.palInput = "console"
	m = send(m, key("enter"))
	if !m.mgmtMode || !m.adminMode {
		t.Fatalf("bare :console should open straight into the console (mgmt=%v admin=%v)",
			m.mgmtMode, m.adminMode)
	}
	out := m.View()
	if !strings.Contains(out, "SERVICES") || !strings.Contains(out, "media") {
		t.Fatalf("console view missing the switcher rail:\n%s", out)
	}
	// esc leaves straight for the dash (no rail limbo in between).
	m = send(m, key("esc"))
	if m.mgmtMode || m.adminMode {
		t.Fatal("esc in the console should leave straight for the dash")
	}
	// A named non-builtin errors; no builtins at all errors too.
	m = send(m, key(":"))
	m.palInput = "console app"
	m = send(m, key("enter"))
	if !m.flashErr || !strings.Contains(m.flash, "manageable") {
		t.Fatalf("non-builtin :console target should error, got %q", m.flash)
	}
	empty := threeInstances()
	empty.resp.Instances = empty.resp.Instances[:2] // postgres + valkey only
	nm, _ := empty.openMgmt("")
	empty = nm.(model)
	if !empty.flashErr || !strings.Contains(empty.flash, "nothing to manage") {
		t.Fatalf("no builtins should error, got %q", empty.flash)
	}
}

func TestDomainAddrDisplay(t *testing.T) {
	v := control.InstanceView{Name: "orders", Engine: "postgres",
		Endpoint: "127.0.0.1:5432", Domain: "orders-pg.local"}
	if got := domainAddr(v); got != "orders-pg.local:5432" {
		t.Fatalf("domainAddr = %q", got)
	}
	if got := domainAddr(control.InstanceView{Endpoint: "127.0.0.1:5432"}); got != "" {
		t.Fatalf("no domain should yield empty, got %q", got)
	}
	// The detail card leads with the friendly name.
	m := threeInstances()
	m.resp.Instances[0].Domain = "app-pg.local"
	m.resp.Instances[0].Endpoint = "127.0.0.1:5432"
	m.resp.Instances[0].PID = 7
	if out := m.View(); !strings.Contains(out, "app-pg.local:") {
		t.Fatalf("detail card should show the domain endpoint:\n%s", out)
	}
}
