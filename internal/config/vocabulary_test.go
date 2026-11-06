package config

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"

	"github.com/doze-dev/doze/engine/process"
)

var update = flag.Bool("update", false, "rewrite website/public/schema/doze-hcl.json")

// editorSchemaPath is where the website serves the editor schema.
var editorSchemaPath = filepath.Join("..", "..", "website", "public", "schema", "doze-hcl.json")

// The published schema is the vocabulary as it stands: regenerate it with
// `go generate ./internal/config`.
func TestEditorSchema(t *testing.T) {
	got, err := EditorSchema()
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.MkdirAll(filepath.Dir(editorSchemaPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(editorSchemaPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(editorSchemaPath)
	if err != nil {
		t.Fatalf("%v: run go generate ./internal/config", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is out of date: run go generate ./internal/config", editorSchemaPath)
	}
}

// names lists a schema's attribute and block names, less the ones dropped.
func schemaNames(s *hcl.BodySchema, drop ...string) (attrs, blocks []string) {
	for _, a := range s.Attributes {
		if !slices.Contains(drop, a.Name) {
			attrs = append(attrs, a.Name)
		}
	}
	for _, b := range s.Blocks {
		if !slices.Contains(drop, b.Type) {
			blocks = append(blocks, b.Type)
		}
	}
	return attrs, blocks
}

func implied(t *testing.T, v any) *hcl.BodySchema {
	t.Helper()
	s, _ := gohcl.ImpliedBodySchema(v)
	return s
}

func sameSet(t *testing.T, what string, got, want []string) {
	t.Helper()
	got, want = slices.Clone(got), slices.Clone(want)
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s: the vocabulary says %v, the loader reads %v", what, got, want)
	}
}

func argNames(args []VocabArg) []string {
	var out []string
	for _, a := range args {
		out = append(out, a.Name)
	}
	return out
}

func blockNames(blocks []VocabBlock) []string {
	var out []string
	for _, b := range blocks {
		out = append(out, b.Name)
	}
	return out
}

// The vocabulary names exactly what the loader reads, block by block: an
// argument added to one and not the other fails here.
func TestVocabularyMatchesTheLoader(t *testing.T) {
	root := Vocabulary()
	byName := map[string]VocabBlock{}
	for _, b := range root.Blocks {
		byName[b.Name] = b
	}

	// the root, less a stack-wide listen (an old form, refused)
	attrs, blocks := schemaNames(rootSchema, "listen")
	sameSet(t, "root arguments", argNames(root.Args), attrs)
	sameSet(t, "root blocks", blockNames(root.Blocks), blocks)
	for _, h := range rootSchema.Blocks {
		if b, ok := byName[h.Type]; ok && !slices.Equal(b.Labels, h.LabelNames) {
			t.Errorf("%s labels: the vocabulary says %v, the loader reads %v", h.Type, b.Labels, h.LabelNames)
		}
	}

	meta, _ := schemaNames(instanceMetaSchema, "depends_on")
	commonArgs, _ := schemaNames(implied(t, &common{}))
	appOnly := []string{"domains", "cert", "key"}
	check := func(block string, want ...[]string) {
		t.Helper()
		var all []string
		for _, w := range want {
			all = append(all, w...)
		}
		sameSet(t, block+" arguments", argNames(byName[block].Args), all)
	}

	check("defaults", attrNames(implied(t, &hclDefaults{})))
	check("tls", attrNames(implied(t, &hclTLS{})))
	check("variable", attrNames(implied(t, &hclVariable{})))
	check("output", attrNames(implied(t, &hclOutput{})))

	// a module: the common fields an app alone may use dropped, its sleep, the
	// meta-arguments (its own come from the module)
	moduleCommon := slices.DeleteFunc(slices.Clone(commonArgs), func(n string) bool { return slices.Contains(appOnly, n) })
	check(KindModule, moduleCommon, []string{"sleep"}, meta)

	// an app: its engine's own schema, the common fields less version and
	// listen (refused on an app), the meta-arguments
	appAttrs, appBlocks := schemaNames(process.AppSchema())
	appCommon := slices.DeleteFunc(slices.Clone(commonArgs), func(n string) bool { return n == "version" || n == "listen" })
	check(KindApp, appAttrs, appCommon, meta)
	sameSet(t, "app blocks", blockNames(byName[KindApp].Blocks), appBlocks)

	svcAttrs, svcBlocks := schemaNames(implied(t, &hclService{}))
	check(KindService, svcAttrs, meta)
	sameSet(t, "service blocks", blockNames(byName[KindService].Blocks), svcBlocks)

	taskAttrs, _ := schemaNames(implied(t, &hclTask{}))
	check(KindTask, taskAttrs, meta)

	restart := attrNames(implied(t, &hclRestart{}))
	for _, kind := range []string{KindApp, KindService} {
		for _, b := range byName[kind].Blocks {
			if b.Name == "restart" {
				sameSet(t, kind+" restart arguments", argNames(b.Args), restart)
			}
		}
	}

	// required arguments are the ones the loader requires
	for _, tc := range []struct {
		block  string
		schema *hcl.BodySchema
	}{
		{KindService, implied(t, &hclService{})},
		{KindTask, implied(t, &hclTask{})},
		{"output", implied(t, &hclOutput{})},
		{KindApp, process.AppSchema()},
	} {
		for _, a := range tc.schema.Attributes {
			for _, v := range byName[tc.block].Args {
				if v.Name == a.Name && v.Required != a.Required {
					t.Errorf("%s.%s: required = %v in the vocabulary, %v in the loader", tc.block, a.Name, v.Required, a.Required)
				}
			}
		}
	}
}

func attrNames(s *hcl.BodySchema) []string {
	a, _ := schemaNames(s)
	return a
}
