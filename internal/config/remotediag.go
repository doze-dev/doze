// Module decode diagnostics: a module decodes its own block out of process and,
// built with a new enough SDK, sends back positioned diagnostics. They render
// through diagError like doze's own, once their positions are moved onto the
// user's file.
package config

import (
	"bytes"

	"github.com/hashicorp/hcl/v2"
)

// remoteDiagError renders a module's decode diagnostics. The module parsed
// pluginSource's rewrite of the file, in which each `module <type> "<name>"`
// header lost its keyword: every other line matches the user's file byte for
// byte, but byte offsets after a rewritten header do not, and HCL's snippet
// printer works in bytes. So each position is moved onto the user's file by
// line; one on a rewritten header points at the block's own header instead.
// The module's identity (and any upgrade) closes the last error, as on the
// string path.
func (l *loader) remoteDiagError(p *pendingInstance, dd hcl.Diagnostics) error {
	out := make(hcl.Diagnostics, 0, len(dd))
	for _, d := range dd {
		if d == nil {
			continue
		}
		c := *d
		c.Subject = l.remoteRange(p, c.Subject)
		c.Context = l.remoteRange(p, c.Context)
		if c.Subject == nil {
			c.Context = nil
		}
		out = append(out, &c)
	}
	if hint := l.hooks.remoteDecodeHint(p.decl.Type); hint != "" {
		for i := len(out) - 1; i >= 0; i-- {
			if out[i].Severity != hcl.DiagError {
				continue
			}
			if out[i].Detail != "" {
				out[i].Detail += "\n\n"
			}
			out[i].Detail += hint
			break
		}
	}
	return diagError(l.parser, out)
}

// remoteRange moves a range a module reported onto the user's file.
func (l *loader) remoteRange(p *pendingInstance, r *hcl.Range) *hcl.Range {
	if r == nil {
		return nil
	}
	m := *r
	// An SDK that never heard the real filename parsed under a synthesized one.
	if m.Filename == "" || m.Filename == p.blockLabel+".doze.hcl" {
		m.Filename = p.defRange.Filename
	}
	f := l.parser.Files()[m.Filename]
	plug := l.pluginBytes(m.Filename)
	if f == nil || plug == nil {
		return &m
	}
	start, okStart := moveByLine(m.Start, plug, f.Bytes)
	end, okEnd := moveByLine(m.End, plug, f.Bytes)
	if okStart && okEnd {
		m.Start, m.End = start, end
		return &m
	}
	if m.Filename == p.defRange.Filename && m.Start.Line == p.defRange.Start.Line {
		d := p.defRange
		return &d
	}
	// Some other rewritten line: point at the whole of it.
	lo, hi, ok := lineSpan(f.Bytes, m.Start.Line)
	if !ok {
		return &m
	}
	m.Start = hcl.Pos{Line: m.Start.Line, Column: 1, Byte: lo}
	m.End = hcl.Pos{Line: m.Start.Line, Column: hi - lo + 1, Byte: hi}
	return &m
}

// moveByLine maps pos from one version of a file to another, keeping its line
// and column. It holds only when that line reads the same in both.
func moveByLine(pos hcl.Pos, from, to []byte) (hcl.Pos, bool) {
	fromLo, fromHi, ok := lineSpan(from, pos.Line)
	if !ok {
		return pos, false
	}
	toLo, toHi, ok := lineSpan(to, pos.Line)
	if !ok || !bytes.Equal(from[fromLo:fromHi], to[toLo:toHi]) {
		return pos, false
	}
	off := pos.Byte - fromLo
	if off < 0 || off > fromHi-fromLo {
		return pos, false
	}
	pos.Byte = toLo + off
	return pos, true
}

// lineSpan returns the byte offsets of line n's content (1-based, newline
// excluded).
func lineSpan(src []byte, n int) (lo, hi int, ok bool) {
	if n < 1 {
		return 0, 0, false
	}
	for line := 1; line < n; line++ {
		i := bytes.IndexByte(src[lo:], '\n')
		if i < 0 {
			return 0, 0, false
		}
		lo += i + 1
	}
	hi = lo
	if i := bytes.IndexByte(src[lo:], '\n'); i >= 0 {
		hi += i
	} else {
		hi = len(src)
	}
	if hi > lo && src[hi-1] == '\r' {
		hi--
	}
	return lo, hi, true
}
