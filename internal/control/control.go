// Package control is the thin admin IPC between the `doze` CLI and a running
// doze daemon. It speaks newline-delimited JSON over a unix socket so commands
// like `status`, `stop`, and `dash` can reflect and steer live state.
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/doze-dev/doze/internal/registry"
)

// Request is a command sent from the CLI to the daemon.
type Request struct {
	Op string `json:"op"`           // "status", "up", "down"
	DB string `json:"db,omitempty"` // target database (empty = all, where meaningful)

	// Admin (builtin data ops): "resources" lists a builtin's sub-resources;
	// "admin" runs Action on Resource (with optional Input) — see engine.Admin.
	Action   string `json:"action,omitempty"`
	Resource string `json:"resource,omitempty"`
	Input    string `json:"input,omitempty"`

	// Follow turns the "logs" op into a held streaming connection that emits
	// LogFrame lines as they arrive (used by `doze up` and `doze logs -f`).
	Follow bool `json:"follow,omitempty"`
	// Names scopes a streaming "logs" follow to specific instances (empty = every
	// running backend). DB still works for a single target.
	Names []string `json:"names,omitempty"`
}

// LogFrame is one streamed, instance-tagged log line (logs follow op).
type LogFrame struct {
	Instance string `json:"instance"`
	Line     string `json:"line"`
}

// ResourceView is a serializable engine.Resource (a builtin's sub-resource).
type ResourceView struct {
	Kind   string            `json:"kind"`
	Name   string            `json:"name"`
	Status string            `json:"status"`
	Info   map[string]string `json:"info,omitempty"`
}

// ActionView is a serializable engine.Action (a builtin data operation).
type ActionView struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Kind        string `json:"kind"`
	Destructive bool   `json:"destructive,omitempty"`
	InputHint   string `json:"input_hint,omitempty"`
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
	IdleSince time.Time `json:"idle_since,omitempty"` // when Conns hit 0; drives the reap countdown
	RAM       int64     `json:"ram,omitempty"`        // resident bytes of the backend, 0 when reaped
	CPU       float64   `json:"cpu,omitempty"`        // CPU usage percent (one core = 100), 0 when reaped
	Endpoint  string    `json:"endpoint,omitempty"`   // client-facing address
	Domain    string    `json:"domain,omitempty"`     // local DNS name (mDNS), e.g. "orders-pg.local"
	URL       string    `json:"url,omitempty"`        // full connection string
	// Resource is the full, directly-addressable path when the client-facing
	// endpoint is a shared front door: an AWS built-in's resource URL/ARN
	// (http://s3.<stack>.doze/<bucket>, a queue URL, a topic ARN) or an
	// ingress process's :80 URL (http://<name>.<stack>.doze). Empty otherwise.
	Resource  string `json:"resource,omitempty"`
	EnvVar    string `json:"env_var,omitempty"`    // conventional env var (DATABASE_URL, …)
	DataDir   string `json:"data_dir,omitempty"`   // where this instance's data is written
	LastError string `json:"last_error,omitempty"` // most recent boot/crash failure
	Tainted   bool   `json:"tainted,omitempty"`    // last convergence failed/incomplete
	Declared  bool   `json:"declared"`
	Disabled  bool   `json:"disabled,omitempty"`   // declared with enabled = false (paused; no listener, not booted)
	KeepAwake bool   `json:"keep_awake,omitempty"` // pinned: exempt from the idle reaper
	// Group is the display heading for status/dash; empty means "infer from engine
	// category" (an explicit `group=` or a module address can set it later).
	Group string `json:"group,omitempty"`
	// RestartCount is how many times a supervised process has been re-booted after
	// an unexpected exit; 0 for DB/AWS backends.
	RestartCount int `json:"restart_count,omitempty"`
	// Healthy is the latest periodic liveness result for a supervised process:
	// nil = not probed (or not a process), else true/false.
	Healthy *bool `json:"healthy,omitempty"`
}

// Response is the daemon's reply.
type Response struct {
	OK          bool           `json:"ok"`
	Error       string         `json:"error,omitempty"`
	Listen      string         `json:"listen,omitempty"`
	IdleTimeout time.Duration  `json:"idle_timeout,omitempty"` // reap window, for the countdown
	Instances   []InstanceView `json:"instances,omitempty"`
	Lines       []string       `json:"lines,omitempty"`
	Resources   []ResourceView `json:"resources,omitempty"` // builtin sub-resources (resources op)
	Actions     []ActionView   `json:"actions,omitempty"`   // available data actions (resources op)
	Result      string         `json:"result,omitempty"`    // an admin action's result line (admin op)
}

// Handler implements the daemon-side operations.
type Handler interface {
	Status() Response
	Up(ctx context.Context, db string) error
	Down(ctx context.Context, db string) error
	Boot(ctx context.Context, db string) error
	Restart(ctx context.Context, db string) error
	Logs(db string) ([]string, error)
	// StreamLogs follows the named instances' logs (empty = all running backends),
	// calling emit for each new line until ctx is cancelled or emit returns an error
	// (the client disconnected).
	StreamLogs(ctx context.Context, names []string, emit func(LogFrame) error) error
	KeepAwake(db string) error // toggle the idle-reaper exemption for db
	Apply(ctx context.Context, db string) error
	Destroy(ctx context.Context, db string) error
	// Reset stops an instance and wipes its data + socket dirs so the next
	// connection re-provisions and re-converges — `doze reset` semantics, NOT
	// Destroy's drop-the-declared-objects sync lifecycle.
	Reset(ctx context.Context, db string) error
	// Resources lists a builtin instance's sub-resources and the data actions it
	// offers; empty (no error) when the engine has no admin capability.
	Resources(ctx context.Context, db string) ([]ResourceView, []ActionView, error)
	// Admin runs a data action on a resource, returning its result line.
	Admin(ctx context.Context, db, action, resource, input string) (string, error)
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
	case "apply":
		if err := s.h.Apply(ctx, req.DB); err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}
	case "destroy":
		if err := s.h.Destroy(ctx, req.DB); err != nil {
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
	case "reset":
		if err := s.h.Reset(ctx, req.DB); err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}
	case "logs":
		if req.Follow {
			s.streamLogs(ctx, conn, req)
			return // streamLogs owns the connection for its lifetime
		}
		lines, err := s.h.Logs(req.DB)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
			resp.Lines = lines
		}
	case "keepawake":
		if err := s.h.KeepAwake(req.DB); err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}
	case "resources":
		res, acts, err := s.h.Resources(ctx, req.DB)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
			resp.Resources = res
			resp.Actions = acts
		}
	case "admin":
		out, err := s.h.Admin(ctx, req.DB, req.Action, req.Resource, req.Input)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
			resp.Result = out
		}
	default:
		resp.Error = "unknown op: " + req.Op
	}
	_ = enc.Encode(resp)
}

// streamLogs holds the connection open and forwards LogFrame lines from the
// handler until the client disconnects or the daemon shuts down. The first frame
// is preceded by an {ok:true} ack so the client knows the stream is live.
func (s *Server) streamLogs(ctx context.Context, conn net.Conn, req Request) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Cancel when the client goes away (it sends nothing more, so any read result
	// other than blocking means EOF/close).
	go func() {
		buf := make([]byte, 1)
		for {
			if _, err := conn.Read(buf); err != nil {
				cancel()
				return
			}
		}
	}()

	enc := json.NewEncoder(conn)
	var mu sync.Mutex
	if err := func() error { mu.Lock(); defer mu.Unlock(); return enc.Encode(Response{OK: true}) }(); err != nil {
		return
	}
	names := req.Names
	if len(names) == 0 && req.DB != "" {
		names = []string{req.DB}
	}
	_ = s.h.StreamLogs(ctx, names, func(f LogFrame) error {
		mu.Lock()
		defer mu.Unlock()
		return enc.Encode(f)
	})
}

// ViewFromRegistry converts a registry instance into a serializable view.
func ViewFromRegistry(inst registry.Instance, engineType, version string, declared bool) InstanceView {
	return InstanceView{
		Name:         inst.Name,
		Engine:       engineType,
		State:        string(inst.State),
		Version:      version,
		PID:          inst.PID,
		Conns:        inst.Conns,
		StartedAt:    inst.StartedAt,
		IdleSince:    inst.IdleSince,
		LastError:    inst.LastError,
		Tainted:      inst.Tainted,
		Declared:     declared,
		KeepAwake:    inst.KeepAwake,
		RestartCount: inst.RestartCount,
		Healthy:      inst.Healthy,
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
