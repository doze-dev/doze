package doze

import (
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
)

// LoadStack parses an existing HCL config into an editable Stack — the
// round-trip: read the current topology, mutate it (AddProcess/AddModule/
// Remove), then re-render (HCL) or serve it. Existing service blocks are
// retained verbatim (their config is the opaque plugin-decoded spec, so only the
// original source renders back exactly); the top-level (name + defaults/tls/
// modules) is preserved as-is. It does not validate — call Validate or Load for
// that. Daemon-less; reads no plugins.
func LoadStack(path string) (*Stack, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, diags := hclwrite.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}
	body := f.Body()

	s := &Stack{}
	var pre strings.Builder
	if attr := body.GetAttribute("name"); attr != nil {
		name := strings.Trim(string(attr.Expr().BuildTokens(nil).Bytes()), " \t\"")
		s.name = name
		fmt.Fprintf(&pre, "name = %q\n\n", name)
	}
	for _, blk := range body.Blocks() {
		switch blk.Type() {
		case "defaults", "tls", "modules":
			// Top-level meta blocks: retain verbatim.
			pre.Write(blk.BuildTokens(nil).Bytes())
			if !strings.HasSuffix(pre.String(), "\n") {
				pre.WriteString("\n")
			}
		default:
			// An engine instance block: type = engine, label = instance name.
			name := ""
			if labels := blk.Labels(); len(labels) > 0 {
				name = labels[0]
			}
			text := strings.TrimRight(string(blk.BuildTokens(nil).Bytes()), "\n")
			s.blocks = append(s.blocks, &rawBlock{name: name, text: text})
		}
	}
	s.rawPreamble = strings.TrimRight(pre.String(), "\n")
	return s, nil
}

// Validate renders the stack and runs it through the full decode + validation
// pipeline (including out-of-process module decode) without booting a daemon,
// returning the first error — the same check Load performs. opts supplies Home
// (and any Vars); its Stack/ConfigPath are ignored.
func (s *Stack) Validate(opts Options) error {
	opts.Stack = s
	opts.ConfigPath = ""
	in, err := Load(opts)
	if err != nil {
		return err
	}
	return in.Close()
}
