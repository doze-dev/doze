package view

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/control/fixture"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/prefs"
)

var update = flag.Bool("update", false, "rewrite the goldens")

// epoch is when every golden is computed, the moment tools/look draws its
// scenes at, so "up 14m" reads the same each run.
var epoch = time.Date(2026, 10, 11, 14, 4, 40, 0, time.UTC)

// stackOf runs a fixture through a real hub, watching stats and logs, and
// returns the stack as the views see it.
func stackOf(t *testing.T, f *fakedaemon.Fake) (hub.StackView, *hub.Hub, string) {
	t.Helper()
	home := t.TempDir()
	run := filepath.Join(home, "projects", "p-1", "run")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(run, "doze.admin.sock")
	if err := os.WriteFile(sock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h := hub.New(hub.Options{Home: home, Dial: func(string) control.Daemon { return f }, Every: 20 * time.Millisecond, Now: func() time.Time { return epoch }})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run(ctx)
	deadline := time.Now().Add(3 * time.Second)
	var key string
	for time.Now().Before(deadline) {
		if st := h.Stacks(); len(st) == 1 && st[0].Conn == hub.ConnLive {
			key = st[0].Key
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if key == "" {
		t.Fatal("the hub never saw the fixture")
	}
	t.Cleanup(h.Watch(key, control.TopicStats))
	t.Cleanup(h.Watch(key, control.TopicLogs))
	for time.Now().Before(deadline) {
		st := h.Stacks()[0]
		if len(st.Series["api"].RAM) == 240 && len(h.Logs(key, "api")) >= 10 {
			return st, h, key
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the hub never got the fixture's meters and logs")
	return hub.StackView{}, nil, ""
}

func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", name+".json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run with -update)", path, err)
	}
	if string(want) != string(got) {
		t.Errorf("%s differs from the golden (run with -update to accept):\n%s", name, got)
	}
}

func TestShopGoldens(t *testing.T) {
	st, h, key := stackOf(t, fixture.Shop(epoch))
	st.Key = "shop-1" // the temp home's path is not for a golden
	home := HomeOf(st, epoch)
	golden(t, "shop-home", home)
	if home.Awake != 5 || home.Total != 10 || len(home.Groups) != 4 {
		t.Errorf("awake %d total %d groups %d", home.Awake, home.Total, len(home.Groups))
	}
	if home.Vitals.Mem.Shares[0].Name != "web" {
		t.Errorf("the largest share is web; got %+v", home.Vitals.Mem.Shares)
	}
	golden(t, "shop-api", InstanceOf(st, "api", epoch))
	golden(t, "shop-db", InstanceOf(st, "db", epoch))
	golden(t, "shop-migrate", InstanceOf(st, "migrate", epoch))
	golden(t, "shop-cloud", InstanceOf(st, "cloud", epoch))
	golden(t, "shop-api-logs", LogsOf(LogsQuery{Instance: "api", N: 5}, h.Logs(key, "api"), nil, epoch))
	golden(t, "shop-all-logs-warn", LogsOf(LogsQuery{Level: 2}, h.Logs(key, ""), st.Events, epoch))
	if m := InstanceOf(st, "nothing", epoch); !m.Missing {
		t.Error("an unknown instance is missing")
	}
}

func TestCrashGolden(t *testing.T) {
	st, _, _ := stackOf(t, fixture.Crash(epoch))
	st.Key = "crash-1"
	golden(t, "crash-home", HomeOf(st, epoch))
	golden(t, "crash-api", InstanceOf(st, "api", epoch))
}

func TestBannerWords(t *testing.T) {
	st := hub.StackView{Running: true, Conn: hub.ConnDown, RetryAt: epoch.Add(3 * time.Second)}
	if b := BannerOf(st, epoch); b.OK || b.Text != "daemon not responding · retrying in 3s" {
		t.Errorf("down banner = %+v", b)
	}
	st = hub.StackView{Running: true, Conn: hub.ConnLive, Metered: true, LastFrame: epoch.Add(-7 * time.Second)}
	if b := BannerOf(st, epoch); b.OK || b.Text != "last update 7s ago" {
		t.Errorf("stale banner = %+v", b)
	}
	// A quiet stack nobody meters is not stale: nothing was due.
	st.Metered = false
	if b := BannerOf(st, epoch); !b.OK {
		t.Errorf("unmetered quiet stack = %+v", b)
	}
	if b := BannerOf(hub.StackView{Running: true, Conn: hub.ConnConnecting}, epoch); b.Text != "connecting to the daemon…" {
		t.Errorf("connecting banner = %+v", b)
	}
}

func TestLogsWindowAndFilters(t *testing.T) {
	kept := []control.LogFrame{
		{Line: "INFO one", Time: epoch.Add(-3 * time.Second), Run: 1},
		{Line: "WARN two", Time: epoch.Add(-2 * time.Second), Run: 1},
		{Line: "ERROR three", Time: epoch.Add(-1 * time.Second), Run: 2},
	}
	all := LogsOf(LogsQuery{}, kept, nil, epoch)
	if all.Total != 3 || len(all.Lines) != 3 || len(all.Runs) != 2 {
		t.Errorf("all = %+v", all)
	}
	warn := LogsOf(LogsQuery{Level: 2, From: 1, N: 5}, kept, nil, epoch)
	if warn.Total != 2 || len(warn.Lines) != 1 || warn.Lines[0].Line != "three" {
		t.Errorf("warn window = %+v", warn)
	}
	run := LogsOf(LogsQuery{Run: 2}, kept, nil, epoch)
	if run.Total != 1 {
		t.Errorf("run 2 = %+v", run)
	}
	search := LogsOf(LogsQuery{Search: "TWO"}, kept, nil, epoch)
	if search.Total != 1 || search.Lines[0].Level != "WRN" {
		t.Errorf("search = %+v", search)
	}
}

func TestDialogsSayWhatTheDashSaid(t *testing.T) {
	st, _, _ := stackOf(t, fixture.Shop(epoch))
	st.Key = "shop-1"
	sleep := DialogOf(st, "db", "sleep", epoch)
	golden(t, "shop-db-sleep", sleep)
	if !sleep.Needed {
		t.Fatal("db is in use; sleeping it asks")
	}
	labels := func(d Dialog) []string {
		var out []string
		for _, b := range d.Buttons {
			out = append(out, b.Label)
		}
		return out
	}
	if got := labels(sleep); len(got) != 3 || got[0] != "Sleep db and api" || got[1] != "Sleep db only" || got[2] != "Cancel" {
		t.Errorf("sleep buttons = %v", got)
	}
	if sleep.Buttons[0].Targets[0] != "api" || sleep.Buttons[0].Targets[1] != "db" {
		t.Errorf("sleep with dependants sleeps api first, then db: %v", sleep.Buttons[0].Targets)
	}
	if DialogOf(st, "web", "sleep", epoch).Needed {
		t.Error("nothing uses web; sleeping it never asks")
	}
	restart := DialogOf(st, "db", "restart", epoch)
	golden(t, "shop-db-restart", restart)
	if got := labels(restart); len(got) != 2 || got[0] != "Restart db" {
		t.Errorf("restart buttons = %v", got)
	}
	reset := DialogOf(st, "db", "reset", epoch)
	golden(t, "shop-db-reset", reset)
	if !reset.Danger || reset.Default != 1 || reset.TypeName != "db" || reset.Command != "doze reset db --yes" {
		t.Errorf("reset = %+v", reset)
	}
	if len(reset.Facts) != 4 || reset.Facts[0].K != "deletes" || reset.Facts[3].Spans[0].Text != "none." || !reset.Facts[3].Spans[0].Strong || !reset.Facts[0].Spans[1].Mono {
		t.Errorf("reset facts = %+v", reset.Facts)
	}
}

func TestPaletteListsWhatIsAtHandThenAVerbThenNothing(t *testing.T) {
	st, _, _ := stackOf(t, fixture.Shop(epoch))
	st.Key = "shop-1"
	stacks := []hub.StackView{st, {Key: "acme", Name: "acme", Dir: "/w/acme", LastSet: []string{"api", "web"}}}
	recent := []prefs.Recent{{Key: "shop-1", Name: "db"}, {Key: "shop-1", Name: "migrate"}}

	// Nothing typed: Recent from prefs, Do with the running stack's sleep
	// and the stopped stack's up.
	p := PaletteOf(stacks, "shop-1", recent, "", epoch)
	golden(t, "shop-palette-empty", p)
	titles := func(p Palette) []string {
		var out []string
		for _, s := range p.Sections {
			out = append(out, s.Title)
		}
		return out
	}
	if got := titles(p); strings.Join(got, ",") != "Recent,Do" {
		t.Errorf("sections = %v", got)
	}
	if r := p.Sections[0].Rows; r[0].Label != "db" || !strings.HasPrefix(r[0].Note, "in shop · copy URL") || r[1].Label != "Run migrate" {
		t.Errorf("recent rows = %+v", r)
	}
	if r := p.Sections[1].Rows; r[0].Label != "Sleep everything in shop" || r[1].Label != "Up acme" || r[1].Note != "stopped · as last time: api, web" || r[2].Do.Kind != "folder" {
		t.Errorf("do rows = %+v", r)
	}

	// A verb and a name: the services it applies to, best match first, with
	// whether it asks, then Did you mean.
	p = PaletteOf(stacks, "shop-1", nil, "sleep ca", epoch)
	golden(t, "shop-palette-sleep-ca", p)
	if got := titles(p); strings.Join(got, ",") != "Sleep,Did you mean" {
		t.Errorf("sections = %v", got)
	}
	if r := p.Sections[0].Rows[0]; r.Label != "cache" || r.Do.Verb != "sleep" || r.CLI != "doze sleep cache" || r.Key != "⏎ sleep now" {
		t.Errorf("first sleep row = %+v", r)
	}

	// A name alone opens it; a stack's name too.
	p = PaletteOf(stacks, "shop-1", nil, "sho", epoch)
	if len(p.Sections) != 1 || p.Sections[0].Rows[0].Label != "shop" || p.Sections[0].Rows[0].Do.Page != "home" {
		t.Errorf("by name = %+v", p.Sections)
	}

	// Nothing called that: the closest name, with the verb carried over.
	p = PaletteOf(stacks, "shop-1", nil, "restart cachee", epoch)
	if p.Nothing == nil || p.Nothing.Closest != "cache" || p.Nothing.Where != "shop or acme" || p.Nothing.Do == nil || p.Nothing.Do.Verb != "restart" {
		t.Errorf("nothing = %+v", p.Nothing)
	}
}

// consoleStack is the modules fixture with db's and events' consoles
// watched, so the stack view carries their specs and rows.
func consoleStack(t *testing.T) (hub.StackView, *hub.Hub, string) {
	t.Helper()
	home := t.TempDir()
	run := filepath.Join(home, "projects", "p-1", "run")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "doze.admin.sock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f := fixture.Modules(epoch)
	h := hub.New(hub.Options{Home: home, Dial: func(string) control.Daemon { return f }, Every: 20 * time.Millisecond, Now: func() time.Time { return epoch }})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run(ctx)
	key := ""
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline) && key == ""; {
		if st := h.Stacks(); len(st) == 1 && st[0].Conn == hub.ConnLive {
			key = st[0].Key
		}
		time.Sleep(10 * time.Millisecond)
	}
	if key == "" {
		t.Fatal("the hub never saw the modules fixture")
	}
	for _, inst := range []string{"db", "events"} {
		t.Cleanup(h.WatchConsole(key, inst))
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := h.Stacks()[0]
		if c, ok := st.Consoles["events"]; ok && c.Spec != nil && len(st.Consoles["db"].Tables["sessions"]) == 4 && len(c.Tables["topics"]) == 3 {
			st.Key = "data-1"
			return st, h, key
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the consoles never arrived")
	return hub.StackView{}, nil, ""
}

func TestConsoleTabDrawsThePgActivityBoard(t *testing.T) {
	st, _, _ := consoleStack(t)
	inst := InstanceOf(st, "db", epoch)
	if len(inst.Tabs) != 3 || inst.Tabs[0].ID != "overview" || inst.Tabs[1].ID != "console:activity" || inst.Tabs[1].Title != "activity" || inst.Tabs[2].ID != "logs" {
		t.Errorf("db's tabs = %+v", inst.Tabs)
	}
	if len(inst.Extend) != 1 || inst.Extend[0].After != "vitals" || inst.Extend[0].Node.Kind != "table" || len(inst.Extend[0].Node.Rows) != 3 {
		t.Errorf("db's overview extension = %+v", inst.Extend)
	}
	c := ConsoleOf(st, "db", "console:activity", map[string]int{})
	golden(t, "modules-db-activity", c)
	if !c.Awake || c.Missing || c.Body.Kind != "col" || len(c.Body.Children) != 2 {
		t.Fatalf("activity = %+v", c)
	}
	stat := c.Body.Children[0]
	if stat.Kind != "stat" || len(stat.Stats) != 5 || stat.Stats[0].Label != "active" || stat.Stats[0].Value != "2" || stat.Stats[4].Value != "4.2 s" {
		t.Errorf("counters = %+v", stat.Stats)
	}
	row := c.Body.Children[1]
	table, side := row.Children[0], row.Children[1]
	if table.Kind != "table" || table.ID != "sessions" || len(table.Cols) != 6 || len(table.Rows) != 4 || table.Selected != 0 {
		t.Errorf("sessions = %+v", table)
	}
	if table.Rows[0].Cells[5].Text != "◐ waiting" || table.Rows[0].Cells[5].Tone != "warn" || table.Rows[3].Cells[5].Text != "· idle" {
		t.Errorf("state cells = %+v %+v", table.Rows[0].Cells[5], table.Rows[3].Cells[5])
	}
	detail, actions := side.Children[0], side.Children[1]
	if detail.Title != "query" || len(detail.Detail) != 7 || detail.Detail[6][1] != "UPDATE orders SET status = $1 WHERE id = $2" {
		t.Errorf("detail = %+v", detail.Detail)
	}
	if len(actions.Actions) != 2 || actions.Actions[0].Key != "C" || actions.Actions[0].Resource != "4412" || !actions.Actions[1].Destructive || actions.Actions[1].Key != "T" {
		t.Errorf("actions = %+v", actions.Actions)
	}
	if len(c.Keys) != 4 || c.Keys[1][1] != "cancel query" {
		t.Errorf("keys = %v", c.Keys)
	}
	// The second row selected: the detail and the actions follow it.
	c2 := ConsoleOf(st, "db", "console:activity", map[string]int{"sessions": 1})
	if d := c2.Body.Children[1].Children[1].Children[0].Detail; d[6][1] != "SELECT * FROM orders WHERE customer_id = $1" {
		t.Errorf("detail of row 2 = %v", d[6])
	}
	if a := c2.Body.Children[1].Children[1].Children[1].Actions; a[0].Resource != "4410" {
		t.Errorf("actions of row 2 = %+v", a)
	}
	if ConsoleOf(st, "db", "console:nope", nil).Missing != true {
		t.Error("a tab the spec lacks is missing")
	}
}

func TestKafkaConsoleLaysOutTablesThenTheStream(t *testing.T) {
	st, _, _ := consoleStack(t)
	c := ConsoleOf(st, "events", "console:_", map[string]int{})
	golden(t, "modules-events-console", c)
	kids := c.Body.Children
	if len(kids) != 3 || kids[0].Kind != "row" || len(kids[0].Children) != 2 || kids[1].Kind != "stream" || kids[2].Kind != "actions" {
		t.Fatalf("kafka body = %+v", kids)
	}
	topics := kids[0].Children[0]
	if len(topics.Cols) != 5 || topics.Cols[4].Key != "from" || topics.Rows[2].Cells[4].Text != "runtime" || topics.Rows[0].Cells[2].Text != "1,043" || topics.Rows[0].Cells[3].Text != "—" {
		t.Errorf("topics = %+v", topics)
	}
	groups := kids[0].Children[1]
	if groups.Rows[1].Cells[2].Text != "12" || groups.Rows[1].Cells[2].Tone != "bad" || groups.Rows[0].Cells[2].Tone != "good" {
		t.Errorf("lag cells = %+v", groups.Rows)
	}
	panel, sector := Follow(st, "events", "console:_", map[string]int{})
	if panel != "tail" || sector != "orders" {
		t.Errorf("follow = %q %q", panel, sector)
	}
	if _, sector := Follow(st, "events", "console:_", map[string]int{"table:topics": 1}); sector != "emails" {
		t.Errorf("follow of row 2 = %q", sector)
	}
	produce := kids[2].Actions[0]
	d := ActionDialog(st, "events", produce)
	golden(t, "modules-events-produce", d)
	if !d.Needed || !d.Input || d.Hint != "message value" || d.Buttons[0].Label != "Do it" || d.Buttons[0].Verb != "produce" || d.Buttons[0].Targets[0] != "orders" || !d.Action || d.Instance != "events" {
		t.Errorf("produce dialog = %+v", d)
	}
	keep := kids[2].Actions[1]
	if ActionDialog(st, "events", keep).Needed {
		t.Error("keep takes no input and deletes nothing: it just runs")
	}
}
