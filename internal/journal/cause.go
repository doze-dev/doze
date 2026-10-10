package journal

import "context"

type causeKey struct{}

// WithCause records on ctx why a start or stop is being asked for, so the
// journal can say what woke an instance or put it to sleep.
func WithCause(ctx context.Context, kind, by string) context.Context {
	return context.WithValue(ctx, causeKey{}, Cause{Kind: kind, By: by})
}

// CauseOf returns the cause recorded on ctx. A context with none is taken to
// be a command: someone asked.
func CauseOf(ctx context.Context) Cause {
	if c, ok := ctx.Value(causeKey{}).(Cause); ok {
		return c
	}
	return Cause{Kind: ByCommand}
}
