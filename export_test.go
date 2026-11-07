package doze

import "github.com/doze-dev/doze/internal/modules"

// UseLocalPluginForTest runs a module built from source for engineType in
// place of the registry's, until the returned func is called. Tests only.
func UseLocalPluginForTest(engineType, path string) (restore func()) {
	return modules.UseLocalPlugin(engineType, path)
}
