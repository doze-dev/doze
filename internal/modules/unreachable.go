package modules

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/doze-dev/doze/internal/binaries"
)

// UnreachableError is a module that could not be fetched because the host it
// lives on did not answer: no DNS record, a refused or timed-out connection, a
// TLS failure, a 5xx. It is the network's (or the registry's) problem, never
// the config's, and it reads that way: the module, then the host and why.
type UnreachableError struct {
	Module string // the engine type, e.g. "valkey"
	Host   string // the host that did not answer
	// Registry is true when Host is the module registry, false for an artifact
	// host (GitHub Releases) the index pointed at.
	Registry bool
	Cause    string // what went wrong, in a few words ("no DNS answer")
	Err      error  // the transport error underneath
}

func (e *UnreachableError) Error() string {
	what := e.Host // an artifact host (GitHub Releases), not the registry
	if e.Registry {
		what = "the module registry"
	}
	return fmt.Sprintf("module %s could not be fetched: %s can't be reached (%s: %s)", e.Module, what, e.Host, e.Cause)
}

func (e *UnreachableError) Unwrap() error { return e.Err }

// IsUnreachable reports whether err is a module fetch that failed because its
// host could not be reached.
func IsUnreachable(err error) bool {
	var u *UnreachableError
	return errors.As(err, &u)
}

// asUnreachable returns err as an *UnreachableError when it is a transport
// failure (or a 5xx), and nil when it is anything else: a 404, a bad
// signature, a checksum mismatch, a release that does not fit.
func (m *Manager) asUnreachable(module string, err error) *UnreachableError {
	u := unreachable(module, err)
	if u != nil {
		u.Registry = u.Host == hostOf(m.base)
	}
	return u
}

func unreachable(module string, err error) *UnreachableError {
	if err == nil {
		return nil
	}
	var already *UnreachableError
	if errors.As(err, &already) {
		return already
	}
	var se *binaries.StatusError
	if errors.As(err, &se) {
		if se.Code < 500 {
			return nil
		}
		status := se.Status
		if http.StatusText(se.Code) == "" { // Go spells a 525 "525 status code 525"
			status = fmt.Sprintf("HTTP %d", se.Code)
		}
		return &UnreachableError{Module: module, Host: hostOf(se.URL), Cause: "it answered " + status, Err: err}
	}
	var ue *url.Error
	if !errors.As(err, &ue) {
		return nil
	}
	return &UnreachableError{Module: module, Host: hostOf(ue.URL), Cause: transportCause(ue.Err), Err: err}
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Hostname()
}

// transportCause says in a few words why a request got no answer.
func transportCause(err error) string {
	var dns *net.DNSError
	switch {
	case errors.As(err, &dns) && dns.IsNotFound:
		return "no DNS answer"
	case errors.As(err, &dns):
		return "DNS lookup failed: " + dns.Err
	case errors.Is(err, context.DeadlineExceeded), isTimeout(err):
		return "no answer in time"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
		return "no route to it (offline?)"
	}
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && strings.Contains(msg, "tls") {
		return "TLS failed: " + msg[i+2:]
	}
	return msg
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// ProbeModule is the module whose index CheckRegistry fetches: one that every
// registry publishes.
const ProbeModule = "valkey"

// CheckRegistry asks the registry for one module's index, within timeout, and
// reports whether it answered with one. nil means it did; otherwise the error
// says why not (an *UnreachableError when nothing answered). It reads no cache
// and writes nothing: it is doctor's question, "can this machine fetch
// modules right now?".
func (m *Manager) CheckRegistry(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	u := strings.TrimRight(m.base, "/") + "/" + DefaultNamespace + "/" + ProbeModule + "/index.yaml"
	var body []byte
	if path, ok := strings.CutPrefix(u, "file://"); ok {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body = b
	} else {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		resp, err := (&http.Client{Timeout: timeout}).Do(req)
		if err != nil {
			if un := m.asUnreachable(ProbeModule, err); un != nil {
				return un
			}
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			se := &binaries.StatusError{URL: u, Code: resp.StatusCode, Status: resp.Status}
			if un := m.asUnreachable(ProbeModule, se); un != nil {
				return un
			}
			return fmt.Errorf("%s answered %s for %s", hostOf(u), resp.Status, u)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return &UnreachableError{Module: ProbeModule, Host: hostOf(u), Cause: "the answer broke off: " + err.Error(), Err: err}
		}
		body = b
	}
	if looksLikeHTML(body) || !strings.Contains(string(body), "releases") {
		return fmt.Errorf("%s answered, but not with a module index (%s)", hostOf(u), u)
	}
	return nil
}
