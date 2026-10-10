package dash

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// The clock every screen is drawn against.
var epoch = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

// shop is the fixture stack as the daemon would report it.
func shop() *fakedaemon.Fake {
	mb := int64(1 << 20)
	return &fakedaemon.Fake{
		Snap: control.Snapshot{
			IdleTimeout: 5 * time.Minute,
			Instances: []control.InstanceView{
				{Name: "api", Kind: "app", Engine: "process", State: "active", RAM: 42 * mb, CPU: 3,
					Endpoint: "127.0.0.11:8080", Domain: "api.shop.doze",
					Holders: []control.HolderView{{Kind: "connection", Count: 2}}},
				{Name: "worker", Kind: "app", Engine: "process", State: "active", RAM: 31 * mb, CPU: 1},
				{Name: "web", Kind: "app", Engine: "process", State: "idle", RAM: 18 * mb,
					Endpoint: "127.0.0.12:8090", Domain: "web.shop.doze",
					IdleSince: epoch.Add(-1*time.Minute - 48*time.Second)},
				{Name: "db", Kind: "module", Engine: "postgres", Version: "16.4", State: "active", RAM: 96 * mb, CPU: 2, Conns: 5,
					Endpoint: "127.0.0.13:5432", Domain: "db.shop.doze",
					Holders: []control.HolderView{{Kind: "dependant", Name: "api"}, {Kind: "dependant", Name: "worker"}, {Kind: "connection", Count: 3}}},
				{Name: "cache", Kind: "module", Engine: "valkey", Version: "9.0", State: "reaped", Endpoint: "127.0.0.14:6379", Domain: "cache.shop.doze"},
				{Name: "events", Kind: "module", Engine: "kafka", Version: "4.0", State: "reaped", Endpoint: "127.0.0.15:9092", Domain: "events.shop.doze"},
				{Name: "cloud", Kind: "module", Engine: "aws", State: "active", RAM: 64 * mb, Endpoint: "127.0.0.16:4566",
					Holders: []control.HolderView{{Kind: "dependant", Name: "worker"}}},
				{Name: "mailpit", Kind: "service", Engine: "process", State: "reaped", Endpoint: "127.0.0.17:1025"},
				{Name: "migrate", Kind: "task", Engine: "process", State: "reaped",
					Task: &control.TaskView{Result: "done", TookMS: 412, RanAt: epoch.Add(-12 * time.Minute), Why: "inputs changed"}},
				{Name: "seed", Kind: "task", Engine: "process", State: "reaped"},
			},
		},
		Lines: map[string][]control.LogFrame{
			"api": {
				{Instance: "api", Line: "listening on :8080", Time: epoch.Add(-9 * time.Minute)},
				{Instance: "api", Line: "GET /healthz 200 1ms", Time: epoch.Add(-2 * time.Minute)},
				{Instance: "api", Line: "GET /orders 200 14ms", Time: epoch.Add(-40 * time.Second)},
			},
		},
	}
}

// screen builds the dash against d, sized w×h, and returns what it draws with
// the colour stripped, one trailing space trimmed per line.
func screen(t *testing.T, d control.Daemon, w, h int, keys ...string) string {
	t.Helper()
	m := New(d, Options{Dark: true, Plain: true, Now: func() time.Time { return epoch }, Loc: time.UTC})
	step := func(msg tea.Msg) {
		next, cmd := m.Update(msg)
		m = next.(*Model)
		// Commands that only ask the daemon something are run inline; the
		// ones that wait on a stream are not, because a test has no stream.
		if cmd != nil {
			if out := cmd(); out != nil {
				if _, ok := out.(tea.BatchMsg); !ok {
					switch out.(type) {
					case frameMsg, tickMsg:
					default:
						next, _ := m.Update(out)
						m = next.(*Model)
					}
				}
			}
		}
	}
	step(tea.WindowSizeMsg{Width: w, Height: h})
	snap, _ := d.Snapshot(nil) //nolint:staticcheck // the fake ignores it
	step(snapshotMsg{snap: snap})
	for _, k := range keys {
		step(tea.KeyPressMsg(tea.Key{Text: k, Code: []rune(k)[0]}))
	}
	var lines []string
	for _, l := range strings.Split(ansi.Strip(m.Render(w, h)), "\n") {
		lines = append(lines, strings.TrimRight(l, " "))
	}
	return strings.Join(lines, "\n") + "\n"
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from its golden file.\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func TestHome(t *testing.T) {
	golden(t, "home_120x36", screen(t, shop(), 120, 36))
}

func TestHomeSelectionMovesAndShowsThePreview(t *testing.T) {
	// Down to the database: its preview says why it is awake.
	got := screen(t, shop(), 120, 36, "j", "j", "j", "j")
	golden(t, "home_120x36_db", got)
}

func TestKeysRunVerbsOnTheSelectedInstance(t *testing.T) {
	d := shop()
	// api is first; five steps down is db (api, web, worker, cache, cloud, db).
	screen(t, d, 120, 36, "j", "j", "j", "j", "j", "w")
	screen(t, d, 120, 36, "j", "s")
	got := d.Done()
	want := []control.Verb{{Name: control.VerbWake, Target: "db"}, {Name: control.VerbSleep, Target: "web"}}
	if len(got) != len(want) {
		t.Fatalf("verbs run = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("verb %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestAClickSelectsARow(t *testing.T) {
	d := shop()
	m := New(d, Options{Dark: true, Plain: true, Now: func() time.Time { return epoch }, Loc: time.UTC})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	snap, _ := d.Snapshot(nil) //nolint:staticcheck
	m.Update(snapshotMsg{snap: snap})
	m.Render(120, 36) // lays the rows out
	// The db row: a two-line header, then Apps (heading, three rows), a blank,
	// then Modules (heading, cache, cloud, db).
	_, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: 10, Y: 10, Button: tea.MouseLeft}))
	if out := ansi.Strip(m.Render(120, 36)); !strings.Contains(out, "db  postgres") {
		t.Errorf("clicking the db row did not select it:\n%s", out)
	}
}

// Keystroke to frame on a stack of thirty, which is more than the dash has
// ever been asked to hold. The figure is recorded in phases.md.
func BenchmarkHomeKeystrokeToFrame30(b *testing.B) {
	d := &fakedaemon.Fake{}
	for i := 0; i < 30; i++ {
		kind, engine := "app", "process"
		if i%3 == 0 {
			kind, engine = "module", "postgres"
		}
		d.Snap.Instances = append(d.Snap.Instances, control.InstanceView{
			Name: "svc" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Kind: kind, Engine: engine,
			State: "active", RAM: int64(i+1) << 20, Endpoint: "127.0.0.1:8080",
		})
	}
	m := New(d, Options{Dark: true, Now: func() time.Time { return epoch }})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m.Update(snapshotMsg{snap: d.Snap})
	key := tea.KeyPressMsg(tea.Key{Text: "j", Code: 'j'})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Update(key)
		_ = m.Render(120, 36)
	}
}
