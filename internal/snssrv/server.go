// Package snssrv is a ground-up, pure-Go SNS-compatible server: no LocalStack,
// no JVM. It speaks the SNS Query/XML protocol, persists topics and
// subscriptions to a bbolt store, supports message-attribute filter policies and
// raw message delivery, and fans published messages out to SQS queues (over the
// backing instance's socket, passed in DOZE_SQS_SOCKET) and to http(s) webhooks
// with the subscription-confirmation handshake. It registers itself with
// awslocal; the engine/sns driver fronts it.
package snssrv

import (
	"io"
	"net/http"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"

	"github.com/nerdmenot/doze/internal/awslocal"
)

func init() { awslocal.RegisterServer("sns", New) }

// New opens the bbolt store under datadir and returns the SNS HTTP handler plus
// the DB as the io.Closer. The SQS backend socket (for sqs-protocol delivery) is
// read from DOZE_SQS_SOCKET, set by the engine/sns driver when a backing SQS
// instance is configured.
func New(datadir string) (http.Handler, io.Closer, error) {
	db, err := bolt.Open(filepath.Join(datadir, "sns.bolt"), 0o600, nil)
	if err != nil {
		return nil, nil, err
	}
	return &server{store: newStore(db), sqsSocket: os.Getenv("DOZE_SQS_SOCKET")}, db, nil
}

type server struct {
	store     *Store
	sqsSocket string // backing SQS instance's unix socket, "" if none
}

func (srv *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, errInvalid(err.Error()))
		return
	}
	action := r.Form.Get("Action")
	if action == "" {
		writeError(w, &apiError{Code: "MissingAction", Status: 400, Msg: "no Action specified"})
		return
	}
	h, ok := dispatch[action]
	if !ok {
		writeError(w, &apiError{Code: "InvalidAction", Status: 400, Msg: "unsupported action: " + action})
		return
	}
	result, err := h(srv, r.Form, r.Host)
	if err != nil {
		writeError(w, err)
		return
	}
	writeResult(w, action, result)
}
