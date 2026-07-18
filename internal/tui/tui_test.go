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

func TestViewRendersInstancesAndKeys(t *testing.T) {
	m := threeInstances()
	out := m.View()
	for _, want := range []string{"app", "cache", "media", "wake", "sleep", "doze"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q:\n%s", want, out)
		}
	}
	// The full key set (incl. restart) and the mouse/state legend live in `?` help.
	m.showHelp = true
	help := m.View()
	for _, want := range []string{"restart", "Mouse", "asleep", "cycle theme", "web console"} {
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
	for _, want := range []string{"wake", "sleep", "help", "quit"} {
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
	// s stages a sleep confirm rather than executing.
	m = send(m, key("s"))
	if m.dashPending != "down:app" {
		t.Fatalf("d should stage down:app, got %q", m.dashPending)
	}
	// The confirm renders as a centered modal (not a lowkey footer line).
	if out := m.View(); !strings.Contains(out, "sleep app?") || !strings.Contains(out, "data is kept") {
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
	// The web-console door completes instance names in argument position.
	m.palInput = "open me"
	sugs = m.paletteSuggestions()
	if len(sugs) == 0 || sugs[0].label != "media" || sugs[0].summary != "s3" {
		t.Fatalf("arg suggestions for 'open me' = %+v", sugs)
	}
	// Argument position: instance names, fuzzy-ranked — the prefix match ranks
	// ahead of substring matches — with the engine as the summary.
	m.palInput = "wake a"
	sugs = m.paletteSuggestions()
	if len(sugs) == 0 || sugs[0].label != "app" || sugs[0].summary != "postgres" {
		t.Fatalf("arg suggestions for 'wake a' = %+v", sugs)
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
		t.Fatalf("bare :sleep should stage a fleet-wide sleep, got %q", m.dashPending)
	}
	if out := m.View(); !strings.Contains(out, "sleep ALL services?") {
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
	if f := m.viewFooter(); !strings.Contains(f, "commands") {
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

// ── spotlight palette, confirm modal, charts, copy, web hand-off ────────────

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
	m = send(m, key("s")) // stage sleep app
	lines := strings.Split(m.View(), "\n")
	promptRow := -1
	for i, ln := range lines {
		if strings.Contains(ln, "sleep app?") {
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
	m = send(m, key("s"))
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

// blockRunes are the eighth-block glyphs banned from the charts.
const blockRunes = "▁▂▃▄▅▆▇█"

func containsAnyRune(s, set string) bool {
	return strings.ContainsAny(s, set)
}

// isBraille reports whether s contains at least one non-empty braille cell.
func hasBraille(s string) bool {
	for _, r := range s {
		if r > 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}

func TestBrailleChartShape(t *testing.T) {
	// Rise then fall across 6 columns and 2 rows.
	vals := []float64{0, 0, 3, 3, 1, 1}
	rows := brailleChart(vals, 6, 2)
	if len(rows) != 2 {
		t.Fatalf("brailleChart rows = %d, want 2", len(rows))
	}
	out := strings.Join(rows, "\n")
	if !hasBraille(out) {
		t.Fatalf("chart should be drawn in braille dots:\n%s", out)
	}
	if containsAnyRune(out, blockRunes) {
		t.Fatalf("chart must not use block glyphs:\n%s", out)
	}
	// Every column carries ink somewhere — the line is continuous.
	for x := 0; x < 6; x++ {
		inked := false
		for _, row := range rows {
			if []rune(row)[x] != 0x2800 {
				inked = true
			}
		}
		if !inked {
			t.Fatalf("column %d has no dots — the line broke:\n%s", x, out)
		}
	}
	// A flat series holds the middle: dots only in the middle row region.
	flat := brailleChart([]float64{5, 5, 5}, 6, 4)
	if !hasBraille(strings.Join(flat, "\n")) {
		t.Fatal("flat series should still draw a line")
	}
	for _, bad := range []string{flat[0], flat[3]} { // top and bottom rows stay empty
		if hasBraille(bad) {
			t.Fatalf("flat line should hold the middle rows:\n%s", strings.Join(flat, "\n"))
		}
	}
}

func TestResampleBucketsMean(t *testing.T) {
	// Downsampling must average buckets, not index-pick (aliasing loses spikes).
	vals := make([]float64, 100)
	for i := range vals {
		vals[i] = 10
	}
	vals[50] = 110 // a one-sample spike between pick points
	out := resample(vals, 10)
	found := false
	for _, v := range out {
		if v > 15 { // the spike survives as a raised bucket mean
			found = true
		}
	}
	if !found {
		t.Fatalf("bucket-mean resample lost the spike: %v", out)
	}
	// Upsampling interpolates between the two points.
	up := resample([]float64{0, 10}, 5)
	if up[0] != 0 || up[4] != 10 || up[2] <= up[1] || up[3] <= up[2] {
		t.Fatalf("interpolated upsample off: %v", up)
	}
}

func TestMemorySectionLayout(t *testing.T) {
	const mb = 1024 * 1024
	// Steady: chart still renders (flat), title carries "steady · value".
	h := &history{}
	for i := 0; i < 50; i++ {
		h.push(float64(10*mb), 0, 1)
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
		h2.push(float64(10*mb+i*mb/5), 0, 1)
	}
	cur := int64(10*mb + 49*mb/5)
	sec2 := memorySection(h2, cur, 80)
	if got := strings.Count(sec2[0], memStr(cur)); got != 1 {
		t.Fatalf("now==peak should print once, got %d in %q", got, sec2[0])
	}
	if !hasBraille(strings.Join(sec2[1:], "\n")) {
		t.Fatalf("varying memory should draw a braille line:\n%s", strings.Join(sec2[1:], "\n"))
	}
	// Distinct peak shows in the title, not beside the chart.
	sec3 := memorySection(h2, 10*mb+5*mb, 80)
	if !strings.Contains(sec3[0], "peak "+memStr(cur)) {
		t.Fatalf("distinct peak should live in the title: %q", sec3[0])
	}
}

func TestCPUSectionThresholds(t *testing.T) {
	// Never moved a whole percentage point → one quiet row (or nothing at 0).
	h := &history{}
	for i := 0; i < 50; i++ {
		h.push(1, 2.2, 1)
	}
	sec := cpuSection(h, 2.2, 80)
	if len(sec) != 1 || !strings.Contains(sec[0], "cpu · 2% for ") {
		t.Fatalf("flat cpu should be one text row, got %+v", sec)
	}
	// Truly idle the whole window → no section at all (the title fact covers it).
	h0 := &history{}
	for i := 0; i < 50; i++ {
		h0.push(1, 0.1, 1)
	}
	if sec := cpuSection(h0, 0.1, 80); sec != nil {
		t.Fatalf("idle cpu should render nothing, got %+v", sec)
	}
	// Real movement → title + 2 braille rows with % gutter labels.
	h2 := &history{}
	for i := 0; i < 50; i++ {
		h2.push(1, float64(5+i%40), 1)
	}
	sec2 := cpuSection(h2, 12, 80)
	if len(sec2) != 3 {
		t.Fatalf("moving cpu rows = %d, want 3 (title + 2 chart)", len(sec2))
	}
	if !strings.Contains(sec2[0], "12% now") || !strings.Contains(sec2[0], "peak 44%") {
		t.Fatalf("cpu title = %q", sec2[0])
	}
	if !hasBraille(strings.Join(sec2[1:], "\n")) {
		t.Fatalf("cpu chart should be braille:\n%s", strings.Join(sec2[1:], "\n"))
	}
}

func TestConnsSectionCollapsesAndSteps(t *testing.T) {
	// Constant count: one quiet row, no chart, no blocks.
	h := &history{}
	for i := 0; i < 60; i++ {
		h.push(1, 0, 4)
	}
	sec := connsSection(h, 4, 80)
	if len(sec) != 1 || !strings.Contains(sec[0], "conns · 4 for ") {
		t.Fatalf("constant conns should be one text row, got %+v", sec)
	}
	// Varying count: title + 2-row step line with integer gutter labels.
	h2 := &history{}
	for i := 0; i < 60; i++ {
		h2.push(1, 0, float64(i%5))
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
		h.push(10*1024*1024, 0, 4)
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
	// Structure: blank / connect / bind / data / blank / memory…. The connect
	// line is the paste-able address; the bind line is the raw truth with pid
	// and the conventional env-var name riding dim.
	if lines[1] != "" {
		t.Fatalf("row 1 should be blank, got %q", lines[1])
	}
	if !strings.Contains(lines[2], "connect") || !strings.Contains(lines[2], "postgres://") {
		t.Fatalf("connect row off: %q", lines[2])
	}
	if !strings.Contains(lines[3], "bind") || !strings.Contains(lines[3], "127.0.0.1:5432") ||
		!strings.Contains(lines[3], "pid 63710") || !strings.Contains(lines[3], "env DATABASE_URL") {
		t.Fatalf("bind row off: %q", lines[3])
	}
	if !strings.Contains(lines[4], "data") || strings.Contains(lines[4], "pid") {
		t.Fatalf("data row should exist without the pid (it moved to bind): %q", lines[4])
	}
	if lines[5] != "" || !strings.Contains(lines[6], "memory · steady · 10.00 MB") {
		t.Fatalf("memory section should follow one blank row: %q / %q", lines[5], lines[6])
	}
	// Chart rows 7..10, then a blank, then the constant-conns row. Idle CPU (0%)
	// renders no section at all — the title fact covers it.
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

func TestBuiltinStatusStrip(t *testing.T) {
	m := threeInstances()
	m.resp.Instances[2] = control.InstanceView{
		Name: "local", Engine: "aws", State: "active", PID: 9,
		URL: "http://aws.demo.doze"}
	m.adminName = "local"
	m.adminRes = []control.ResourceView{
		{Kind: "service", Name: "s3", Status: "4 buckets"},
		{Kind: "service", Name: "sqs", Status: "5 queues"},
	}
	h := &history{}
	for i := 0; i < 20; i++ {
		h.push(5*1024*1024, 0, 0)
	}
	m.hist["local"] = h
	lines := m.detailLines(m.resp.Instances[2], 100)
	strip := -1
	for i, ln := range lines {
		if strings.Contains(ln, "2 services") {
			strip = i
		}
	}
	if strip < 1 {
		t.Fatalf("aws status strip missing:\n%s", strings.Join(lines, "\n"))
	}
	if lines[strip-1] != "" {
		t.Fatalf("status strip must keep a blank row above, got %q", lines[strip-1])
	}
	if !strings.Contains(lines[strip+1], "s3 4 buckets") {
		t.Fatalf("service rows should carry their counts: %q", lines[strip+1])
	}
	// The aws engine serves its own console — the strip advertises the web door.
	if !strings.Contains(strings.Join(lines, "\n"), "opens the web console") {
		t.Fatalf("strip should advertise the web console:\n%s", strings.Join(lines, "\n"))
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

// ── web hand-off (enter / o / :open) ─────────────────────────────────────────

func withAWS(m model) model {
	m.resp.Instances = append(m.resp.Instances, control.InstanceView{
		Name: "local", Engine: "aws", State: "active", PID: 7,
		URL: "http://aws.demo.doze"})
	return m
}

func TestWebURLResolution(t *testing.T) {
	m := withAWS(threeInstances())
	// The aws engine opens its own console, never the bare gateway (whose
	// root answers as S3 ListBuckets XML).
	local, _ := m.instanceByName("local")
	if got := m.webURL(local); got != "http://aws.demo.doze/_console" {
		t.Fatalf("webURL(aws) = %q, want its /_console", got)
	}
	// A database has nothing to open.
	app, _ := m.instanceByName("app")
	if got := m.webURL(app); got != "" {
		t.Fatalf("webURL(postgres) = %q, want empty", got)
	}
}

func TestEnterWakesOrOpens(t *testing.T) {
	// enter on an asleep instance wakes it.
	m := threeInstances()
	m.cursor = 0 // app, PID 0
	next, cmd := m.Update(key("enter"))
	m = next.(model)
	if cmd == nil || !strings.Contains(m.flash, "waking app") {
		t.Fatalf("enter should wake an asleep instance (flash=%q)", m.flash)
	}
	// enter on the running aws instance opens its console.
	m = withAWS(threeInstances())
	m.cursor = 3 // the appended aws instance
	next, cmd = m.Update(key("enter"))
	m = next.(model)
	if cmd == nil || !strings.Contains(m.flash, "opening http://aws.demo.doze/_console") {
		t.Fatalf("enter on the aws instance should open its console (flash=%q)", m.flash)
	}
	// o opens explicitly; on a running service with no web door it says so.
	m = threeInstances()
	m.resp.Instances[0].PID = 7
	m.cursor = 0
	next, _ = m.Update(key("o"))
	m = next.(model)
	if !strings.Contains(m.flash, "nothing to open") {
		t.Fatalf("o on a database should explain itself (flash=%q)", m.flash)
	}
	// w wakes explicitly.
	m = threeInstances()
	m.cursor = 0
	next, cmd = m.Update(key("w"))
	m = next.(model)
	if cmd == nil || !strings.Contains(m.flash, "waking app") {
		t.Fatalf("w should wake (flash=%q)", m.flash)
	}
}

func TestPaletteOpenVerb(t *testing.T) {
	m := withAWS(threeInstances())
	m = send(m, key(":"))
	m.palInput = "open local"
	next, cmd := m.Update(key("enter"))
	m = next.(model)
	if cmd == nil || !strings.Contains(m.flash, "opening http://aws.demo.doze/_console") {
		t.Fatalf(":open local should open the console (flash=%q)", m.flash)
	}
	// :console is an alias for :open.
	m = send(m, key(":"))
	m.palInput = "console local"
	next, cmd = m.Update(key("enter"))
	m = next.(model)
	if cmd == nil || !strings.Contains(m.flash, "opening http://aws.demo.doze/_console") {
		t.Fatalf(":console <name> should open (flash=%q)", m.flash)
	}
	// Unknown name errors actionably.
	m = send(m, key(":"))
	m.palInput = "open nope"
	m = send(m, key("enter"))
	if !m.flashErr || !strings.Contains(m.flash, "unknown service") {
		t.Fatalf(":open nope flash = %q", m.flash)
	}
}

func TestConnectLinePrecedence(t *testing.T) {
	// URL wins over everything.
	v := control.InstanceView{URL: "postgres://app@db:5432/app",
		Resource: "http://s3.demo.doze/uploads", Domain: "db.demo.doze", Endpoint: "127.0.0.1:5432"}
	if got := connectLine(v); got != "postgres://app@db:5432/app" {
		t.Fatalf("connectLine = %q, want the URL", got)
	}
	// Resource next (AWS built-in / forwarded process).
	v.URL = ""
	if got := connectLine(v); got != "http://s3.demo.doze/uploads" {
		t.Fatalf("connectLine = %q, want the resource", got)
	}
	// Then the DNS name with the endpoint's port — never a raw IP.
	v.Resource = ""
	if got := connectLine(v); got != "db.demo.doze:5432" {
		t.Fatalf("connectLine = %q, want domain:port", got)
	}
	// Raw endpoint only as the last resort (unix sockets, no domains).
	v.Domain = ""
	if got := connectLine(v); got != "127.0.0.1:5432" {
		t.Fatalf("connectLine = %q, want the endpoint", got)
	}
	// The detail card leads with the connect line and shows the raw bind under it.
	m := threeInstances()
	m.resp.Instances[0].Domain = "app-pg.demo.doze"
	m.resp.Instances[0].Endpoint = "127.0.0.11:5432"
	m.resp.Instances[0].Bind = "127.0.0.11:5432"
	m.resp.Instances[0].PID = 7
	out := m.View()
	if !strings.Contains(out, "app-pg.demo.doze:") {
		t.Fatalf("detail card should lead with the domain connect line:\n%s", out)
	}
	if !strings.Contains(out, "127.0.0.11:5432") {
		t.Fatalf("detail card should show the raw bind line:\n%s", out)
	}
}

func TestSidebarGroupsByEngineType(t *testing.T) {
	m := threeInstances()
	m.resp.Instances = append(m.resp.Instances,
		control.InstanceView{Name: "jobs", Engine: "sqs", State: "active"},
		control.InstanceView{Name: "emails", Engine: "sqs", State: "active"},
		control.InstanceView{Name: "worker", Engine: "process", State: "active"},
		control.InstanceView{Name: "billing", Engine: "postgres", State: "active", Group: "team billing"},
	)
	var headers []string
	for _, ln := range m.sidebarLines() {
		if ln.header != "" {
			headers = append(headers, ln.header)
		}
	}
	// Engine-type lanes in architecture order — databases, caches, AWS, custom
	// group= headings, processes last — with member counts where they help.
	want := []string{"postgres", "valkey", "s3", "sqs · 2", "team billing", "processes"}
	if len(headers) != len(want) {
		t.Fatalf("headers = %v, want %v", headers, want)
	}
	for i := range want {
		if headers[i] != want[i] {
			t.Fatalf("headers = %v, want %v", headers, want)
		}
	}
	// An explicit group= heading owns its instance.
	vis := m.visible()
	var order []string
	for _, i := range vis {
		order = append(order, m.resp.Instances[i].Name)
	}
	// billing (custom group) sits after the AWS lanes, before processes.
	joined := strings.Join(order, ",")
	if !strings.Contains(joined, "emails,jobs,billing,worker") {
		t.Fatalf("display order = %v", order)
	}
}
