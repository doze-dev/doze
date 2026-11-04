package brand

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestTokensCSSIsCurrent fails when tokens.css was not regenerated after a
// change here, so the app cannot drift from the brand.
func TestTokensCSSIsCurrent(t *testing.T) {
	got, err := os.ReadFile("../desk/frontend/src/tokens.css")
	if err != nil {
		t.Fatalf("tokens.css is missing: run go generate ./internal/brand (%v)", err)
	}
	if string(got) != CSS() {
		t.Error("tokens.css is stale: run go generate ./internal/brand")
	}
}

// TestNoLiteralColoursOutsideTokens keeps every colour in one place: the
// frontend's own CSS may only use var(--…).
func TestNoLiteralColoursOutsideTokens(t *testing.T) {
	hex := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	for _, f := range []string{"../desk/frontend/src/app.css"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if hex.MatchString(line) {
				t.Errorf("%s:%d: a literal colour; use a token", f, i+1)
			}
		}
	}
}

func TestThemesAreComplete(t *testing.T) {
	for _, th := range []Theme{Light, Dark} {
		for _, v := range th.vars() {
			if strings.HasSuffix(v, ": ") {
				t.Errorf("%s theme: %s is empty", th.Name, v)
			}
		}
	}
}
