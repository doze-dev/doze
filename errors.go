package doze

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors the library returns, so callers can branch on failure kind
// with errors.Is instead of string-matching. Operations wrap these with
// context; use errors.Is to test and errors.As is not needed.
var (
	// ErrNotFound means the named service isn't declared or doesn't exist.
	ErrNotFound = errors.New("doze: service not found")
	// ErrAlreadyExists means a live Add targeted a name already in the stack.
	ErrAlreadyExists = errors.New("doze: service already exists")
	// ErrPortConflict means two services want the same address.
	ErrPortConflict = errors.New("doze: port conflict")
	// ErrBootFailed means a service failed to boot or converge.
	ErrBootFailed = errors.New("doze: boot failed")
	// ErrUnsupported means the operation isn't supported for this engine
	// (e.g. live-adding a legacy AWS built-in).
	ErrUnsupported = errors.New("doze: unsupported operation")
)

// notFound wraps ErrNotFound with the offending name.
func notFound(name string) error {
	return fmt.Errorf("%q: %w", name, ErrNotFound)
}

// opError wraps an operation failure, classifying it against the sentinels by
// inspecting the underlying (possibly wire-transported) message so both the
// direct and socket backends land on the same typed error.
func opError(verb, name string, err error) error {
	if err == nil {
		return nil
	}
	if s := classify(err); s != nil {
		return fmt.Errorf("%s %q: %w", verb, name, s)
	}
	return fmt.Errorf("%s %q: %w", verb, name, err)
}

// classify maps a raw error to a sentinel when it already is one (direct
// backend) or its message matches a known daemon phrasing (socket backend).
func classify(err error) error {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrAlreadyExists),
		errors.Is(err, ErrPortConflict), errors.Is(err, ErrBootFailed),
		errors.Is(err, ErrUnsupported):
		return nil // already typed; opError will wrap it as-is
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "already exists"):
		return ErrAlreadyExists
	case strings.Contains(msg, "not found"), strings.Contains(msg, "no instance named"), strings.Contains(msg, "no service named"):
		return ErrNotFound
	case strings.Contains(msg, "port conflict"), strings.Contains(msg, "both use port"), strings.Contains(msg, "address already in use"):
		return ErrPortConflict
	case strings.Contains(msg, "not yet supported"), strings.Contains(msg, "unsupported"):
		return ErrUnsupported
	case strings.Contains(msg, "booting"), strings.Contains(msg, "converge"), strings.Contains(msg, "boot failed"):
		return ErrBootFailed
	}
	return nil
}
