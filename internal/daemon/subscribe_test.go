package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/journal"
	"github.com/doze-dev/doze/internal/logstore"
	"github.com/doze-dev/doze/internal/runtime"
)

// quietProc and quietDriver are an engine that "runs" without a process.
type quietProc struct {
	stopped atomic.Bool
	done    chan struct{}
}

func (p *quietProc) PID() int       { return 0 }
func (p *quietProc) Alive() bool    { return !p.stopped.Load() }
func (p *quietProc) Logs() []string { return nil }
func (p *quietProc) Wait() error    { <-p.done; return nil }
func (p *quietProc) Stop(context.Context) error {
	if p.stopped.CompareAndSwap(false, true) {
		close(p.done)
	}
	return nil
}

type quietDriver struct{}

func (quietDriver) Type() string { return "tquiet" }
func (quietDriver) Resolve(context.Context, engine.VersionSpec, engine.Platform, engine.Locker, engine.Fetcher) (engine.Toolchain, error) {
	return engine.Toolchain{Engine: "tquiet"}, nil
}
func (quietDriver) Provision(context.Context, engine.Instance, engine.Toolchain) error { return nil }
func (quietDriver) Provisioned(string) bool                                            { return true }
func (quietDriver) Spawn(context.Context, engine.Instance, engine.Toolchain) (engine.Process, error) {
	return &quietProc{done: make(chan struct{})}, nil
}
func (quietDriver) WaitReady(context.Context, engine.Instance, engine.Toolchain, engine.Process) error {
	return nil
}
func (quietDriver) BackendSocket(dir string, _ int) string                       { return dir + "/q.sock" }
func (quietDriver) ConnString(engine.Instance, engine.Endpoint) (string, string) { return "", "" }

func testDaemon(t *testing.T) (*Daemon, *handler) {
	t.Helper()
	t.Setenv("DOZE_HOME", t.TempDir())
	engine.Register(quietDriver{})
	cfg, err := config.Parse([]byte(`
module tquiet "db" {
  version = 1
  port    = 15432
}
module tquiet "api" {
  version = 1
  port    = 18080
  after   = [module.db]
}
`), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := runtime.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.EnsureDataRoot(); err != nil {
		t.Fatal(err)
	}
	j, err := journal.Open(filepath.Join(t.TempDir(), "journal.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	rt.SetJournal(j)
	d := &Daemon{cfg: cfg, rt: rt, logf: t.Logf}
	return d, &handler{d: d}
}

// frames collects what a subscription emits.
type frames struct {
	mu  sync.Mutex
	all []control.Frame
}

func (f *frames) emit(fr control.Frame) error {
	f.mu.Lock()
	f.all = append(f.all, fr)
	f.mu.Unlock()
	return nil
}

func (f *frames) snapshot() []control.Frame {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]control.Frame{}, f.all...)
}

// until polls for cond over the frames so far.
func (f *frames) until(t *testing.T, what string, cond func([]control.Frame) bool) []control.Frame {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := f.snapshot()
		if cond(got) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("never saw %s; frames so far: %d", what, len(got))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func subscribe(t *testing.T, h *handler, sub control.Subscription) *frames {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	out := &frames{}
	done := make(chan struct{})
	go func() { defer close(done); _ = h.Subscribe(ctx, sub, out.emit) }()
	t.Cleanup(func() { cancel(); <-done })
	return out
}

// latest folds instances frames into the state a client would hold.
func latest(all []control.Frame) map[string]control.InstanceView {
	out := map[string]control.InstanceView{}
	for _, f := range all {
		if f.Topic != control.TopicInstances {
			continue
		}
		for _, v := range f.Instances {
			out[v.Name] = v
		}
		for _, name := range f.Removed {
			delete(out, name)
		}
	}
	return out
}

func TestInstancesTopic(t *testing.T) {
	d, h := testDaemon(t)
	out := subscribe(t, h, control.Subscription{Topics: []string{control.TopicInstances}})

	first := out.until(t, "the opening snapshot", func(fs []control.Frame) bool { return len(fs) >= 1 })[0]
	if !first.Snapshot || len(first.Instances) != 2 {
		t.Fatalf("the first frame must be a full snapshot of both instances, got %+v", first)
	}
	for _, v := range first.Instances {
		if v.State != "reaped" || !v.Declared {
			t.Fatalf("before anything runs: %+v", v)
		}
	}

	if _, err := d.rt.Boot(context.Background(), "api"); err != nil {
		t.Fatal(err)
	}
	got := out.until(t, "both running", func(fs []control.Frame) bool {
		now := latest(fs)
		return now["api"].State == "idle" && now["db"].State == "active"
	})
	db := latest(got)["db"]
	if len(db.Holders) != 1 || db.Holders[0] != (control.HolderView{Kind: "dependant", Name: "api"}) {
		t.Fatalf("db should be held by api, got %+v", db.Holders)
	}
	for _, f := range got[1:] {
		if f.Snapshot {
			t.Fatal("only the first frame is a snapshot; later ones carry changes")
		}
	}

	// A pin and a stop both reach the subscriber, though neither is a boot.
	d.rt.ToggleKeepAwake("db")
	out.until(t, "the pin", func(fs []control.Frame) bool { return latest(fs)["db"].KeepAwake })
	if err := d.rt.Stop(context.Background(), "api"); err != nil {
		t.Fatal(err)
	}
	out.until(t, "api asleep and db released", func(fs []control.Frame) bool {
		now := latest(fs)
		return now["api"].State == "reaped" && now["db"].State == "idle"
	})
}

func TestJournalTopicResumesFromASequence(t *testing.T) {
	d, h := testDaemon(t)
	if _, err := d.rt.Boot(context.Background(), "db"); err != nil {
		t.Fatal(err)
	}
	// Two events are on disk: wake db, ready db. Ask for everything after the first.
	out := subscribe(t, h, control.Subscription{Topics: []string{control.TopicJournal}, Since: 1})
	out.until(t, "the event already on disk", func(fs []control.Frame) bool { return len(fs) >= 1 })

	if err := d.rt.Stop(context.Background(), "db"); err != nil {
		t.Fatal(err)
	}
	got := out.until(t, "the live event", func(fs []control.Frame) bool { return len(fs) >= 2 })
	var kinds []string
	for i, f := range got {
		if f.Event == nil || f.Event.Seq != uint64(i+2) {
			t.Fatalf("frame %d = %+v; want seq %d", i, f.Event, i+2)
		}
		kinds = append(kinds, f.Event.Kind)
	}
	if strings.Join(kinds, " ") != "ready sleep" {
		t.Fatalf("journal topic delivered %q, want \"ready sleep\"", strings.Join(kinds, " "))
	}

	// With no cursor, only what happens from now on.
	fresh := subscribe(t, h, control.Subscription{Topics: []string{control.TopicJournal}})
	time.Sleep(30 * time.Millisecond)
	d.rt.ToggleKeepAwake("db")
	only := fresh.until(t, "the pin", func(fs []control.Frame) bool { return len(fs) >= 1 })
	if only[0].Event.Kind != journal.Pin {
		t.Fatalf("a subscription with no cursor replayed the past: %+v", only[0].Event)
	}
}

func TestLogsTopicSendsABacklogThenFollows(t *testing.T) {
	d, h := testDaemon(t)
	run, err := d.rt.Logs().Begin("api")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{"one", "two", "three"} {
		run.Line(logstore.Stdout, l)
	}
	out := subscribe(t, h, control.Subscription{Topics: []string{control.TopicLogs}, Names: []string{"api"}, Backlog: 2})
	out.until(t, "the backlog", func(fs []control.Frame) bool { return len(fs) >= 2 })
	run.Line(logstore.Stderr, "four")
	got := out.until(t, "the new line", func(fs []control.Frame) bool { return len(fs) >= 3 })

	var lines []string
	for _, f := range got {
		lines = append(lines, f.Log.Stream+":"+f.Log.Line)
		if f.Log.Instance != "api" || f.Log.Run != 1 || f.Log.Time.IsZero() {
			t.Fatalf("log frame = %+v", f.Log)
		}
	}
	if strings.Join(lines, " ") != "o:two o:three e:four" {
		t.Fatalf("logs topic delivered %q", strings.Join(lines, " "))
	}
}

func TestStatsTopicAndUnknownTopic(t *testing.T) {
	_, h := testDaemon(t)
	out := subscribe(t, h, control.Subscription{Topics: []string{control.TopicStats}, Interval: 250 * time.Millisecond})
	got := out.until(t, "two samples", func(fs []control.Frame) bool { return len(fs) >= 2 })
	if got[0].Topic != control.TopicStats {
		t.Fatalf("frame = %+v", got[0])
	}
	err := h.Subscribe(context.Background(), control.Subscription{Topics: []string{"weather"}}, func(control.Frame) error { return nil })
	if err == nil || !strings.Contains(err.Error(), `no topic called "weather"`) {
		t.Fatalf("unknown topic: %v", err)
	}
}
