package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"

	"github.com/doze-dev/doze-sdk/engine"
	_ "github.com/doze-dev/doze/engine/process" // the app engine
)

func init() { engine.Register(remoteFake{fakeDriver{}}) }

// remoteFake decodes its blocks the way an out-of-process module does: it
// re-parses the source doze hands it (pluginSource's rewrite, under the real
// filename) and reports what it finds. Label "db" fails with diagnostics, as a
// module built with a diagnostics-aware SDK does; "header" fails with one
// pinned to the block's header line; "old" fails with a bare string, as an
// older module does.
type remoteFake struct{ engine.Driver }

func (remoteFake) Type() string { return "remotefake" }

// sdkDiagError is the shape the SDK's client returns: the old flat string as
// Error(), the diagnostics under it.
type sdkDiagError struct {
	msg   string
	diags hcl.Diagnostics
}

func (e *sdkDiagError) Error() string { return e.msg }
func (e *sdkDiagError) Unwrap() error { return e.diags }

func (remoteFake) DecodeRemote(file []byte, filename, blockType, blockLabel string, _ map[string]cty.Value, _ string, _ engine.VersionSpec) (any, error) {
	f, diags := hclparse.NewParser().ParseHCL(file, filename)
	if diags.HasErrors() {
		return nil, diags
	}
	content, _, _ := f.Body.PartialContent(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{{Type: blockType, LabelNames: []string{"name"}}},
	})
	for _, b := range content.Blocks {
		if b.Labels[0] != blockLabel {
			continue
		}
		switch blockLabel {
		case "db":
			schema := &hcl.BodySchema{Attributes: []hcl.AttributeSchema{{Name: "color"}}}
			for _, name := range engine.CoreArguments { // the SDK strips these
				schema.Attributes = append(schema.Attributes, hcl.AttributeSchema{Name: name})
			}
			_, d := b.Body.Content(schema)
			if d.HasErrors() {
				return nil, &sdkDiagError{msg: d.Error(), diags: d}
			}
		case "header":
			d := hcl.Diagnostics{{Severity: hcl.DiagError, Summary: "this module needs a password", Subject: b.DefRange.Ptr()}}
			return nil, &sdkDiagError{msg: d.Error(), diags: d}
		case "old":
			return nil, errors.New("old.doze.hcl:9,3-9: Unsupported argument; An argument named \"colour\" is not expected here.")
		}
	}
	return &fakeConfig{}, nil
}

func mustFail(t *testing.T, src string, hooks *Hooks, want ...string) string {
	t.Helper()
	_, err := Parse([]byte(src), "doze.hcl", hooks)
	if err == nil {
		t.Fatalf("expected an error, got none for:\n%s", src)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error should contain %q, got:\n%v", w, err)
		}
	}
	return err.Error()
}

// An app's decode errors render like every other config error: file, line,
// and the offending line as a snippet (L5).
func TestAppErrorsArePositioned(t *testing.T) {
	for name, tc := range map[string]struct {
		src  string
		want []string
	}{
		"unknown argument": {`
app "api" {
  run   = "./api"
  bogus = 1
}`, []string{"Error: Unsupported argument", "on doze.hcl line 4", "bogus = 1"}},
		"wrong type": {`
app "api" {
  run   = "./api"
  watch = { a = 1 }
}`, []string{"on doze.hcl line 4", "watch = { a = 1 }"}},
		"old argument": {`
app "api" {
  run     = "./api"
  command = "x"
}`, []string{"Error: `command` is no longer an argument; use `run`", "on doze.hcl line 4", `command = "x"`}},
		"unplaceable": {`
app "api" {
  run = " "
}`, []string{"app \"api\": an app needs a non-empty `run`", "on doze.hcl line 2", `app "api" {`}},
	} {
		t.Run(name, func(t *testing.T) {
			got := mustFail(t, tc.src, nil, tc.want...)
			if strings.Count(got, "Error:") != 1 {
				t.Errorf("want one rendered diagnostic, got:\n%s", got)
			}
		})
	}
}

// A module that sends positioned diagnostics renders them against the user's
// file, though it decoded a rewrite whose byte offsets differ.
func TestModuleDiagnosticsArePositioned(t *testing.T) {
	src := `module remotefake "a" {
  version = 1
}

module remotefake "db" {
  version = 1
  colour  = "red"
}
`
	hooks := &Hooks{RemoteDecodeHint: func(string) string { return "this block is decoded by module remotefake 1.2.0" }}
	got := mustFail(t, src, hooks,
		"Error: Unsupported argument",
		`on doze.hcl line 7, in module "remotefake" "db":`,
		`7:   colour  = "red"`,
		"this block is decoded by module remotefake 1.2.0")
	// Only line 7 is quoted: the byte offsets were moved onto the real file.
	if strings.Contains(got, "6:   version") {
		t.Errorf("snippet strayed onto the header:\n%s", got)
	}
}

// A diagnostic on a rewritten header line points at the block's real header.
func TestModuleDiagnosticOnHeader(t *testing.T) {
	src := `module remotefake "a" {
  version = 1
}
module remotefake "header" {
  version = 1
}
`
	mustFail(t, src, nil,
		"Error: this module needs a password",
		"on doze.hcl line 4",
		`4: module remotefake "header" {`)
}

// An older module sends only a string: today's rendering, unchanged.
func TestModuleStringErrorFallsBack(t *testing.T) {
	src := `module remotefake "old" {
  version = 1
}
`
	got := mustFail(t, src, nil, `module remotefake "old": doze.hcl:9,3-9: Unsupported argument`)
	if strings.Contains(got, "on doze.hcl line") {
		t.Errorf("a string error has no position to render:\n%s", got)
	}
}
