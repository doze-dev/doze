// Package sqssrv is a ground-up, pure-Go SQS-compatible server: no LocalStack,
// no JVM. It speaks both wire protocols (AWS JSON 1.0 used by modern SDKs and
// the legacy Query/XML protocol), persists to a bbolt store in the instance's
// data directory, and supports visibility timeout, delay, retention, message
// attributes, long polling, FIFO queues (group ordering + deduplication), and
// dead-letter redrive. It registers itself with awslocal; the engine/sqs driver
// fronts it.
package sqssrv

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/nerdmenot/doze/internal/awslocal"
)

// sweepInterval is how often the background janitor reclaims expired messages
// and stale dedup entries from write-only queues.
const sweepInterval = time.Minute

func init() { awslocal.RegisterServer("sqs", New) }

// New opens the bbolt store under datadir and returns the SQS HTTP handler plus
// a closer that stops the janitor and closes the DB.
func New(datadir string) (http.Handler, io.Closer, error) {
	db, err := bolt.Open(filepath.Join(datadir, "sqs.bolt"), 0o600, nil)
	if err != nil {
		return nil, nil, err
	}
	s := newStore(db)
	stop := make(chan struct{})
	go janitor(s, stop)
	return &server{store: s}, &closer{db: db, stop: stop}, nil
}

// closer stops the janitor goroutine, then closes the bbolt DB.
type closer struct {
	db   *bolt.DB
	stop chan struct{}
}

func (c *closer) Close() error {
	close(c.stop)
	return c.db.Close()
}

func janitor(s *Store, stop <-chan struct{}) {
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.Sweep()
		}
	}
}

type server struct {
	store *Store
}

func (srv *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, aerr := parseRequest(r)
	if aerr != nil {
		writeError(w, r.Header.Get("X-Amz-Target") != "", aerr)
		return
	}
	h, ok := handlers[req.action]
	if !ok {
		writeError(w, req.json, &apiError{Code: "InvalidAction", Status: 400, Msg: "unsupported action: " + req.action})
		return
	}
	result, err := h(srv.store, req)
	if err != nil {
		writeError(w, req.json, err)
		return
	}
	writeResult(w, req, req.action, result)
}

// parseRequest decodes either protocol into a request. JSON is signalled by the
// X-Amz-Target header (AmazonSQS.<Action>); otherwise it is the Query protocol.
func parseRequest(r *http.Request) (*request, *apiError) {
	host := r.Host
	if target := r.Header.Get("X-Amz-Target"); target != "" {
		action := target
		if i := strings.LastIndex(target, "."); i >= 0 {
			action = target[i+1:]
		}
		body, _ := io.ReadAll(r.Body)
		var obj map[string]json.RawMessage
		if len(body) > 0 {
			if err := json.Unmarshal(body, &obj); err != nil {
				return nil, &apiError{Code: "InvalidRequest", Status: 400, Msg: "invalid JSON body: " + err.Error()}
			}
		}
		return &request{action: action, json: true, host: host, p: params{obj: obj}}, nil
	}
	if err := r.ParseForm(); err != nil {
		return nil, &apiError{Code: "InvalidRequest", Status: 400, Msg: err.Error()}
	}
	action := r.Form.Get("Action")
	if action == "" {
		return nil, &apiError{Code: "MissingAction", Status: 400, Msg: "no Action specified"}
	}
	return &request{action: action, json: false, host: host, p: params{form: r.Form}}, nil
}
