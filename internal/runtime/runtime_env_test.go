package runtime

import (
	"strings"
	"testing"

	"github.com/doze-dev/doze-sdk/engine"
)

func TestExtraEnvReachesSpecsTasksAndStepsAndKeepsTheInheritedEnvironment(t *testing.T) {
	r := &Runtime{}
	if got := r.envWithExtra(nil); got != nil {
		t.Fatalf("with nothing extra, nil stays nil (inherit): %v", got)
	}
	r.SetExtraEnv([]string{"DOZE_CA_FILE=/x/root.crt", "SSL_CERT_FILE=/x/bundle.pem"})
	got := r.envWithExtra(nil)
	has := func(prefix string) bool {
		for _, kv := range got {
			if strings.HasPrefix(kv, prefix) {
				return true
			}
		}
		return false
	}
	if !has("PATH=") || !has("DOZE_CA_FILE=/x/root.crt") || !has("SSL_CERT_FILE=/x/bundle.pem") {
		t.Errorf("a nil env becomes this process's plus the extras: %d vars, PATH %v", len(got), has("PATH="))
	}
	own := r.envWithExtra([]string{"A=1"})
	if len(own) != 3 || own[0] != "A=1" || own[2] != "SSL_CERT_FILE=/x/bundle.pem" {
		t.Errorf("a given env keeps its own and gains the extras: %v", own)
	}
	plan := r.withExtraEnv(engine.SpawnPlan{Specs: []engine.SpawnSpec{{Name: "a", Env: []string{"B=2"}}, {Name: "b"}}})
	if len(plan.Specs[0].Env) != 3 || plan.Specs[0].Env[0] != "B=2" || len(plan.Specs[1].Env) < 3 {
		t.Errorf("every spec of a plan gets the extras: %v / %d", plan.Specs[0].Env, len(plan.Specs[1].Env))
	}
}

// doze's variables replace what a process merely inherited, and yield to
// what it was given on purpose: an app pointing REQUESTS_CA_BUNDLE at a
// corporate bundle keeps it.
func TestAProcessesOwnVariableWinsOverDozesAndAnInheritedOneDoesNot(t *testing.T) {
	t.Setenv("SSL_CERT_FILE", "/inherited/certs.pem")
	r := &Runtime{}
	r.SetExtraEnv([]string{"SSL_CERT_FILE=/x/bundle.pem", "REQUESTS_CA_BUNDLE=/x/bundle.pem"})
	got := r.envWithExtra([]string{"A=1", "SSL_CERT_FILE=/inherited/certs.pem", "REQUESTS_CA_BUNDLE=/corp/bundle.pem"})
	want := []string{"A=1", "REQUESTS_CA_BUNDLE=/corp/bundle.pem", "SSL_CERT_FILE=/x/bundle.pem"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("env = %v\nwant %v", got, want)
	}
	// A nil env is this process's own: the inherited copy is replaced, once.
	n := 0
	for _, kv := range r.envWithExtra(nil) {
		if strings.HasPrefix(kv, "SSL_CERT_FILE=") {
			n++
			if kv != "SSL_CERT_FILE=/x/bundle.pem" {
				t.Errorf("inherited SSL_CERT_FILE survived: %s", kv)
			}
		}
	}
	if n != 1 {
		t.Errorf("SSL_CERT_FILE appears %d times, want once", n)
	}
}
