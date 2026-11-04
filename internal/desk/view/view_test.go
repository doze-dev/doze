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
	home := HomeOf(st, epoch)
	golden(t, "shop-home", home)
	if home.Awake != 5 || home.Total != 10 || len(home.Groups) != 4 {
		t.Errorf("awake %d total %d groups %d", home.Awake, home.Total, len(home.Groups))
	}
	if home.Meters.Mem.Shares[0].Name != "web" {
		t.Errorf("the largest share is web; got %+v", home.Meters.Mem.Shares)
	}
	golden(t, "shop-api", InstanceOf(st, "api", epoch))
	golden(t, "shop-db", InstanceOf(st, "db", epoch))
	golden(t, "shop-migrate", InstanceOf(st, "migrate", epoch))
	golden(t, "shop-cloud", InstanceOf(st, "cloud", epoch))
	golden(t, "shop-api-logs", LogsOf(LogsQuery{Instance: "api", N: 5}, h.Logs(key, "api"), epoch))
	golden(t, "shop-all-logs-warn", LogsOf(LogsQuery{Level: 2}, h.Logs(key, ""), epoch))
	if m := InstanceOf(st, "nothing", epoch); !m.Missing {
		t.Error("an unknown instance is missing")
	}
}

func TestCrashGolden(t *testing.T) {
	st, _, _ := stackOf(t, fixture.Crash(epoch))
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
	all := LogsOf(LogsQuery{}, kept, epoch)
	if all.Total != 3 || len(all.Lines) != 3 || len(all.Runs) != 2 {
		t.Errorf("all = %+v", all)
	}
	warn := LogsOf(LogsQuery{Level: 2, From: 1, N: 5}, kept, epoch)
	if warn.Total != 2 || len(warn.Lines) != 1 || warn.Lines[0].Line != "ERROR three" {
		t.Errorf("warn window = %+v", warn)
	}
	run := LogsOf(LogsQuery{Run: 2}, kept, epoch)
	if run.Total != 1 {
		t.Errorf("run 2 = %+v", run)
	}
	search := LogsOf(LogsQuery{Search: "TWO"}, kept, epoch)
	if search.Total != 1 || search.Lines[0].Level != "WRN" {
		t.Errorf("search = %+v", search)
	}
}
