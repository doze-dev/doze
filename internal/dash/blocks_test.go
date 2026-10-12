package dash

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/fixture"
)

// consoleModel is the data stack's dash with name opened and its console
// frames delivered, as a daemon would send them.
func consoleModel(t *testing.T, name string) *Model {
	t.Helper()
	f := fixture.Modules(epoch)
	m := New(f, Options{Plain: true, Now: func() time.Time { return epoch }, Loc: time.UTC})
	m.now = epoch
	m.snap = f.Snap
	m.sel = name
	m.page = pageFocus
	m.open()
	for _, fr := range f.Frames {
		if fr.Console != nil && fr.Console.Instance == name && fr.Console.Row == nil {
			m.applyConsole(consoleMsg{inst: name, f: *fr.Console})
		}
	}
	return m
}

// follow lets the dash choose what its stream follows and then delivers the
// rows the daemon would send for it.
func follow(m *Model, name string) {
	m.syncConsole()
	c := m.con
	c.streams = map[string][]control.ConsoleRow{}
	for _, fr := range fixture.Modules(epoch).Frames {
		if fr.Console == nil || fr.Console.Instance != name || fr.Console.Row == nil {
			continue
		}
		if fr.Console.Panel != c.panel {
			continue
		}
		if f := fr.Console.Row.Cells["_for"]; f != "" && f != c.sector {
			continue
		}
		m.applyConsole(consoleMsg{inst: name, f: *fr.Console})
	}
}

// A module that published panels and no layout gets its tables side by side
// and its stream below, which is what the kafka board draws.
func TestPanelsWithNoLayoutGetTheDefaultOne(t *testing.T) {
	m := consoleModel(t, "events")
	m.tab = 0
	follow(m, "events")
	got := frame(m, 136, 42)
	for _, want := range []string{"topics", "consumer groups", "tail", "following orders", "ord_1043", "runtime", "Produce a message", "Keep topic in doze.hcl"} {
		if !strings.Contains(got, want) {
			t.Errorf("kafka console lacks %q:\n%s", want, got)
		}
	}
	// the tables are as tall as their rows: the stream is under them, not beside
	if strings.Index(got, "tail") < strings.Index(got, "orders.dlq") {
		t.Errorf("the stream is not below the tables")
	}
}

// Selecting another row moves what the stream follows.
func TestTheStreamFollowsTheSelectedRow(t *testing.T) {
	m := consoleModel(t, "events")
	m.tab = 0
	m.syncConsole()
	if m.con.panel != "tail" || m.con.sector != "orders" {
		t.Fatalf("following %q of %q, want orders of tail", m.con.sector, m.con.panel)
	}
	m.key("down")
	m.syncConsole()
	if m.con.sector != "emails" {
		t.Errorf("after down it follows %q, want emails", m.con.sector)
	}
}

// A published layout is drawn as published: a stat, a table, a detail that
// follows it, and the actions.
func TestAPublishedLayoutIsDrawnAsPublished(t *testing.T) {
	m := consoleModel(t, "db")
	m.tab = 1 // overview, activity
	got := frame(m, 136, 42)
	for _, want := range []string{"activity", "active", "waiting", "sessions", "4412", "query", "UPDATE orders SET status", "Cancel query", "Terminate session"} {
		if !strings.Contains(got, want) {
			t.Errorf("postgres activity lacks %q:\n%s", want, got)
		}
	}
	// the detail follows the selection
	m.key("down")
	if got := frame(m, 136, 42); !strings.Contains(got, "customer_id = $1") {
		t.Errorf("the detail did not follow the selected session:\n%s", got)
	}
}

// What a module adds to the generic overview lands after the block it names.
func TestAModuleExtendsTheGenericOverview(t *testing.T) {
	m := consoleModel(t, "db")
	m.tab = 0
	got := frame(m, 136, 42)
	vitals, databases, recent := strings.Index(got, "vitals"), strings.Index(got, "databases"), strings.Index(got, "recent")
	if vitals < 0 || databases < 0 || recent < 0 || !(vitals < databases && databases < recent) {
		t.Errorf("databases is not between vitals and recent (%d %d %d):\n%s", vitals, databases, recent, got)
	}
	for _, want := range []string{"shop_test", "38 MB", "postgres"} {
		if !strings.Contains(got, want) {
			t.Errorf("overview lacks %q", want)
		}
	}
}

// A block kind this dash does not know is a card; the rest still draws.
func TestAnUnknownBlockIsACardAndTheRestStillDraws(t *testing.T) {
	m := consoleModel(t, "db")
	spec := *m.con.spec
	spec.Tabs = []control.ConsoleTab{{ID: "activity", Title: "activity", Body: control.ConsoleNode{Kind: "col", Children: []control.ConsoleNode{
		{Kind: "hologram", ID: "x"},
		{Kind: "table", Panel: "sessions", ID: "sessions"},
	}}}}
	m.con.spec = &spec
	m.tab = 1
	got := frame(m, 100, 30)
	if !strings.Contains(got, "a block kind this doze does not know") || !strings.Contains(got, "4412") {
		t.Errorf("unknown block took the console down:\n%s", got)
	}
}

// Nesting is limited, so a module bug cannot make the layout run away.
func TestNestingIsLimited(t *testing.T) {
	m := consoleModel(t, "db")
	deep := control.ConsoleNode{Kind: "note", Text: "bottom"}
	for i := 0; i < 8; i++ {
		deep = control.ConsoleNode{Kind: "col", Children: []control.ConsoleNode{deep}}
	}
	spec := *m.con.spec
	spec.Tabs = []control.ConsoleTab{{ID: "activity", Title: "activity", Body: deep}}
	m.con.spec = &spec
	m.tab = 1
	if got := frame(m, 100, 30); !strings.Contains(got, "nested too deeply") {
		t.Errorf("deep nesting was drawn:\n%s", got)
	}
}

// The dash knows no module by name. A module is drawn from what it published.
func TestTheDashNamesNoModule(t *testing.T) {
	names := regexp.MustCompile(`"(postgres|valkey|kvrocks|mariadb|ferret|temporal|kafka|aws)"`)
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if loc := names.FindIndex(b); loc != nil {
			t.Errorf("%s names a module: %s", f, b[loc[0]:loc[1]])
		}
	}
}

// A chart block is drawn as the generic charts are, with its axes: a module's
// numbers over time, a value scale and a time scale that ends at now.
func TestAChartBlockIsDrawnWithAxes(t *testing.T) {
	m := consoleModel(t, "db")
	spec := *m.con.spec
	spec.Tabs = []control.ConsoleTab{{ID: "activity", Title: "activity", Body: control.ConsoleNode{Kind: "col", Children: []control.ConsoleNode{
		{Kind: "chart", Panel: "databases", ID: "c", Title: "database sizes"},
	}}}}
	m.con.spec = &spec
	m.tab = 1
	got := frame(m, 100, 30)
	if !strings.Contains(got, "database sizes") || !strings.Contains(got, "now") {
		t.Errorf("a chart block should have its title and a time axis ending at now:\n%s", got)
	}
}
