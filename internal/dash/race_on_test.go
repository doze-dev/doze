//go:build race

package dash

// raceEnabled: the race detector slows code several times over, so wall-clock
// limits in tests are not meaningful under it.
const raceEnabled = true
