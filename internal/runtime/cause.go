package runtime

import "github.com/doze-dev/doze/internal/journal"

// SetJournal gives the runtime the journal it records to. Without one, nothing
// is recorded.
func (r *Runtime) SetJournal(j *journal.Journal) { r.journal = j }

// Journal returns the journal the runtime records to, or nil.
func (r *Runtime) Journal() *journal.Journal { return r.journal }
