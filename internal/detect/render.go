package detect

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/hclwrite"
)

// A known service: how to run it from a binary on the PATH, and as a container.
type known struct {
	binary string
	run    string // with $HOST, $PORT, $DATA
	port   int
	image  string // for the container form
	cport  int    // the port inside the container
}

var knownServices = map[string]known{
	"mongo":       {binary: "mongod", run: "mongod --dbpath $DATA --bind_ip $HOST --port $PORT", port: 27017, image: "mongo:7", cport: 27017},
	"mailpit":     {binary: "mailpit", run: "mailpit --smtp $HOST:$PORT --listen $HOST:$((PORT + 1)) --database $DATA/mail.db", port: 1025, image: "axllent/mailpit", cport: 1025},
	"minio":       {binary: "minio", run: "minio server $DATA --address $HOST:$PORT --console-address $HOST:$((PORT + 1))", port: 9000, image: "minio/minio", cport: 9000},
	"nats":        {binary: "nats-server", run: "nats-server -a $HOST -p $PORT -sd $DATA", port: 4222, image: "nats", cport: 4222},
	"meilisearch": {binary: "meilisearch", run: "meilisearch --http-addr $HOST:$PORT --db-path $DATA", port: 7700, image: "getmeili/meilisearch", cport: 7700},
	"memcached":   {binary: "memcached", run: "memcached -l $HOST -p $PORT", port: 11211, image: "memcached", cport: 11211},
	"rabbitmq":    {port: 5672, image: "rabbitmq:3", cport: 5672},
	"opensearch":  {port: 9200, image: "opensearchproject/opensearch", cport: 9200},
	"clickhouse":  {port: 8123, image: "clickhouse/clickhouse-server", cport: 8123},
}

// KnownService reports whether a service name is one doze knows how to run,
// and its conventional port.
func KnownService(name string) (port int, ok bool) {
	k, ok := knownServices[name]
	return k.port, ok
}

// ServiceBlock renders a `service` block for a name. found is the path of its
// binary when one is on the PATH ("" when not); image is a container image to
// use when there is none, from a compose file or the known table.
//
// The block is always something that runs or says clearly what is missing: a
// binary that is there is run; one that is not becomes a container command (a
// container is a command: `run` must stay in the foreground), or, when nothing
// is known, a download with the URL left to fill in.
func ServiceBlock(name string, port int, found, image string) string {
	k := knownServices[name]
	if port == 0 {
		port = k.port
	}
	var b strings.Builder
	fmt.Fprintf(&b, "service %q {\n", name)
	switch {
	case found != "" && k.run != "":
		fmt.Fprintf(&b, "  run  = %q\n", k.run)
	case image != "" || k.image != "":
		if image == "" {
			image = k.image
		}
		cport := k.cport
		if cport == 0 {
			cport = port
		}
		fmt.Fprintf(&b, "  # a container: a container is just a command, and `run` stays in the foreground\n")
		fmt.Fprintf(&b, "  run  = %q\n", fmt.Sprintf("docker run --rm -p $HOST:$PORT:%d %s", cport, image))
	default:
		fmt.Fprintf(&b, "  # fetched once, pinned in doze.lock by its checksum, and run from the archive\n")
		fmt.Fprintf(&b, "  download = \"https://example.com/%s-darwin-arm64.tar.gz\" # fill this in\n", name)
		fmt.Fprintf(&b, "  run      = \"./%s --port $PORT\"\n", name)
	}
	if port > 0 {
		fmt.Fprintf(&b, "  port = %d\n", port)
	}
	b.WriteString("}")
	return b.String()
}

// Versions says which version of each module type to write; a type with none
// is written without a version, which the lint then asks for.
type Versions map[string]string

// Scaffold renders a doze.hcl for what was found.
func (r Result) Scaffold(versions Versions, lookPath func(string) string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "stack = %q\n", r.Stack)

	moduleName := map[string]string{} // module type -> block name
	for _, m := range r.Modules {
		moduleName[m.Type] = m.Name
		b.WriteString("\n")
		fmt.Fprintf(&b, "# %s\n", m.Why)
		fmt.Fprintf(&b, "module %s %q {\n", m.Type, m.Name)
		if v := versions[m.Type]; v != "" {
			fmt.Fprintf(&b, "  version = %s\n", v)
		}
		if m.Port > 0 {
			fmt.Fprintf(&b, "  port    = %d\n", m.Port)
		}
		b.WriteString("}\n")
	}

	for _, s := range r.Services {
		b.WriteString("\n")
		fmt.Fprintf(&b, "# %s\n", s.Why)
		found := ""
		if k, ok := knownServices[s.Name]; ok && k.binary != "" && lookPath != nil {
			found = lookPath(k.binary)
		}
		b.WriteString(ServiceBlock(s.Name, s.Port, found, s.Image))
		b.WriteString("\n")
	}

	taskName := map[string]bool{}
	for _, t := range r.Tasks {
		taskName[t.Name] = true
		b.WriteString("\n")
		fmt.Fprintf(&b, "# %s\n", t.Why)
		fmt.Fprintf(&b, "task %q {\n  run = %q\n", t.Name, t.Run)
		if t.Manual {
			b.WriteString("  manual = true\n")
		}
		b.WriteString("}\n")
	}

	nextPort := 8080
	used := map[int]bool{}
	for _, a := range r.Apps {
		used[a.Port] = true
	}
	for _, a := range r.Apps {
		b.WriteString("\n")
		fmt.Fprintf(&b, "# %s\n", a.Why)
		fmt.Fprintf(&b, "app %q {\n  run = %q\n", a.Name, a.Run)
		port := a.Port
		if port == 0 && a.ReadsPort {
			// It asks for $PORT: give it one to be told, and say it is a guess.
			for used[nextPort] {
				nextPort++
			}
			port = nextPort
			used[port] = true
			b.WriteString("  # it reads $PORT, so it is given a port — change it if you like\n")
		}
		if port > 0 {
			fmt.Fprintf(&b, "  port = %d\n", port)
		}
		if r.EnvFile != "" {
			fmt.Fprintf(&b, "  env_file = %q\n", r.EnvFile)
		}
		var lines []string
		for _, u := range a.Uses {
			name := moduleName[u.Kind]
			if name == "" {
				name = u.Kind // a service
			}
			ref := "module." + name + "." + u.Attr
			if _, isModule := moduleName[u.Kind]; !isModule {
				ref = "service." + name + ".address"
				if u.Env == "MONGO_URL" || u.Env == "MONGODB_URI" || u.Env == "MONGO_URI" {
					ref = fmt.Sprintf("\"mongodb://${%s}\"", ref)
				}
			}
			lines = append(lines, fmt.Sprintf("    %s = %s", u.Env, ref))
		}
		sort.Strings(lines)
		if len(lines) > 0 {
			b.WriteString("\n  env = {\n" + strings.Join(lines, "\n") + "\n  }\n")
		}
		// An app that reads a database waits for its migration.
		for _, u := range a.Uses {
			if u.Kind == "postgres" || u.Kind == "mariadb" {
				for _, name := range []string{"migrate", "migrate-up", "db-migrate", "migrations"} {
					if taskName[name] {
						fmt.Fprintf(&b, "\n  after = [task.%s]\n", name)
					}
				}
				break
			}
		}
		b.WriteString("}\n")
	}
	return string(hclwrite.Format([]byte(b.String())))
}

// DefaultName is the block name `doze add` and `doze init` give a module type.
func DefaultName(moduleType string) string {
	for _, k := range kinds {
		if k.module == moduleType {
			return k.name
		}
	}
	return moduleType
}

// Wiring is how an app conventionally reads a module: the environment
// variable, and the module attribute that answers it.
func Wiring(moduleType string) (env, attr string) {
	switch moduleType {
	case "postgres":
		return "DATABASE_URL", "url"
	case "valkey", "kvrocks":
		return "REDIS_URL", "url"
	case "kafka":
		return "KAFKA_BROKERS", "address"
	case "aws":
		return "AWS_ENDPOINT_URL", "url"
	case "mariadb":
		return "MYSQL_URL", "url"
	}
	return "", ""
}

// ConventionalPort is the port a module type usually runs on.
func ConventionalPort(moduleType string) int {
	for _, k := range kinds {
		if k.module == moduleType {
			return k.port
		}
	}
	switch moduleType {
	case "kvrocks":
		return 6380
	case "ferret":
		return 27017
	case "temporal":
		return 7233
	}
	return 0
}

// DownloadBlock renders a `service` that is fetched, with the URL left for the
// person to fill in: doze cannot guess where an archive lives.
func DownloadBlock(name string, port int) string {
	if port == 0 {
		port = knownServices[name].port
	}
	var b strings.Builder
	fmt.Fprintf(&b, "service %q {\n", name)
	b.WriteString("  # fetched once, pinned in doze.lock by its checksum, and run from the archive\n")
	fmt.Fprintf(&b, "  download = \"https://example.com/%s-darwin-arm64.tar.gz\" # fill this in\n", name)
	fmt.Fprintf(&b, "  run      = \"./%s --port $PORT\"\n", name)
	if port > 0 {
		fmt.Fprintf(&b, "  port     = %d\n", port)
	}
	b.WriteString("}")
	return b.String()
}
