// Package detect reads a project directory and says what a doze.hcl for it
// would contain: the programs to run, the databases and queues they talk to,
// the one-off commands that prepare them. It reads files — go.mod, source
// imports and the environment variables the code asks for, package.json,
// compose files, a Makefile — and starts nothing.
//
// What it finds is a suggestion with the reason for each line, so the person
// who runs `doze init` can see why a block is there and delete it if the guess
// was wrong.
package detect

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Result is what a directory suggests.
type Result struct {
	Root     string
	Stack    string
	Apps     []App
	Modules  []Module
	Services []Service
	Tasks    []Task
	// EnvFile is ".env" when one exists beside the project.
	EnvFile string
	// Notes are things worth saying that no block carries.
	Notes []string
}

// App is a program the project runs.
type App struct {
	Name string
	Run  string
	Dir  string // relative to Root, "" for Root
	Port int    // 0 when none was found
	// ReadsPort says the code reads $PORT: it expects to be told where to
	// listen, so it wants a port declared even when none was written in it.
	ReadsPort bool
	Why       string
	// Uses are the environment variables its code reads that name something
	// doze provides: each is a reference to wire.
	Uses []Use
}

// Use is one environment variable an app reads that a module (or service) can
// answer, and what to put there.
type Use struct {
	Env  string
	Kind string // the module type or service name that answers it
	Attr string // "url" or "address"
}

// Module is a registry module the project talks to.
type Module struct {
	Type string
	Name string // the default block name
	Port int
	Why  string
}

// Service is something the project needs that is not a module.
type Service struct {
	Name  string
	Image string // from a compose file; "" when it came from code
	Port  int
	Why   string
}

// Task is a command that prepares something.
type Task struct {
	Name   string
	Run    string
	Manual bool
	Why    string
}

// kinds doze has modules for, by what makes the project need them.
type kind struct {
	module, name string
	port         int
	goPkgs       []string // prefixes of Go import paths
	npmPkgs      []string
	images       []string          // compose image names, without tag
	envs         map[string]string // env var -> attribute it wants
}

var kinds = []kind{
	{module: "postgres", name: "db", port: 5432,
		goPkgs:  []string{"github.com/jackc/pgx", "github.com/lib/pq", "gorm.io/driver/postgres", "github.com/uptrace/bun/driver/pgdriver"},
		npmPkgs: []string{"pg", "postgres", "@prisma/client", "typeorm", "sequelize"},
		images:  []string{"postgres", "postgis/postgis", "timescale/timescaledb"},
		envs:    map[string]string{"DATABASE_URL": "url", "POSTGRES_URL": "url", "POSTGRES_DSN": "url", "PG_URL": "url"}},
	{module: "valkey", name: "cache", port: 6379,
		goPkgs:  []string{"github.com/redis/go-redis", "github.com/go-redis/redis", "github.com/gomodule/redigo", "github.com/valkey-io/valkey-go"},
		npmPkgs: []string{"redis", "ioredis", "bullmq"},
		images:  []string{"redis", "valkey/valkey"},
		envs:    map[string]string{"REDIS_URL": "url", "VALKEY_URL": "url", "CACHE_ADDR": "address", "REDIS_ADDR": "address"}},
	{module: "kafka", name: "events", port: 9092,
		goPkgs:  []string{"github.com/twmb/franz-go", "github.com/segmentio/kafka-go", "github.com/IBM/sarama", "github.com/Shopify/sarama", "github.com/confluentinc/confluent-kafka-go"},
		npmPkgs: []string{"kafkajs", "node-rdkafka"},
		images:  []string{"confluentinc/cp-kafka", "bitnami/kafka", "apache/kafka", "redpandadata/redpanda"},
		envs:    map[string]string{"KAFKA_BROKERS": "address", "KAFKA_BOOTSTRAP_SERVERS": "address", "KAFKA_URL": "address"}},
	{module: "aws", name: "cloud", port: 4566,
		goPkgs:  []string{"github.com/aws/aws-sdk-go"},
		npmPkgs: []string{"aws-sdk", "@aws-sdk/client-s3", "@aws-sdk/client-sqs", "@aws-sdk/client-sns", "@aws-sdk/client-dynamodb"},
		images:  []string{"localstack/localstack"},
		envs:    map[string]string{"AWS_ENDPOINT": "url", "AWS_ENDPOINT_URL": "url", "AWS_ENDPOINT_URL_S3": "url", "LOCALSTACK_URL": "url"}},
	{module: "mariadb", name: "sql", port: 3306,
		goPkgs:  []string{"github.com/go-sql-driver/mysql"},
		npmPkgs: []string{"mysql", "mysql2"},
		images:  []string{"mariadb", "mysql"},
		envs:    map[string]string{"MYSQL_URL": "url", "MARIADB_URL": "url"}},
}

// serviceKinds are things a project needs that doze has no module for.
var serviceKinds = []struct {
	name, binary string
	port         int
	goPkgs       []string
	npmPkgs      []string
	images       []string
	envs         []string
}{
	{name: "mongo", binary: "mongod", port: 27017,
		goPkgs: []string{"go.mongodb.org/mongo-driver"}, npmPkgs: []string{"mongodb", "mongoose"},
		images: []string{"mongo"}, envs: []string{"MONGO_URL", "MONGODB_URI", "MONGO_URI"}},
}

var (
	reGetenv   = regexp.MustCompile(`(?:os\.Getenv|os\.LookupEnv|getenv|env)\(\s*"([A-Z][A-Z0-9_]+)"`)
	reImport   = regexp.MustCompile(`"([a-zA-Z0-9.\-]+\.[a-z]+/[^"\s]+)"`)
	rePortLit  = regexp.MustCompile(`(?:ListenAndServe\(\s*"[^"]*:|"PORT",\s*")(\d{2,5})"`)
	rePackage  = regexp.MustCompile(`(?m)^package\s+main\b`)
	reMakeRule = regexp.MustCompile(`(?m)^([a-zA-Z][a-zA-Z0-9_-]*):(?:\s|$)`)
)

// taskTargets are the Makefile targets that read as preparing something.
var taskTargets = map[string]bool{
	"migrate": true, "migrate-up": true, "db-migrate": true, "migrations": true,
	"seed": true, "db-seed": true, "setup": true, "bootstrap": true, "generate": true, "codegen": true,
}

// Scan reads dir.
func Scan(dir string) (Result, error) {
	res := Result{Root: dir, Stack: slug(filepath.Base(mustAbs(dir)))}
	mods := map[string]*Module{}
	svcs := map[string]*Service{}
	addModule := func(k kind, why string) {
		if _, ok := mods[k.module]; !ok {
			mods[k.module] = &Module{Type: k.module, Name: k.name, Port: k.port, Why: why}
		}
	}
	addService := func(name string, port int, image, why string) {
		if _, ok := svcs[name]; !ok {
			svcs[name] = &Service{Name: name, Port: port, Image: image, Why: why}
		}
	}
	noteImports := func(imports []string, source string) {
		for _, imp := range imports {
			for _, k := range kinds {
				if hasPrefix(imp, k.goPkgs) {
					addModule(k, "the code imports "+imp)
				}
			}
			for _, s := range serviceKinds {
				if hasPrefix(imp, s.goPkgs) {
					addService(s.name, s.port, "", "the code imports "+imp)
				}
			}
		}
	}

	// ── Go ──
	if mod, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		var reqs []string
		for _, line := range strings.Split(string(mod), "\n") {
			f := strings.Fields(strings.TrimSpace(line))
			if len(f) >= 2 && !strings.HasPrefix(f[0], "//") && f[0] != "module" && f[0] != "go" && f[0] != "require" && strings.Contains(f[0], ".") {
				reqs = append(reqs, f[0])
			}
		}
		noteImports(reqs, "go.mod")
		for _, m := range goMains(dir) {
			app := App{Name: m.name, Dir: "", Why: "a Go main package at " + displayDir(m.rel)}
			if m.rel == "." {
				app.Run = "go run ."
			} else {
				app.Run = "go run ./" + m.rel
			}
			src, imps := readSources(filepath.Join(dir, m.rel))
			noteImports(imps, m.rel)
			envs := uniq(reGetenv.FindAllStringSubmatch(src, -1))
			app.Uses = appUses(envs, mods, svcs, addModule, addService)
			if p := rePortLit.FindStringSubmatch(src); p != nil {
				app.Port = atoi(p[1])
			}
			for _, e := range envs {
				if e == "PORT" {
					app.ReadsPort = true
				}
			}
			res.Apps = append(res.Apps, app)
		}
	}

	// ── Node ──
	if raw, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Name         string            `json:"name"`
			Scripts      map[string]string `json:"scripts"`
			Dependencies map[string]string `json:"dependencies"`
			DevDeps      map[string]string `json:"devDependencies"`
		}
		if json.Unmarshal(raw, &pkg) == nil {
			deps := map[string]bool{}
			for d := range pkg.Dependencies {
				deps[d] = true
			}
			for d := range pkg.DevDeps {
				deps[d] = true
			}
			for _, k := range kinds {
				for _, n := range k.npmPkgs {
					if deps[n] {
						addModule(k, "package.json depends on "+n)
						break
					}
				}
			}
			for _, s := range serviceKinds {
				for _, n := range s.npmPkgs {
					if deps[n] {
						addService(s.name, s.port, "", "package.json depends on "+n)
						break
					}
				}
			}
			script := ""
			for _, s := range []string{"dev", "start", "serve"} {
				if _, ok := pkg.Scripts[s]; ok {
					script = s
					break
				}
			}
			if script != "" {
				name := slug(pkg.Name)
				if name == "" {
					name = "web"
				}
				res.Apps = append(res.Apps, App{
					Name: name, Run: packageManager(dir) + " run " + script,
					Why:  "package.json has a " + script + " script",
					Uses: appUses(uniq(reGetenv.FindAllStringSubmatch(readTree(dir, []string{".js", ".ts", ".mjs"}), -1)), mods, svcs, addModule, addService),
				})
			}
		}
	}

	// ── compose ──
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var c struct {
			Services map[string]struct {
				Image string   `yaml:"image"`
				Ports []string `yaml:"ports"`
			} `yaml:"services"`
		}
		if yaml.Unmarshal(raw, &c) != nil {
			continue
		}
		names := make([]string, 0, len(c.Services))
		for n := range c.Services {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			s := c.Services[n]
			img := imageBase(s.Image)
			matched := false
			for _, k := range kinds {
				if contains(k.images, img) {
					addModule(k, name+" has a "+img+" service")
					matched = true
				}
			}
			for _, sk := range serviceKinds {
				if contains(sk.images, img) {
					addService(sk.name, sk.port, s.Image, name+" has a "+img+" service")
					matched = true
				}
			}
			if !matched && s.Image != "" && !looksLikeBuild(n) {
				port := 0
				if len(s.Ports) > 0 {
					port = containerPort(s.Ports[0])
				}
				addService(slug(n), port, s.Image, name+" has a "+n+" service ("+s.Image+")")
			}
		}
		break
	}

	// ── Makefile ──
	for _, name := range []string{"Makefile", "makefile", "GNUmakefile"} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		var text strings.Builder
		for sc.Scan() {
			text.WriteString(sc.Text() + "\n")
		}
		f.Close()
		for _, m := range reMakeRule.FindAllStringSubmatch(text.String(), -1) {
			if taskTargets[m[1]] {
				res.Tasks = append(res.Tasks, Task{
					Name: slug(m[1]), Run: "make " + m[1],
					Manual: strings.Contains(m[1], "seed"), Why: name + " has a " + m[1] + " target",
				})
			}
		}
		break
	}

	// ── env files ──
	if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
		res.EnvFile = ".env"
	}
	if _, err := os.Stat(filepath.Join(dir, ".env.example")); err == nil && res.EnvFile == "" {
		res.Notes = append(res.Notes, ".env.example lists variables; copy it to .env and the apps will read it")
	}

	for _, k := range kinds {
		if m := mods[k.module]; m != nil {
			res.Modules = append(res.Modules, *m)
		}
	}
	for _, s := range sortedServices(svcs) {
		res.Services = append(res.Services, s)
	}
	return res, nil
}

// appUses resolves the env vars an app reads to the things that answer them,
// noting the module or service when the variable is the only sign of it.
func appUses(envs []string, mods map[string]*Module, svcs map[string]*Service,
	addModule func(kind, string), addService func(string, int, string, string)) []Use {
	var out []Use
	for _, e := range envs {
		for _, k := range kinds {
			if attr, ok := k.envs[e]; ok {
				addModule(k, "the code reads "+e)
				out = append(out, Use{Env: e, Kind: k.module, Attr: attr})
			}
		}
		for _, s := range serviceKinds {
			if contains(s.envs, e) {
				addService(s.name, s.port, "", "the code reads "+e)
				out = append(out, Use{Env: e, Kind: s.name, Attr: "address"})
			}
		}
	}
	return out
}

type goMain struct{ name, rel string }

// goMains finds the main packages of a Go module: under cmd/, and the root.
func goMains(dir string) []goMain {
	var out []goMain
	if hasMain(dir) {
		out = append(out, goMain{name: slug(filepath.Base(mustAbs(dir))), rel: "."})
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "cmd"))
	for _, e := range entries {
		if e.IsDir() && hasMain(filepath.Join(dir, "cmd", e.Name())) {
			out = append(out, goMain{name: slug(e.Name()), rel: "cmd/" + e.Name()})
		}
	}
	return out
}

func hasMain(dir string) bool {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil && rePackage.Match(b) {
			return true
		}
	}
	return false
}

// readSources returns the concatenated .go source of a directory (not its
// tests), and the import paths it names.
func readSources(dir string) (string, []string) {
	var b strings.Builder
	var imps []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		b.Write(raw)
		b.WriteByte('\n')
		for _, m := range reImport.FindAllStringSubmatch(importBlock(string(raw)), -1) {
			imps = append(imps, m[1])
		}
	}
	return b.String(), imps
}

// importBlock is the import section of a Go file, so string literals elsewhere
// are not taken for import paths.
func importBlock(src string) string {
	i := strings.Index(src, "import")
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if j := strings.Index(rest, "\nfunc "); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// readTree concatenates the files of a project with the given extensions, to a
// bounded depth and size, skipping what is not the project's own.
func readTree(dir string, exts []string) string {
	var b strings.Builder
	var walk func(d string, depth int)
	walk = func(d string, depth int) {
		if depth > 4 || b.Len() > 4<<20 {
			return
		}
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() {
				if n == "node_modules" || n == ".git" || n == "dist" || n == "build" || strings.HasPrefix(n, ".") {
					continue
				}
				walk(filepath.Join(d, n), depth+1)
				continue
			}
			for _, x := range exts {
				if strings.HasSuffix(n, x) {
					if raw, err := os.ReadFile(filepath.Join(d, n)); err == nil && len(raw) < 1<<20 {
						b.Write(raw)
						b.WriteByte('\n')
					}
				}
			}
		}
	}
	walk(dir, 0)
	return b.String()
}

func packageManager(dir string) string {
	switch {
	case exists(dir, "bun.lockb"), exists(dir, "bun.lock"):
		return "bun"
	case exists(dir, "pnpm-lock.yaml"):
		return "pnpm"
	case exists(dir, "yarn.lock"):
		return "yarn"
	}
	return "npm"
}

func exists(dir, name string) bool { _, err := os.Stat(filepath.Join(dir, name)); return err == nil }

// ── small helpers ──

func hasPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func uniq(matches [][]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

func imageBase(image string) string {
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		image = image[:i]
	}
	return image
}

func looksLikeBuild(name string) bool {
	n := strings.ToLower(name)
	return n == "app" || n == "api" || n == "web" || n == "worker"
}

func containerPort(spec string) int {
	parts := strings.Split(spec, ":")
	return atoi(strings.Split(parts[len(parts)-1], "/")[0])
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func mustAbs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

func displayDir(rel string) string {
	if rel == "." {
		return "the project root"
	}
	return rel
}

func sortedServices(m map[string]*Service) []Service {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Service, 0, len(names))
	for _, n := range names {
		out = append(out, *m[n])
	}
	return out
}
