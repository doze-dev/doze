// Package control is the thin admin IPC between the `doze` CLI and a running
// `doze serve` daemon. It speaks newline-delimited JSON over a unix socket so
// commands like `status`, `down`, and `dash` can reflect and steer live state.
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/nerdmenot/doze/internal/registry"
)

// Request is a command sent from the CLI to the daemon.
type Request struct {
	Op string `json:"op"`           // "status", "up", "down"
	DB string `json:"db,omitempty"` // target database (empty = all, where meaningful)
}

// InstanceView is a serializable snapshot of one backend's state.
type InstanceView struct {
	Name      string    `json:"name"`
	Engine    string    `json:"engine"`
	State     string    `json:"state"`
	Version   string    `json:"version"`
	PID       int       `json:"pid"`
	Conns     int       `json:"conns"`
	StartedAt time.Time `json:"started_at"`
	Endpoint  string    `json:"endpoint,omitempty"`   // client-facing address
	LastError string    `json:"last_error,omitempty"` // most recent boot/crash failure
	Declared  bool      `json:"declared"`
}

// Response is the daemon's reply.
type Response struct {
	OK        bool           `json:"ok"`
	Error     string         `json:"error,omitempty"`
	Listen    string         `json:"listen,omitempty"`
	Instances []InstanceView `json:"instances,omitempty"`
	Lines     []string       `json:"lines,omitempty"`
}

// Handler implements the daemon-side operations.
type Handler interface {
	Status() Response
	Up(ctx context.Context, db string) error
	Down(ctx context.Context, db string) error
	Boot(ctx context.Context, db string) error
	Restart(ctx context.Context, db string) error
	Logs(db string) ([]string, error)
}

// Server listens on a unix socket and dispatches requests to a Handler.
type Server struct {
	path string
	h    Handler
	ln   net.Listener
}

// NewServer binds the control socket at path.
func NewServer(path string, h Handler) (*Server, error) {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	return &Server{path: path, h: h, ln: ln}, nil
}

// Serve handles control connections until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) {
	go func() {
		<-ctx.Done()
		_ = s.ln.Close()
		_ = os.Remove(s.path)
	}()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(bufio.NewReader(conn))
	enc := json.NewEncoder(conn)
	var req Request
	if err := dec.Decode(&req); err != nil {
		_ = enc.Encode(Response{Error: "bad request: " + err.Error()})
		return
	}
	var resp Response
	switch req.Op {
	case "status":
		resp = s.h.Status()
		resp.OK = true
	case "up":
		if err := s.h.Up(ctx, req.DB); err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}
	case "down":
		if err := s.h.Down(ctx, req.DB); err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}
	case "boot":
		if err := s.h.Boot(ctx, req.DB); err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}
	case "restart":
		if err := s.h.Restart(ctx, req.DB); err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}
	case "logs":
		lines, err := s.h.Logs(req.DB)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
			resp.Lines = lines
		}
	default:
		resp.Error = "unknown op: " + req.Op
	}
	_ = enc.Encode(resp)
}

// ViewFromRegistry converts a registry instance into a serializable view.
func ViewFromRegistry(inst registry.Instance, engineType, version string, declared bool) InstanceView {
	return InstanceView{
		Name:      inst.Name,
		Engine:    engineType,
		State:     string(inst.State),
		Version:   version,
		PID:       inst.PID,
		Conns:     inst.Conns,
		StartedAt: inst.StartedAt,
		LastError: inst.LastError,
		Declared:  declared,
	}
}

// --- client ---

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
