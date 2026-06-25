package supervisor

import (
	"context"
	"sync"
)

// Composite supervises several Processes as one unit: it reports the primary's
// PID, merges their logs, stops them in reverse start order, and — if any one
// exits unexpectedly — tears the rest down and reports the unit as exited. This is
// the generic form of a multi-process SpawnPlan (e.g. documentdb's Postgres +
// FerretDB) so the runtime's restart/reap logic treats it like any single backend.
type Composite struct {
	procs    []*Process // start order; procs[0] is the primary (its PID is the unit's)
	exited   chan error
	stopOnce sync.Once
}

// NewComposite supervises procs as a unit (procs[0] is the primary). If any
// process exits, the others are stopped and the unit's Wait unblocks.
func NewComposite(procs []*Process) *Composite {
	c := &Composite{procs: procs, exited: make(chan error, 1)}
	for _, p := range procs {
		go c.watch(p)
	}
	return c
}

func (c *Composite) watch(p *Process) {
	err := p.Wait()
	c.stopOnce.Do(func() {
		c.stopAll(context.Background())
		c.exited <- err
	})
}

func (c *Composite) stopAll(ctx context.Context) {
	for i := len(c.procs) - 1; i >= 0; i-- { // reverse start order
		_ = c.procs[i].Stop(ctx)
	}
}

// PID reports the primary process's pid (the unit's identity).
func (c *Composite) PID() int {
	if len(c.procs) == 0 {
		return 0
	}
	return c.procs[0].PID()
}

// Alive is true only while every member is running.
func (c *Composite) Alive() bool {
	for _, p := range c.procs {
		if !p.Alive() {
			return false
		}
	}
	return len(c.procs) > 0
}

// Logs merges every member's recent log lines (backends first, primary last).
func (c *Composite) Logs() []string {
	var out []string
	for i := len(c.procs) - 1; i >= 0; i-- {
		out = append(out, c.procs[i].Logs()...)
	}
	return out
}

// LogsSince streams the primary's ring (per-member streaming for a composite is
// out of scope; the primary is the user-facing process).
func (c *Composite) LogsSince(n int) ([]string, int) {
	if len(c.procs) == 0 {
		return nil, n
	}
	return c.procs[0].LogsSince(n)
}

// Stop tears the unit down in reverse start order.
func (c *Composite) Stop(ctx context.Context) error {
	c.stopOnce.Do(func() {
		c.stopAll(ctx)
		c.exited <- nil
	})
	return nil
}

// Wait blocks until the unit exits (any member exiting ends the unit).
func (c *Composite) Wait() error { return <-c.exited }
