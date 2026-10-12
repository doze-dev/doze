package dash

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
)

// Until the daemon has said what is in the stack, the page does not say the stack
// is empty: it says it is asking.
func TestConnectingIsNotEmpty(t *testing.T) {
	m := New(&fakedaemon.Fake{}, Options{Plain: true, Now: func() time.Time { return epoch }, Loc: time.UTC})
	m.conn, m.waiting = connConnecting, true
	m.snap = control.Snapshot{Stack: "shop"}
	out := frame(m, 100, 24)
	if strings.Contains(out, "Nothing is declared") {
		t.Errorf("a stack not yet known was called empty:\n%s", out)
	}
	if !strings.Contains(out, "connecting") {
		t.Errorf("the page does not say it is connecting:\n%s", out)
	}
	// and once the daemon answers with nothing, then it is empty
	m.Update(connMsg{up: true})
	m.Update(snapshotMsg{snap: control.Snapshot{Stack: "shop"}})
	if out := frame(m, 100, 24); !strings.Contains(out, "Nothing is declared") || strings.Contains(out, "connecting") {
		t.Errorf("an answered, empty stack:\n%s", out)
	}
}

// A daemon that goes away is said at the top of every page, verbs are refused
// rather than sent into the void, and everything is as it was when it returns.
func TestADeadDaemonIsSaidAndAVerbIsRefused(t *testing.T) {
	m := board()
	m.w, m.h = 140, 40
	if out := frame(m, 140, 40); strings.Contains(out, "not responding") {
		t.Fatalf("a live daemon is called dead:\n%s", out)
	}
	m.Update(connMsg{up: false, err: fakedaemon.ErrGone, retry: 4 * time.Second})
	out := frame(m, 140, 40)
	if !strings.Contains(out, "daemon not responding") || !strings.Contains(out, "retrying in 4s") {
		t.Errorf("the banner:\n%s", out)
	}
	for _, page := range []int{pageHome, pageFocus, pageLogs} {
		m.page = page
		if m.page == pageFocus {
			m.sel = "api"
		}
		if got := frame(m, 140, 40); !strings.Contains(got, "daemon not responding") {
			t.Errorf("page %d does not say the daemon is gone:\n%s", page, got)
		}
	}
	m.page = pageHome
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if cmd == nil {
		t.Fatal("w did nothing at all")
	}
	if msg, ok := cmd().(failMsg); !ok || !strings.Contains(string(msg), "not responding") {
		t.Errorf("w while down should say nothing was sent, got %v", cmd())
	}
	if len(m.d.(*fakedaemon.Fake).Done()) != 0 {
		t.Error("a verb was sent to a daemon that is not there")
	}
	// it comes back: live, a snapshot is asked for, the banner is gone, the logs kept
	m.cache = map[string][]control.LogFrame{"api": {{Instance: "api", Line: "INFO kept", Time: epoch}}}
	_, cmd = m.Update(connMsg{up: true})
	if cmd == nil {
		t.Error("coming back should ask for a snapshot")
	}
	if out := frame(m, 140, 40); strings.Contains(out, "not responding") {
		t.Errorf("the banner stayed after the daemon returned:\n%s", out)
	}
	if len(m.cache["api"]) != 1 {
		t.Error("the kept lines were lost over a reconnect")
	}
}

// Silence is also a failure: nothing for three seconds says so, with how long.
func TestSilenceIsSaid(t *testing.T) {
	now := epoch
	m := New(&fakedaemon.Fake{}, Options{Plain: true, Now: func() time.Time { return now }, Loc: time.UTC})
	m.snap = board().snap
	m.Update(frameMsg(control.Frame{Topic: control.TopicStats}))
	now = epoch.Add(2 * time.Second)
	m.Update(tickMsg(now))
	if out := frame(m, 140, 40); strings.Contains(out, "last update") {
		t.Errorf("two seconds is not late:\n%s", out)
	}
	now = epoch.Add(9 * time.Second)
	m.Update(tickMsg(now))
	if out := frame(m, 140, 40); !strings.Contains(out, "last update 9s ago") {
		t.Errorf("nine seconds of silence unsaid:\n%s", out)
	}
}

// A subscription that ends is made again, and the connection state follows it.
func TestKeepSubscribedReconnects(t *testing.T) {
	f := &fakedaemon.Fake{Hold: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msgs := make(chan connMsg, 16)
	go keepSubscribed(ctx, f, func() control.Subscription {
		return control.Subscription{Topics: []string{control.TopicStats}}
	}, func(m tea.Msg) {
		if c, ok := m.(connMsg); ok {
			msgs <- c
		}
	}, func(control.Frame) {}, true)

	want := func(up bool, what string) connMsg {
		t.Helper()
		select {
		case c := <-msgs:
			if c.up != up {
				t.Fatalf("%s: got up=%v", what, c.up)
			}
			return c
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: nothing came", what)
		}
		return connMsg{}
	}
	// the first frame is what says it is up; the fake sends none until something
	// is pushed
	go func() {
		for i := 0; i < 40; i++ {
			f.Push(control.Frame{Topic: control.TopicStats, Stats: []control.StatView{{Name: "x"}}})
			time.Sleep(50 * time.Millisecond)
		}
	}()
	want(true, "first connection")
	f.Drop()
	if c := want(false, "after the drop"); c.err == nil || c.retry <= 0 {
		t.Errorf("a drop should carry its reason and when it will retry: %+v", c)
	}
	f.Refuse(false)
	go func() {
		for i := 0; i < 60; i++ {
			f.Push(control.Frame{Topic: control.TopicStats, Stats: []control.StatView{{Name: "x"}}})
			time.Sleep(50 * time.Millisecond)
		}
	}()
	want(true, "after it came back")
}

// What the daemon says again after a reconnect (its history, its backlog) is not
// kept twice.
func TestAReconnectDoesNotDuplicate(t *testing.T) {
	m := board()
	at := epoch.Add(-time.Minute)
	for i := 0; i < 3; i++ {
		m.Update(frameMsg(control.Frame{Topic: control.TopicStats, At: at.Add(time.Duration(i) * time.Second), Stats: []control.StatView{{Name: "api", RAM: int64(i) << 20}}}))
	}
	n := len(m.ram["api"])
	for i := 0; i < 3; i++ { // the same three again
		m.Update(frameMsg(control.Frame{Topic: control.TopicStats, At: at.Add(time.Duration(i) * time.Second), Stats: []control.StatView{{Name: "api", RAM: int64(i) << 20}}}))
	}
	if len(m.ram["api"]) != n {
		t.Errorf("readings doubled: %d -> %d", n, len(m.ram["api"]))
	}
	line := logMsg{"api", control.LogFrame{Instance: "api", Line: "INFO once", Time: epoch, Run: 1}}
	m.Update(line)
	m.Update(line)
	c := 0
	for _, l := range m.cache["api"] {
		if l.Line == "INFO once" {
			c++
		}
	}
	if c != 1 {
		t.Errorf("a log line was kept %d times", c)
	}
}
