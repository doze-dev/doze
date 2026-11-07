package modules

import "sync"

// Modules come only from the registry. Tests are the one exception: they run a
// module built from source in place of a fetched one. A test in this process
// calls UseLocalPlugin; a doze binary built with the dozetest build tag (the
// integration suite builds one) also reads DOZE_TEST_<TYPE>_PLUGIN, see
// localplugin_dozetest.go. A release build has neither path.

var (
	localMu      sync.RWMutex
	localPlugins = map[string]string{}

	// localPluginFromEnv is set only in a dozetest build.
	localPluginFromEnv func(engineType string) (string, bool)
)

// UseLocalPlugin makes engineType resolve to the plugin binary at path, ahead
// of the registry, until the returned func is called. For tests.
func UseLocalPlugin(engineType, path string) (restore func()) {
	localMu.Lock()
	old, had := localPlugins[engineType]
	localPlugins[engineType] = path
	localMu.Unlock()
	return func() {
		localMu.Lock()
		defer localMu.Unlock()
		if had {
			localPlugins[engineType] = old
		} else {
			delete(localPlugins, engineType)
		}
	}
}

// LocalPlugin returns the locally built plugin a test put in place of the
// registry module for engineType, if any.
func LocalPlugin(engineType string) (string, bool) {
	localMu.RLock()
	p, ok := localPlugins[engineType]
	localMu.RUnlock()
	if ok {
		return p, true
	}
	if localPluginFromEnv != nil {
		return localPluginFromEnv(engineType)
	}
	return "", false
}

// LocalLookup is LocalPlugin in the shape of a plugin resolver.
func LocalLookup(engineType string) (string, []string, bool) {
	p, ok := LocalPlugin(engineType)
	return p, nil, ok // inherit the daemon's env (DOZE_HOME, PATH, …)
}
