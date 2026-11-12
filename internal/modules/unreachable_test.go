package modules

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dozeplugin "github.com/doze-dev/doze-sdk/plugin"
)

// A registry that does not answer is not a module that does not exist.
// "lookup registry.doze-dev.com: no such host" contains "no such", and was
// taken for a miss: the config then said "no module called valkey", blaming
// the user's file for a DNS outage (F3).
func TestUnreachableRegistryIsNotAMiss(t *testing.T) {
	refused := httptest.NewServer(http.NotFoundHandler())
	refusedURL := refused.URL
	refused.Close() // nothing listens there now: connection refused

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(525) // what Cloudflare answers when the origin's TLS fails
	}))
	defer down.Close()

	for _, tc := range []struct {
		name, base, cause string
	}{
		// .invalid never resolves (RFC 2606): the DNS outage, as on 2026-11-13.
		{"no DNS answer", "https://registry.invalid", "no DNS answer"},
		{"connection refused", refusedURL, "connection refused"},
		{"a 5xx", down.URL, "525"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManager(t, tc.base)
			if _, _, ok := m.Lookup("valkey"); ok {
				t.Fatal("Lookup found a module with no registry")
			}
			err := m.LastError("valkey")
			var un *UnreachableError
			if !errors.As(err, &un) {
				t.Fatalf("LastError = %v (%T), want an *UnreachableError — a nil here is read as 'no module called valkey'", err, err)
			}
			if un.Module != "valkey" || !un.Registry || !strings.Contains(un.Cause, tc.cause) {
				t.Errorf("unreachable = %+v, want module valkey, the registry, cause %q", un, tc.cause)
			}
			msg := err.Error()
			if !strings.Contains(msg, "module valkey could not be fetched: the module registry can't be reached") {
				t.Errorf("message = %q", msg)
			}
			if notPublished(err) {
				t.Error("an unreachable registry was classified as 'not published'")
			}
		})
	}
}

// A real miss stays a miss: a 404 from a registry that answered.
func TestMissingModuleIsStillAMiss(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	m := newTestManager(t, srv.URL)
	if _, _, ok := m.Lookup("nosuch"); ok {
		t.Fatal("found a module the registry does not have")
	}
	if err := m.LastError("nosuch"); err != nil {
		t.Errorf("a 404 is a miss, not a failure: LastError = %v", err)
	}
}

// doctor's registry check (F5): it fetches one module's index, briefly, and
// says why when it can't.
func TestCheckRegistry(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	base, _ := signedRegistry(t, "doze", ProbeModule, priv,
		release{version: "0.2.0", protocol: dozeplugin.ProtocolVersion, engines: []string{"8"}})
	if err := newTestManager(t, base).CheckRegistry(context.Background(), time.Second); err != nil {
		t.Errorf("a registry with the probe module: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/doze/valkey/index.yaml" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("schema: 1\nmodule: valkey\nreleases: {}\n"))
	}))
	defer srv.Close()
	if err := newTestManager(t, srv.URL).CheckRegistry(context.Background(), time.Second); err != nil {
		t.Errorf("over http: %v", err)
	}

	err := newTestManager(t, "https://registry.invalid").CheckRegistry(context.Background(), 2*time.Second)
	var un *UnreachableError
	if !errors.As(err, &un) || un.Cause != "no DNS answer" || un.Host != "registry.invalid" {
		t.Errorf("no DNS: err = %v, want unreachable (registry.invalid: no DNS answer)", err)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()
	start := time.Now()
	err = newTestManager(t, slow.URL).CheckRegistry(context.Background(), 200*time.Millisecond)
	if !errors.As(err, &un) || time.Since(start) > 2*time.Second {
		t.Errorf("a registry that never answers: err = %v after %s, want unreachable within the timeout", err, time.Since(start))
	}

	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<!doctype html><title>nope</title>"))
	}))
	defer html.Close()
	if err := newTestManager(t, html.URL).CheckRegistry(context.Background(), time.Second); err == nil || IsUnreachable(err) {
		t.Errorf("a host answering HTML is reachable but no registry: err = %v", err)
	}
}
