package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/doze-dev/doze/internal/desk/view"
)

// Handler serves the service over HTTP, for the browser: POST /api/<call>
// with a JSON body, and GET /events as server-sent events carrying the
// pushes. Wails is not involved; the page's transport.ts picks this when
// it finds no Wails runtime.
func Handler(s *Service) http.Handler {
	mux := http.NewServeMux()
	sse := &broadcaster{subs: map[chan event]struct{}{}}
	s.SetEmitter(sse.emit)
	mux.HandleFunc("GET /events", sse.serve)
	call := func(name string, fn func(json.RawMessage) (any, error)) {
		mux.HandleFunc("POST /api/"+name, func(w http.ResponseWriter, r *http.Request) {
			var body json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			out, err := fn(body)
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(out)
		})
	}
	call("stacks", func(json.RawMessage) (any, error) { return s.Stacks(), nil })
	call("pending", func(json.RawMessage) (any, error) { return s.Pending(), nil })
	call("home", func(b json.RawMessage) (any, error) {
		var a struct{ Key string }
		if err := json.Unmarshal(b, &a); err != nil {
			return nil, err
		}
		return s.Home(a.Key), nil
	})
	call("instance", func(b json.RawMessage) (any, error) {
		var a struct{ Key, Name string }
		if err := json.Unmarshal(b, &a); err != nil {
			return nil, err
		}
		return s.Instance(a.Key, a.Name), nil
	})
	call("version", func(json.RawMessage) (any, error) { return s.Version(), nil })
	call("logs", func(b json.RawMessage) (any, error) {
		var a struct {
			Key   string
			Query view.LogsQuery
		}
		if err := json.Unmarshal(b, &a); err != nil {
			return nil, err
		}
		return s.Logs(a.Key, a.Query), nil
	})
	call("focus", func(b json.RawMessage) (any, error) {
		var f Focus
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, err
		}
		s.SetFocus(f)
		return map[string]bool{"ok": true}, nil
	})
	call("do", func(b json.RawMessage) (any, error) {
		var a struct{ Key, Verb, Target string }
		if err := json.Unmarshal(b, &a); err != nil {
			return nil, err
		}
		if err := s.Do(a.Key, a.Verb, a.Target); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
	return mux
}

type event struct {
	name string
	data any
}

// broadcaster fans pushes out to every open /events stream.
type broadcaster struct {
	mu   sync.Mutex
	subs map[chan event]struct{}
}

func (b *broadcaster) emit(name string, data any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- event{name, data}:
		default: // a reader that cannot keep up misses one; the next push catches it up
		}
	}
}

func (b *broadcaster) serve(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ch := make(chan event, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}()
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-ch:
			data, err := json.Marshal(e.data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.name, data)
			fl.Flush()
		}
	}
}
