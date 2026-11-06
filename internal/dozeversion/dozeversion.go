// Package dozeversion holds the running doze's own version, for the parts
// that compare against it: which module releases this doze can run, and
// whether a doze.lock or doze.hcl asks for a newer doze. cmd/doze sets it at
// start; a development build stays "dev", which every check lets through.
package dozeversion

import (
	"strconv"
	"strings"
)

// Current is this doze's version ("0.2.1"), or "dev".
var Current = "dev"

// Released says v is a released doze's version (dotted numbers, an
// optional "v"), not a development build.
func Released(v string) bool {
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return false
	}
	for _, p := range strings.Split(v, ".") {
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}

// Older says a is an older doze than b; false when either is not a
// released version (a development build is never "older").
func Older(a, b string) bool {
	if !Released(a) || !Released(b) {
		return false
	}
	as := strings.Split(strings.TrimPrefix(a, "v"), ".")
	bs := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		x, y := 0, 0
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// NeedsNewer says this doze is older than need.
func NeedsNewer(need string) bool { return Older(Current, need) }

// Marker is the line a new doze.hcl starts with: the oldest doze the stack
// needs, this one's major and minor ("0.2"). Empty for a development build,
// which has no version to name.
func Marker() string {
	if !Released(Current) {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(Current, "v"), ".")
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return `doze = ">= ` + strings.Join(parts, ".") + `" # the oldest doze this stack needs` + "\n\n"
}
