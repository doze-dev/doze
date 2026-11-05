package fixture

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
)

// longNames are the shop's names as the LongText board writes them: a
// monorepo with a long path, services named the way real ones are, and a
// domain under a long company name.
var longNames = [][2]string{
	{"shop", "customer-success-platform-monorepo"},
	{"~/code/customer-success-platform-monorepo", "~/Work/clients/acme-holdings/customer-success-platform-monorepo/services"},
	{"api", "customer-success-api-gateway"},
	{"web", "web-frontend-nextjs-storefront"},
	{"db", "primary-postgres-cluster-db"},
	{"cache", "session-cache"},
	{"dev.customer-success-platform-monorepo.example", "customer-success-platform.dev.acme-holdings.co.uk"},
}

// Long is the shop with long names everywhere, for the LongText board:
// the same stack, every name and address rewritten, so truncation and
// wrapping can be looked at with real data behind them.
func Long(now time.Time) *fakedaemon.Fake {
	f := Shop(now)
	f.Snap = rename[control.Snapshot](f.Snap)
	f.Frames = rename[[]control.Frame](f.Frames)
	f.Lines = rename[map[string][]control.LogFrame](f.Lines)
	return f
}

// rename rewrites every name in v, through its JSON, so no field is missed.
func rename[T any](v T) T {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	for _, pair := range longNames {
		// A bare name is matched whole; a path or domain carries its own edges.
		pat := regexp.QuoteMeta(pair[0])
		if isWord(pair[0]) {
			pat = `\b` + pat + `\b`
		}
		b = regexp.MustCompile(pat).ReplaceAll(b, []byte(pair[1]))
	}
	var out T
	if json.Unmarshal(b, &out) != nil {
		return v
	}
	return out
}

// isWord says a name is letters and digits only, so \b can bound it.
func isWord(s string) bool {
	for _, r := range s {
		word := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_'
		if !word {
			return false
		}
	}
	return s != ""
}
