package postgres

import (
	"strings"
	"testing"

	"github.com/nerdmenot/doze/internal/config"
)

func parsePG(t *testing.T, src string) *Config {
	t.Helper()
	cfg, err := config.Parse([]byte(src), "doze.hcl")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	inst := cfg.Lookup("app")
	if inst == nil {
		t.Fatal("instance app not found")
	}
	pg, ok := inst.Spec.(*Config)
	if !ok {
		t.Fatalf("spec is %T, want *postgres.Config", inst.Spec)
	}
	return pg
}

func TestPostgresBlockDecode(t *testing.T) {
	pg := parsePG(t, `
postgres "app" {
  version        = 16
  owner          = "app"
  encoding       = "UTF8"
  shared_buffers = "32MB"
  fsync          = true
  extensions     = ["uuid-ossp"]

  role "app" {
    password         = "secret"
    connection_limit = 20
    member_of        = ["rw"]
  }
  role "ro" { login = false }

  schema "billing" { owner = "app" }

  extension "postgis" {
    version = "3.4"
    schema  = "public"
  }

  grant {
    role       = "app"
    database   = "app"
    privileges = ["ALL"]
  }
  grant {
    role       = "ro"
    schema     = "public"
    objects    = "tables"
    privileges = ["SELECT"]
  }
}
`)
	if pg.Owner != "app" || pg.Encoding != "UTF8" || pg.SharedBuffers != "32MB" || !pg.Fsync {
		t.Errorf("scalars wrong: %+v", pg)
	}
	if len(pg.Roles) != 2 {
		t.Fatalf("roles = %+v", pg.Roles)
	}
	app := pg.Roles[0]
	if app.Name != "app" || app.Password != "secret" || !app.Login || !app.Inherit || app.ConnectionLimit != 20 {
		t.Errorf("app role = %+v", app)
	}
	if len(app.MemberOf) != 1 || app.MemberOf[0] != "rw" {
		t.Errorf("member_of = %v", app.MemberOf)
	}
	if pg.Roles[1].Login {
		t.Errorf("ro role login should be false")
	}
	if len(pg.Extensions) != 2 {
		t.Fatalf("extensions = %+v", pg.Extensions)
	}
	var postgis *Extension
	for i := range pg.Extensions {
		if pg.Extensions[i].Name == "postgis" {
			postgis = &pg.Extensions[i]
		}
	}
	if postgis == nil || postgis.Version != "3.4" || postgis.Schema != "public" {
		t.Errorf("postgis = %+v", postgis)
	}
	if len(pg.Schemas) != 1 || pg.Schemas[0].Name != "billing" || pg.Schemas[0].Owner != "app" {
		t.Errorf("schemas = %+v", pg.Schemas)
	}
	if len(pg.Grants) != 2 || pg.Grants[0].Database != "app" || pg.Grants[1].Objects != "tables" {
		t.Errorf("grants = %+v", pg.Grants)
	}
}

func TestExtensionOptionalDecode(t *testing.T) {
	pg := parsePG(t, `
postgres "app" {
  version = 16
  extension "postgis" {}
  extension "rum" { optional = true }
}
`)
	byName := map[string]Extension{}
	for _, e := range pg.Extensions {
		byName[e.Name] = e
	}
	if byName["postgis"].Optional {
		t.Errorf("postgis should be required (optional=false) by default")
	}
	if !byName["rum"].Optional {
		t.Errorf("rum should be optional")
	}
}

func TestSettingsPassthrough(t *testing.T) {
	pg := parsePG(t, `
postgres "app" {
  version = 16
  settings = {
    work_mem         = "8MB"
    listen_addresses = "*"   # locked: must be ignored
  }
}
`)
	kv := settings(pg)
	var listenIdx, workMemIdx = -1, -1
	for i, p := range kv {
		switch p[0] {
		case "listen_addresses":
			listenIdx = i
			if p[1] != "''" {
				t.Errorf("listen_addresses must stay locked to '', got %s", p[1])
			}
		case "work_mem":
			workMemIdx = i
			if p[1] != "'8MB'" {
				t.Errorf("work_mem = %s, want '8MB'", p[1])
			}
		}
	}
	if workMemIdx == -1 {
		t.Fatal("user setting work_mem not emitted")
	}
	if listenIdx == -1 || listenIdx != len(kv)-1 {
		t.Fatalf("listen_addresses must be emitted last (idx %d of %d)", listenIdx, len(kv))
	}
}

func TestDatabaseAndRoleOptions(t *testing.T) {
	pg := parsePG(t, `
postgres "app" {
  version          = 16
  connection_limit = 25
  is_template      = true
  lc_collate       = "C"
  comment          = "primary db"

  role "rls_admin" {
    bypassrls = true
    comment   = "owns RLS bypass"
    config    = { search_path = "app, public" }
  }
}
`)
	if pg.ConnectionLimit != 25 || !pg.IsTemplate || pg.LCCollate != "C" || pg.Comment != "primary db" {
		t.Errorf("db options wrong: %+v", pg)
	}
	if len(pg.Roles) != 1 {
		t.Fatalf("roles = %+v", pg.Roles)
	}
	r := pg.Roles[0]
	if !r.BypassRLS || r.Comment != "owns RLS bypass" || r.Config["search_path"] != "app, public" {
		t.Errorf("role options wrong: %+v", r)
	}
	// roleOptions must emit BYPASSRLS for an RLS-bypass role.
	if !strings.Contains(roleOptions(r), "BYPASSRLS") || strings.Contains(roleOptions(r), "NOBYPASSRLS") {
		t.Errorf("roleOptions missing BYPASSRLS: %s", roleOptions(r))
	}
}

func TestPostgresDefaults(t *testing.T) {
	pg := parsePG(t, `postgres "app" { version = 16 }`)
	if pg.SharedBuffers != defaultSharedBuffers || pg.MaxConnections != defaultMaxConnections {
		t.Errorf("tuning defaults wrong: %+v", pg)
	}
	if pg.Fsync || pg.Autovacuum {
		t.Errorf("fsync/autovacuum should default off")
	}
}

func TestGrantValidation(t *testing.T) {
	cases := []struct{ name, grant, want string }{
		{"no target", `grant {
    role       = "r"
    privileges = ["ALL"]
  }`, "database"},
		{"both targets", `grant {
    role       = "r"
    privileges = ["X"]
    database   = "d"
    schema     = "s"
  }`, "not both"},
		{"objects without schema", `grant {
    role       = "r"
    privileges = ["X"]
    database   = "d"
    objects    = "tables"
  }`, "requires"},
		{"bad objects", `grant {
    role       = "r"
    privileges = ["X"]
    schema     = "s"
    objects    = "bad"
  }`, "invalid objects"},
		{"bad privilege", `grant {
    role       = "r"
    privileges = ["NONSENSE"]
    schema     = "s"
  }`, "unknown privilege"},
	}
	for _, c := range cases {
		src := "postgres \"app\" {\n  version = 16\n  " + c.grant + "\n}"
		_, err := config.Parse([]byte(src), "doze.hcl")
		if err == nil {
			t.Errorf("%s: expected error", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q should mention %q", c.name, err.Error(), c.want)
		}
	}
}
