package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
	"github.com/doze-dev/doze/internal/control/fixture"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/view"
)

var epoch = time.Date(2026, 10, 11, 14, 4, 40, 0, time.UTC)

func newHub(t *testing.T, f *fakedaemon.Fake) (*hub.Hub, string) {
	t.Helper()
	home := t.TempDir()
	run := filepath.Join(home, "projects", "p-1", "run")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "doze.admin.sock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h := hub.New(hub.Options{Home: home, Dial: func(string) control.Daemon { return f }, Every: 20 * time.Millisecond, Now: func() time.Time { return epoch }})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st := h.Stacks(); len(st) == 1 && st[0].Conn == hub.ConnLive {
			return h, st[0].Key
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the hub never saw the fixture")
	return nil, ""
}

type recorder struct {
	mu     sync.Mutex
	events []string
	data   map[string][]json.RawMessage
}

func (r *recorder) emit(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, name)
	if r.data == nil {
		r.data = map[string][]json.RawMessage{}
	}
	b, _ := json.Marshal(data)
	r.data[name] = append(r.data[name], b)
}

func (r *recorder) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.data[name])
}

func TestPushesFollowTheFocusAndOnlyChanges(t *testing.T) {
	h, key := newHub(t, fixture.Shop(epoch))
	rec := &recorder{}
	s := New(h, rec.emit)
	s.SetClock(func() time.Time { return epoch })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	s.SetFocus(Focus{Key: key, Page: "home"})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && rec.count(EventHome) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if rec.count(EventHome) == 0 || rec.count(EventStacks) == 0 {
		t.Fatalf("no push: %v", rec.events)
	}
	var home view.Home
	if err := json.Unmarshal(rec.data[EventHome][0], &home); err != nil {
		t.Fatal(err)
	}
	if home.Stack != "shop" || home.Awake != 5 {
		t.Errorf("home pushed = stack %q awake %d", home.Stack, home.Awake)
	}
	// The meters settle (the fixture's 240 readings arrive in a burst), so
	// homes may be pushed a few times; never more than the pace allows.
	time.Sleep(600 * time.Millisecond)
	if n := rec.count(EventHome); n > 4 {
		t.Errorf("%d home pushes in under a second; the pace is %s", n, Pace)
	}
	before := rec.count(EventHome)
	time.Sleep(300 * time.Millisecond)
	if rec.count(EventHome) != before {
		t.Error("an unchanged home was pushed again")
	}

	// Blurred, nothing streams and nothing is pushed.
	s.Blur()
	time.Sleep(50 * time.Millisecond)
	before = rec.count(EventHome)
	h.Refresh()
	time.Sleep(300 * time.Millisecond)
	if rec.count(EventHome) != before {
		t.Error("a blurred page was pushed to")
	}
}

func TestDoRefusesWithTheDashSentence(t *testing.T) {
	f := fixture.Shop(epoch)
	h, key := newHub(t, f)
	s := New(h, nil)
	if err := s.Do(key, control.VerbWake, "cloud"); err != nil {
		t.Fatal(err)
	}
	if done := f.Done(); len(done) != 1 || done[0].Target != "cloud" {
		t.Errorf("verbs = %+v", done)
	}
	f.Drop()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && h.Stacks()[0].Conn != hub.ConnDown {
		time.Sleep(10 * time.Millisecond)
	}
	err := s.Do(key, control.VerbSleep, "db")
	if err == nil || err.Error() != "the daemon is not responding: nothing was sent" {
		t.Errorf("err = %v", err)
	}
}

func TestHTTPCallsAndEvents(t *testing.T) {
	h, key := newHub(t, fixture.Shop(epoch))
	s := New(h, nil)
	s.SetClock(func() time.Time { return epoch })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	srv := httptest.NewServer(Handler(s))
	defer srv.Close()

	post := func(call string, body any, out any) {
		t.Helper()
		b, _ := json.Marshal(body)
		resp, err := http.Post(srv.URL+"/api/"+call, "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d", call, resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	var stacks []StackSummary
	post("stacks", nil, &stacks)
	if len(stacks) != 1 || stacks[0].Name != "shop" || !stacks[0].Live {
		t.Fatalf("stacks = %+v", stacks)
	}
	var home view.Home
	post("home", map[string]string{"key": key}, &home)
	if home.Stack != "shop" {
		t.Errorf("home = %+v", home.Stack)
	}

	// Events: focus on logs, then the stream says when lines arrive.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var ok map[string]bool
	post("focus", Focus{Key: key, Page: "logs", Instance: "api"}, &ok)
	rd := bufio.NewReader(resp.Body)
	found := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !found {
		line, err := rd.ReadString('\n')
		if err != nil {
			break
		}
		if strings.HasPrefix(line, "event: "+EventLogs) {
			found = true
		}
	}
	if !found {
		t.Error("no logs event on the stream")
	}
	var logs view.Logs
	post("logs", map[string]any{"key": key, "query": view.LogsQuery{Instance: "api", N: 3}}, &logs)
	if logs.Total != 10 || len(logs.Lines) != 3 {
		t.Errorf("logs = total %d lines %d", logs.Total, len(logs.Lines))
	}
	var fail map[string]string
	b, _ := json.Marshal(map[string]string{"key": "nope", "verb": "wake", "target": "x"})
	r2, err := http.Post(srv.URL+"/api/do", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	_ = json.NewDecoder(r2.Body).Decode(&fail)
	if r2.StatusCode != http.StatusUnprocessableEntity || fail["error"] == "" {
		t.Errorf("do on an unknown stack: %d %v", r2.StatusCode, fail)
	}
}

func TestAConsoleTabIsWatchedFollowedAndPushed(t *testing.T) {
	f := fixture.Modules(epoch)
	h, key := newHub(t, f)
	rec := &recorder{}
	s := New(h, rec.emit)
	s.SetClock(func() time.Time { return epoch })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.Run(ctx)

	// Opening db's page watches its console; the activity tab is pushed
	// once its rows are in.
	s.SetFocus(Focus{Key: key, Page: "instance", Instance: "db", Tab: "console:activity", Sel: map[string]int{}})
	deadline := time.Now().Add(3 * time.Second)
	var c view.Console
	for time.Now().Before(deadline) {
		if n := rec.count(EventConsole); n > 0 {
			rec.mu.Lock()
			b := rec.data[EventConsole][n-1]
			rec.mu.Unlock()
			if err := json.Unmarshal(b, &c); err != nil {
				t.Fatal(err)
			}
			if len(c.Body.Children) == 2 && len(c.Body.Children[1].Children[0].Rows) == 4 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(c.Body.Children) != 2 || len(c.Body.Children[1].Children[0].Rows) != 4 {
		t.Fatalf("the activity tab never arrived: %+v", c)
	}
	// The instance view now knows the console's tabs.
	if tabs := s.Instance(key, "db").Tabs; len(tabs) != 3 || tabs[1].ID != "console:activity" {
		t.Errorf("tabs = %+v", tabs)
	}
	// An action runs on the selected row and is said.
	f.AdminResult = "query cancelled"
	if err := s.Admin(key, "db", "cancel", "4412", ""); err != nil {
		t.Fatal(err)
	}
	if acts := f.Actions(); len(acts) != 1 || acts[0].Instance != "db" || acts[0].Action != "cancel" || acts[0].Resource != "4412" {
		t.Errorf("admin calls = %+v", acts)
	}
	if r := s.Recent(); len(r) == 0 || r[0].Text != "query cancelled" || r[0].Bad {
		t.Errorf("said = %+v", r)
	}
	// Kafka's tail follows the selected topic: the stream is resubscribed
	// for the row picked.
	s.SetFocus(Focus{Key: key, Page: "instance", Instance: "events", Tab: "console:_", Sel: map[string]int{"table:topics": 1}})
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st, _ := s.stack(key)
		if cv, ok := st.Consoles["events"]; ok && len(cv.Streams["tail"]) == 1 && cv.Streams["tail"][0].Cells["key"] == "c_88" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	st, _ := s.stack(key)
	if rows := st.Consoles["events"].Streams["tail"]; len(rows) != 1 || rows[0].Cells["key"] != "c_88" {
		t.Errorf("the tail for emails = %+v", rows)
	}
	// Leaving the page releases the console: its rows are forgotten.
	s.Blur()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := s.stack(key); len(st.Consoles) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the consoles stayed watched after blur")
}
