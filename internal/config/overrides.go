// Per-developer overrides: a gitignored *.override.doze.hcl changes a few
// arguments of a block declared elsewhere, without redeclaring it.
package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// OverrideSuffix names an override file: local.override.doze.hcl.
const OverrideSuffix = ".override.doze.hcl"

// overridable are the arguments an override may change: the ones doze reads
// itself, so they take effect whatever kind or module the block is.
var overridable = []string{"port", "enabled", "sleep", "version"}

// isOverride says whether a file is an override file.
func isOverride(name string) bool { return strings.HasSuffix(filepath.Base(name), OverrideSuffix) }

// overrides maps a block header ("module postgres db") to the override
// body that changes it.
type overrides map[string]*hclsyntax.Body

func headKey(b *hcl.Block) string { return b.Type + " " + strings.Join(b.Labels, " ") }

// splitOverrides separates the override files from the rest and reads
// their blocks, refusing anything an override cannot change.
func splitOverrides(parser *hclparse.Parser, files []*hcl.File) ([]*hcl.File, overrides, error) {
	var base []*hcl.File
	ov := overrides{}
	for _, f := range files {
		body, ok := f.Body.(*hclsyntax.Body)
		name := ""
		if ok {
			name = body.SrcRange.Filename
		}
		if !ok || !isOverride(name) {
			base = append(base, f)
			continue
		}
		for attrName, attr := range body.Attributes {
			return nil, nil, posErr(parser, attr.NameRange, fmt.Sprintf("an override file changes blocks; %s is a stack-wide setting", attrName),
				"set it in doze.hcl, or move it into a block")
		}
		for _, blk := range body.Blocks {
			if !isKind(blk.Type) {
				return nil, nil, posErr(parser, blk.DefRange(), fmt.Sprintf("an override file changes module, app, service and task blocks, not %s", blk.Type), "")
			}
			for _, inner := range blk.Body.Blocks {
				return nil, nil, posErr(parser, inner.DefRange(), "an override changes "+strings.Join(overridable, ", ")+"; not nested blocks",
					"change it in the block where it is declared")
			}
			for n, attr := range blk.Body.Attributes {
				if !contains(overridable, n) {
					return nil, nil, posErr(parser, attr.NameRange, fmt.Sprintf("an override changes %s; not %s", strings.Join(overridable, ", "), n),
						"change "+n+" in the block where it is declared")
				}
			}
			h := &hcl.Block{Type: blk.Type, Labels: blk.Labels}
			if _, dup := ov[headKey(h)]; dup {
				return nil, nil, posErr(parser, blk.DefRange(), "this block is overridden twice", "keep one override per block")
			}
			ov[headKey(h)] = blk.Body
		}
	}
	return base, ov, nil
}

// apply wraps each overridden block's body, and refuses an override for a
// block nothing declares (a typo, or a block that was renamed).
func (ov overrides) apply(parser *hclparse.Parser, blocks []*hcl.Block) error {
	used := map[string]bool{}
	for _, b := range blocks {
		if o, ok := ov[headKey(b)]; ok {
			b.Body = overrideBody{base: b.Body, over: o}
			used[headKey(b)] = true
		}
	}
	var stray []string
	for k := range ov {
		if !used[k] {
			stray = append(stray, k)
		}
	}
	if len(stray) > 0 {
		sort.Strings(stray)
		o := ov[stray[0]]
		return posErr(parser, o.SrcRange, "the override for "+stray[0]+" matches nothing declared",
			"an override changes a block declared in doze.hcl; check the kind, type and name match")
	}
	return nil
}

// overrideBody is a block body with some arguments replaced: the base
// body's content, with each argument the override sets taken from it
// instead, positions and all.
type overrideBody struct {
	base hcl.Body
	over *hclsyntax.Body
}

func (o overrideBody) Content(s *hcl.BodySchema) (*hcl.BodyContent, hcl.Diagnostics) {
	c, diags := o.base.Content(s)
	o.mergeInto(c, s)
	return c, diags
}

func (o overrideBody) PartialContent(s *hcl.BodySchema) (*hcl.BodyContent, hcl.Body, hcl.Diagnostics) {
	c, rest, diags := o.base.PartialContent(s)
	oc, orest, _ := o.over.PartialContent(s)
	if c != nil && oc != nil {
		for n, a := range oc.Attributes {
			c.Attributes[n] = a
		}
	}
	if ob, ok := orest.(*hclsyntax.Body); ok {
		rest = overrideBody{base: rest, over: ob}
	}
	return c, rest, diags
}

func (o overrideBody) mergeInto(c *hcl.BodyContent, s *hcl.BodySchema) {
	if c == nil {
		return
	}
	oc, _, _ := o.over.PartialContent(s)
	for n, a := range oc.Attributes {
		c.Attributes[n] = a
	}
}

func (o overrideBody) JustAttributes() (hcl.Attributes, hcl.Diagnostics) {
	attrs, diags := o.base.JustAttributes()
	over, _ := o.over.JustAttributes()
	for n, a := range over {
		attrs[n] = a
	}
	return attrs, diags
}

func (o overrideBody) MissingItemRange() hcl.Range { return o.base.MissingItemRange() }

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
