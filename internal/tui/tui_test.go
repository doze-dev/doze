package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/doze-dev/doze/internal/control"
)

func key(s string) tea.KeyMsg {
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func threeInstances() model {
	fi := textinput.New()
	fi.Prompt = "/"
	return model{
		width: 110, height: 30,
		follow: true,
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

func TestInspectorComposeKey(t *testing.T) {
	m := inspectorModel()
	m = send(m, key("n")) // new → the queue's send composer
	if !m.composerMode || m.composerVerb != "send" {
		t.Fatalf("n should open the send composer, got mode=%v verb=%q", m.composerMode, m.composerVerb)
	}
}

func TestComposerSubmit(t *testing.T) {
	m := inspectorModel()
	nm, _ := m.openComposer("send")
	m = nm.(model)
	if len(m.composerFlds) != 3 {
		t.Fatalf("send composer should have 3 fields, got %d", len(m.composerFlds))
	}
	m.composerFlds[0].value = "hello"
	m.composerFlds[2].value = "tier=gold"
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
	m.cursor = 2 // media (s3)
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
	for _, want := range []string{"app", "cache", "media", "boot", "reap", "follow", "doze"} {
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
