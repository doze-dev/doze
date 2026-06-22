package postgres

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nerdmenot/doze/internal/engine"
)

// extensionAliases maps friendly config names to the identifier CREATE
// EXTENSION expects.
var extensionAliases = map[string]string{"pgvector": "vector"}

// Logf, if set, receives convergence progress/warnings. The runtime installs it.
var Logf = func(string, ...any) {}

// Converge brings an instance's cluster up to its declared state: roles, the
// database itself, schemas, extensions, and grants. Every step is idempotent.
// It connects over the backend's private unix socket as the postgres superuser
// (local trust). It does not seed data or run migrations. (engine.Converger)
func (Driver) Converge(ctx context.Context, inst engine.Instance, tc engine.Toolchain, _ engine.Endpoint) error {
	cfg, ok := inst.Spec.(*Config)
	if !ok || cfg == nil {
		return fmt.Errorf("instance %q: missing postgres config", inst.Name)
	}
	psql := &psqlRunner{bin: tc.Path("psql"), socketDir: inst.SocketDir, port: inst.Port}
	dbName := inst.Name

	// 1. Roles (before the database, so an owner role exists when we create it).
	for _, role := range cfg.Roles {
		if err := convergeRole(ctx, psql, role); err != nil {
			return fmt.Errorf("role %q: %w", role.Name, err)
		}
	}

	// 2. The database itself.
	if err := convergeDatabase(ctx, psql, dbName, cfg); err != nil {
		return fmt.Errorf("database %q: %w", dbName, err)
	}

	// 3. Schemas.
	for _, sc := range cfg.Schemas {
		stmt := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", sqlIdent(sc.Name))
		if sc.Owner != "" {
			stmt += " AUTHORIZATION " + sqlIdent(sc.Owner)
		}
		if err := psql.exec(ctx, dbName, stmt); err != nil {
			return fmt.Errorf("schema %q: %w", sc.Name, err)
		}
	}

	// 4. Extensions.
	inst2 := newInstaller(tc.Path("pg_config"))
	for _, ext := range cfg.Extensions {
		name := ext.Name
		if alias, ok := extensionAliases[ext.Name]; ok {
			name = alias
		}
		if ext.Source != "" && !inst2.available(name) {
			if err := inst2.install(name, resolveExtSource(cfg.BaseDir, ext.Source)); err != nil {
				Logf("warning: could not install extension %q for %q: %v", ext.Name, dbName, err)
				continue
			}
			Logf("installed extension %q into the Postgres toolchain", name)
		}
		if !inst2.available(name) {
			Logf("warning: extension %q is not available for %q (declare a `source` bundle, "+
				"or use a Postgres build that includes it)", ext.Name, dbName)
			continue
		}
		stmt := fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", sqlIdent(name))
		if ext.Schema != "" {
			stmt += " SCHEMA " + sqlIdent(ext.Schema)
		}
		if ext.Version != "" {
			stmt += " VERSION " + sqlLit(ext.Version)
		}
		if err := psql.exec(ctx, dbName, stmt); err != nil {
			Logf("warning: CREATE EXTENSION %q for %q failed: %v", ext.Name, dbName, err)
		}
	}

	// 5. Grants.
	for _, g := range cfg.Grants {
		if err := convergeGrant(ctx, psql, dbName, g); err != nil {
			return fmt.Errorf("grant to %q: %w", g.Role, err)
		}
	}
	return nil
}

func resolveExtSource(baseDir, source string) string {
	if strings.Contains(source, "://") || filepath.IsAbs(source) {
		return source
	}
	if baseDir == "" {
		baseDir = "."
	}
	return filepath.Join(baseDir, source)
}

func convergeRole(ctx context.Context, psql *psqlRunner, role Role) error {
	exists, err := psql.scalarBool(ctx, "postgres",
		fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = %s)", sqlLit(role.Name)))
	if err != nil {
		return err
	}
	verb := "CREATE ROLE"
	if exists {
		verb = "ALTER ROLE"
	}
	if err := psql.exec(ctx, "postgres", fmt.Sprintf("%s %s WITH %s", verb, sqlIdent(role.Name), roleOptions(role))); err != nil {
		return err
	}
	for _, parent := range role.MemberOf {
		if err := psql.exec(ctx, "postgres", fmt.Sprintf("GRANT %s TO %s", sqlIdent(parent), sqlIdent(role.Name))); err != nil {
			return fmt.Errorf("granting membership in %q: %w", parent, err)
		}
	}
	return nil
}

func roleOptions(r Role) string {
	flag := func(yes bool, on, off string) string {
		if yes {
			return on
		}
		return off
	}
	parts := []string{
		flag(r.Login, "LOGIN", "NOLOGIN"),
		flag(r.Superuser, "SUPERUSER", "NOSUPERUSER"),
		flag(r.CreateDB, "CREATEDB", "NOCREATEDB"),
		flag(r.CreateRole, "CREATEROLE", "NOCREATEROLE"),
		flag(r.Replication, "REPLICATION", "NOREPLICATION"),
		flag(r.Inherit, "INHERIT", "NOINHERIT"),
		"CONNECTION LIMIT " + strconv.Itoa(r.ConnectionLimit),
	}
	if r.Password != "" {
		parts = append(parts, "PASSWORD "+sqlLit(r.Password))
	}
	if r.ValidUntil != "" {
		parts = append(parts, "VALID UNTIL "+sqlLit(r.ValidUntil))
	}
	return strings.Join(parts, " ")
}

func convergeDatabase(ctx context.Context, psql *psqlRunner, name string, cfg *Config) error {
	exists, err := psql.scalarBool(ctx, "postgres",
		fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = %s)", sqlLit(name)))
	if err != nil {
		return fmt.Errorf("checking existence: %w", err)
	}
	if !exists {
		stmt := "CREATE DATABASE " + sqlIdent(name)
		var with []string
		if cfg.Owner != "" {
			with = append(with, "OWNER "+sqlIdent(cfg.Owner))
		}
		template := cfg.Template
		if (cfg.Encoding != "" || cfg.Locale != "") && template == "" {
			template = "template0"
		}
		if template != "" {
			with = append(with, "TEMPLATE "+sqlIdent(template))
		}
		if cfg.Encoding != "" {
			with = append(with, "ENCODING "+sqlLit(cfg.Encoding))
		}
		if cfg.Locale != "" {
			with = append(with, "LC_COLLATE "+sqlLit(cfg.Locale), "LC_CTYPE "+sqlLit(cfg.Locale))
		}
		if len(with) > 0 {
			stmt += " WITH " + strings.Join(with, " ")
		}
		if err := psql.exec(ctx, "postgres", stmt); err != nil {
			return fmt.Errorf("creating: %w", err)
		}
	} else if cfg.Owner != "" {
		if err := psql.exec(ctx, "postgres", fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", sqlIdent(name), sqlIdent(cfg.Owner))); err != nil {
			return fmt.Errorf("setting owner: %w", err)
		}
	}
	return nil
}

func convergeGrant(ctx context.Context, psql *psqlRunner, dbName string, g Grant) error {
	privs := strings.Join(g.Privileges, ", ")
	switch {
	case g.Database != "":
		return psql.exec(ctx, "postgres", fmt.Sprintf("GRANT %s ON DATABASE %s TO %s", privs, sqlIdent(g.Database), sqlIdent(g.Role)))
	case g.Objects != "":
		kind := strings.ToUpper(g.Objects)
		if err := psql.exec(ctx, dbName, fmt.Sprintf("GRANT %s ON ALL %s IN SCHEMA %s TO %s", privs, kind, sqlIdent(g.Schema), sqlIdent(g.Role))); err != nil {
			return err
		}
		return psql.exec(ctx, dbName, fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT %s ON %s TO %s", sqlIdent(g.Schema), privs, kind, sqlIdent(g.Role)))
	default:
		return psql.exec(ctx, dbName, fmt.Sprintf("GRANT %s ON SCHEMA %s TO %s", privs, sqlIdent(g.Schema), sqlIdent(g.Role)))
	}
}

// psqlRunner executes SQL against a backend over its unix socket.
type psqlRunner struct {
	bin       string
	socketDir string
	port      int
}

func (p *psqlRunner) base(db string) []string {
	return []string{
		"-h", p.socketDir, "-p", strconv.Itoa(p.port), "-U", "postgres", "-d", db,
		"-v", "ON_ERROR_STOP=1", "-X", "-q",
	}
}

func (p *psqlRunner) exec(ctx context.Context, db, sql string) error {
	_, err := p.output(ctx, append(p.base(db), "-c", sql))
	return err
}

func (p *psqlRunner) scalarBool(ctx context.Context, db, sql string) (bool, error) {
	out, err := p.output(ctx, append(p.base(db), "-tAc", sql))
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "t", nil
}

func (p *psqlRunner) output(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, p.bin, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

func sqlIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func sqlLit(s string) string   { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
