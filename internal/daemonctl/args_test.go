package daemonctl

import (
	"reflect"
	"testing"
)

// The daemon must load the config with the same --var overrides as the command
// that started it, or it runs a different stack. They travel in the
// environment, after anything inherited, so the override wins.
func TestDaemonEnvCarriesVars(t *testing.T) {
	base := []string{"PATH=/bin", "DOZE_VAR_pg=15"}
	got := daemonEnv(base, map[string]string{"pg": "16", "domains": "false"})
	want := []string{"PATH=/bin", "DOZE_VAR_pg=15", "DOZE_VAR_domains=false", "DOZE_VAR_pg=16"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("daemonEnv = %q, want %q", got, want)
	}
	if got := daemonEnv(base, nil); !reflect.DeepEqual(got, base) {
		t.Fatalf("no vars: %q", got)
	}
}
