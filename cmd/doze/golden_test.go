package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/journal"
)

// What the commands print is checked against files in testdata, produced from
// a scripted daemon: the same bytes on every run. After a deliberate change,
// regenerate them with
//
//	go test ./cmd/doze -run Golden -update
//
// and read the diff.
var update = flag.Bool("update", false, "rewrite the golden files")

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
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
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from %s\n--- got ---\n%s\n--- want ---\n%s", name, path, got, want)
	}
}

// at is the instant every scripted timestamp counts from, in a zone a golden
// file can be read in anywhere.
var at = time.Date(2026, 10, 10, 9, 15, 0, 0, time.UTC)

func fixClock(t *testing.T) {
	t.Helper()
	oldNow, oldLocal := now, time.Local
	now, time.Local = func() time.Time { return at }, time.UTC
	t.Cleanup(func() { now, time.Local = oldNow, oldLocal })
}

// shopConfig is a stack shaped like examples/shop, parsed without its modules.
func shopConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.ParseShape([]byte(`
stack = "shop"

module postgres "db" {
  version = 16
  port    = 5432
}

module valkey "cache" {
  version = 9
  port    = 6379
}

module kafka "events" {
  version = 4
  port    = 9092
}

app "api" {
  run  = "go run ./cmd/api"
  port = 8080
  env  = { DATABASE_URL = module.db.url }
}

app "worker" {
  run = "go run ./cmd/worker"
  env = { KAFKA = module.events.address }
}

app "web" {
  run  = "go run ./cmd/web"
  port = 8090
}
`), "doze.hcl")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// shopSnapshot is that stack on an ordinary bad day: most things up, one thing
// counting down to sleep, one crash-looping, one that gave up.
func shopSnapshot() control.Snapshot {
	healthy := true
	return control.Snapshot{
		Listen:      "127.0.0.1:6432",
		IdleTimeout: 5 * time.Minute,
		Instances: []control.InstanceView{
			{Name: "db", Engine: "postgres", Version: "16", State: "active", PID: 4101, Conns: 3, Declared: true,
				Endpoint: "127.0.0.1:5432", RAM: 11 << 20, CPU: 0.4,
				Holders: []control.HolderView{{Kind: "dependant", Name: "api"}, {Kind: "connection", Count: 2}}},
			{Name: "cache", Engine: "valkey", Version: "9", State: "idle", PID: 4102, Declared: true,
				Endpoint: "127.0.0.1:6379", RAM: 5 << 20, IdleSince: at.Add(-48 * time.Second)},
			{Name: "events", Engine: "kafka", Version: "4", State: "active", PID: 4103, Conns: 1, Declared: true,
				Endpoint: "127.0.0.1:9092", RAM: 22 << 20, CPU: 1.2, KeepAwake: true,
				Holders: []control.HolderView{{Kind: "pin"}, {Kind: "connection", Count: 1}}},
			{Name: "api", Engine: "process", Version: "builtin", State: "active", PID: 4201, Declared: true,
				Endpoint: "127.0.0.1:8080", RAM: 43 << 20, CPU: 2.5, Healthy: &healthy},
			{Name: "worker", Engine: "process", Version: "builtin", State: "crashing", Declared: true,
				RestartCount: 2, RestartMax: 5, NextRestart: at.Add(4 * time.Second),
				LastError: "exited with status 1; restarting (2/5) in 4s"},
			{Name: "web", Engine: "process", Version: "builtin", State: "failed", Declared: true,
				Endpoint: "127.0.0.1:8090", LastError: "exited with status 2; gave up after 5 restarts"},
		},
	}
}

func views(snap control.Snapshot) map[string]control.InstanceView {
	out := map[string]control.InstanceView{}
	for _, v := range snap.Instances {
		out[v.Name] = v
	}
	return out
}

func TestGoldenStatus(t *testing.T) {
	fixClock(t)
	cfg, snap := shopConfig(t), shopSnapshot()

	var up bytes.Buffer
	renderTable(&up, cfg, views(snap), true, snap.IdleTimeout)
	golden(t, "status", up.Bytes())

	var down bytes.Buffer
	renderTable(&down, cfg, nil, false, 0)
	golden(t, "status-daemon-down", down.Bytes())
}

// status -w, piped: the table, then the table again after each change.
func TestGoldenStatusWatch(t *testing.T) {
	fixClock(t)
	cfg, snap := shopConfig(t), shopSnapshot()
	recovered := snap.Instances[4] // worker comes back
	recovered.State, recovered.PID, recovered.LastError, recovered.RestartMax = "active", 4301, "", 0
	d := &fakedaemon.Fake{Snap: snap, Frames: []control.Frame{
		{Topic: control.TopicInstances, Snapshot: true, Instances: snap.Instances},
		{Topic: control.TopicInstances, Instances: []control.InstanceView{recovered}},
		{Topic: control.TopicStats, Stats: []control.StatView{{Name: "worker", PID: 4301, RAM: 46 << 20, CPU: 0.9}}},
	}}
	var out bytes.Buffer
	if err := watchStatus(context.Background(), d, &out, cfg, false); err != nil {
		t.Fatal(err)
	}
	golden(t, "status-watch", out.Bytes())
}

func TestGoldenWake(t *testing.T) {
	ready := func(seq uint64, name string, took time.Duration) control.Frame {
		return control.Frame{Topic: control.TopicJournal, Event: &journal.Event{Seq: seq, Kind: journal.Ready, Instance: name, Took: took}}
	}
	d := &fakedaemon.Fake{Frames: []control.Frame{
		ready(2, "db", 38*time.Millisecond),
		ready(4, "events", 1240*time.Millisecond),
		ready(6, "api", 2*time.Minute+5*time.Second),
	}}
	var out bytes.Buffer
	if err := runWake(context.Background(), slow{d}, &out, []string{"db", "events", "api"}); err != nil {
		t.Fatal(err)
	}
	golden(t, "wake", out.Bytes())
	if done := d.Done(); len(done) != 3 || done[0] != (control.Verb{Name: control.VerbWake, Target: "db"}) {
		t.Fatalf("verbs run: %+v", done)
	}

	// A wake that fails stops there, says why, and exits 1.
	failing := &fakedaemon.Fake{Fail: map[string]error{"wake events": errors.New("events not ready: did not become ready within 1m0s")}}
	out.Reset()
	err := runWake(context.Background(), slow{failing}, &out, []string{"db", "events", "api"})
	var code exitCodeError
	if !errors.As(err, &code) || int(code) != 1 {
		t.Fatalf("a failed wake must exit 1, got %v", err)
	}
	golden(t, "wake-failed", out.Bytes())
	if len(failing.Done()) != 2 {
		t.Fatalf("nothing after the failure should be attempted: %+v", failing.Done())
	}
}

// slow makes every verb take long enough that the command waits for the
// journal's duration, as a real wake would.
type slow struct{ *fakedaemon.Fake }

func (s slow) Do(ctx context.Context, v control.Verb) error {
	time.Sleep(6 * time.Millisecond)
	return s.Fake.Do(ctx, v)
}

func TestGoldenSleepRestartResetPin(t *testing.T) {
	snap := shopSnapshot()
	d := &fakedaemon.Fake{Snap: snap}
	d.Frames = []control.Frame{{Topic: control.TopicJournal, Event: &journal.Event{Seq: 9, Kind: journal.Ready, Instance: "api", Took: 310 * time.Millisecond}}}
	ctx := context.Background()
	var out bytes.Buffer

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(runSleep(ctx, d, &out, []string{"cache", "web"}))
	must(runSleep(ctx, d, &out, nil))
	must(runRestart(ctx, slow{d}, &out, []string{"api"}))
	must(runReset(ctx, d, &out, []string{"db"}))
	// events is pinned in the snapshot and cache is not: pin reports each as it now stands.
	must(runPin(ctx, d, &out, []string{"events", "cache"}))
	golden(t, "sleep-restart-reset-pin", out.Bytes())

	want := []control.Verb{
		{Name: "sleep", Target: "cache"}, {Name: "sleep", Target: "web"}, {Name: "sleep"},
		{Name: "restart", Target: "api"}, {Name: "reset", Target: "db"},
		{Name: "pin", Target: "events"}, {Name: "pin", Target: "cache"},
	}
	got := d.Done()
	if len(got) != len(want) {
		t.Fatalf("verbs run: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("verb %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func scriptedLogs() map[string][]control.LogFrame {
	l := func(inst string, run int, sec int, stream, text string) control.LogFrame {
		return control.LogFrame{Instance: inst, Run: run, Time: at.Add(time.Duration(sec) * time.Second), Stream: stream, Line: text}
	}
	return map[string][]control.LogFrame{
		"api": {
			l("api", 1, -600, "o", "listening on :8080"),
			l("api", 1, -590, "e", "panic: nil pointer dereference"),
			l("api", 2, 0, "s:setup", "migrated 3 files"),
			l("api", 2, 1, "o", "listening on :8080"),
			l("api", 2, 5, "o", "GET /products 200 4ms"),
			l("api", 2, 9, "e", "slow query: 812ms"),
		},
		"worker": {
			l("worker", 4, 3, "o", "consuming orders"),
			l("worker", 4, 7, "e", "kafka: connection refused"),
		},
		"db": {
			l("db", 2, 2, "o:postgres", "database system is ready to accept connections"),
		},
	}
}

func TestGoldenLogs(t *testing.T) {
	fixClock(t)
	d := &fakedaemon.Fake{Lines: scriptedLogs()}
	ctx := context.Background()

	var one bytes.Buffer
	if err := printLogs(ctx, d, &one, []string{"api"}, control.LogQuery{Run: 2}, false); err != nil {
		t.Fatal(err)
	}
	golden(t, "logs-one", one.Bytes())

	// The run before: what it said before it died.
	var previous bytes.Buffer
	if err := printLogs(ctx, d, &previous, []string{"api"}, control.LogQuery{Run: 1}, false); err != nil {
		t.Fatal(err)
	}
	golden(t, "logs-previous-run", previous.Bytes())

	// Several at once are merged by time, each line saying whose it is.
	var many bytes.Buffer
	if err := printLogs(ctx, d, &many, []string{"api", "worker", "db"}, control.LogQuery{Since: at}, true); err != nil {
		t.Fatal(err)
	}
	golden(t, "logs-merged", many.Bytes())

	// Following prints what the subscription sends.
	following := &fakedaemon.Fake{Frames: []control.Frame{
		{Topic: control.TopicLogs, Log: &scriptedLogs()["api"][4]},
		{Topic: control.TopicLogs, Log: &scriptedLogs()["worker"][1]},
		{Topic: control.TopicLogs, Log: &scriptedLogs()["api"][5]},
	}}
	var follow bytes.Buffer
	if err := followLogs(ctx, following, &follow, []string{"api", "worker"}, true, 0, time.Time{}); err != nil {
		t.Fatal(err)
	}
	golden(t, "logs-follow", follow.Bytes())

	var none bytes.Buffer
	if err := printLogs(ctx, &fakedaemon.Fake{Lines: map[string][]control.LogFrame{"web": nil}}, &none, []string{"web"}, control.LogQuery{}, false); err != nil {
		t.Fatal(err)
	}
	golden(t, "logs-nothing-yet", none.Bytes())
}
