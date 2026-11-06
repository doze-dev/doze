// HCL decode targets and decoders for the fixed root blocks (defaults, tls) and the common fields config reads from every engine block.
package config

import (
	"fmt"
	"strconv"
	"strings"

	names "github.com/doze-dev/doze-names"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/doze-dev/doze/internal/runspec"
)

// common is the partial-decode target for the fields config reads from every
// engine block; the rest of the body goes to the driver.
type common struct {
	Version string `hcl:"version,optional"`
	Listen  string `hcl:"listen,optional"`
	Port    int    `hcl:"port,optional"`
	// Domains are an app's own names outside the doze zone, served over
	// https by the front door (names.ValidDomain); Cert and Key are the
	// person's own certificate for them, else one is minted from the doze CA.
	Domains []string `hcl:"domains,optional"`
	Cert    string   `hcl:"cert,optional"`
	Key     string   `hcl:"key,optional"`
	Remain  hcl.Body `hcl:",remain"`
}

type hclTLS struct {
	Cert     string `hcl:"cert,optional"`
	Key      string `hcl:"key,optional"`
	Required bool   `hcl:"required,optional"`
}

type hclDefaults struct {
	Sleep   string `hcl:"sleep,optional"`
	Names   bool   `hcl:"names,optional"`
	LogRuns int    `hcl:"log_runs,optional"`
	LogSize string `hcl:"log_size,optional"`
}

// oldDefaults are the defaults arguments a later spelling replaced: the
// words now match what a block says for itself (`sleep`), and `domains`
// is left for your own hostnames on an app.
var oldDefaults = map[string]func(text string) (string, string){
	"idle_timeout": func(text string) (string, string) {
		if strings.Trim(text, `"`) == "0s" || strings.Trim(text, `"`) == "0" {
			text = `"never"`
		}
		return "`idle_timeout` is now `sleep`, the word a block uses for its own", "write: sleep = " + text
	},
	"domains": func(text string) (string, string) {
		return "in defaults, `domains` is now `names`: it turns on the <name>.<stack>.doze names", "write: names = " + text + " (`domains` on an app is for your own hostnames)"
	},
}

func (cfg *Config) decodeDefaults(parser *hclparse.Parser, block *hcl.Block, ctx *hcl.EvalContext) error {
	if body, ok := block.Body.(*hclsyntax.Body); ok {
		for name, hint := range oldDefaults {
			if attr, ok := body.Attributes[name]; ok {
				msg, fix := hint(exprText(parser, attr.Expr.Range()))
				return posErr(parser, attr.NameRange, msg, fix)
			}
		}
	}
	var d hclDefaults
	if diags := gohcl.DecodeBody(block.Body, ctx, &d); diags.HasErrors() {
		return diagError(parser, diags)
	}
	if d.Sleep != "" {
		sl, err := runspec.ParseSleep(d.Sleep)
		if err != nil {
			return posErr(parser, block.DefRange, "invalid sleep in defaults", err.Error())
		}
		// Zero is "never": only a block with a sleep of its own sleeps.
		cfg.Defaults.IdleTimeout = sl.After
		if sl.Never {
			cfg.Defaults.IdleTimeout = 0
		}
	}
	cfg.Defaults.Domains = d.Names && !names.Disabled()
	if d.LogRuns < 0 {
		return posErr(parser, block.DefRange, "invalid log_runs", "must not be negative")
	}
	cfg.Defaults.LogRuns = d.LogRuns
	if d.LogSize != "" {
		n, err := parseSize(d.LogSize)
		if err != nil {
			return posErr(parser, block.DefRange, "invalid log_size", err.Error())
		}
		cfg.Defaults.LogBytes = n
	}
	return nil
}

func (cfg *Config) decodeTLS(parser *hclparse.Parser, block *hcl.Block, ctx *hcl.EvalContext) error {
	var t hclTLS
	if diags := gohcl.DecodeBody(block.Body, ctx, &t); diags.HasErrors() {
		return diagError(parser, diags)
	}
	cfg.TLS = TLSSettings{Enabled: true, Cert: t.Cert, Key: t.Key, Required: t.Required}
	if (cfg.TLS.Cert == "") != (cfg.TLS.Key == "") {
		return posErr(parser, block.DefRange, "incomplete tls block", "set both cert and key, or neither (to auto-generate a self-signed cert)")
	}
	return nil
}

// parseSize reads a size such as "10MB" or "512KB" into bytes. Units are
// powers of 1024.
func parseSize(s string) (int64, error) {
	units := []struct {
		suffix string
		bytes  int64
	}{{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1}}
	for _, u := range units {
		if num, ok := strings.CutSuffix(strings.ToUpper(strings.TrimSpace(s)), u.suffix); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(num), 10, 64)
			if err != nil || n <= 0 {
				break
			}
			return n * u.bytes, nil
		}
	}
	return 0, fmt.Errorf("%q is not a size (try \"10MB\", \"512KB\")", s)
}
