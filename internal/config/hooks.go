package config

// Hooks lets the caller integrate the module fetcher into config decode:
// pointing it at the configured mirror/pins, feeding it declared engine
// versions, and enriching lookup/decode errors with module context. A nil
// *Hooks (or any nil field) means "pure parse" — config decodes with no
// module integration.
type Hooks struct {
	// ConfigureModules is handed the decoded modules{} block so the plugin
	// module fetcher can be pointed at the configured mirror/versions before
	// any instance's driver is resolved.
	ConfigureModules func(ModulesConfig)
	// RequireEngine is told each declared (engine type, engine version) just
	// before the driver lookup that may fetch its module — so module selection
	// can pick a release supporting what the project declares.
	RequireEngine func(engineType, version string)
	// CheckSupport validates one declared (engine type, engine version)
	// against the resolved module's supported engine majors, after all driver
	// bodies are decoded. It returns an actionable error ("run 'doze modules
	// upgrade …'") for a version the pinned module can't serve.
	CheckSupport func(engineType, version string) error
	// LookupError returns the REAL reason a plugin engine failed to load (a
	// failed signature, a protocol/engine-support gate, a network error)
	// recorded by the module fetcher — so an unknown-engine diagnostic reads
	// as the actual failure, not "no such engine".
	LookupError func(engineType string) error
	// EngineNames returns the engine-type names the registry catalog offers —
	// so a typo'd block type gets a "did you mean postgres?" even when nothing
	// but `process` is compiled in. Best-effort, consulted only on the
	// unknown-engine error path.
	EngineNames func() []string
	// RemoteDecodeHint describes the module that decodes an engine type's
	// blocks (identity + pinned version) and, best-effort, whether a newer
	// release exists — appended to remote-decode errors only. It must never
	// fail: a "" return degrades to a bare diagnostic.
	RemoteDecodeHint func(engineType string) string
}

// The invokers below are nil-safe on both the receiver and the field, so the
// decode pipeline calls them unconditionally.

func (h *Hooks) configureModules(mc ModulesConfig) {
	if h == nil || h.ConfigureModules == nil {
		return
	}
	h.ConfigureModules(mc)
}

func (h *Hooks) requireEngine(engineType, version string) {
	if h == nil || h.RequireEngine == nil {
		return
	}
	h.RequireEngine(engineType, version)
}

func (h *Hooks) checkSupport(engineType, version string) error {
	if h == nil || h.CheckSupport == nil {
		return nil
	}
	return h.CheckSupport(engineType, version)
}

func (h *Hooks) lookupError(engineType string) error {
	if h == nil || h.LookupError == nil {
		return nil
	}
	return h.LookupError(engineType)
}

func (h *Hooks) engineNames() []string {
	if h == nil || h.EngineNames == nil {
		return nil
	}
	return h.EngineNames()
}

// remoteDecodeErrSuffix renders the hook's hint as an error suffix.
func (h *Hooks) remoteDecodeErrSuffix(engineType string) string {
	if h == nil || h.RemoteDecodeHint == nil {
		return ""
	}
	if hint := h.RemoteDecodeHint(engineType); hint != "" {
		return "\n  " + hint
	}
	return ""
}
