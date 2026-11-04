package web

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// The frontend's budgets. Light by rule (design D14): the page must paint
// fast on a cold web view and stay small enough to audit.
const (
	maxGzippedJS = 250 << 10 // every script, gzipped, together
	maxStatic    = 1 << 20   // everything in dist
)

// TestFrontendBudget holds the built frontend to its size budgets. It is
// skipped when the frontend is not built, so `go test ./...` without bun
// still passes; CI builds it first.
func TestFrontendBudget(t *testing.T) {
	if !Built() {
		t.Skip("frontend not built (make frontend)")
	}
	dist := Dist()
	var js, total int
	err := fs.WalkDir(dist, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(dist, p)
		if err != nil {
			return err
		}
		total += len(b)
		if strings.HasSuffix(p, ".js") {
			var z bytes.Buffer
			w := gzip.NewWriter(&z)
			_, _ = w.Write(b)
			_ = w.Close()
			js += z.Len()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("gzipped JS %d KB of %d; static %d KB of %d", js>>10, maxGzippedJS>>10, total>>10, maxStatic>>10)
	if js > maxGzippedJS {
		t.Errorf("the scripts gzip to %d KB; the budget is %d KB", js>>10, maxGzippedJS>>10)
	}
	if total > maxStatic {
		t.Errorf("dist is %d KB; the budget is %d KB", total>>10, maxStatic>>10)
	}
}

// TestNothingKeepsTheGPUBusy bans what makes a resting window cost CPU: blur,
// looping animation and WebGL (design D14, rule 5).
func TestNothingKeepsTheGPUBusy(t *testing.T) {
	if !Built() {
		t.Skip("frontend not built (make frontend)")
	}
	banned := []*regexp.Regexp{
		regexp.MustCompile(`backdrop-filter|filter:\s*blur`),
		regexp.MustCompile(`animation-iteration-count:\s*infinite|\binfinite\b`),
		regexp.MustCompile(`getContext\(["']webgl`),
	}
	err := fs.WalkDir(Dist(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !(strings.HasSuffix(p, ".css") || strings.HasSuffix(p, ".js")) {
			return err
		}
		b, _ := fs.ReadFile(Dist(), p)
		for _, re := range banned {
			if loc := re.FindIndex(b); loc != nil {
				t.Errorf("%s: %q", p, b[loc[0]:min(loc[1]+20, len(b))])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHandlerServesThePageForAnyRoute(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/", "/s/shop", "/s/shop/i/db"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>doze</title>") {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Body.String()[:min(80, rec.Body.Len())])
		}
	}
}
