package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/doze-dev/doze/internal/config"
)

// The hcl examples in the docs are configs, not prose: each one is extracted
// and run through the offline pass, so an example that no longer parses fails
// here instead of in a reader's terminal.
//
// A fence is checked when it declares something (a module, app, service or
// task, or the stack name). A fence that only shows part of a block — a few
// arguments, a nested block — is not a config and is skipped, unless it uses a
// form doze no longer reads. An example may lean on blocks it does not show:
// a reference to something undeclared gets a stand-in and the fence is retried.

var docRoots = []string{
	"../../README.md",
	"../../CONTRIBUTING.md",
	"../../docs/getting-started.md",
	"../../website/src/content/docs",
}

// fence is one ```hcl block: where it is and what it holds.
type fence struct {
	file string
	line int // the line of the opening ```
	src  string
}

var (
	fenceOpen    = regexp.MustCompile("^(\\s*)```(?:hcl|terraform)\\b(.*)$")
	declares     = regexp.MustCompile(`(?m)^(?:module\s+\w+\s+"|app\s+"|service\s+"|task\s+"|stack\s*=)`)
	oldHeader    = regexp.MustCompile(`(?m)^(?:use\s+\w+\s+"|process\s+"|(?:postgres|valkey|kvrocks|ferret|mariadb|kafka|temporal|aws|documentdb|dynamodb|lambda|s3|sqs|sns)\s+"|modules \{|name\s*=)`)
	oldReference = regexp.MustCompile(`\b(?:postgres|valkey|kvrocks|ferret|mariadb|kafka|temporal|aws|s3|sqs|sns|process)\.[a-z_][a-z0-9_]*\.(?:url|address|host|port|name|arn|socket|env_var)\b`)
	undeclared   = regexp.MustCompile(`reference to undeclared (module|app|service|task) "([^"]+)"`)
	dependsOnGap = regexp.MustCompile(`depends_on references undeclared instance "([^"]+)"`)
	unknownVar   = regexp.MustCompile(`(?s)Unsupported attribute.*?\bvar\.([a-z_][a-z0-9_]*).*?does not have an attribute named`)
)

func fencesIn(t *testing.T, path string) []fence {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []fence
	var cur *fence
	indent := ""
	for i, line := range strings.Split(string(data), "\n") {
		if cur == nil {
			if m := fenceOpen.FindStringSubmatch(line); m != nil {
				indent = m[1]
				cur = &fence{file: path, line: i + 1}
			}
			continue
		}
		if strings.TrimSpace(line) == "```" {
			out = append(out, *cur)
			cur = nil
			continue
		}
		cur.src += strings.TrimPrefix(line, indent) + "\n"
	}
	return out
}

// published are the modules in the doze registry. The offline pass cannot know
// them, so the docs check does: an example for a module that is not there
// would never start.
var published = map[string]bool{
	"aws": true, "ferret": true, "kafka": true, "kvrocks": true,
	"mariadb": true, "postgres": true, "temporal": true, "valkey": true,

	// Not published: the module the module-author guide builds.
	"httpd": true,
}

// lintFence runs the offline pass over an example, standing in whatever the
// example references but does not show.
func lintFence(f fence) error {
	src := f.src
	for range 20 {
		cfg, err := config.ParseShape([]byte(src), filepath.Join(filepath.Dir(f.file), "doze.hcl"))
		if err == nil {
			for _, d := range cfg.Instances {
				if d.Kind == config.KindModule && d.Type != "standin" && !published[d.Type] {
					return fmt.Errorf("%s: the registry has no %q module", d.Title(), d.Type)
				}
			}
			return nil
		}
		msg := err.Error()
		if m := dependsOnGap.FindStringSubmatch(msg); m != nil {
			src += fmt.Sprintf("\nmodule standin %q {\n  port = 1\n}\n", m[1])
			continue
		}
		if m := undeclared.FindStringSubmatch(msg); m != nil {
			switch m[1] {
			case config.KindModule:
				src += fmt.Sprintf("\nmodule standin %q {\n  port = 1\n}\n", m[2])
			case config.KindApp:
				src += fmt.Sprintf("\napp %q {\n  run  = \"true\"\n  port = 1\n}\n", m[2])
			default:
				src += fmt.Sprintf("\n%s %q {\n  run = \"true\"\n}\n", m[1], m[2])
			}
			continue
		}
		if m := unknownVar.FindStringSubmatch(msg); m != nil {
			src += fmt.Sprintf("\nvariable %q {\n  default = \"x\"\n}\n", m[1])
			continue
		}
		return err
	}
	return fmt.Errorf("gave up standing in undeclared references")
}

func TestDocExamplesParse(t *testing.T) {
	var files []string
	for _, root := range docRoots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && (strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".mdx")) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	checked := 0
	for _, file := range files {
		for _, f := range fencesIn(t, file) {
			where := fmt.Sprintf("%s:%d", strings.TrimPrefix(f.file, "../../"), f.line)
			if !declares.MatchString(f.src) {
				// Part of a block. Not a config, but it must not teach a dead form.
				if m := oldHeader.FindString(f.src); m != "" {
					t.Errorf("%s: an example uses a form doze no longer reads: %q", where, strings.TrimSpace(m))
				} else if m := oldReference.FindString(f.src); m != "" {
					t.Errorf("%s: an example references a block type-first: %q", where, m)
				}
				continue
			}
			checked++
			if err := lintFence(f); err != nil {
				t.Errorf("%s: this example does not parse:\n%v", where, err)
			}
		}
	}
	if checked < 50 {
		t.Fatalf("only %d examples were checked — the docs moved, or the fence pattern stopped matching", checked)
	}
	t.Logf("checked %d examples in %d files", checked, len(files))
}
