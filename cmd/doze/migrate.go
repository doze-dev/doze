package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/binaries"
	"github.com/doze-dev/doze/internal/config"
)

// migrateCmd moves a postgres instance's data to the major doze.hcl now
// declares. Postgres cannot read another major's data directory, so it is
// dumped with the old major's server, set aside (kept, never deleted), and
// restored into a fresh cluster on the new major, set up as the module sets
// one up (superuser postgres, local trust, UTF8).
func migrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate <name>",
		Short: "Move a postgres instance's data to the major version doze.hcl now declares",
		Long: `Move a postgres instance's data to the major version doze.hcl declares.

Change version in doze.hcl first (16 → 17), bring the stack down, then run
doze migrate db. doze dumps the data with Postgres 16, sets the old data
folder aside (it is kept, named for its version), makes a fresh Postgres 17
cluster and restores the dump into it. Bring the stack up again after.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return migrate(cmd.Context(), args[0])
		},
	}
}

func migrate(ctx context.Context, name string) error {
	cliVars, err := parseVarFlags(varFlags)
	if err != nil {
		return err
	}
	cfg, err := config.LoadShape(configPath, cliVars)
	if err != nil {
		return err
	}
	decl := cfg.Lookup(name)
	if decl == nil {
		return fmt.Errorf("doze.hcl declares nothing called %q", name)
	}
	if decl.Type != "postgres" {
		return fmt.Errorf("%s is %s; doze migrate moves postgres data between majors, and %s keeps its data across versions or has no such step", name, decl.Type, decl.Type)
	}
	if daemonRunning(cfg) {
		return errors.New("the stack is up: bring it down first (doze down), then run doze migrate again; nothing was changed")
	}
	dataDir := cfg.ClusterDir(name)
	raw, err := os.ReadFile(filepath.Join(dataDir, "PG_VERSION"))
	if os.IsNotExist(err) {
		return fmt.Errorf("%s has no data yet (%s): there is nothing to move; it starts on the declared version", name, dataDir)
	}
	if err != nil {
		return err
	}
	from := strings.TrimSpace(string(raw))
	to := string(decl.Version)
	if i := strings.IndexByte(to, '.'); i >= 0 {
		to = to[:i]
	}
	if from == to {
		return fmt.Errorf("%s is already on Postgres %s, the version doze.hcl declares; nothing to move", name, from)
	}

	plat, err := binaries.HostPlatform()
	if err != nil {
		return err
	}
	bm := binaries.NewManager(cfg.Home)
	binFor := func(major string) (string, error) {
		full, err := bm.ResolveMajor("postgres", major)
		if err != nil {
			return "", err
		}
		say("fetching Postgres %s (%s) if this machine does not have it", major, full)
		dir, _, err := bm.Ensure(ctx, "postgres", full, plat, "")
		return dir, err
	}
	oldBin, err := binFor(from)
	if err != nil {
		return fmt.Errorf("Postgres %s, to read the old data: %w", from, err)
	}
	newBin, err := binFor(to)
	if err != nil {
		return fmt.Errorf("Postgres %s: %w", to, err)
	}

	work, err := os.MkdirTemp("", "doze-migrate-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()
	// Sockets live in the work folder; a unix socket path must stay short.
	sock := filepath.Join(work, "s")
	if err := os.MkdirAll(sock, 0o700); err != nil {
		return err
	}
	dump := filepath.Join(filepath.Dir(dataDir), name+"-pg"+from+"-dump.sql")

	// 1. Dump with the old server, using the new pg_dumpall (the newer tools
	// read older servers; the reverse is not promised).
	say("reading %s's data with Postgres %s", name, from)
	stopOld, err := startPostgres(ctx, oldBin, dataDir, sock)
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, filepath.Join(newBin, "pg_dumpall"), "-h", sock, "-p", "5432", "-U", "postgres", "-f", dump).CombinedOutput()
	stopOld()
	if err != nil {
		return fmt.Errorf("dumping %s failed; nothing was changed: %v\n%s", name, err, out)
	}

	// 2. Set the old data aside, named for its version; it is never deleted.
	kept := fmt.Sprintf("%s.pg%s-%s", dataDir, from, time.Now().Format("20060102-150405"))
	if err := os.Rename(dataDir, kept); err != nil {
		return fmt.Errorf("setting the old data aside: %w (the dump is at %s)", err, dump)
	}
	restoreOld := func() { _ = os.RemoveAll(dataDir); _ = os.Rename(kept, dataDir) }

	// 3. A fresh cluster on the new major, set up as the module sets one up.
	say("making a Postgres %s cluster for %s", to, name)
	if out, err := exec.CommandContext(ctx, filepath.Join(newBin, "initdb"), "-D", dataDir, "-U", "postgres", "-A", "trust", "-E", "UTF8", "--no-sync").CombinedOutput(); err != nil {
		restoreOld()
		return fmt.Errorf("initdb for Postgres %s failed; the old data is back where it was: %v\n%s", to, err, out)
	}

	// 4. Restore into it. Lines that fail are said, not hidden: the role
	// postgres already exists, for one, and that is expected.
	say("restoring into Postgres %s", to)
	stopNew, err := startPostgres(ctx, newBin, dataDir, sock)
	if err != nil {
		restoreOld()
		return fmt.Errorf("%w; the old data is back where it was", err)
	}
	out, err = exec.CommandContext(ctx, filepath.Join(newBin, "psql"), "-h", sock, "-p", "5432", "-U", "postgres", "-d", "postgres", "-q", "-f", dump).CombinedOutput()
	stopNew()
	if err != nil {
		restoreOld()
		return fmt.Errorf("restoring failed; the old data is back where it was: %v\n%s", err, out)
	}
	errs := 0
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, "ERROR:") && !strings.Contains(l, `role "postgres" already exists`) {
			errs++
		}
	}
	fmt.Printf("✓ %s is on Postgres %s now. The Postgres %s data is kept at %s, and the dump at %s; delete them when you are sure.\n", name, to, from, kept, dump)
	if errs > 0 {
		fmt.Printf("! %d statements did not restore (often an extension the new major does not have yet). What psql said:\n%s\n", errs, strings.TrimSpace(string(out)))
	}
	fmt.Println("  Bring the stack up: doze up")
	return nil
}

// startPostgres runs a server on dataDir, listening only on a unix socket in
// sock, and waits for it; the returned func stops it.
func startPostgres(ctx context.Context, bin, dataDir, sock string) (func(), error) {
	cmd := exec.CommandContext(ctx, filepath.Join(bin, "postgres"), "-D", dataDir, "-k", sock, "-c", "listen_addresses=", "-p", "5432")
	var log strings.Builder
	cmd.Stdout, cmd.Stderr = &log, &log
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stop := func() {
		_ = cmd.Process.Signal(os.Interrupt) // fast shutdown
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			_ = cmd.Process.Kill()
		}
	}
	path := filepath.Join(sock, ".s.PGSQL.5432")
	for i := 0; i < 150; i++ {
		if c, err := net.Dial("unix", path); err == nil {
			_ = c.Close()
			// The socket answers before the server takes queries; pg_isready knows.
			if exec.Command(filepath.Join(bin, "pg_isready"), "-h", sock, "-p", "5432", "-q").Run() == nil {
				return stop, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	stop()
	return nil, fmt.Errorf("postgres did not start on %s:\n%s", dataDir, log.String())
}

// say prints a step.
func say(format string, a ...any) { fmt.Printf("→ "+format+"\n", a...) }
