package postgres

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"

	"github.com/nerdmenot/doze/internal/engine"
)

// Defaults for the light/dev tuning profile.
const (
	defaultSharedBuffers  = "16MB"
	defaultMaxConnections = 50
	unlimitedConnections  = -1
)

// Config is the Postgres-specific configuration decoded from a `postgres` block.
// It is stored opaquely in engine.Instance.Spec and type-asserted by the driver.
type Config struct {
	Owner     string
	Encoding  string
	Locale    string
	LCCollate string
	LCCtype   string
	Template  string

	// Database-level options.
	ConnectionLimit int // -1 = unlimited (default)
	IsTemplate      bool
	AllowConns      bool // default true
	Tablespace      string
	Comment         string

	// Light/dev tuning profile (per instance).
	SharedBuffers  string
	MaxConnections int
	Fsync          bool
	Autovacuum     bool

	// Settings is a raw postgresql.conf passthrough (work_mem, wal_level, …) for
	// any parameter doze doesn't model with a typed field. Applied after the typed
	// tuning, so it can override it; doze-locked parameters (listen_addresses) still
	// win.
	Settings map[string]string

	Roles      []Role
	Schemas    []Schema
	Extensions []Extension
	Grants     []Grant

	// BaseDir is the config file's directory, for resolving relative extension
	// source bundle paths.
	BaseDir string
}

// Role is a Postgres role (a "user" is a role with LOGIN).
type Role struct {
	Name            string
	Login           bool
	Password        string
	Superuser       bool
	CreateDB        bool
	CreateRole      bool
	Replication     bool
	Inherit         bool
	BypassRLS       bool
	ConnectionLimit int
	ValidUntil      string
	MemberOf        []string
	Comment         string
	// Config is a set of per-role parameters applied with ALTER ROLE … SET
	// (e.g. search_path, statement_timeout).
	Config map[string]string
}

// Schema is a schema to create within the database.
type Schema struct {
	Name  string
	Owner string
}

// Extension is an extension to create within the database.
type Extension struct {
	Name    string
	Version string
	Schema  string
	Source  string
	// Optional downgrades an unavailable extension or a failed CREATE EXTENSION
	// from a hard convergence error to a logged warning. Default false: a missing
	// or failed extension fails the apply (and taints the instance).
	Optional bool
	// Cascade adds CASCADE to CREATE EXTENSION, creating dependency extensions too.
	Cascade bool
}

// Grant is a privilege grant targeting a database, a schema, or all objects of
// a kind within a schema.
type Grant struct {
	Role            string
	Privileges      []string
	Database        string
	Schema          string
	Objects         string
	WithGrantOption bool
}

// --- raw HCL shapes (decode targets) ---

type pgBody struct {
	Owner           string            `hcl:"owner,optional"`
	Encoding        string            `hcl:"encoding,optional"`
	Locale          string            `hcl:"locale,optional"`
	LCCollate       string            `hcl:"lc_collate,optional"`
	LCCtype         string            `hcl:"lc_ctype,optional"`
	Template        string            `hcl:"template,optional"`
	ConnectionLimit *int              `hcl:"connection_limit,optional"`
	IsTemplate      *bool             `hcl:"is_template,optional"`
	AllowConns      *bool             `hcl:"allow_connections,optional"`
	Tablespace      string            `hcl:"tablespace,optional"`
	Comment         string            `hcl:"comment,optional"`
	SharedBuffers   string            `hcl:"shared_buffers,optional"`
	MaxConnections  int               `hcl:"max_connections,optional"`
	Fsync           *bool             `hcl:"fsync,optional"`
	Autovacuum      *bool             `hcl:"autovacuum,optional"`
	Settings        map[string]string `hcl:"settings,optional"`
	Extensions      []string          `hcl:"extensions,optional"`
	ExtensionBlocks []hclExtension    `hcl:"extension,block"`
	Roles           []hclRole         `hcl:"role,block"`
	Schemas         []hclSchema       `hcl:"schema,block"`
	Grants          []hclGrant        `hcl:"grant,block"`
}

type hclRole struct {
	Name            string            `hcl:"name,label"`
	Login           *bool             `hcl:"login,optional"`
	Password        string            `hcl:"password,optional"`
	Superuser       bool              `hcl:"superuser,optional"`
	CreateDB        bool              `hcl:"createdb,optional"`
	CreateRole      bool              `hcl:"createrole,optional"`
	Replication     bool              `hcl:"replication,optional"`
	Inherit         *bool             `hcl:"inherit,optional"`
	BypassRLS       bool              `hcl:"bypassrls,optional"`
	ConnectionLimit *int              `hcl:"connection_limit,optional"`
	ValidUntil      string            `hcl:"valid_until,optional"`
	MemberOf        []string          `hcl:"member_of,optional"`
	Comment         string            `hcl:"comment,optional"`
	Config          map[string]string `hcl:"config,optional"`
}

type hclExtension struct {
	Name     string `hcl:"name,label"`
	Version  string `hcl:"version,optional"`
	Schema   string `hcl:"schema,optional"`
	Source   string `hcl:"source,optional"`
	Optional bool   `hcl:"optional,optional"`
	Cascade  bool   `hcl:"cascade,optional"`
}

type hclSchema struct {
	Name  string `hcl:"name,label"`
	Owner string `hcl:"owner,optional"`
}

type hclGrant struct {
	Role            string   `hcl:"role"`
	Privileges      []string `hcl:"privileges"`
	Database        string   `hcl:"database,optional"`
	Schema          string   `hcl:"schema,optional"`
	Objects         string   `hcl:"objects,optional"`
	WithGrantOption bool     `hcl:"with_grant_option,optional"`
}

// DecodeConfig implements engine.ConfigDecoder for the postgres block.
func (Driver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, baseDir string) (engine.EngineConfig, error) {
	var raw pgBody
	if diags := gohcl.DecodeBody(body, ctx, &raw); diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}

	c := &Config{
		Owner:           raw.Owner,
		Encoding:        raw.Encoding,
		Locale:          raw.Locale,
		LCCollate:       raw.LCCollate,
		LCCtype:         raw.LCCtype,
		Template:        raw.Template,
		ConnectionLimit: unlimitedConnections,
		AllowConns:      true,
		Tablespace:      raw.Tablespace,
		Comment:         raw.Comment,
		SharedBuffers:   defaultSharedBuffers,
		MaxConnections:  defaultMaxConnections,
		Settings:        raw.Settings,
		BaseDir:         baseDir,
	}
	if raw.ConnectionLimit != nil {
		c.ConnectionLimit = *raw.ConnectionLimit
	}
	if raw.IsTemplate != nil {
		c.IsTemplate = *raw.IsTemplate
	}
	if raw.AllowConns != nil {
		c.AllowConns = *raw.AllowConns
	}
	if raw.SharedBuffers != "" {
		c.SharedBuffers = raw.SharedBuffers
	}
	if raw.MaxConnections != 0 {
		c.MaxConnections = raw.MaxConnections
	}
	if raw.Fsync != nil {
		c.Fsync = *raw.Fsync
	}
	if raw.Autovacuum != nil {
		c.Autovacuum = *raw.Autovacuum
	}

	roleSeen := map[string]bool{}
	for _, rr := range raw.Roles {
		role, err := normalizeRole(rr)
		if err != nil {
			return nil, err
		}
		if roleSeen[role.Name] {
			return nil, fmt.Errorf("role %q is declared more than once", role.Name)
		}
		roleSeen[role.Name] = true
		c.Roles = append(c.Roles, role)
	}

	for _, sc := range raw.Schemas {
		if sc.Name == "" {
			return nil, fmt.Errorf("schema block is missing a name label")
		}
		c.Schemas = append(c.Schemas, Schema{Name: sc.Name, Owner: sc.Owner})
	}

	extSeen := map[string]bool{}
	for _, name := range raw.Extensions {
		if !extSeen[name] {
			extSeen[name] = true
			c.Extensions = append(c.Extensions, Extension{Name: name})
		}
	}
	for _, ex := range raw.ExtensionBlocks {
		if ex.Name == "" {
			return nil, fmt.Errorf("extension block is missing a name label")
		}
		if extSeen[ex.Name] {
			return nil, fmt.Errorf("extension %q is declared more than once", ex.Name)
		}
		extSeen[ex.Name] = true
		c.Extensions = append(c.Extensions, Extension{Name: ex.Name, Version: ex.Version, Schema: ex.Schema, Source: ex.Source, Optional: ex.Optional, Cascade: ex.Cascade})
	}

	for i, g := range raw.Grants {
		grant, err := normalizeGrant(i, g)
		if err != nil {
			return nil, err
		}
		c.Grants = append(c.Grants, grant)
	}
	return c, nil
}

func normalizeRole(rr hclRole) (Role, error) {
	if rr.Name == "" {
		return Role{}, fmt.Errorf("role block is missing a name label")
	}
	role := Role{
		Name:            rr.Name,
		Login:           true,
		Password:        rr.Password,
		Superuser:       rr.Superuser,
		CreateDB:        rr.CreateDB,
		CreateRole:      rr.CreateRole,
		Replication:     rr.Replication,
		Inherit:         true,
		BypassRLS:       rr.BypassRLS,
		ConnectionLimit: unlimitedConnections,
		ValidUntil:      rr.ValidUntil,
		MemberOf:        rr.MemberOf,
		Comment:         rr.Comment,
		Config:          rr.Config,
	}
	if rr.Login != nil {
		role.Login = *rr.Login
	}
	if rr.Inherit != nil {
		role.Inherit = *rr.Inherit
	}
	if rr.ConnectionLimit != nil {
		role.ConnectionLimit = *rr.ConnectionLimit
	}
	return role, nil
}

var validGrantObjects = map[string]bool{"": true, "tables": true, "sequences": true, "functions": true}

var validPrivileges = map[string]bool{
	"ALL": true, "ALL PRIVILEGES": true,
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
	"TRUNCATE": true, "REFERENCES": true, "TRIGGER": true,
	"CREATE": true, "CONNECT": true, "TEMPORARY": true, "TEMP": true,
	"EXECUTE": true, "USAGE": true, "SET": true, "ALTER SYSTEM": true,
	"MAINTAIN": true,
}

func normalizeGrant(idx int, g hclGrant) (Grant, error) {
	loc := fmt.Sprintf("grant #%d", idx+1)
	if g.Role == "" {
		return Grant{}, fmt.Errorf("%s: missing required \"role\"", loc)
	}
	if len(g.Privileges) == 0 {
		return Grant{}, fmt.Errorf("%s: missing required \"privileges\"", loc)
	}
	if g.Database == "" && g.Schema == "" {
		return Grant{}, fmt.Errorf("%s: must target a \"database\" or a \"schema\"", loc)
	}
	if g.Database != "" && g.Schema != "" {
		return Grant{}, fmt.Errorf("%s: set either \"database\" or \"schema\", not both", loc)
	}
	if !validGrantObjects[g.Objects] {
		return Grant{}, fmt.Errorf("%s: invalid objects %q (want tables, sequences, or functions)", loc, g.Objects)
	}
	if g.Objects != "" && g.Schema == "" {
		return Grant{}, fmt.Errorf("%s: \"objects\" requires \"schema\"", loc)
	}
	for _, p := range g.Privileges {
		if !validPrivileges[strings.ToUpper(strings.TrimSpace(p))] {
			return Grant{}, fmt.Errorf("%s: unknown privilege %q", loc, p)
		}
	}
	return Grant{Role: g.Role, Privileges: g.Privileges, Database: g.Database, Schema: g.Schema, Objects: g.Objects, WithGrantOption: g.WithGrantOption}, nil
}
