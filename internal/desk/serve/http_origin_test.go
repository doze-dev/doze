package serve

import (
	"net/http/httptest"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	for _, c := range []struct {
		host, origin string
		ok           bool
	}{
		{"127.0.0.1:4000", "", true},
		{"127.0.0.1:4000", "http://127.0.0.1:4000", true},
		{"localhost:5173", "http://localhost:5173", true},
		{"127.0.0.1:4000", "https://evil.example", false},
		{"evil.example:4000", "", false},
		{"[::1]:4000", "", true},
	} {
		r := httptest.NewRequest("POST", "/api/do", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := sameOrigin(r); got != c.ok {
			t.Errorf("host %q origin %q: got %v, want %v", c.host, c.origin, got, c.ok)
		}
	}
}
