package config

import (
	"fmt"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// lintWarnings finds what loads but is likely a mistake: a variable no
// expression reads, and a task that nothing lists in after or references,
// so that only doze task would ever run it.
func (cfg *Config) lintWarnings(parser *hclparse.Parser, varBlocks []*hcl.Block) []string {
	readVars := map[string]bool{}
	for _, f := range parser.Files() {
		body, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		_ = hclsyntax.VisitAll(body, func(n hclsyntax.Node) hcl.Diagnostics {
			if e, ok := n.(*hclsyntax.ScopeTraversalExpr); ok && e.Traversal.RootName() == "var" && len(e.Traversal) > 1 {
				if a, ok := e.Traversal[1].(hcl.TraverseAttr); ok {
					readVars[a.Name] = true
				}
			}
			return nil
		})
	}
	var out []string
	for _, b := range varBlocks {
		if name := b.Labels[0]; !readVars[name] {
			out = append(out, fmt.Sprintf("%s: variable %q is declared but nothing reads var.%s", b.DefRange.String(), name, name))
		}
	}
	waited := map[string]bool{}
	for _, d := range cfg.Instances {
		for _, dep := range d.Deps {
			waited[dep.Name] = true
		}
	}
	for _, t := range cfg.Tasks {
		if t.Manual || waited[t.Name] {
			continue
		}
		out = append(out, fmt.Sprintf("%s: task %q runs only by doze task %s: nothing lists it in after; add manual = true to say so, or task.%s to the after of what needs it",
			t.block.DefRange.String(), t.Name, t.Name, t.Name))
	}
	sort.Strings(out)
	return out
}
