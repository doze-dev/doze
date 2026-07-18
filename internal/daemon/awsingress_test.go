package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// backend returns an httptest server that echoes a fixed tag plus the request
// path, and its host:port.
func backend(t *testing.T, tag string) (string, func()) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, tag+" "+r.URL.Path)
	}))
	return s.Listener.Addr().String(), s.Close
}

// TestIngressAWSSingleForward: the aws host is exactly one hop — every path,
// every method, forwarded whole to the instance's backend (which is the entire
// doze-aws stack: gateway + /_console).
func TestIngressAWSSingleForward(t *testing.T) {
	a := newAWSRouter(t.TempDir())
	target, closeB := backend(t, "STACK")
	defer closeB()
	rt := awsRoute{Target: target}

	for _, path := range []string{"/", "/uploads/receipt.json", "/_console/", "/000000000000/emails"} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		a.serve(rec, req, rt)
		if rec.Code != 200 {
			t.Fatalf("%s: status %d", path, rec.Code)
		}
		if got := rec.Body.String(); got != "STACK "+path {
			t.Fatalf("%s: body %q — the forward must not rewrite the path", path, got)
		}
	}
}

// TestIngressAWSNoBackend: a route without a target answers 502, not a panic.
func TestIngressAWSNoBackend(t *testing.T) {
	a := newAWSRouter(t.TempDir())
	rec := httptest.NewRecorder()
	a.serve(rec, httptest.NewRequest("POST", "/", strings.NewReader("{}")), awsRoute{})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}
