package proxy

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/doze-dev/doze-sdk/engine"
)

type countingRouter struct {
	mu                        sync.Mutex
	boots, acquires, releases int
}

func (r *countingRouter) Boot(context.Context, string) (engine.Endpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.boots++
	return engine.Endpoint{}, nil
}
func (r *countingRouter) Acquire(string) { r.mu.Lock(); r.acquires++; r.mu.Unlock() }
func (r *countingRouter) Release(string) { r.mu.Lock(); r.releases++; r.mu.Unlock() }

type anyDriver struct{ engine.Driver }

// A module's second listener has a unix socket for a backend: a connection to
// the public port wakes the instance and is spliced to that socket.
func TestAForwardReachesAUnixBackendAndWakesTheInstance(t *testing.T) {
	dir, err := os.MkdirTemp("", "fwd")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "console.sock")
	backend, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		c, err := backend.Accept()
		if err != nil {
			return
		}
		line, _ := bufio.NewReader(c).ReadString('\n')
		_, _ = c.Write([]byte("backend got " + line))
		_ = c.Close()
	}()

	pub, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &countingRouter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = New(r).ServeForward(ctx, pub, "events", anyDriver{}, sock) }()

	c, err := net.Dial("tcp", pub.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	got, _ := bufio.NewReader(c).ReadString('\n')
	if got != "backend got hello\n" {
		t.Fatalf("through the forward: %q", got)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.boots != 1 || r.acquires != 1 {
		t.Errorf("boots %d, acquires %d: a connection to a second port must wake and hold the instance", r.boots, r.acquires)
	}
}
