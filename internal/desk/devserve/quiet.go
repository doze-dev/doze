package devserve

import (
	"context"
	"errors"
	"sync"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
)

// quiet is a daemon that answers its first snapshot and nothing after: the
// stale fixture, for the "daemon not answering" banner and frozen rows.
type quiet struct {
	*fakedaemon.Fake
	mu       sync.Mutex
	answered bool
}

var errQuiet = errors.New("the daemon is not answering")

func (q *quiet) Snapshot(ctx context.Context) (control.Snapshot, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.answered {
		return control.Snapshot{}, errQuiet
	}
	q.answered = true
	return q.Fake.Snapshot(ctx)
}

func (q *quiet) Subscribe(context.Context, control.Subscription, func(control.Frame) error) error {
	return errQuiet
}

func (q *quiet) Do(context.Context, control.Verb) error { return errQuiet }
