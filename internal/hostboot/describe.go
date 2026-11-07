package hostboot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze-sdk/plugin"
)

// describeTimeout bounds one `<plugin> __describe`. It runs during a config
// load, so it is short: a module that cannot describe itself in this long is
// treated as one that says nothing.
const describeTimeout = 3 * time.Second

// describeCache holds each plugin binary's Description, keyed by its path,
// size and modification time: a module's binary is immutable once fetched, and
// a test's locally built module that is rebuilt is asked again. A long-lived
// daemon reloading its doze.hcl runs each module's __describe once.
var describeCache sync.Map // describeKey -> engine.Description

type describeKey struct {
	path  string
	size  int64
	mtime int64
}

// Describe returns what an engine's module says about itself (its
// engine.Description: title, conventional port, config schema), by running its
// plugin binary's __describe subcommand. It errors when no plugin provides the
// engine (it is compiled in, or cannot be fetched) or the plugin cannot
// describe itself.
func (h *Host) Describe(engineType string) (engine.Description, error) {
	if h == nil || h.resolver == nil {
		return engine.Description{}, fmt.Errorf("no module resolver")
	}
	path, env, ok := h.resolver(engineType)
	if !ok {
		return engine.Description{}, fmt.Errorf("unknown engine %q (no module provides it)", engineType)
	}
	return DescribePlugin(path, env)
}

// DescribePlugin runs a plugin binary's __describe subcommand and decodes its
// engine.Description JSON, once per binary per process.
func DescribePlugin(path string, env []string) (engine.Description, error) {
	var key describeKey
	if fi, err := os.Stat(path); err == nil {
		key = describeKey{path: path, size: fi.Size(), mtime: fi.ModTime().UnixNano()}
		if d, ok := describeCache.Load(key); ok {
			return d.(engine.Description), nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), describeTimeout)
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
	if key.path != "" {
		describeCache.Store(key, desc)
	}
	return desc, nil
}

// defaultPort is the config hook behind a module block with no `port`: the
// port the module's own Describe gives, or 0 when it gives none (or cannot be
// asked), so the loader falls back to doze's table.
func defaultPort(resolve plugin.Resolver) func(engineType string) int {
	return func(engineType string) int {
		path, env, ok := resolve(engineType)
		if !ok {
			return 0
		}
		desc, err := DescribePlugin(path, env)
		if err != nil {
			return 0
		}
		return desc.Port
	}
}
