package serve

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/doze-dev/doze/internal/control/fixture"
	"github.com/doze-dev/doze/internal/dozeversion"
)

func TestUpdateSaysWhenANewerDozeIsOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.3.0","html_url":"https://example/r","published_at":"2026-11-10T10:00:00Z"}`))
	}))
	defer srv.Close()
	wasURL, wasV := latestURL, dozeversion.Current
	t.Cleanup(func() { latestURL, dozeversion.Current = wasURL, wasV })
	latestURL = srv.URL
	h, _ := newHub(t, fixture.Shop(epoch))
	s := New(h, nil)

	dozeversion.Current = "0.2.1"
	if u := s.Update(true); !u.Newer || u.Latest != "0.3.0" || u.URL != "https://example/r" {
		t.Errorf("0.2.1 = %+v", u)
	}
	dozeversion.Current = "0.3.0"
	if u := s.Update(true); u.Newer {
		t.Errorf("0.3.0 = %+v", u)
	}
	dozeversion.Current = "dev"
	if u := s.Update(true); !u.Dev || u.Newer {
		t.Errorf("dev = %+v", u)
	}
}
