package config

// rust-oracle only: test hooks for tools/oracle-dump, which writes what this
// package loads (or the diagnostics it refuses with, positions included) as
// JSON so the Rust port can be diffed against it. Never merged to main.

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty/function"
)

// OracleDiags, when set, receives every diagnostic set the package renders
// into an error, after dedupe and fix hints, in the order it renders them.
var OracleDiags func(hcl.Diagnostics)

// OracleFunctions is the function table doze.hcl expressions see.
func OracleFunctions() map[string]function.Function { return stdlibFunctions() }
