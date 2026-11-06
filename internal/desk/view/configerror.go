package view

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/display"
)

// ConfigError is a running stack whose doze.hcl was saved and does not
// load, as the ConfigError board draws it: the stack keeps running as it
// was, and the page shows what is wrong and where.
type ConfigError struct {
	// Text is the banner's line: "doze.hcl changed 20 s ago and does not load".
	Text string `json:"text"`
	// Summary and Detail are the loader's words: "Unsuitable value type",
	// "a number is required".
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
	// Hint is what to write instead, for the mistakes doze knows: a number
	// in quotes, text without them, an unknown or missing argument.
	Hint string `json:"hint,omitempty"`
	// File is the config's path, Line the line it points at (0 when it
	// names none), Excerpt the lines around it.
	File    string    `json:"file"`
	Line    int       `json:"line,omitempty"`
	Excerpt []SrcLine `json:"excerpt,omitempty"`
}

// SrcLine is one line of the excerpt.
type SrcLine struct {
	N    int    `json:"n"`
	Text string `json:"text"`
	Bad  bool   `json:"bad,omitempty"`
}

var (
	reLine    = regexp.MustCompile(`line (\d+)`)
	reOnFile  = regexp.MustCompile(`on (\S+) line (\d+)`)
	reSummary = regexp.MustCompile(`(?m)^Error: (.+)$`)
)

// ConfigErrorOf lays out a load error for the file at path, changed at
// changed: the summary and the line from the loader's words, and the two
// lines either side of it from the file.
func ConfigErrorOf(path string, err error, changed, now time.Time) *ConfigError {
	if err == nil {
		return nil
	}
	msg := err.Error()
	ce := &ConfigError{File: path, Text: ConfigErrorText(changed, now)}
	if m := reSummary.FindStringSubmatch(msg); m != nil {
		ce.Summary = strings.TrimSpace(m[1])
	} else {
		ce.Summary = display.FirstLine(display.Humanise(err))
	}
	// The detail is the last line with words that is not the quoted source.
	lines := strings.Split(strings.TrimSpace(msg), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || reNumbered.MatchString(l) || strings.HasPrefix(l, "on ") {
			continue
		}
		if l != "Error: "+ce.Summary {
			ce.Detail = l
		}
		break
	}
	if m := reOnFile.FindStringSubmatch(msg); m != nil {
		// The line may be in a sibling *.doze.hcl, merged with doze.hcl.
		ce.Line, _ = strconv.Atoi(m[2])
		if f := m[1]; filepath.IsAbs(f) {
			ce.File = f
		} else {
			ce.File = filepath.Join(filepath.Dir(path), f)
		}
	} else if m := reLine.FindStringSubmatch(msg); m != nil {
		ce.Line, _ = strconv.Atoi(m[1])
	}
	ce.Hint = configHint(msg)
	if ce.Line > 0 {
		if b, err := os.ReadFile(ce.File); err == nil {
			src := strings.Split(string(b), "\n")
			for n := max(1, ce.Line-3); n <= min(len(src), ce.Line+2); n++ {
				ce.Excerpt = append(ce.Excerpt, SrcLine{N: n, Text: strings.ReplaceAll(src[n-1], "\t", "  "), Bad: n == ce.Line})
			}
		}
	}
	return ce
}

// reNumbered is the loader's quoted source line: "   5:   port = ...".
var reNumbered = regexp.MustCompile(`^\d+:`)

// ConfigErrorText is the banner's line for a file changed at changed.
func ConfigErrorText(changed, now time.Time) string {
	if changed.IsZero() {
		return "doze.hcl does not load"
	}
	return "doze.hcl changed " + display.Ago(now.Sub(changed)) + " ago and does not load"
}

// ConfigChanged is a running stack whose doze.hcl was saved after its
// daemon read it, and loads: the daemon runs the old file until the stack
// is brought down and up again.
type ConfigChanged struct {
	Text string `json:"text"` // "doze.hcl changed 2m ago"
}

// ConfigChangedOf says so when the file at mtime is newer than loaded.
func ConfigChangedOf(mtime, loaded, now time.Time) *ConfigChanged {
	if loaded.IsZero() || !mtime.After(loaded) {
		return nil
	}
	return &ConfigChanged{Text: "doze.hcl changed " + display.Ago(now.Sub(mtime)) + " ago, after the stack read it"}
}

// configHint is what to write instead, for the errors people meet most.
func configHint(msg string) string {
	switch {
	case strings.Contains(msg, "a number is required"):
		return "Write the number without quotes: port = 8081."
	case strings.Contains(msg, "a string is required"), strings.Contains(msg, "string required"):
		return "Put text in double quotes: run = \"pnpm dev\"."
	case strings.Contains(msg, "a bool is required"):
		return "Write true or false, without quotes."
	case strings.Contains(msg, "Unsupported argument"):
		return "That name is not one this block takes; check its spelling against the docs for the block."
	case strings.Contains(msg, "Missing required argument"):
		return "The block needs that argument; add it inside the block's braces."
	case strings.Contains(msg, "Invalid single-argument block"):
		return "A block on one line holds one argument; put each on its own line."
	case strings.Contains(msg, "Unclosed configuration block"), strings.Contains(msg, "Missing newline"):
		return "A { is not closed: every block ends with } on a line of its own."
	}
	return ""
}
