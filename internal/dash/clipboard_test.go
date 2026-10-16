package dash

import "errors"

// No test may touch the real clipboard or tmux: the tools are stubbed for the
// whole package, and the ones that test the transport put their own in.
func init() {
	copyNative = func(string) (string, error) { return "", errors.New("no clipboard in tests") }
	copyTmux = func(string) error { return errors.New("no tmux in tests") }
}

func init() { rememberTheme = func(string) {} } // a test never writes the user's config
