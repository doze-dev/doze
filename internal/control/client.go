// Client side of the control IPC: dials a daemon's unix control socket for
// one-shot requests and held streaming connections (logs follow, events).
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client dials a daemon's control socket.
type Client struct {
	path string
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

// Do sends a request and returns the response.
func (c *Client) Do(req Request) (Response, error) {
	conn, err := net.DialTimeout("unix", c.path, 2*time.Second)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return Response{}, err
	}
	if resp.Error != "" {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}

// DoContext sends a request bounded by ctx instead of the fixed 60s Do deadline —
// for long synchronous ops like booting a process whose command must compile first.
func (c *Client) DoContext(ctx context.Context, req Request) (Response, error) {
	conn, err := net.DialTimeout("unix", c.path, 2*time.Second)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, err
	}
	if resp.Error != "" {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}

// Stream opens a held connection for a streaming op (logs follow) and invokes onLine
// for each LogFrame until ctx is cancelled, the connection closes, or onLine returns
// an error. The first message is an {ok}/{error} ack; an error ack is returned
// before any frames. The caller typically cancels ctx on Ctrl-C.
func (c *Client) Stream(ctx context.Context, req Request, onLine func(LogFrame)) error {
	conn, err := net.DialTimeout("unix", c.path, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Close the connection when the caller cancels, unblocking the decode loop.
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	dec := json.NewDecoder(bufio.NewReader(conn))
	// First frame is the ack.
	var ack Response
	if err := dec.Decode(&ack); err != nil {
		return err
	}
	if ack.Error != "" {
		return fmt.Errorf("%s", ack.Error)
	}
	for {
		var f LogFrame
		if err := dec.Decode(&f); err != nil {
			if ctx.Err() != nil {
				return nil // caller cancelled
			}
			return err
		}
		onLine(f)
	}
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

// StreamEvents opens a held connection for the events op and invokes onEvent for
// each EventFrame until ctx is cancelled, the connection closes, or the daemon
// stops. The first message is an {ok}/{error} ack.
func (c *Client) StreamEvents(ctx context.Context, onEvent func(EventFrame)) error {
	conn, err := net.DialTimeout("unix", c.path, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	if err := json.NewEncoder(conn).Encode(Request{Op: "events"}); err != nil {
		return err
	}
	dec := json.NewDecoder(bufio.NewReader(conn))
	var ack Response
	if err := dec.Decode(&ack); err != nil {
		return err
	}
	if ack.Error != "" {
		return fmt.Errorf("%s", ack.Error)
	}
	for {
		var f EventFrame
		if err := dec.Decode(&f); err != nil {
			if ctx.Err() != nil {
				return nil // caller cancelled
			}
			return err
		}
		onEvent(f)
	}
}
