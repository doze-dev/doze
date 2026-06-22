package engine

import "sort"

// drivers holds every registered engine driver, keyed by Type().
var drivers = map[string]Driver{}

// Register makes a driver available under its Type(). Engine packages call this
// from an init function; cmd/doze blank-imports them to wire the set.
func Register(d Driver) {
	drivers[d.Type()] = d
}

// Lookup returns the driver registered for an engine type.
func Lookup(engineType string) (Driver, bool) {
	d, ok := drivers[engineType]
	return d, ok
}

// Types returns the registered engine types, sorted.
func Types() []string {
	out := make([]string, 0, len(drivers))
	for t := range drivers {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
