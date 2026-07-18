// Diagnostic rendering: positioned errors, dedupe, fix hints, and the
// "did you mean" suggester.
package config

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/agext/levenshtein"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
)

// nearest returns the closest candidate within a small edit distance, or "".
func nearest(s string, candidates []string) string {
	best, bestD := "", 1<<30
	for _, c := range candidates {
		if d := levenshtein.Distance(s, c, nil); d < bestD {
			best, bestD = c, d
		}
	}
	if bestD <= 3 {
		return best
	}
	return ""
}

// posErr builds a single positioned diagnostic (file/line/snippet) so validation
// errors point at the offending block, like HCL's own syntax errors.
func posErr(parser *hclparse.Parser, rng hcl.Range, summary, detail string) error {
	r := rng
	return diagError(parser, hcl.Diagnostics{{
		Severity: hcl.DiagError,
		Summary:  summary,
		Detail:   detail,
		Subject:  &r,
	}})
}

// diagError renders HCL diagnostics into a single Go error, after augmenting any
// with a doze-specific fix hint. The caller (realMain) already prints an
// "error:" prefix, so the wrapper stays a plain framing line — no second colon
// stacked in front of HCL's own "Error:" headers. Duplicate diagnostics (the
// same typo can surface three times through nested evaluation) are collapsed,
// and the trailing blank lines HCL emits are trimmed.
func diagError(parser *hclparse.Parser, diags hcl.Diagnostics) error {
	diags = dedupeDiags(diags)
	addFixHints(diags)
	var buf bytes.Buffer
	wr := hcl.NewDiagnosticTextWriter(&buf, parser.Files(), 0, false)
	_ = wr.WriteDiagnostics(diags)
	return fmt.Errorf("invalid config —\n\n%s", strings.TrimRight(buf.String(), "\n \t"))
}

// dedupeDiags drops diagnostics that repeat the same summary, detail, and source
// range — a single mistake evaluated through multiple passes otherwise prints
// two or three identical blocks.
func dedupeDiags(diags hcl.Diagnostics) hcl.Diagnostics {
	seen := make(map[string]bool, len(diags))
	out := make(hcl.Diagnostics, 0, len(diags))
	for _, d := range diags {
		var rng string
		if d.Subject != nil {
			rng = d.Subject.String()
		}
		key := d.Summary + "\x00" + d.Detail + "\x00" + rng
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	return out
}

// addFixHints appends an actionable fix to HCL grammar errors that are easy to hit
// but cryptic on their own. The common one for the short health/restart blocks is
// writing several arguments on one line, which HCL's single-line block grammar
// forbids — the raw message never says "use multiple lines", so we add that.
func addFixHints(diags hcl.Diagnostics) {
	const hint = "\n\nHCL single-line blocks take exactly one argument; put each on its " +
		"own line instead:\n" +
		"    health {\n" +
		"      http     = \"http://localhost:8080/health\"\n" +
		"      interval = \"2s\"\n" +
		"    }"
	for _, d := range diags {
		if d == nil {
			continue
		}
		if strings.Contains(d.Summary, "single-argument block") && !strings.Contains(d.Detail, "own line") {
			d.Detail += hint
		}
	}
}
