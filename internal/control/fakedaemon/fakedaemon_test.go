package fakedaemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
)

func TestScriptIsPlayedByTopic(t *testing.T) {
	f := &Fake{Frames: []control.Frame{
		{Topic: control.TopicInstances, Snapshot: true},
		{Topic: control.TopicStats},
		{Topic: control.TopicInstances},
	}}
	var got []string
	err := f.Subscribe(context.Background(), control.Subscription{Topics: []string{control.TopicInstances}}, func(fr control.Frame) error {
		got = append(got, fr.Topic)
		return nil
	})
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v; want the two instances frames and then the end", got, err)
	}
}

func TestHoldKeepsTheStreamOpen(t *testing.T) {
	f := &Fake{Hold: true}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- f.Subscribe(ctx, control.Subscription{Topics: []string{control.TopicStats}}, func(control.Frame) error { return nil })
	}()
	select {
	case <-done:
		t.Fatal("a held subscription ended by itself")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestVerbsAreRecordedAndCanFail(t *testing.T) {
	boom := errors.New("port 5432 is in use")
	f := &Fake{Fail: map[string]error{"wake db": boom}}
	if err := f.Do(context.Background(), control.Verb{Name: control.VerbWake, Target: "db"}); !errors.Is(err, boom) {
		t.Fatalf("wake db = %v", err)
	}
	if err := f.Do(context.Background(), control.Verb{Name: control.VerbWake, Target: "api"}); err != nil {
		t.Fatalf("wake api = %v", err)
	}
	if done := f.Done(); len(done) != 2 || done[1].Target != "api" {
		t.Fatalf("recorded %+v", done)
	}
}

func TestLogsHonourTheQuery(t *testing.T) {
	at := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	f := &Fake{Lines: map[string][]control.LogFrame{"api": {
		{Instance: "api", Run: 1, Time: at, Line: "old run"},
		{Instance: "api", Run: 2, Time: at.Add(time.Minute), Line: "a"},
		{Instance: "api", Run: 2, Time: at.Add(2 * time.Minute), Line: "b"},
	}}}
	last, _ := f.Logs(context.Background(), control.LogQuery{Instance: "api", Tail: 1})
	run1, _ := f.Logs(context.Background(), control.LogQuery{Instance: "api", Run: 1})
	recent, _ := f.Logs(context.Background(), control.LogQuery{Instance: "api", Since: at.Add(90 * time.Second)})
	if len(last) != 1 || last[0].Line != "b" || len(run1) != 1 || run1[0].Line != "old run" || len(recent) != 1 || recent[0].Line != "b" {
		t.Fatalf("tail %v, run 1 %v, since %v", last, run1, recent)
	}
	if _, err := f.Logs(context.Background(), control.LogQuery{Instance: "nope"}); err == nil {
		t.Fatal("an undeclared instance is an error")
	}
}
