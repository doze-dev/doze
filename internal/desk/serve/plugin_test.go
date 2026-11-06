package serve

import "testing"

func TestIsPlugin(t *testing.T) {
	for exe, want := range map[string]bool{
		"/Users/a/.doze/modules/doze/valkey/0.2.1-aarch64-apple-darwin/bin/valkey-plugin":                    true,
		"/var/folders/x/T/TestApp1/003/modules/doze/postgres/0.2.1-aarch64-apple-darwin/bin/postgres-plugin": true,
		"/usr/local/bin/valkey-server":                               false,
		"/Users/a/.doze/modules/doze/valkey/0.2.1/bin/valkey-server": false,
		"/opt/thing/bin/x-plugin":                                    false,
	} {
		if got := isPlugin(exe); got != want {
			t.Errorf("isPlugin(%q) = %v, want %v", exe, got, want)
		}
	}
}
