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
