package health

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nerdmenot/doze/internal/engine"
)

func TestProbeHTTP(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	r := &engine.Ready{Kind: "http", Target: ok.URL, Timeout: time.Second}
	if err := Probe(context.Background(), r, nil); err != nil {
		t.Fatalf("2xx should pass: %v", err)
	}
	r.Target = bad.URL
	if err := Probe(context.Background(), r, nil); err == nil {
		t.Fatal("5xx should fail the http probe")
	}
}

func TestProbeTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	r := &engine.Ready{Kind: "tcp", Target: ln.Addr().String(), Timeout: time.Second}
	if err := Probe(context.Background(), r, nil); err != nil {
		t.Fatalf("listening port should accept: %v", err)
	}
	_ = ln.Close()
	r2 := &engine.Ready{Kind: "tcp", Target: "127.0.0.1:1", Timeout: 200 * time.Millisecond}
	if err := Probe(context.Background(), r2, nil); err == nil {
		t.Fatal("a closed port should fail the tcp probe")
	}
}

func TestProbeExec(t *testing.T) {
	pass := &engine.Ready{Kind: "exec", Target: "true", Timeout: time.Second}
	if err := Probe(context.Background(), pass, nil); err != nil {
		t.Fatalf("`true` should pass: %v", err)
	}
	fail := &engine.Ready{Kind: "exec", Target: "false", Timeout: time.Second}
	if err := Probe(context.Background(), fail, nil); err == nil {
		t.Fatal("`false` should fail the exec probe")
	}
}

func TestProbeLogLine(t *testing.T) {
	r := &engine.Ready{Kind: "log_line", Target: "listening on", Timeout: time.Second}
	logs := func() []string { return []string{"starting up", "listening on :8080"} }
	if err := Probe(context.Background(), r, logs); err != nil {
		t.Fatalf("matching line should pass: %v", err)
	}
	none := func() []string { return []string{"starting up"} }
	if err := Probe(context.Background(), r, none); err == nil {
		t.Fatal("no matching line should fail the log_line probe")
	}
}

// A nil spec is a liveness check: alive after the grace window passes.
func TestWaitReadyLiveness(t *testing.T) {
	if err := WaitReady(context.Background(), nil, func() bool { return true }, nil); err != nil {
		t.Fatalf("alive process should pass liveness: %v", err)
	}
	if err := WaitReady(context.Background(), nil, func() bool { return false }, nil); err == nil {
		t.Fatal("a process that exited immediately should fail liveness")
	}
}
