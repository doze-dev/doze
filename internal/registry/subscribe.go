package registry

// This file adds a lossy, non-blocking subscription feed on top of the
// registry. It exists so embedders (the root `doze` facade's Session.Events and
// the daemon's control "events" stream) can watch instance-state transitions
// without polling Snapshot. It is deliberately lossy: a slow consumer drops
// updates rather than back-pressuring the proxy/reaper hot path.

// instSig is the observable slice of an Instance the feed reacts to. Pure
// connection-count churn (Conns/IdleSince) is intentionally excluded so a busy
// proxy doesn't flood subscribers — only lifecycle-visible changes emit.
type instSig struct {
	state     State
	healthy   int8 // -1 unknown, 0 false, 1 true
	lastError string
	tainted   bool
}

func sigOf(inst *Instance) instSig {
	h := int8(-1)
	if inst.Healthy != nil {
		if *inst.Healthy {
			h = 1
		} else {
			h = 0
		}
	}
	return instSig{state: inst.State, healthy: h, lastError: inst.LastError, tainted: inst.Tainted}
}

// Subscribe registers a feed of instance snapshots, delivered whenever an
// instance's observable state (State, Healthy, LastError, Tainted) transitions.
// buf is the channel buffer; when full, updates are dropped (lossy). The
// returned function unsubscribes and closes the channel; call it exactly once.
func (r *Registry) Subscribe(buf int) (<-chan Instance, func()) {
	if buf < 1 {
		buf = 1
	}
	ch := make(chan Instance, buf)
	r.mu.Lock()
	if r.subs == nil {
		r.subs = map[*chan Instance]struct{}{}
	}
	r.subs[&ch] = struct{}{}
	r.mu.Unlock()

	var once bool
	cancel := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if once {
			return
		}
		once = true
		delete(r.subs, &ch)
		close(ch)
	}
	return ch, cancel
}

// emit fans an instance snapshot out to subscribers, but only when its
// observable signature has changed since the last emit. Caller must hold r.mu;
// the non-blocking sends never block, so holding the lock is safe.
func (r *Registry) emit(inst *Instance) {
	if len(r.subs) == 0 {
		// Still track the signature so the first real subscriber doesn't get a
		// spurious "changed" for a pre-existing state.
		if r.lastSig == nil {
			r.lastSig = map[string]instSig{}
		}
		r.lastSig[inst.Name] = sigOf(inst)
		return
	}
	sig := sigOf(inst)
	if r.lastSig == nil {
		r.lastSig = map[string]instSig{}
	}
	if prev, ok := r.lastSig[inst.Name]; ok && prev == sig {
		return // no observable transition
	}
	r.lastSig[inst.Name] = sig
	snap := *inst
	for chp := range r.subs {
		select {
		case *chp <- snap:
		default: // lossy: drop rather than block the caller
		}
	}
}
