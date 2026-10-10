package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stub is a Handler whose slow operations and streams the tests steer.
type stub struct {
	Handler                  // anything not overridden panics, which is what a test wants
	slow       chan struct{} // Boot blocks until this closes
	bootDone   atomic.Bool
	streaming  atomic.Int32 // subscriptions currently running
	subscribed chan struct{}
}

func (s *stub) Status() Response { return Response{Listen: "here"} }
func (s *stub) Up(_ context.Context, name string) error {
	if name == "nope" {
		return errors.New("no instance called nope")
	}
	return nil
}
func (s *stub) Boot(ctx context.Context, _ string) error {
	select {
	case <-s.slow:
		s.bootDone.Store(true)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *stub) Subscribe(ctx context.Context, sub Subscription, emit func(Frame) error) error {
	s.streaming.Add(1)
	defer s.streaming.Add(-1)
	if s.subscribed != nil {
		s.subscribed <- struct{}{}
	}
	for i := 1; ; i++ {
		if err := emit(Frame{Topic: sub.Topics[0], Stats: []StatView{{Name: "n", PID: i}}}); err != nil {
			return err
		}
		if sub.Backlog > 0 && i == sub.Backlog {
			return nil // a stream the daemon ends by itself
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func serve(t *testing.T, h Handler) *Client {
	t.Helper()
	// A short path: a unix socket's has a hard limit the test temp dir can pass.
	dir, err := os.MkdirTemp("/tmp", "dzp")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s.sock")
	srv, err := NewServer(path, h)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Serve(ctx)
	t.Cleanup(func() { cancel(); os.RemoveAll(dir) })
	return NewClient(path)
}

func TestUnaryRoundTrip(t *testing.T) {
	c := serve(t, &stub{})
	resp, err := c.Do(Request{Op: "status"})
	if err != nil || !resp.OK || resp.Listen != "here" {
		t.Fatalf("status = %+v, %v", resp, err)
	}
	if _, err := c.Do(Request{Op: "up", DB: "nope"}); err == nil || err.Error() != "no instance called nope" {
		t.Fatalf("a handler error must come back as itself, got %v", err)
	}
	if _, err := c.Do(Request{Op: "frobnicate"}); err == nil || !strings.Contains(err.Error(), "unknown op") {
		t.Fatalf("unknown op: %v", err)
	}
}

// A client from before the handshake sends a bare request. It must be told
// what is wrong in a field it reads, not handed something it cannot decode.
func TestAnOldClientIsToldInWords(t *testing.T) {
	c := serve(t, &stub{})
	conn, err := net.Dial("unix", c.path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintln(conn, `{"op":"status"}`)
	var old struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&old); err != nil {
		t.Fatalf("the reply must decode as the old response shape: %v", err)
	}
	if old.OK || !strings.Contains(old.Error, "protocol 2") || !strings.Contains(old.Error, "doze down") {
		t.Fatalf("reply = %+v", old)
	}

	// And a newer client than the daemon.
	conn2, _ := net.Dial("unix", c.path)
	defer conn2.Close()
	fmt.Fprintln(conn2, `{"hello":{"proto":3}}`)
	var f Frame
	_ = json.NewDecoder(bufio.NewReader(conn2)).Decode(&f)
	if !strings.Contains(f.Error, "protocol 3") {
		t.Fatalf("mismatch reply = %+v", f)
	}
}

// One connection carries a slow command and a quick one at once, and the quick
// one does not wait.
func TestRequestsAreMultiplexed(t *testing.T) {
	h := &stub{slow: make(chan struct{})}
	c := serve(t, h)
	cn, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cn.Close()

	booted := make(chan error, 1)
	go func() {
		_, err := cn.Do(context.Background(), Request{Op: "wake", DB: "db"})
		booted <- err
	}()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if resp, err := cn.Do(context.Background(), Request{Op: "status"}); err != nil || resp.Listen != "here" {
				t.Errorf("status while a wake is in flight: %+v, %v", resp, err)
			}
		}()
	}
	wg.Wait()
	select {
	case err := <-booted:
		t.Fatalf("the slow wake returned early: %v", err)
	default:
	}
	close(h.slow)
	if err := <-booted; err != nil {
		t.Fatalf("wake: %v", err)
	}
}

// Hanging up on a slow command does not abandon it: the daemon finishes what
// it was asked to do.
func TestACommandOutlivesItsClient(t *testing.T) {
	h := &stub{slow: make(chan struct{})}
	c := serve(t, h)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.DoContext(ctx, Request{Op: "wake", DB: "db"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the client should have given up, got %v", err)
	}
	time.Sleep(50 * time.Millisecond) // the connection is closed by now
	close(h.slow)
	deadline := time.Now().Add(2 * time.Second)
	for !h.bootDone.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the wake was cancelled when its client hung up")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSubscribeStreamsUntilCancelled(t *testing.T) {
	h := &stub{subscribed: make(chan struct{}, 4)}
	c := serve(t, h)
	cn, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var got atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- cn.Subscribe(ctx, Subscription{Topics: []string{TopicStats}}, func(f Frame) error {
			if f.Topic != TopicStats || len(f.Stats) != 1 {
				return fmt.Errorf("unexpected frame %+v", f)
			}
			got.Add(1)
			return nil
		})
	}()
	<-h.subscribed
	for got.Load() < 3 {
		time.Sleep(2 * time.Millisecond)
	}
	// The connection is still usable for something else meanwhile.
	if _, err := cn.Do(context.Background(), Request{Op: "status"}); err != nil {
		t.Fatalf("status beside a subscription: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	// Cancelling stops the daemon's side: nothing keeps sampling for nobody.
	deadline := time.Now().Add(2 * time.Second)
	for h.streaming.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the daemon kept the subscription running after it was cancelled")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAStreamTheDaemonEnds(t *testing.T) {
	c := serve(t, &stub{})
	cn, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cn.Close()
	n := 0
	err = cn.Subscribe(context.Background(), Subscription{Topics: []string{TopicStats}, Backlog: 4}, func(Frame) error { n++; return nil })
	if err != nil || n != 4 {
		t.Fatalf("a finite stream delivered %d frames, err %v; want 4 and nil", n, err)
	}
	if err := cn.Subscribe(context.Background(), Subscription{}, func(Frame) error { return nil }); err == nil || !strings.Contains(err.Error(), "at least one topic") {
		t.Fatalf("subscribing to nothing: %v", err)
	}
}

// Closing the connection ends its subscriptions on the daemon's side too.
func TestHangingUpEndsSubscriptions(t *testing.T) {
	h := &stub{subscribed: make(chan struct{}, 4)}
	c := serve(t, h)
	cn, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	go cn.Subscribe(context.Background(), Subscription{Topics: []string{TopicStats}}, func(Frame) error { return nil })
	<-h.subscribed
	cn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for h.streaming.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("a subscription outlived its connection")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
