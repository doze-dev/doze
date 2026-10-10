// Client side of the control IPC. Conn is one connection to a daemon, carrying
// any number of requests and streams at once. Client is the convenience over
// it for a caller that wants one answer: it dials, asks, and hangs up.
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// Client dials a daemon's control socket.
type Client struct {
	path string
	// Name identifies this client in the hello ("cli" when empty).
	Name string
}

// NewClient returns a client for the socket at path.
func NewClient(path string) *Client { return &Client{path: path} }

// Available reports whether a daemon is listening.
func (c *Client) Available() bool {
	conn, err := net.DialTimeout("unix", c.path, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Conn is an open, greeted connection to a daemon. It is safe for concurrent
// use: every request gets its own id and its own frames.
type Conn struct {
	conn net.Conn

	wmu sync.Mutex // one request on the wire at a time
	enc *json.Encoder

	mu      sync.Mutex
	nextID  uint64
	waiting map[uint64]*waiter
	err     error         // why the connection ended
	closed  chan struct{} // closed when the read loop ends
}

// waiter is one request's inbox. gone is closed when the request stops
// listening, so a frame that arrives late is dropped instead of blocking the
// frames behind it.
type waiter struct {
	ch   chan Frame
	gone chan struct{}
}

// Dial connects and exchanges hellos. A daemon that speaks another protocol
// version is an error that says so.
func (c *Client) Dial(ctx context.Context) (*Conn, error) {
	var d net.Dialer
	dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	nc, err := d.DialContext(dctx, "unix", c.path)
	if err != nil {
		return nil, err
	}
	name := c.Name
	if name == "" {
		name = "cli"
	}
	cn := &Conn{conn: nc, enc: json.NewEncoder(nc), waiting: map[uint64]*waiter{}, closed: make(chan struct{})}
	dec := json.NewDecoder(bufio.NewReader(nc))

	_ = nc.SetDeadline(time.Now().Add(5 * time.Second))
	if err := cn.enc.Encode(Request{Hello: &Hello{Proto: ProtoVersion, Client: name}}); err != nil {
		nc.Close()
		return nil, err
	}
	var hello Frame
	if err := dec.Decode(&hello); err != nil {
		nc.Close()
		return nil, fmt.Errorf("the daemon did not answer the hello (it may be an older doze — run `doze down` and try again): %w", err)
	}
	if hello.Error != "" {
		nc.Close()
		return nil, errors.New(hello.Error)
	}
	if hello.Hello == nil || hello.Hello.Proto != ProtoVersion {
		nc.Close()
		return nil, fmt.Errorf("the daemon speaks a different control protocol than this doze (%d) — run `doze down` and try again", ProtoVersion)
	}
	_ = nc.SetDeadline(time.Time{})
	go cn.read(dec)
	return cn, nil
}

// read delivers each frame to the request waiting for it, until the
// connection ends.
func (cn *Conn) read(dec *json.Decoder) {
	var err error
	for {
		var f Frame
		if err = dec.Decode(&f); err != nil {
			break
		}
		cn.mu.Lock()
		w := cn.waiting[f.ID]
		cn.mu.Unlock()
		if w != nil {
			select {
			case w.ch <- f:
			case <-w.gone:
			}
		}
	}
	cn.mu.Lock()
	cn.err = err
	cn.mu.Unlock()
	close(cn.closed)
}

// Close hangs up. Every request and stream in flight ends.
func (cn *Conn) Close() error { return cn.conn.Close() }

// start sends req under a fresh id and returns the channel its frames arrive on.
func (cn *Conn) start(req Request) (uint64, chan Frame, error) {
	cn.mu.Lock()
	cn.nextID++
	id := cn.nextID
	ch := make(chan Frame, 64)
	cn.waiting[id] = &waiter{ch: ch, gone: make(chan struct{})}
	cn.mu.Unlock()

	req.ID = id
	cn.wmu.Lock()
	err := cn.enc.Encode(req)
	cn.wmu.Unlock()
	if err != nil {
		cn.forget(id)
		return 0, nil, err
	}
	return id, ch, nil
}

func (cn *Conn) forget(id uint64) {
	cn.mu.Lock()
	if w := cn.waiting[id]; w != nil {
		close(w.gone)
		delete(cn.waiting, id)
	}
	cn.mu.Unlock()
}

func (cn *Conn) gone() error {
	cn.mu.Lock()
	defer cn.mu.Unlock()
	if cn.err != nil {
		return fmt.Errorf("the connection to the daemon closed: %w", cn.err)
	}
	return errors.New("the connection to the daemon closed")
}

// Do sends a unary request and waits for its response. Giving up (ctx) does
// not stop the daemon doing what was asked.
func (cn *Conn) Do(ctx context.Context, req Request) (Response, error) {
	id, ch, err := cn.start(req)
	if err != nil {
		return Response{}, err
	}
	defer cn.forget(id)
	select {
	case f := <-ch:
		if f.Response == nil {
			return Response{}, fmt.Errorf("the daemon answered %q with no response", req.Op)
		}
		if f.Response.Error != "" {
			return *f.Response, errors.New(f.Response.Error)
		}
		return *f.Response, nil
	case <-ctx.Done():
		return Response{}, ctx.Err()
	case <-cn.closed:
		return Response{}, cn.gone()
	}
}

// Stream sends a streaming request and calls on for each frame until the
// daemon ends the stream, on returns an error, or ctx is done — in which case
// the daemon is told to stop and Stream returns nil.
func (cn *Conn) Stream(ctx context.Context, req Request, on func(Frame) error) error {
	id, ch, err := cn.start(req)
	if err != nil {
		return err
	}
	defer cn.forget(id)
	cancel := func() {
		cn.wmu.Lock()
		_ = cn.enc.Encode(Request{ID: id, Op: "cancel"})
		cn.wmu.Unlock()
	}
	acked := false
	for {
		select {
		case f := <-ch:
			switch {
			case !acked:
				if f.Response == nil {
					return fmt.Errorf("the daemon answered %q with no acknowledgement", req.Op)
				}
				if f.Response.Error != "" {
					return errors.New(f.Response.Error)
				}
				acked = true
			case f.End:
				if f.Error != "" {
					return errors.New(f.Error)
				}
				return nil
			default:
				if err := on(f); err != nil {
					cancel()
					return err
				}
			}
		case <-ctx.Done():
			cancel()
			return nil
		case <-cn.closed:
			if ctx.Err() != nil {
				return nil
			}
			return cn.gone()
		}
	}
}

// Subscribe opens a subscription and calls on for each frame; see Stream.
func (cn *Conn) Subscribe(ctx context.Context, sub Subscription, on func(Frame) error) error {
	req := Request{Op: "subscribe", Topics: sub.Topics, Names: sub.Names, Since: sub.Since, Backlog: sub.Backlog}
	if sub.Interval > 0 {
		req.Interval = sub.Interval.String()
	}
	return cn.Stream(ctx, req, on)
}

// --- one-shot helpers: dial, ask, hang up ---

// Do sends a request and returns the response, giving up after a minute.
func (c *Client) Do(req Request) (Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return c.DoContext(ctx, req)
}

// DoContext sends a request bounded by ctx instead of the fixed minute — for
// long synchronous ops like waking an app whose command must compile first.
func (c *Client) DoContext(ctx context.Context, req Request) (Response, error) {
	cn, err := c.Dial(ctx)
	if err != nil {
		return Response{}, err
	}
	defer cn.Close()
	return cn.Do(ctx, req)
}

// Stream opens a connection for the logs-follow op and invokes onLine for each
// line until ctx is cancelled or the connection closes.
func (c *Client) Stream(ctx context.Context, req Request, onLine func(LogFrame)) error {
	cn, err := c.Dial(ctx)
	if err != nil {
		return err
	}
	defer cn.Close()
	return cn.Stream(ctx, req, func(f Frame) error {
		if f.Log != nil {
			onLine(*f.Log)
		}
		return nil
	})
}

// StreamEvents opens a connection for the events op and invokes onEvent for
// each instance transition until ctx is cancelled or the connection closes.
func (c *Client) StreamEvents(ctx context.Context, onEvent func(EventFrame)) error {
	cn, err := c.Dial(ctx)
	if err != nil {
		return err
	}
	defer cn.Close()
	return cn.Stream(ctx, Request{Op: "events"}, func(f Frame) error {
		for _, inst := range f.Instances {
			onEvent(EventFrame{Instance: inst})
		}
		return nil
	})
}

// Add wires a rendered HCL block into the running stack, returning the new
// instance's view.
func (c *Client) Add(ctx context.Context, block string) (InstanceView, error) {
	resp, err := c.DoContext(ctx, Request{Op: "add", Block: block})
	if err != nil {
		return InstanceView{}, err
	}
	if len(resp.Instances) == 0 {
		return InstanceView{}, fmt.Errorf("add: daemon returned no instance")
	}
	return resp.Instances[0], nil
}

// Remove tears an instance down (optionally wiping its data).
func (c *Client) Remove(ctx context.Context, name string, wipe bool) error {
	_, err := c.DoContext(ctx, Request{Op: "remove", DB: name, Wipe: wipe})
	return err
}
