package postgres

import (
	"context"
	"fmt"

	"github.com/nerdmenot/doze/internal/engine"
)

// Object kinds tracked in the state file for plan/apply/destroy. Grants are
// intentionally not tracked: they are additive and re-applied idempotently by
// Converge, and dropping their role/database/schema removes them anyway.
const (
	kindRole      = "role"
	kindDatabase  = "database"
	kindSchema    = "schema"
	kindExtension = "extension"
)

// Objects implements engine.Inventory: the structural objects this instance
// manages, each fingerprinted so a plan can tell changed from unchanged. The
// order (roles, database, schemas, extensions) is the create order; deletes run
// in reverse.
func (Driver) Objects(inst engine.Instance) []engine.Object {
	cfg, ok := inst.Spec.(*Config)
	if !ok || cfg == nil {
		return nil
	}
	var objs []engine.Object
	for _, r := range cfg.Roles {
		objs = append(objs, engine.Object{Kind: kindRole, Name: r.Name, Hash: engine.HashOf(r)})
	}
	objs = append(objs, engine.Object{Kind: kindDatabase, Name: inst.Name, Hash: engine.HashOf(databaseFingerprint(cfg))})
	for _, s := range cfg.Schemas {
		objs = append(objs, engine.Object{Kind: kindSchema, Name: s.Name, Hash: engine.HashOf(s)})
	}
	for _, e := range cfg.Extensions {
		objs = append(objs, engine.Object{Kind: kindExtension, Name: e.Name, Hash: engine.HashOf(e)})
	}
	return objs
}

// databaseFingerprint captures the database-level fields a change to which should
// surface as an update in a plan.
func databaseFingerprint(cfg *Config) any {
	return struct {
		Owner, Encoding, LCCollate, LCCtype, Template, Tablespace, Comment string
		ConnectionLimit                                                    int
		IsTemplate, AllowConns                                             bool
	}{cfg.Owner, cfg.Encoding, cfg.LCCollate, cfg.LCCtype, cfg.Template, cfg.Tablespace, cfg.Comment, cfg.ConnectionLimit, cfg.IsTemplate, cfg.AllowConns}
}

// Prune implements engine.Pruner: drop the given previously-applied objects that
// are no longer declared. removed arrives in safe drop order (from the planner).
// Dropping a database or schema is destructive — that is the point of destroy and
// of removing a declaration. Connects as superuser over the backend socket.
func (Driver) Prune(ctx context.Context, inst engine.Instance, tc engine.Toolchain, _ engine.Endpoint, removed []engine.Object) error {
	psql := &psqlRunner{bin: tc.Path("psql"), socketDir: inst.SocketDir, port: inst.Port}
	for _, o := range removed {
		var stmt, db string
		switch o.Kind {
		case kindExtension:
			db, stmt = inst.Name, fmt.Sprintf("DROP EXTENSION IF EXISTS %s CASCADE", sqlIdent(o.Name))
		case kindSchema:
			db, stmt = inst.Name, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", sqlIdent(o.Name))
		case kindDatabase:
			// Terminate other sessions so the drop can proceed (PG13+ FORCE).
			db, stmt = "postgres", fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", sqlIdent(o.Name))
		case kindRole:
			db, stmt = "postgres", fmt.Sprintf("DROP ROLE IF EXISTS %s", sqlIdent(o.Name))
		default:
			continue
		}
		if err := psql.exec(ctx, db, stmt); err != nil {
			return fmt.Errorf("dropping %s %q: %w", o.Kind, o.Name, err)
		}
	}
	return nil
}
