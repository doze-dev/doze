package doze

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze-sdk/plugin"
	"github.com/doze-dev/doze/internal/hostboot"
)

// Schema describes the config an engine block accepts — what you can Set on an
// AddModule, discoverable so a UI or a validator can offer the right keys.
type Schema struct {
	Engine      string
	Title       string
	Category    string // "database" | "cache" | "queue" | …
	Description string
	Versions    []string // selectable version labels
	Port        int      // conventional client port
	Example     string   // a complete HCL block example
	Args        []SchemaArg
	Blocks      []SchemaBlock
}

// SchemaArg is one top-level argument an engine block accepts.
type SchemaArg struct {
	Name     string
	Type     string // "string" | "number" | "bool" | "map(string)" | …
	Default  string
	Desc     string
	Required bool
}

// SchemaBlock is a nested block type an engine accepts (role, topic, bucket, …).
type SchemaBlock struct {
	Name  string
	Label string
	Desc  string
	Args  []SchemaArg
}

// EngineSchema returns an engine's config schema. Since engines author their
// schema in their (out-of-process) module, this resolves the module's plugin
// binary and asks it to describe itself locally — no registry round-trip. It
// works for describe-capable module builds; a built-in engine with no published
// schema returns an empty schema.
func EngineSchema(opts Options, engineType string) (Schema, error) {
	host, err := initHost(opts)
	if err != nil {
		return Schema{}, err
	}
	defer host.Close()
	path, env, ok := host.ResolvePlugin(engineType)
	if !ok {
		return Schema{}, fmt.Errorf("doze: unknown engine %q (no module provides it)", engineType)
	}
	desc, err := execDescribe(path, env)
	if err != nil {
		return Schema{}, fmt.Errorf("doze: engine %q exposes no local schema: %w", engineType, err)
	}
	return schemaFrom(engineType, desc), nil
}

// initHost initializes the process-global engine host without loading a config —
// for schema discovery, which needs the module resolver but no stack.
func initHost(opts Options) (*hostboot.Host, error) {
	home := opts.Home
	if home == "" {
		home = defaultHome()
	}
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return hostboot.Init(hostboot.Options{
		Home:        home,
		Logf:        logf,
		LockPath:    func() string { return filepath.Join(home, "doze.lock") },
		PersistLock: func() bool { return false },
	})
}

// execDescribe runs a plugin binary's __describe subcommand and decodes its
// engine.Description JSON.
func execDescribe(path string, env []string) (engine.Description, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, plugin.DescribeArg)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	if err != nil {
		return engine.Description{}, err
	}
	var desc engine.Description
	if err := json.Unmarshal(out, &desc); err != nil {
		return engine.Description{}, fmt.Errorf("decoding schema: %w", err)
	}
	return desc, nil
}

func schemaFrom(engineType string, d engine.Description) Schema {
	s := Schema{
		Engine:      engineType,
		Title:       d.Title,
		Category:    d.Category,
		Description: d.Description,
		Versions:    d.Versions,
		Port:        d.Port,
		Example:     d.Example,
	}
	for _, a := range d.Config {
		s.Args = append(s.Args, schemaArg(a))
	}
	for _, blk := range d.Blocks {
		sb := SchemaBlock{Name: blk.Name, Label: blk.Label, Desc: blk.Desc}
		for _, a := range blk.Args {
			sb.Args = append(sb.Args, schemaArg(a))
		}
		s.Blocks = append(s.Blocks, sb)
	}
	return s
}

func schemaArg(a engine.ConfigArg) SchemaArg {
	return SchemaArg{Name: a.Name, Type: a.Type, Default: a.Default, Desc: a.Desc, Required: a.Required}
}
