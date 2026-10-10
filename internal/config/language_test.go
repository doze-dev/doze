package config_test

import (
	"strings"
	"testing"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/engine/process"
	"github.com/doze-dev/doze/internal/config"
)

// whole is the example at the top of docs/design/next/config.md.
const whole = `
stack = "shop"

module postgres "db" {
  version = 17
  port    = 5432
  role "shop" {}
}

service "mongo" {
  download = "https://fastdl.mongodb.org/osx/mongodb-macos-arm64-8.0.4.tgz"
  run      = "bin/mongod --dbpath $DATA --port $PORT"
  port     = 27017
}

app "api" {
  run   = "go run ./cmd/api"
  port  = 8080
  after = [task.migrate]
  env = {
    DATABASE_URL = module.db.url
    MONGO_URL    = "mongodb://${service.mongo.address}"
  }
}

task "migrate" {
  run    = "go run ./cmd/migrate up"
  inputs = ["migrations/*.sql"]
  env    = { DATABASE_URL = module.db.url }
}
`

func shape(t *testing.T, src string) *config.Config {
	t.Helper()
	cfg, err := config.ParseShape([]byte(src), "/work/shop/doze.hcl")
	if err != nil {
		t.Fatalf("shape pass: %v", err)
	}
	return cfg
}

// wantErr runs the shape pass and requires an error holding every fragment.
func wantErr(t *testing.T, src string, fragments ...string) {
	t.Helper()
	_, err := config.ParseShape([]byte(src), "/work/shop/doze.hcl")
	if err == nil {
		t.Fatalf("expected an error mentioning %q, got none for:\n%s", fragments, src)
	}
	for _, f := range fragments {
		if !strings.Contains(err.Error(), f) {
			t.Errorf("error should mention %q, got:\n%v", f, err)
		}
	}
}

func TestTheWholeLanguage(t *testing.T) {
	cfg := shape(t, whole)
	if cfg.Stack() != "shop" {
		t.Errorf("stack = %q", cfg.Stack())
	}

	db := cfg.Lookup("db")
	if db == nil || db.Kind != config.KindModule || db.Type != "postgres" || db.Version != "17" {
		t.Fatalf("module db = %+v", db)
	}
	if got := db.Title(); got != `module postgres "db"` {
		t.Errorf("title = %s", got)
	}

	api := cfg.Lookup("api")
	if api == nil || api.Kind != config.KindApp || api.Type != config.AppEngine || api.Port != 8080 {
		t.Fatalf("app api = %+v", api)
	}
	// The module and the service it references are boot dependencies. The task
	// in `after` is recorded, but nothing boots a task yet.
	if len(api.Deps) != 2 || api.Deps[0].Name != "db" || api.Deps[1].Name != "mongo" ||
		api.Deps[0].Condition != engine.Healthy || api.Deps[1].Condition != engine.Healthy {
		t.Errorf("api deps = %+v", api.Deps)
	}
	if len(api.After) != 1 || api.After[0] != (config.Ref{Kind: config.KindTask, Name: "migrate"}) {
		t.Errorf("api after = %+v", api.After)
	}
	if len(api.Refs) != 2 {
		t.Errorf("api refs = %+v", api.Refs)
	}

	mongo := cfg.Service("mongo")
	if mongo == nil || mongo.Port != 27017 || mongo.Download[""] == "" || !strings.Contains(mongo.Run, "$PORT") {
		t.Fatalf("service mongo = %+v", mongo)
	}
	if mongo.Dir != "/work/shop" {
		t.Errorf("a service's dir defaults to the config's folder, got %q", mongo.Dir)
	}
	migrate := cfg.Task("migrate")
	if migrate == nil || len(migrate.Inputs) != 1 || len(migrate.Refs) != 1 || migrate.Refs[0].Name != "db" {
		t.Fatalf("task migrate = %+v", migrate)
	}

	if err := cfg.Unbuilt(); err == nil || !strings.Contains(err.Error(), `service "mongo"`) || !strings.Contains(err.Error(), `task "migrate"`) {
		t.Errorf("a stack with a service and a task is not runnable yet, got %v", err)
	}
}

func TestServiceAttributesReachAnApp(t *testing.T) {
	cfg, err := config.Parse([]byte(`
service "mongo" {
  run  = "mongod"
  port = 27017
}
app "api" {
  run = "x"
  env = {
    ADDR = service.mongo.address
    HOST = service.mongo.host
    PORT = service.mongo.port
    DATA = service.mongo.data
  }
}
`), "/work/shop/doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	env := envOf(t, cfg, "api")
	if env["ADDR"] != "127.0.0.1:27017" || env["HOST"] != "127.0.0.1" || env["PORT"] != "27017" || !strings.HasSuffix(env["DATA"], "/mongo") {
		t.Errorf("env = %v", env)
	}
}

func envOf(t *testing.T, cfg *config.Config, app string) map[string]string {
	t.Helper()
	spec, ok := cfg.Lookup(app).Spec.(*process.Config)
	if !ok {
		t.Fatalf("app %q spec is %T", app, cfg.Lookup(app).Spec)
	}
	return spec.Env
}

func TestAppReferencesAnApp(t *testing.T) {
	cfg, err := config.Parse([]byte(`
app "api" {
  run  = "x"
  port = 8080
}
app "web" {
  run = "y"
  env = { API_URL = app.api.url }
}
`), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if env := envOf(t, cfg, "web"); env["API_URL"] != "http://127.0.0.1:8080" {
		t.Errorf("API_URL = %q", env["API_URL"])
	}
	if deps := cfg.Lookup("web").Deps; len(deps) != 1 || deps[0].Name != "api" {
		t.Errorf("web deps = %+v", deps)
	}
}

func TestAfterOrdersInstances(t *testing.T) {
	cfg, err := config.Parse([]byte(`
app "worker" {
  run   = "x"
  after = [app.api]
}
app "api" {
  run = "y"
}
`), "doze.hcl", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if deps := cfg.Lookup("worker").Deps; len(deps) != 1 || deps[0].Name != "api" {
		t.Errorf("`after` should order the worker behind the api, got %+v", deps)
	}
}

func TestOldFormsNameTheNewOne(t *testing.T) {
	for name, tc := range map[string]struct {
		src  string
		want []string
	}{
		"bare module block": {`postgres "db" { version = 16 }`, []string{"starts with `module`", `module postgres "db"`}},
		"use":               {`use postgres "db" { version = 16 }`, []string{"`use` is now `module`", `module postgres "db"`}},
		"process":           {`process "api" { command = "x" }`, []string{"`process` is now `app`", `app "api"`, "`run`"}},
		"root name":         {`name = "shop"`, []string{"`name` is now `stack`", `stack = "shop"`}},
		"modules block":     {"modules {\n  mirror = \"https://example.com\"\n}", []string{"the `modules` block is gone", "one registry"}},
		"module process":    {`module process "api" { run = "x" }`, []string{"`process` is not a module", `app "api"`}},
		"keyword as type":   {`module task "x" {}`, []string{"is a keyword, not a module type"}},
		"a slip":            {`apps "api" { run = "x" }`, []string{`unknown block "apps"`, `did you mean "app"?`}},
		"type-first reference": {`
module postgres "db" { version = 16 }
app "api" {
  run = "x"
  env = { DATABASE_URL = postgres.db.url }
}`, []string{"starts with the kind", "write module.db in place of postgres.db"}},
		"process reference": {`
app "api" { run = "x" }
app "web" {
  run = "y"
  env = { API = process.api.url }
}`, []string{"write app.api in place of process.api"}},
		"old app arguments": {`
app "api" {
  command = "x"
}`, []string{"`command` is no longer an argument", "use `run`"}},
	} {
		t.Run(name, func(t *testing.T) { wantErr(t, tc.src, tc.want...) })
	}
}

func TestReferencesAreChecked(t *testing.T) {
	const base = `
module postgres "db" { version = 16 }
app "api" { run = "x" }
task "migrate" { run = "y" }
`
	for name, tc := range map[string]struct {
		src  string
		want []string
	}{
		"undeclared":       {`app "w" { run = "x"  env = { A = module.dbb.url } }`, []string{`undeclared module "dbb"`, "Did you mean module.db?"}},
		"wrong kind":       {`app "w" { run = "x"  env = { A = app.db.url } }`, []string{`"db" is a module, not an app`, "write module.db"}},
		"after undeclared": {`app "w" { run = "x"  after = [task.seed] }`, []string{`undeclared task "seed"`}},
		"after not a ref":  {`app "w" { run = "x"  after = ["migrate"] }`, []string{"invalid after", "after = [task.migrate, module.db]"}},
		"after too deep":   {`app "w" { run = "x"  after = [module.db.url] }`, []string{"invalid after"}},
		"in a service":     {`service "s" { run = "x"  env = { A = module.nope.url } }`, []string{`undeclared module "nope"`}},
		"in a task":        {`task "t" { run = "x"  after = [app.nope] }`, []string{`undeclared app "nope"`}},
	} {
		t.Run(name, func(t *testing.T) { wantErr(t, base+lines(tc.src), tc.want...) })
	}
}

func TestNamesAreUniqueAcrossKinds(t *testing.T) {
	wantErr(t, `
module postgres "db" { version = 16 }
service "db" { run = "x" }
`, `the name "db" is already taken`, "whatever the kind")
	wantErr(t, `
task "api" { run = "x" }
app "api" { run = "y" }
`, `the name "api" is already taken`)
}

func TestCyclesAcrossKinds(t *testing.T) {
	wantErr(t, `
app "api" {
  run   = "x"
  port  = 8080
  after = [task.migrate]
}
task "migrate" {
  run = "y"
  env = { API = app.api.url }
}
`, "dependency cycle", "api → migrate → api")
}

func TestServiceArguments(t *testing.T) {
	cfg := shape(t, `
service "minio" {
  run      = "./minio server $DATA --address :$PORT"
  port     = 9000
  download = {
    mac   = "https://dl.min.io/server/minio/release/darwin-arm64/minio"
    linux = "https://dl.min.io/server/minio/release/linux-amd64/minio"
  }
  setup         = "chmod +x minio"
  on_ready      = ["./mc alias set local", "./mc mb local/uploads"]
  stop          = "pkill minio"
  ready         = http("/minio/health/live")
  ready_timeout = "90s"
  sleep         = "10m"
  console       = "http://localhost:9001"
  logs          = "quiet"
  restart {
    policy = "always"
  }
}
`)
	s := cfg.Service("minio")
	if len(s.Download) != 2 || len(s.Setup) != 1 || len(s.OnReady) != 2 || len(s.Stop) != 1 {
		t.Fatalf("service = %+v", s)
	}
	if s.Ready == nil || s.Ready.Kind != "http" || s.ReadyTimeout.Seconds() != 90 || !s.QuietLogs || s.Restart.Policy != "always" {
		t.Fatalf("service = %+v", s)
	}

	for name, tc := range map[string]struct{ body, want string }{
		"no run":             {`port = 1`, `"run" is required`},
		"bad platform":       {`run = "x"  download = { windows = "https://x" }`, `"windows" is not a platform`},
		"download not a URL": {`run = "x"  download = "minio.tgz"`, "want an http:// or https:// URL"},
		"probe with no port": {`run = "x"  ready = tcp()`, "needs a `port` to probe"},
		"bad port":           {`run = "x"  port = 70000`, "port 70000 is out of range"},
		"bad sleep":          {`run = "x"  sleep = "soon"`, `want a duration such as "10m", or "never"`},
		"unknown argument":   {`run = "x"  container = "mongo:8"`, `"container" is not expected here`},
	} {
		t.Run(name, func(t *testing.T) {
			wantErr(t, "service \"s\" {\n"+strings.ReplaceAll(tc.body, "  ", "\n")+"\n}", tc.want)
		})
	}
	wantErr(t, "task \"t\" {\nrun = \"x\"\nport = 1\n}", `"port" is not expected here`)
}

// The shape pass must not need a module: an unknown type, and attributes only
// the module could vouch for, are the full load's to judge.
func TestShapePassResolvesNoModule(t *testing.T) {
	const src = `
module nosuchengine "jobs" {
  made_up = true
}
app "worker" {
  run = "x"
  env = {
    QUEUE = module.jobs.arn
    PORT  = module.jobs.port
  }
}
`
	cfg := shape(t, src)
	if d := cfg.Lookup("jobs"); d == nil || d.Spec != nil {
		t.Fatalf("the shape pass decodes no module body, got %+v", d)
	}
	if _, err := config.Parse([]byte(src), "doze.hcl", nil); err == nil || !strings.Contains(err.Error(), `no module called "nosuchengine"`) {
		t.Fatalf("the full load should reject the unknown module, got %v", err)
	}
}

// lines spreads a block written on one line for a test table over several:
// HCL lets a single-line block hold only one argument.
func lines(block string) string {
	block = strings.Replace(block, "{ ", "{\n", 1)
	block = strings.ReplaceAll(block, "  ", "\n")
	return strings.TrimSuffix(block, " }") + "\n}"
}

// A service is an instance of the command engine like an app, with its own
// kind, so the runtime boots, holds and sleeps it by the same rules.
func TestAServiceIsAnInstanceOfTheCommandEngine(t *testing.T) {
	cfg := shape(t, whole)
	mongo := cfg.Lookup("mongo")
	if mongo == nil || mongo.Kind != config.KindService || mongo.Type != config.AppEngine || !mongo.Enabled {
		t.Fatalf("service mongo = %+v", mongo)
	}
	if mongo.Title() != `service "mongo"` {
		t.Errorf("title = %s", mongo.Title())
	}
	if mongo.Port == 0 {
		t.Errorf("a service with a port has Port set: %+v", mongo)
	}
}
