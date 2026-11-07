//go:build dozetest

package modules

import (
	"os"
	"strings"
)

// A dozetest build (the integration suite's doze) runs a module built from
// source when DOZE_TEST_<TYPE>_PLUGIN names its binary. Release builds do not
// compile this file.
func init() {
	localPluginFromEnv = func(engineType string) (string, bool) {
		p := os.Getenv("DOZE_TEST_" + strings.ToUpper(engineType) + "_PLUGIN")
		if p == "" {
			return "", false
		}
		if fi, err := os.Stat(p); err != nil || fi.IsDir() {
			return "", false
		}
		return p, true
	}
}
