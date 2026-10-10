// Server side of the control IPC: a unix-socket listener that speaks protocol
// 2. A connection opens with a hello, then carries any number of requests,
// each answered by frames tagged with the request's id — one frame for a unary
// request, a stream of them for subscribe, logs-follow and events.
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

// frameWriter serialises frames from the goroutines answering one connection.
type frameWriter struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (w *frameWriter) send(f Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enc.Encode(f)
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(bufio.NewReader(conn))
	w := &frameWriter{enc: json.NewEncoder(conn)}

	// The hello. A client from before protocol 2 sends a bare request here; it
	// reads the reply's "error" field, so it is told what is wrong in words.
	var first Request
	if err := dec.Decode(&first); err != nil {
		_ = w.send(Frame{Error: "bad request: " + err.Error()})
		return
	}
	if first.Hello == nil || first.Hello.Proto != ProtoVersion {
		spoke := "no protocol version"
		if first.Hello != nil {
			spoke = fmt.Sprintf("protocol %d", first.Hello.Proto)
		}
		_ = w.send(Frame{Error: fmt.Sprintf(
			"this daemon speaks control protocol %d and the client sent %s — they are different versions of doze; run `doze down` and try again",
			ProtoVersion, spoke)})
		return
	}
	if err := w.send(Frame{Hello: &Hello{Proto: ProtoVersion}}); err != nil {
		return
	}

	// A stream this connection started ends with it. A command does not: a
	// client that stops waiting for a slow wake has not asked for it to be
	// abandoned halfway, so commands run on the daemon's own context.
	daemonCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu      sync.Mutex
		running = map[uint64]context.CancelFunc{}
		wg      sync.WaitGroup
	)
	defer wg.Wait()
	defer cancel()
	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			return // the client went away
		}
		if req.Op == "cancel" {
			mu.Lock()
			if stop := running[req.ID]; stop != nil {
				stop()
			}
			mu.Unlock()
			continue
		}
		rctx, stop := context.WithCancel(ctx)
		mu.Lock()
		running[req.ID] = stop
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				mu.Lock()
				delete(running, req.ID)
				mu.Unlock()
				stop()
			}()
			s.dispatch(daemonCtx, rctx, req, w)
		}()
	}
}

// dispatch answers one request: a stream for the streaming ops, which lasts as
// long as ctx; one response frame for everything else, run on daemonCtx.
func (s *Server) dispatch(daemonCtx, ctx context.Context, req Request, w *frameWriter) {
	stream := func(run func(emit func(Frame) error) error) {
		if err := w.send(Frame{ID: req.ID, Response: &Response{OK: true}}); err != nil {
			return
		}
		emit := func(f Frame) error {
			f.ID = req.ID
			return w.send(f)
		}
		end := Frame{ID: req.ID, End: true}
		if err := run(emit); err != nil && ctx.Err() == nil {
			end.Error = err.Error()
		}
		_ = w.send(end)
	}
	switch {
	case req.Op == "subscribe":
		sub := Subscription{Topics: req.Topics, Names: req.Names, Since: req.Since, Backlog: req.Backlog}
		if req.Interval != "" {
			d, err := time.ParseDuration(req.Interval)
			if err != nil || d <= 0 {
				_ = w.send(Frame{ID: req.ID, Response: &Response{Error: fmt.Sprintf("subscribe: %q is not an interval", req.Interval)}})
				return
			}
			sub.Interval = d
		}
		if len(sub.Topics) == 0 {
			_ = w.send(Frame{ID: req.ID, Response: &Response{Error: "subscribe: name at least one topic"}})
			return
		}
		stream(func(emit func(Frame) error) error { return s.h.Subscribe(ctx, sub, emit) })
	case req.Op == "logs" && req.Follow:
		names := req.Names
		if len(names) == 0 && req.DB != "" {
			names = []string{req.DB}
		}
		stream(func(emit func(Frame) error) error {
			return s.h.StreamLogs(ctx, names, func(l LogFrame) error {
				return emit(Frame{Topic: TopicLogs, Log: &l})
			})
		})
	case req.Op == "events":
		stream(func(emit func(Frame) error) error {
			return s.h.StreamEvents(ctx, func(e EventFrame) error {
				return emit(Frame{Topic: TopicInstances, Instances: []InstanceView{e.Instance}})
			})
		})
	default:
		resp := s.unary(daemonCtx, req)
		_ = w.send(Frame{ID: req.ID, Response: &resp})
	}
}

// unary runs a request that has one answer.
func (s *Server) unary(ctx context.Context, req Request) Response {
	var resp Response
	done := func(err error) Response {
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}
		return resp
	}
	switch req.Op {
	case "status":
		resp = s.h.Status()
		resp.OK = true
		return resp
	case "up":
		return done(s.h.Up(ctx, req.DB))
	case "apply":
		return done(s.h.Apply(ctx, req.DB))
	case "destroy":
		return done(s.h.Destroy(ctx, req.DB))
	case "down", "sleep":
		return done(s.h.Down(ctx, req.DB))
	case "boot", "wake":
		return done(s.h.Boot(ctx, req.DB))
	case "restart":
		return done(s.h.Restart(ctx, req.DB))
	case "reset":
		return done(s.h.Reset(ctx, req.DB))
	case "logs":
		lines, err := s.h.Logs(req.DB)
		resp.Lines = lines
		return done(err)
	case "add":
		view, err := s.h.AddInstance(ctx, req.Block)
		if err == nil {
			resp.Instances = []InstanceView{view}
		}
		return done(err)
	case "remove":
		return done(s.h.RemoveInstance(ctx, req.DB, req.Wipe))
	case "keepawake", "pin":
		return done(s.h.KeepAwake(req.DB))
	case "resources":
		res, acts, err := s.h.Resources(ctx, req.DB)
		resp.Resources, resp.Actions = res, acts
		return done(err)
	case "admin":
		out, err := s.h.Admin(ctx, req.DB, req.Action, req.Resource, req.Input)
		resp.Result = out
		return done(err)
	default:
		resp.Error = "unknown op: " + req.Op
		return resp
	}
}
