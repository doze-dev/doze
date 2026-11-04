package view

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/control/fixture"
	"github.com/doze-dev/doze/internal/desk/hub"
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
	st = hub.StackView{Running: true, Conn: hub.ConnLive, LastFrame: epoch.Add(-7 * time.Second)}
	if b := BannerOf(st, epoch); b.OK || b.Text != "last update 7s ago" {
		t.Errorf("stale banner = %+v", b)
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

func TestPaletteMatchesWordStartsAndShowsTheCommand(t *testing.T) {
	st, _, _ := stackOf(t, fixture.Shop(epoch))
	st.Key = "shop-1"
	p := PaletteOf(st, "db", "dark", "sl")
	golden(t, "shop-palette-sl", p)
	if len(p.Rows) == 0 || p.Rows[0].Label != "sleep db" || p.Rows[0].CLI != "doze sleep db" {
		t.Errorf("first row for \"sl\" with db selected = %+v", p.Rows)
	}
	all := PaletteOf(st, "db", "dark", "")
	if all.Rows[0].Label != "sleep db" || all.Rows[len(all.Rows)-1].CLI == "" && all.Rows[len(all.Rows)-1].Do.Kind == "verb" {
		t.Errorf("unfiltered rows start with the selected instance: %v", all.Rows[0].Label)
	}
	if n := PaletteOf(st, "db", "dark", "stripe"); len(n.Rows) != 0 {
		t.Errorf("nothing does that: %v", n.Rows)
	}
}
