// Package runspec decodes the attributes that `app`, `service` and `task`
// blocks share: step lists, the `ready` probe call, and the small enumerated
// settings. The config package (shape) and the process engine (runtime) both
// decode through it, so the two can never disagree about what a value means.
package runspec

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// DefaultReadyTimeout is how long a command gets to become ready when the
// block sets no `ready_timeout`.
const DefaultReadyTimeout = 60 * time.Second

// Probe is one decoded `ready = …` call. Arg is the call's argument: the path
// for http, the text for log, the command for exec, and empty for tcp.
type Probe struct {
	Kind string // "tcp" | "http" | "log" | "exec"
	Arg  string
}

var probeType = cty.Object(map[string]cty.Type{"kind": cty.String, "arg": cty.String})

func probeFunc(kind string, params ...function.Parameter) function.Function {
	return function.New(&function.Spec{
		Params: params,
		Type:   function.StaticReturnType(probeType),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			arg := ""
			if len(args) == 1 {
				arg = args[0].AsString()
			}
			return cty.ObjectVal(map[string]cty.Value{"kind": cty.StringVal(kind), "arg": cty.StringVal(arg)}), nil
		},
	})
}

// Functions are the probe constructors a `ready` attribute is written with:
// tcp(), http("/path"), log("text") and exec("cmd").
func Functions() map[string]function.Function {
	str := func(name string) function.Parameter { return function.Parameter{Name: name, Type: cty.String} }
	return map[string]function.Function{
		"tcp":  probeFunc("tcp"),
		"http": probeFunc("http", str("path")),
		"log":  probeFunc("log", str("text")),
		"exec": probeFunc("exec", str("command")),
	}
}

// ParseReady decodes a `ready` value. A null value is no probe (nil, nil).
func ParseReady(v cty.Value) (*Probe, error) {
	if v.IsNull() {
		return nil, nil
	}
	if !v.IsKnown() || !v.Type().Equals(probeType) {
		return nil, fmt.Errorf(`ready takes a probe: tcp(), http("/path"), log("text") or exec("cmd")`)
	}
	p := &Probe{Kind: v.GetAttr("kind").AsString(), Arg: v.GetAttr("arg").AsString()}
	switch p.Kind {
	case "http":
		if !strings.HasPrefix(p.Arg, "/") {
			return nil, fmt.Errorf(`ready = http(%q): the path must start with "/" (the host and port are the block's own)`, p.Arg)
		}
	case "log", "exec":
		if strings.TrimSpace(p.Arg) == "" {
			return nil, fmt.Errorf("ready = %s(…) needs a non-empty argument", p.Kind)
		}
	}
	return p, nil
}

// NeedsPort reports whether the probe can only run against a declared port.
func (p *Probe) NeedsPort() bool { return p.Kind == "tcp" || p.Kind == "http" }

// Target renders the probe for the health package, which names the kinds
// "tcp", "http", "exec" and "log_line". port is the port the command listens on.
func (p *Probe) Target(port int) (kind, target string) {
	switch p.Kind {
	case "tcp":
		return "tcp", fmt.Sprintf("127.0.0.1:%d", port)
	case "http":
		return "http", fmt.Sprintf("http://127.0.0.1:%d%s", port, p.Arg)
	case "log":
		return "log_line", regexp.QuoteMeta(p.Arg)
	default:
		return "exec", p.Arg
	}
}

// Steps decodes `setup`, `on_ready` or `stop`: one command, or a list of them.
func Steps(attr string, v cty.Value) ([]string, error) {
	if v.IsNull() {
		return nil, nil
	}
	bad := fmt.Errorf("%s takes a command or a list of commands", attr)
	if !v.IsKnown() {
		return nil, bad
	}
	if v.Type() == cty.String {
		return checkSteps(attr, []string{v.AsString()})
	}
	if !v.Type().IsListType() && !v.Type().IsTupleType() && !v.Type().IsSetType() {
		return nil, bad
	}
	var out []string
	for it := v.ElementIterator(); it.Next(); {
		_, e := it.Element()
		if e.IsNull() || e.Type() != cty.String {
			return nil, bad
		}
		out = append(out, e.AsString())
	}
	return checkSteps(attr, out)
}

func checkSteps(attr string, steps []string) ([]string, error) {
	for _, s := range steps {
		if strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("%s has an empty command", attr)
		}
	}
	return steps, nil
}

// Sleep is a decoded `sleep` setting.
type Sleep struct {
	Set   bool          // the block said something
	Never bool          // sleep = "never"
	After time.Duration // otherwise, the idle time before sleeping
}

// ParseSleep decodes `sleep`: a duration, or "never".
func ParseSleep(s string) (Sleep, error) {
	switch s {
	case "":
		return Sleep{}, nil
	case "never":
		return Sleep{Set: true, Never: true}, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return Sleep{}, fmt.Errorf(`sleep = %q: want a duration such as "10m", or "never"`, s)
	}
	return Sleep{Set: true, After: d}, nil
}

// ParseReadyTimeout decodes `ready_timeout`, defaulting when unset.
func ParseReadyTimeout(s string) (time.Duration, error) {
	if s == "" {
		return DefaultReadyTimeout, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf(`ready_timeout = %q: want a duration such as "90s"`, s)
	}
	return d, nil
}

// ParseLogs decodes `logs`: "normal" (the default) or "quiet".
func ParseLogs(s string) (quiet bool, err error) {
	switch s {
	case "", "normal":
		return false, nil
	case "quiet":
		return true, nil
	}
	return false, fmt.Errorf(`logs = %q: want "normal" or "quiet"`, s)
}

// CheckConsole validates a `console` URL.
func CheckConsole(s string) error {
	if s == "" || strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return nil
	}
	return fmt.Errorf("console = %q: want an http:// or https:// URL", s)
}
