package view

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// RefsIn reads the references a stack's doze.hcl (and the *.doze.hcl
// beside it) writes, as the lines that write them, for the Grid's
// tooltips: the file's own words, not a paraphrase. A file that does not
// parse gives what the rest do; none at all gives nil.
func RefsIn(dir string) Refs {
	files, _ := filepath.Glob(filepath.Join(dir, "*.doze.hcl"))
	files = append([]string{filepath.Join(dir, "doze.hcl")}, files...)
	var out Refs
	for _, p := range files {
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if out == nil {
			out = Refs{}
		}
		refsOf(src, p, out)
	}
	return out
}

// refKinds are the roots of a reference to another block.
var refKinds = map[string]bool{"app": true, "module": true, "service": true, "task": true}

func refsOf(src []byte, name string, out Refs) {
	f, _ := hclsyntax.ParseConfig(src, name, hcl.InitialPos)
	if f == nil {
		return
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return
	}
	lines := strings.Split(string(src), "\n")
	for _, b := range body.Blocks {
		if !refKinds[b.Type] || len(b.Labels) == 0 {
			continue
		}
		from := b.Labels[len(b.Labels)-1]
		if out[from] == nil {
			out[from] = map[string]string{}
		}
		walkBody(b.Body, func(attr *hclsyntax.Attribute) {
			if attr.Name == "after" {
				if _, ok := out[from]["after"]; !ok {
					out[from]["after"] = oneLine(slice(src, attr.SrcRange))
				}
			}
			_ = hclsyntax.VisitAll(attr.Expr, func(node hclsyntax.Node) hcl.Diagnostics {
				st, ok := node.(*hclsyntax.ScopeTraversalExpr)
				if !ok || len(st.Traversal) < 2 || !refKinds[st.Traversal.RootName()] {
					return nil
				}
				step, ok := st.Traversal[1].(hcl.TraverseAttr)
				if !ok {
					return nil
				}
				if _, seen := out[from][step.Name]; seen {
					return nil
				}
				ln := st.SrcRange.Start.Line - 1
				if ln < 0 || ln >= len(lines) {
					return nil
				}
				text := oneLine(lines[ln])
				if attr.Name == "after" {
					text = out[from]["after"]
				}
				out[from][step.Name] = shorten(text, st.Traversal.RootName()+"."+step.Name)
				return nil
			})
		})
	}
}

func walkBody(b *hclsyntax.Body, fn func(*hclsyntax.Attribute)) {
	for _, a := range b.Attributes {
		fn(a)
	}
	for _, nb := range b.Blocks {
		walkBody(nb.Body, fn)
	}
}

func slice(src []byte, r hcl.Range) string {
	if r.Start.Byte < 0 || r.End.Byte > len(src) || r.Start.Byte > r.End.Byte {
		return ""
	}
	return string(src[r.Start.Byte:r.End.Byte])
}

// oneLine is a line of the file as a tooltip shows it: no comment, no
// trailing comma, the spaces that align it closed up.
func oneLine(s string) string {
	if i := strings.Index(s, " #"); i >= 0 && !strings.Contains(s[:i], `"`) {
		s = s[:i]
	}
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSuffix(s, ",")
	s = strings.ReplaceAll(s, "[ ", "[")
	return strings.ReplaceAll(s, " ]", "]")
}

// shorten keeps a long line to its key and the reference in it:
// DATABASE_URL = "…${module.postgres.address}…".
func shorten(s, ref string) string {
	const most = 64
	if len([]rune(s)) <= most {
		return s
	}
	key, expr, ok := strings.Cut(s, " = ")
	if !ok {
		key, expr = "", s
	}
	i := strings.Index(expr, ref)
	if i < 0 {
		return string([]rune(s)[:most-1]) + "…"
	}
	end := i + len(ref)
	for end < len(expr) && (expr[end] == '.' || expr[end] == '_' || expr[end] == '-' || isAlnum(expr[end])) {
		end++
	}
	part := expr[i:end]
	if strings.HasPrefix(expr[:i], `"`) || strings.Contains(expr[:i], "${") {
		part = `"…${` + part + `}…"`
	} else {
		part = "…" + part + "…"
	}
	if key == "" {
		return part
	}
	return key + " = " + part
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
