package postgres

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nerdmenot/doze/internal/engine"
)

// VersionMismatchError is returned when a data dir's PG_VERSION does not match
// the major version the instance is declared to use.
type VersionMismatchError struct {
	Name     string
	OnDisk   string
	Declared string
}

func (e *VersionMismatchError) Error() string {
	return fmt.Sprintf("instance %q: data directory is locked to Postgres %s but config declares version %s. "+
		"Run `doze migrate %s` to upgrade.", e.Name, e.OnDisk, e.Declared, e.Name)
}

// provisioned reports whether a data dir has already been initialized.
func provisioned(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, "PG_VERSION"))
	return err == nil
}

// provision makes a data directory ready to boot: initdb if needed, verify the
// version lock, and (re)write the tuned configuration. Idempotent.
func provision(ctx context.Context, inst engine.Instance, tc engine.Toolchain, cfg *Config) error {
	if !provisioned(inst.DataDir) {
		if err := initdb(ctx, inst, tc); err != nil {
			return err
		}
	}
	if err := checkVersion(inst); err != nil {
		return err
	}
	if err := writeConf(inst.DataDir, cfg); err != nil {
		return err
	}
	return writeHBA(inst.DataDir)
}

// majorOf returns the major version string from a spec ("16" from "16" or "16.14").
func majorOf(spec engine.VersionSpec) string {
	s := spec.String()
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return s[:i]
	}
	return s
}

func checkVersion(inst engine.Instance) error {
	raw, err := os.ReadFile(filepath.Join(inst.DataDir, "PG_VERSION"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	onDisk := strings.TrimSpace(string(raw))
	declared := majorOf(inst.Version)
	if onDisk != declared {
		return &VersionMismatchError{Name: inst.Name, OnDisk: onDisk, Declared: declared}
	}
	return nil
}

func initdb(ctx context.Context, inst engine.Instance, tc engine.Toolchain) error {
	if err := os.MkdirAll(filepath.Dir(inst.DataDir), 0o700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, tc.Path("initdb"),
		"-D", inst.DataDir,
		"-U", "postgres",
		"-A", "trust",
		"-E", "UTF8",
		"--no-sync",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("initdb for %q failed: %w\n%s", inst.Name, err, out.String())
	}
	return nil
}

// writeConf writes doze's tuned settings into doze.conf, included by the main
// postgresql.conf so we never clobber the initdb-generated file.
func writeConf(dataDir string, cfg *Config) error {
	var b strings.Builder
	b.WriteString("# Managed by doze — do not edit. Regenerated on every boot.\n")
	for _, kv := range settings(cfg) {
		fmt.Fprintf(&b, "%s = %s\n", kv[0], kv[1])
	}
	if err := os.WriteFile(filepath.Join(dataDir, "doze.conf"), []byte(b.String()), 0o600); err != nil {
		return err
	}
	return ensureInclude(dataDir)
}

func ensureInclude(dataDir string) error {
	mainConf := filepath.Join(dataDir, "postgresql.conf")
	data, err := os.ReadFile(mainConf)
	if err != nil {
		return err
	}
	const directive = "include = 'doze.conf'"
	if bytes.Contains(data, []byte(directive)) {
		return nil
	}
	f, err := os.OpenFile(mainConf, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n# Added by doze\n%s\n", directive)
	return err
}

// lockedSettings are parameters doze always controls; a user `settings = {}`
// entry for one of these is ignored (with the locked value winning) so the
// socket-only, single-instance model can't be broken from config.
var lockedSettings = map[string]bool{
	"listen_addresses":        true,
	"port":                    true,
	"unix_socket_directories": true,
	"hba_file":                true,
	"data_directory":          true,
}

func settings(cfg *Config) [][2]string {
	onoff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	out := [][2]string{
		{"shared_buffers", quote(cfg.SharedBuffers)},
		{"max_connections", strconv.Itoa(cfg.MaxConnections)},
		{"fsync", onoff(cfg.Fsync)},
		{"autovacuum", onoff(cfg.Autovacuum)},
		// The light/dev profile: no point being synchronous if not fsyncing.
		{"synchronous_commit", onoff(cfg.Fsync)},
		{"full_page_writes", onoff(cfg.Fsync)},
	}
	// Raw postgresql.conf passthrough, applied after the typed tuning so it can
	// override it. Sorted for deterministic output. Locked params are skipped.
	for _, k := range sortedKeys(cfg.Settings) {
		if lockedSettings[strings.ToLower(k)] {
			continue
		}
		out = append(out, [2]string{k, quote(cfg.Settings[k])})
	}
	// TCP off entirely; clients reach the backend only via the unix socket. Emitted
	// last so it always wins, even over a user `settings` entry.
	out = append(out, [2]string{"listen_addresses", quote("")})
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func writeHBA(dataDir string) error {
	const hba = `# Managed by doze — local trust only.
# TYPE  DATABASE  USER  ADDRESS  METHOD
local   all       all            trust
`
	return os.WriteFile(filepath.Join(dataDir, "pg_hba.conf"), []byte(hba), 0o600)
}
