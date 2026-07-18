// Server side of the control IPC: a unix-socket listener that decodes
// Requests, dispatches them to a Handler, and encodes Responses (including
// the held streaming connections for logs-follow and events).
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"sync"
)

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
	case "add":
		view, err := s.h.AddInstance(ctx, req.Block)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
			resp.Instances = []InstanceView{view}
		}
	case "remove":
		if err := s.h.RemoveInstance(ctx, req.DB, req.Wipe); err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}
	case "events":
		s.streamEvents(ctx, conn)
		return // streamEvents owns the connection for its lifetime
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

// streamEvents holds the connection open and forwards EventFrame lines from the
// handler until the client disconnects or the daemon shuts down. Like
// streamLogs, the first frame is an {ok:true} ack.
func (s *Server) streamEvents(ctx context.Context, conn net.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
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
	_ = s.h.StreamEvents(ctx, func(f EventFrame) error {
		mu.Lock()
		defer mu.Unlock()
		return enc.Encode(f)
	})
}
