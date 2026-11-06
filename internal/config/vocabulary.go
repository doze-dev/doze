// The words doze.hcl is written in, with a line on each, for editors and
// tools. Hand-written for the docs; vocabulary_test.go holds it to the
// loader's own schemas both ways, so an argument added to the loader and not
// here (or the reverse) fails the build.
package config

//go:generate go test -run TestEditorSchema -update .

import (
	"bytes"
	"encoding/json"

	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// VocabArg is one argument: its name, its type as cty writes types in JSON
// ("string", ["list", "string"], "dynamic" for anything or several shapes),
// and a line on what it does.
type VocabArg struct {
	Name     string
	Type     cty.Type
	Doc      string
	Required bool
}

// VocabBlock is one block type: its labels, its arguments and nested blocks.
// Open means the block takes more arguments than listed: a module's own,
// which only the module knows (its Describe, and its registry page).
type VocabBlock struct {
	Name   string
	Labels []string
	Doc    string
	Args   []VocabArg
	Blocks []VocabBlock
	Many   bool // may appear more than once
	Open   bool
}

var (
	str     = cty.String
	num     = cty.Number
	boolean = cty.Bool
	dyn     = cty.DynamicPseudoType
)

// metaArgs are the arguments every module, app, service and task block takes.
var metaArgs = []VocabArg{
	{Name: "count", Type: num, Doc: "Stamp this many copies of the block; count.index is 0, 1, …"},
	{Name: "for_each", Type: dyn, Doc: "Stamp one copy per key of a map or element of a set; each.key and each.value name it."},
	{Name: "enabled", Type: boolean, Doc: "false keeps the block declared but never started."},
	{Name: "after", Type: cty.List(dyn), Doc: "Start only after these: [module.db, task.migrate] wait until ready; started(app.x) until it runs."},
}

var restartBlock = VocabBlock{
	Name: "restart", Doc: "How doze restarts the command after an unexpected exit.",
	Args: []VocabArg{
		{Name: "policy", Type: str, Doc: `"no", "on_failure" (default) or "always".`},
		{Name: "backoff", Type: str, Doc: "The first wait between restarts, doubled each time up to 30s, e.g. \"1s\"."},
		{Name: "max_retries", Type: num, Doc: "How many restarts before the command is marked failed (default 5)."},
	},
}

// commandArgs are the arguments an app and a service share.
var commandArgs = []VocabArg{
	{Name: "run", Type: str, Required: true, Doc: "The command line, run with sh -c."},
	{Name: "dir", Type: str, Doc: "The working directory, relative to the file that declares the block."},
	{Name: "port", Type: num, Doc: "The port everyone else uses. doze holds it and starts the command with $PORT, a private port, to listen on."},
	{Name: "env", Type: cty.Map(str), Doc: "Environment variables; values may reference other blocks (module.db.url)."},
	{Name: "env_file", Type: str, Doc: "A KEY=VALUE file, below env in precedence."},
	{Name: "ready", Type: dyn, Doc: `When it is ready: tcp(), http("/path"), log("text") or exec("cmd").`},
	{Name: "ready_timeout", Type: str, Doc: `How long it has to become ready (default "60s").`},
	{Name: "setup", Type: dyn, Doc: "Shell lines run once per data folder, before run."},
	{Name: "on_ready", Type: dyn, Doc: "Shell lines run on every wake, once it is ready."},
	{Name: "stop", Type: dyn, Doc: "Shell lines run before it is signalled to stop."},
	{Name: "sleep", Type: str, Doc: `Sleep after this long with nothing holding it ("5m"), or "never". A duration needs a port.`},
	{Name: "console", Type: str, Doc: "A web console it serves on a second port, e.g. http://127.0.0.1:8025."},
}

// Vocabulary is doze.hcl's root: its arguments and its blocks.
func Vocabulary() VocabBlock {
	return VocabBlock{
		Name: "doze.hcl",
		Args: []VocabArg{
			{Name: "doze", Type: str, Doc: `The oldest doze this stack needs, e.g. ">= 0.2".`},
			{Name: "stack", Type: str, Doc: "The stack's name, in <name>.<stack>.doze; the folder's name if unset."},
			{Name: "home", Type: str, Doc: "The shared toolchain store and cache ($DOZE_HOME or ~/.doze)."},
			{Name: "data_dir", Type: str, Doc: "This project's state: data, sockets, logs."},
		},
		Blocks: []VocabBlock{
			{Name: "defaults", Doc: "Settings for every block in the stack.", Args: []VocabArg{
				{Name: "sleep", Type: str, Doc: `Sleep after this long at zero connections (default "2m"), or "never".`},
				{Name: "names", Type: boolean, Doc: "Give every service a <name>.<stack>.doze name."},
				{Name: "log_runs", Type: num, Doc: "How many runs of each service's log are kept (default 5)."},
				{Name: "log_size", Type: str, Doc: `The most log kept per service, across its runs (default "10MB").`},
				{Name: "ready_timeout", Type: str, Doc: `How long every app and service gets to become ready (default "60s"), unless its block says.`},
				{Name: "env", Type: cty.Map(str), Doc: "Variables every app, service and task gets; a block's own env wins key by key."},
			},
				Blocks: []VocabBlock{restartBlock}},
			{Name: "tls", Doc: "Client TLS for Postgres, terminated by doze.", Args: []VocabArg{
				{Name: "cert", Type: str, Doc: "A PEM certificate; with no cert and key, one is made."},
				{Name: "key", Type: str, Doc: "The certificate's PEM private key."},
				{Name: "required", Type: boolean, Doc: "Refuse clients that do not use TLS."},
			}},
			{Name: "variable", Labels: []string{"name"}, Many: true, Doc: "An input, read as var.<name>; set by --var, DOZE_VAR_<name> or *.auto.doze.vars.", Args: []VocabArg{
				{Name: "type", Type: dyn, Doc: "A type constraint: string, number, bool, list(string), map(string), …"},
				{Name: "default", Type: dyn, Doc: "The value when nothing sets it; without one the variable is required."},
				{Name: "description", Type: str, Doc: "What it is for."},
				{Name: "sensitive", Type: boolean, Doc: "The value is a secret."},
			}},
			{Name: "locals", Many: true, Open: true, Doc: "Named values, read as local.<name>; any argument is one."},
			{Name: "output", Labels: []string{"name"}, Many: true, Doc: "A value worked out from the stack, recorded with its state.", Args: []VocabArg{
				{Name: "value", Type: dyn, Doc: "The value, e.g. module.db.url."},
				{Name: "description", Type: str, Doc: "What it is."},
				{Name: "sensitive", Type: boolean, Doc: "The value is a secret."},
			}},
			{Name: KindModule, Labels: []string{"type", "name"}, Many: true, Open: true,
				Doc: "An engine from the doze registry (postgres, valkey, …). Its own arguments are the module's: see its registry page.",
				Args: append([]VocabArg{
					{Name: "version", Type: dyn, Doc: `The engine version: a major (16) or exact ("16.14", quoted). Required except for aws.`},
					{Name: "port", Type: num, Doc: "The port clients use; the engine's own (5432, 6379, …) if unset."},
					{Name: "listen", Type: str, Doc: `A full address instead: "unix:/path.sock", or "0.0.0.0:5544".`},
					{Name: "sleep", Type: str, Doc: `Sleep after this long with nothing holding it, or "never".`},
				}, metaArgs...)},
			{Name: KindApp, Labels: []string{"name"}, Many: true, Doc: "Your own code, run on the host.",
				Args: concat(commandArgs, []VocabArg{
					{Name: "watch", Type: cty.List(str), Doc: "Restart when a file matching one of these globs changes."},
					{Name: "direct", Type: boolean, Doc: "The app binds port itself: no $PORT, and nothing wakes it."},
					{Name: "domains", Type: cty.List(str), Doc: "Your own hostnames for the app, served over https."},
					{Name: "cert", Type: str, Doc: "A PEM certificate for domains; one is made if unset."},
					{Name: "key", Type: str, Doc: "The certificate's PEM private key."},
				}, metaArgs),
				Blocks: []VocabBlock{restartBlock}},
			{Name: KindService, Labels: []string{"name"}, Many: true, Doc: "A long-running command from outside the registry.",
				Args: concat(commandArgs, []VocabArg{
					{Name: "download", Type: dyn, Doc: "An archive to fetch and run from: a URL, or a map of mac, mac_intel, linux, linux_arm to URLs."},
				}, metaArgs),
				Blocks: []VocabBlock{restartBlock}},
			{Name: KindTask, Labels: []string{"name"}, Many: true, Doc: "A command that runs to completion, such as a migration.",
				Args: concat([]VocabArg{
					{Name: "run", Type: str, Required: true, Doc: "The command line, run with sh -c."},
					{Name: "dir", Type: str, Doc: "The working directory, relative to the file that declares the block."},
					{Name: "env", Type: cty.Map(str), Doc: "Environment variables; values may reference other blocks."},
					{Name: "env_file", Type: str, Doc: "A KEY=VALUE file, below env in precedence."},
					{Name: "inputs", Type: cty.List(str), Doc: "Globs whose content, when changed, makes the task run again."},
					{Name: "manual", Type: boolean, Doc: "Run only by doze task <name>, never as something's after."},
				}, metaArgs)},
		},
	}
}

func concat(lists ...[]VocabArg) []VocabArg {
	var out []VocabArg
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// EditorSchema is the vocabulary as JSON, in the shape of the block schemas
// `terraform providers schema -json` prints (attributes, block_types,
// nesting_mode), so tools that read those can be pointed at it.
func EditorSchema() ([]byte, error) {
	root := Vocabulary()
	doc := map[string]any{
		"$comment":       "Generated from internal/config/vocabulary.go by `go generate ./internal/config`. Do not edit.",
		"format_version": "1",
		"files":          []string{"doze.hcl", "*.doze.hcl"},
		"block":          schemaBlock(root),
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // ">= 0.2", not ">= 0.2"
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func schemaBlock(b VocabBlock) map[string]any {
	attrs := map[string]any{}
	for _, a := range b.Args {
		t, _ := ctyjson.MarshalType(a.Type)
		attr := map[string]any{"type": json.RawMessage(t), "description": a.Doc}
		if a.Required {
			attr["required"] = true
		} else {
			attr["optional"] = true
		}
		attrs[a.Name] = attr
	}
	out := map[string]any{"attributes": attrs}
	if b.Doc != "" {
		out["description"] = b.Doc
	}
	if b.Open {
		out["open"] = true
	}
	if len(b.Blocks) > 0 {
		types := map[string]any{}
		for _, nb := range b.Blocks {
			bt := map[string]any{"nesting_mode": "single", "block": schemaBlock(nb)}
			if nb.Many {
				bt["nesting_mode"] = "list"
			}
			if len(nb.Labels) > 0 {
				bt["labels"] = nb.Labels
			}
			types[nb.Name] = bt
		}
		out["block_types"] = types
	}
	return out
}
