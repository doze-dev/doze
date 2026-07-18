// The `use <type> "<name>"` surface syntax, desugared before parsing.
package config

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
)

// rewriteUseBlocks desugars `use <type> "<name>" { … }` into the bare
// `<type> "<name>" { … }` block the rest of the pipeline expects. `use` is the
// surface marker that a block is backed by a resolved module; the built-in
// `process` engine stays bare and is the only engine block written without it.
// Only the block header is rewritten — the body is preserved verbatim — so the
// out-of-process plugin decoder, which re-parses these same bytes to find its
// `<type> "<name>"` block (see plugin/server.go), never sees `use`. A file with no
// `use` block is returned unchanged. The bare `<type> "<name>"` form still parses,
// so this is purely additive.
func rewriteUseBlocks(src []byte, filename string) ([]byte, error) {
	f, diags := hclwrite.ParseConfig(src, filename, hcl.InitialPos)
	if diags.HasErrors() {
		return src, nil // defer syntax errors to the real parser, which reports them with full fidelity
	}
	changed := false
	for _, blk := range f.Body().Blocks() {
		if blk.Type() != "use" {
			continue
		}
		labels := blk.Labels()
		if len(labels) != 2 {
			return nil, fmt.Errorf("%s: `use` takes an engine type and a name, e.g. `use postgres \"app\"` (got %d label(s))", filename, len(labels))
		}
		etype, name := labels[0], labels[1]
		if etype == "process" {
			return nil, fmt.Errorf("%s: `process` is built in, not a module — declare it as a bare `process %q` block, without `use`", filename, name)
		}
		if reservedBlocks[etype] {
			return nil, fmt.Errorf("%s: %q is a reserved block, not an engine type — `use %s …` is not valid", filename, etype, etype)
		}
		blk.SetType(etype)
		blk.SetLabels([]string{name})
		changed = true
	}
	if !changed {
		return src, nil
	}
	return f.Bytes(), nil
}
