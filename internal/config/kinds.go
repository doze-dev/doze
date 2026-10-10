// The four nouns of a doze config — module, app, service, task — and the
// checks that turn a block written the old way into a pointer at the new one.
package config

import (
	"fmt"

	"github.com/agext/levenshtein"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
)

// The kinds of thing a stack declares. A reference is always
// <kind>.<name>.<attribute>.
const (
	KindModule  = "module"  // an engine from the doze registry
	KindApp     = "app"     // a long-running command that is your code
	KindService = "service" // a long-running command from outside
	KindTask    = "task"    // a command that runs to completion
)

// AppEngine is the built-in engine every `app` block runs on.
const AppEngine = "process"

var kinds = []string{KindModule, KindApp, KindService, KindTask}

func isKind(s string) bool {
	for _, k := range kinds {
		if k == s {
			return true
		}
	}
	return false
}

// title renders a block header the way the user wrote it, for diagnostics:
// `module postgres "db"`, `app "api"`.
func title(kind, etype, name string) string {
	if kind == KindModule {
		return fmt.Sprintf("module %s %q", etype, name)
	}
	return fmt.Sprintf("%s %q", kind, name)
}

// Title is the instance's block header as written: `module postgres "db"`.
func (d *InstanceDecl) Title() string {
	kind := d.Kind
	if kind == "" { // synthetic instances added at runtime carry no kind
		kind = KindModule
		if d.Type == AppEngine {
			kind = KindApp
		}
	}
	return title(kind, d.Type, d.Name)
}

// blockHead is an instance block's header, read the same way whichever of
// `module <type> "<name>"` and `app "<name>"` it is.
type blockHead struct {
	kind  string
	etype string // the engine type: the module's, or AppEngine
	label string // the name as written (the count/for_each base)
}

func (h blockHead) String() string { return title(h.kind, h.etype, h.label) }

// headOf reads an instance block's header. A module may not take the name of a
// keyword or of the built-in app engine as its type.
func headOf(parser *hclparse.Parser, block *hcl.Block) (blockHead, error) {
	if block.Type == KindApp {
		return blockHead{kind: KindApp, etype: AppEngine, label: block.Labels[0]}, nil
	}
	etype, name := block.Labels[0], block.Labels[1]
	switch {
	case etype == AppEngine:
		return blockHead{}, posErr(parser, block.DefRange, "`process` is not a module",
			fmt.Sprintf("an app is declared as: app %q { run = \"…\" }", name))
	case reservedBlocks[etype] || isKind(etype):
		return blockHead{}, posErr(parser, block.DefRange, fmt.Sprintf("%q is a keyword, not a module type", etype),
			"a module block reads: module <type> \"<name>\" { … }, for example: module postgres \"db\" { … }")
	}
	return blockHead{kind: KindModule, etype: etype, label: name}, nil
}

// checkOldForms reports the first top-level block or root argument written in
// a form doze no longer reads, naming what replaces it. It runs on the parsed
// files before the schema decode, whose own "unsupported block type" says
// nothing about the fix.
func checkOldForms(parser *hclparse.Parser, files []*hcl.File) error {
	for _, f := range files {
		body, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		if attr, ok := body.Attributes["name"]; ok {
			return posErr(parser, attr.NameRange, "`name` is now `stack`", "write: stack = "+exprText(parser, attr.Expr.Range()))
		}
		for _, blk := range body.Blocks {
			if reservedBlocks[blk.Type] || isKind(blk.Type) {
				continue
			}
			rng := blk.DefRange()
			label := "<name>"
			if len(blk.Labels) > 0 {
				label = blk.Labels[len(blk.Labels)-1]
			}
			switch {
			case blk.Type == "use" && len(blk.Labels) == 2:
				return posErr(parser, rng, "`use` is now `module`",
					fmt.Sprintf("write: module %s %q { … }", blk.Labels[0], label))
			case blk.Type == AppEngine:
				return posErr(parser, rng, "`process` is now `app`",
					fmt.Sprintf("write: app %q { … }, with `run` in place of `command` and `dir` in place of `cwd`", label))
			}
			if kw := nearKeyword(blk.Type); kw != "" {
				return posErr(parser, rng, fmt.Sprintf("unknown block %q", blk.Type), fmt.Sprintf("did you mean %q?", kw))
			}
			if len(blk.Labels) == 1 {
				return posErr(parser, rng, fmt.Sprintf("a module block starts with `module`"),
					fmt.Sprintf("write: module %s %q { … }", blk.Type, label))
			}
			return posErr(parser, rng, fmt.Sprintf("unknown block %q", blk.Type),
				"a stack declares module, app, service and task blocks")
		}
	}
	return nil
}

// nearKeyword returns the keyword s is a one-letter slip away from, or "".
// The bar is deliberately high: at the usual distance of three, `kafka` is a
// misspelt `task`.
func nearKeyword(s string) string {
	candidates := append([]string{}, kinds...)
	for k := range reservedBlocks {
		candidates = append(candidates, k)
	}
	for _, c := range candidates {
		if levenshtein.Distance(s, c, nil) == 1 {
			return c
		}
	}
	return ""
}

// exprText returns the source text of a range, for echoing a value back in a
// diagnostic.
func exprText(parser *hclparse.Parser, rng hcl.Range) string {
	if f := parser.Files()[rng.Filename]; f != nil && rng.End.Byte <= len(f.Bytes) {
		return string(f.Bytes[rng.Start.Byte:rng.End.Byte])
	}
	return `"…"`
}

// pluginSource rewrites `module <type> "<name>" { … }` to the bare
// `<type> "<name>" { … }` a plugin looks for. An out-of-process module decodes
// its own block by re-parsing the file and finding it by type and name
// (doze-sdk plugin/server.go), and the plugin protocol predates the `module`
// keyword. Only headers change, so every line number a plugin reports still
// points at the user's file.
func pluginSource(src []byte, filename string) []byte {
	f, diags := hclwrite.ParseConfig(src, filename, hcl.InitialPos)
	if diags.HasErrors() {
		return src
	}
	changed := false
	for _, blk := range f.Body().Blocks() {
		if labels := blk.Labels(); blk.Type() == KindModule && len(labels) == 2 {
			blk.SetType(labels[0])
			blk.SetLabels(labels[1:])
			changed = true
		}
	}
	if !changed {
		return src
	}
	return f.Bytes()
}
