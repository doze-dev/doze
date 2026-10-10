package config

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// Edit is a doze.hcl being added to. It keeps everything already there — the
// comments, the order, the way the person wrote it — and appends blocks in the
// canonical format, which is what `doze add` and `doze init` need from a file
// that people also edit by hand.
type Edit struct {
	file *hclwrite.File
}

// OpenEdit parses src for editing. An empty src is a new file.
func OpenEdit(src []byte) (*Edit, error) {
	f, diags := hclwrite.ParseConfig(src, "doze.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, fmt.Errorf("doze.hcl: %s", diags.Error())
	}
	return &Edit{file: f}, nil
}

// Declared reports whether a block of any kind already has this name: names
// are unique across a stack.
func (e *Edit) Declared(name string) bool {
	for _, b := range e.file.Body().Blocks() {
		switch b.Type() {
		case KindModule:
			if l := b.Labels(); len(l) == 2 && l[1] == name {
				return true
			}
		case KindApp, KindService, KindTask:
			if l := b.Labels(); len(l) == 1 && l[0] == name {
				return true
			}
		}
	}
	return false
}

// Names lists the declared block names, in file order.
func (e *Edit) Names() []string {
	var out []string
	for _, b := range e.file.Body().Blocks() {
		l := b.Labels()
		if len(l) > 0 {
			out = append(out, l[len(l)-1])
		}
	}
	return out
}

// HasStack reports whether the file sets `stack = …` at its root.
func (e *Edit) HasStack() bool { return e.file.Body().GetAttribute("stack") != nil }

// SetStack sets the root `stack` name, placed first.
func (e *Edit) SetStack(name string) {
	b := e.file.Body()
	if b.GetAttribute("stack") != nil {
		return
	}
	existing := b.Blocks()
	attrs := b.Attributes()
	if len(existing) == 0 && len(attrs) == 0 {
		b.SetAttributeValue("stack", ctyString(name))
		return
	}
	// Rebuild with the stack line on top: hclwrite appends, so write the new
	// attribute into a fresh body and put the old tokens after it.
	old := e.file.Bytes()
	f := hclwrite.NewEmptyFile()
	f.Body().SetAttributeValue("stack", ctyString(name))
	f.Body().AppendNewline()
	merged, diags := hclwrite.ParseConfig(append(f.Bytes(), old...), "doze.hcl", hcl.InitialPos)
	if !diags.HasErrors() {
		e.file = merged
	}
}

// AppendBlock adds one block, given as HCL text, after what is there. The text
// is parsed, so a malformed block is an error here and not a broken file.
func (e *Edit) AppendBlock(block string) error {
	// Formatted before it is parsed, so the new block is canonical and the rest
	// of the file — which this does not reformat — is exactly as it was.
	snippet, diags := hclwrite.ParseConfig(hclwrite.Format([]byte(block)), "block", hcl.InitialPos)
	if diags.HasErrors() {
		return fmt.Errorf("the block does not parse: %s", diags.Error())
	}
	blocks := snippet.Body().Blocks()
	if len(blocks) != 1 {
		return fmt.Errorf("expected one block, got %d", len(blocks))
	}
	body := e.file.Body()
	if len(body.Blocks()) > 0 || len(body.Attributes()) > 0 {
		body.AppendNewline()
	}
	body.AppendBlock(blocks[0])
	return nil
}

// Bytes is the file as it will be written.
func (e *Edit) Bytes() []byte {
	return []byte(strings.TrimRight(string(e.file.Bytes()), "\n") + "\n")
}

func ctyString(s string) cty.Value { return cty.StringVal(s) }
