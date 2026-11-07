package view

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fixture"
	"github.com/doze-dev/doze/internal/display"
)

func larderRows() []display.Row {
	f := fixture.Larder(epoch)
	return display.Rows(control.Snapshot{Stack: "larder", Instances: f.Snap.Instances})
}

// TestLarderMap is the MapFlow board's layout, card for card: the calls
// in columns by depth, the event lane, the band, every line clear of
// every card.
func TestLarderMap(t *testing.T) {
	m := mapOf(larderRows(), epoch)
	golden(t, "larder-map", m)

	at := map[string]MapNode{}
	for _, n := range m.Nodes {
		at[n.Name] = n
	}
	// The board's places, spaced as the first map was (72 px between
	// columns where the board has 20, 140 down a column where it has 116),
	// so the lines have room: storefront → gateway →
	// identity, fulfilment, recommendations → orders → catalog.
	want := map[string][4]int{
		"storefront":      {24, 192, 214, 74},
		"gateway":         {310, 192, 214, 74},
		"identity":        {596, 52, 250, 74},
		"fulfilment":      {596, 192, 250, 74},
		"recommendations": {596, 332, 250, 74},
		"orders":          {918, 192, 214, 74},
		"catalog":         {1204, 192, 214, 74},
		"payments":        {24, 444, 214, 74},
		"notifications":   {310, 444, 214, 74},
	}
	for name, w := range want {
		n, ok := at[name]
		if !ok {
			t.Errorf("%s is not on the map", name)
			continue
		}
		if got := [4]int{n.X, n.Y, n.W, n.H}; got != w {
			t.Errorf("%s at %v, the board has %v", name, got, w)
		}
	}
	if len(m.Nodes) != len(want) {
		t.Errorf("%d cards, want %d (tasks that ran fold into their app)", len(m.Nodes), len(want))
	}
	if !at["payments"].Lane || !at["notifications"].Lane || at["gateway"].Lane {
		t.Error("the lane is payments and notifications")
	}
	caps := []string{}
	for _, c := range m.Caps {
		caps = append(caps, c.Text+" · "+c.Sub+" @"+strconv.Itoa(c.Y))
	}
	if want := []string{"Who calls whom · left to right, the way a request travels @16", "Nobody calls these · they work from events @420", "Shared by the stack · one line each would be 17 lines; the chips on each card say who uses what @544"}; !slices.Equal(caps, want) {
		t.Errorf("captions %q", caps)
	}
	if m.Band != 534 {
		t.Errorf("band at %d, want 534 (the board's 486, spaced)", m.Band)
	}

	// The chips are what each uses, in doze.hcl's order; ✓ its tasks.
	if got := at["recommendations"].Chips; !slices.Equal(got, []string{"postgres", "kafka", "redis"}) {
		t.Errorf("recommendations chips %v", got)
	}
	if at["storefront"].Quiet != "no shared services" || at["gateway"].Fold != nil {
		t.Error("storefront uses nothing shared; gateway has no tasks")
	}
	if f := at["orders"].Fold; f == nil || f.Glyph != "✓" || f.Title != "before it: migrate-orders ✓ 16m ago" {
		t.Errorf("orders fold %+v", f)
	}
	if l := at["fulfilment"].Lang + at["storefront"].Lang + at["gateway"].Lang; l != "pytsgo" {
		t.Errorf("languages %q", l)
	}

	// The band: what it is, how many use it.
	used := map[string]string{}
	for _, s := range m.Shared {
		used[s.Name] = s.Used + "/" + s.Only + "/" + s.What
	}
	for name, w := range map[string]string{"postgres": "used by 5//postgres 18", "kafka": "used by 6//kafka 4", "redis": "used by 3//valkey 9", "aws": "used by 2//aws", "mailpit": "used by 1/notifications/brought in by you"} {
		if used[name] != w {
			t.Errorf("%s: %q, want %q", name, used[name], w)
		}
	}

	// The selection's words, as MapFlowSelected says them.
	d := at["orders"].Detail
	if d.Head != "Go · 47 MB · awake" {
		t.Errorf("orders head %q", d.Head)
	}
	rows := map[string]string{}
	for _, r := range d.Rows {
		rows[r.K] = r.V + "|" + r.Dim
	}
	if rows["needs"] != "catalog · and postgres, kafka below|" || rows["needed by"] != "gateway, fulfilment · they keep it awake|" || rows["before it"] != "migrate-orders ✓|16m ago" {
		t.Errorf("orders detail %q", rows)
	}
	if m.Detail != (MapAt{970, 14}) {
		t.Errorf("detail at %+v, want 970,14 (the board's 762, moved with the wider gaps)", m.Detail)
	}

	// Every call is drawn, and no line passes behind a card.
	edges := map[string]bool{}
	for _, e := range m.Edges {
		edges[e.From+">"+e.To] = true
	}
	for _, e := range []string{"storefront>gateway", "gateway>identity", "gateway>catalog", "gateway>orders", "gateway>fulfilment", "gateway>recommendations", "fulfilment>orders", "orders>catalog", "recommendations>catalog"} {
		if !edges[e] {
			t.Errorf("no edge %s", e)
		}
	}
	if len(m.Edges) != 9 || len(m.Lines) != 17 {
		t.Errorf("%d edges, %d lines", len(m.Edges), len(m.Lines))
	}
	clearOfCards(t, m)
}

// clearOfCards samples every path and fails when a point is inside a card
// that is not one of its ends.
func clearOfCards(t *testing.T, m MapView) {
	t.Helper()
	cards := map[string]box{}
	for _, n := range m.Nodes {
		cards[n.Name] = box{n.X, n.Y, n.W, n.H}
	}
	for _, s := range m.Shared {
		cards[s.Name] = box{s.X, s.Y, s.W, s.H}
	}
	for _, e := range append(append([]MapEdge{}, m.Edges...), m.Lines...) {
		for _, p := range samplePath(t, e.D) {
			for name, c := range cards {
				if name != e.From && name != e.To && c.inside(p.x, p.y) {
					t.Errorf("%s>%s passes behind %s at %.0f,%.0f: %s", e.From, e.To, name, p.x, p.y, e.D)
					return
				}
			}
		}
	}
}

// samplePath reads the M, L, C and Q the map writes and samples along it.
func samplePath(t *testing.T, d string) []pt {
	t.Helper()
	f := strings.Fields(strings.ReplaceAll(d, ",", " "))
	var out []pt
	var cur pt
	num := func(i int) float64 {
		s := f[i]
		for len(s) > 0 && (s[0] < '0' || s[0] > '9') && s[0] != '-' {
			s = s[1:]
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Fatalf("path %q: %v", d, err)
		}
		return v
	}
	for i := 0; i < len(f); {
		cmd := f[i][:1]
		if f[i] == cmd {
			i++
		} else {
			f[i] = f[i][1:]
		}
		switch cmd {
		case "M":
			cur = pt{num(i), num(i + 1)}
			i += 2
		case "L":
			p := pt{num(i), num(i + 1)}
			out = append(out, line(cur, p)...)
			cur, i = p, i+2
		case "C":
			c1, c2, p := pt{num(i), num(i + 1)}, pt{num(i + 2), num(i + 3)}, pt{num(i + 4), num(i + 5)}
			out = append(out, cubicSamples(cur, c1, c2, p)...)
			cur, i = p, i+6
		case "Q":
			c, p := pt{num(i), num(i + 1)}, pt{num(i + 2), num(i + 3)}
			out = append(out, cubicSamples(cur, pt{cur.x + 2*(c.x-cur.x)/3, cur.y + 2*(c.y-cur.y)/3}, pt{p.x + 2*(c.x-p.x)/3, p.y + 2*(c.y-p.y)/3}, p)...)
			cur, i = p, i+4
		default:
			t.Fatalf("path %q: %q", d, cmd)
		}
	}
	return out
}

// TestMapShapes are stacks the board does not draw: no calls at all (the
// shop), a cycle, a failed task, a band wider than a row.
func TestMapShapes(t *testing.T) {
	shop := display.Rows(control.Snapshot{Instances: fixture.Shop(epoch).Snap.Instances})
	m := mapOf(shop, epoch)
	if len(m.Caps) == 0 || m.Caps[0].Text != "No callers" {
		t.Errorf("shop: %+v", m.Caps)
	}
	for _, n := range m.Nodes {
		if !n.Lane {
			t.Errorf("shop: %s is not in the lane; nothing in the shop calls anything", n.Name)
		}
	}
	if len(m.Shared) != 7 || len(m.Edges) != 0 {
		t.Errorf("shop: %d shared, %d edges", len(m.Shared), len(m.Edges))
	}
	clearOfCards(t, m)

	app := func(name string, needs ...string) control.InstanceView {
		return control.InstanceView{Name: name, Kind: "app", State: "active", Needs: needs, Run: "go run ."}
	}
	in := []control.InstanceView{
		app("a", "b", "db", "q", "s1", "s2", "s3", "s4", "s5", "s6"), app("b", "c", "db"), app("c", "a", "db"), app("d", "c", "mig"),
		{Name: "mig", Kind: "task", State: "reaped", Task: &control.TaskView{Result: "failed"}},
		{Name: "db", Kind: "module", Engine: "postgres"}, {Name: "q", Kind: "module", Engine: "kafka"},
	}
	for i := 1; i <= 6; i++ {
		in = append(in, control.InstanceView{Name: "s" + strconv.Itoa(i), Kind: "service"})
	}
	m = mapOf(display.Rows(control.Snapshot{Instances: in}), epoch)
	at := map[string]MapNode{}
	for _, n := range m.Nodes {
		at[n.Name] = n
	}
	if !at["mig"].Task || at["mig"].X <= at["d"].X {
		t.Errorf("the failed task is a card after d: %+v", at["mig"])
	}
	if len(m.Edges) != 5 {
		t.Errorf("%d edges, want a>b b>c c>a d>c d>mig", len(m.Edges))
	}
	rows := map[int]int{}
	for _, s := range m.Shared {
		rows[s.Y]++
	}
	if len(rows) != 2 {
		t.Errorf("eight shared on %d rows", len(rows))
	}
	clearOfCards(t, m)
}

// TestLarderGrid is the MapGrid board: rows in the map's order, the dots
// where doze.hcl refers, the tooltips quoting it.
func TestLarderGrid(t *testing.T) {
	g := gridOf(larderRows(), epoch)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doze.hcl"), []byte(fixture.LarderHCL), 0o644); err != nil {
		t.Fatal(err)
	}
	g.Refer(RefsIn(dir))
	golden(t, "larder-grid", g)

	var rows, cols []string
	for _, r := range g.Rows {
		rows = append(rows, r.Name)
	}
	for _, c := range g.Cols {
		cols = append(cols, c.Name)
	}
	if want := []string{"storefront", "gateway", "identity", "fulfilment", "recommendations", "orders", "catalog", "payments", "notifications"}; !slices.Equal(rows, want) {
		t.Errorf("rows %v", rows)
	}
	if want := []string{"gateway", "identity", "fulfilment", "recommendations", "orders", "catalog", "kafka", "postgres", "redis", "aws", "mailpit", "tasks"}; !slices.Equal(cols, want) {
		t.Errorf("cols %v", cols)
	}
	if !slices.Equal(g.Foot, []string{"1", "1", "1", "1", "2", "3", "6", "5", "3", "2", "1", ""}) {
		t.Errorf("needed by %v", g.Foot)
	}
	dots := 0
	for _, r := range g.Rows {
		for _, c := range r.Cells {
			if c.Dot {
				dots++
			}
		}
	}
	if dots != 9+17 {
		t.Errorf("%d dots, want 9 calls and 17 uses", dots)
	}
	cell := func(row, col string) GridCell {
		for _, r := range g.Rows {
			if r.Name == row {
				return r.Cells[slices.Index(cols, col)]
			}
		}
		t.Fatalf("no row %s", row)
		return GridCell{}
	}
	tip := cell("orders", "catalog").Tip
	if tip == nil || tip.Head != "orders needs catalog" || tip.Why != "keeps it awake" || tip.Ref != "CATALOG_URL = app.catalog.url" || tip.Keys != "⏎ opens catalog · o opens orders" {
		t.Errorf("orders × catalog: %+v", tip)
	}
	if tip := cell("orders", "postgres").Tip; tip == nil || tip.Ref != `DATABASE_URL = "…${module.postgres.address}…"` || tip.Head != "orders uses postgres" {
		t.Errorf("orders × postgres: %+v", tip)
	}
	if tip := cell("notifications", "mailpit").Tip; tip == nil || tip.Ref != "SMTP_ADDR = service.mailpit.address" {
		t.Errorf("notifications × mailpit: %+v", tip)
	}
	if c := cell("recommendations", "tasks"); c.Text != "2 tasks ✓" || c.Tip.Ref != "after = [task.migrate-recommendations, task.install-recommendations]" {
		t.Errorf("recommendations' tasks: %+v %+v", c, c.Tip)
	}
	if c := cell("orders", "orders"); !c.Self {
		t.Error("a service against itself is a dash")
	}
	if c := cell("storefront", "aws"); c.Dot || c.Tip != nil {
		t.Error("storefront uses no aws")
	}
}

func TestLang(t *testing.T) {
	for run, want := range map[string]string{
		"go run ./services/orders":                 "go",
		"uv run --frozen python -m fulfilment":     "py",
		"python3 app.py":                           "py",
		"poetry run serve":                         "py",
		"bun src/index.ts":                         "ts",
		"bun run dev":                              "ts",
		"pnpm dev":                                 "js",
		"npm run dev -- --config vite.config.ts":   "ts",
		"node server.js":                           "js",
		"deno run -A main.ts":                      "ts",
		"PORT=1 go run .":                          "go",
		"./bin/svc --port $PORT":                   "",
		"docker run --rm -p 7700:7700 meilisearch": "",
		"": "",
	} {
		if got := lang(run); got != want {
			t.Errorf("lang(%q) = %q, want %q", run, got, want)
		}
	}
}

func TestShorten(t *testing.T) {
	for in, want := range map[string]string{
		"CATALOG_URL = app.catalog.url": "CATALOG_URL = app.catalog.url",
		`DATABASE_URL = "postgres://recommendations:recommendations@${module.postgres.address}/postgres?sslmode=disable"`: `DATABASE_URL = "…${module.postgres.address}…"`,
	} {
		ref := "app.catalog"
		if strings.Contains(in, "module.postgres") {
			ref = "module.postgres"
		}
		if got := shorten(in, ref); got != want {
			t.Errorf("shorten(%q) = %q, want %q", in, got, want)
		}
	}
}
