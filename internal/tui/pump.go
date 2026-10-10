package tui

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/doze-dev/doze/internal/control"
)

// pump feeds the dash from one subscription. It replaces the status poll: the
// daemon sends each instance when it changes and samples meters only while
// the subscription is open. If the connection drops — the daemon restarting —
// it says so and reconnects.
func pump(ctx context.Context, c *control.Client, send func(tea.Msg)) {
	c.Name = "dash"
	for ctx.Err() == nil {
		err := pumpOnce(ctx, c, send)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("the daemon closed the connection")
		}
		send(statusMsg{err: err})
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// pumpOnce holds one connection for as long as it lasts.
func pumpOnce(ctx context.Context, c *control.Client, send func(tea.Msg)) error {
	cn, err := c.Dial(ctx)
	if err != nil {
		return err
	}
	defer cn.Close()

	// The opening status gives the listen address, the idle time, the order
	// the dash keeps its rows in, and a first set of meters.
	resp, err := cn.Do(ctx, control.Request{Op: "status"})
	if err != nil {
		return err
	}
	state := &pumpState{resp: resp}
	send(state.msg(true))

	sub := control.Subscription{
		Topics:   []string{control.TopicInstances, control.TopicStats},
		Interval: refreshMS,
	}
	return cn.Subscribe(ctx, sub, func(f control.Frame) error {
		switch f.Topic {
		case control.TopicInstances:
			state.instances(f)
			send(state.msg(false))
		case control.TopicStats:
			state.stats(f)
			send(state.msg(true))
		}
		return nil
	})
}

// pumpState is the stack as the frames so far describe it.
type pumpState struct{ resp control.Response }

// msg snapshots the state for the model, which must not share the slice the
// next frame will write to.
func (s *pumpState) msg(sample bool) statusMsg {
	resp := s.resp
	resp.Instances = append([]control.InstanceView(nil), s.resp.Instances...)
	return statusMsg{resp: resp, sample: sample}
}

func (s *pumpState) index(name string) int {
	for i := range s.resp.Instances {
		if s.resp.Instances[i].Name == name {
			return i
		}
	}
	return -1
}

// instances applies an instances frame. Rows keep their place: a changed
// instance is replaced where it is and a new one goes at the end, so the
// cursor does not jump when something wakes. An instances frame carries no
// meters, so the ones in hand are kept for as long as the process is the same.
func (s *pumpState) instances(f control.Frame) {
	for _, v := range f.Instances {
		i := s.index(v.Name)
		if i < 0 {
			s.resp.Instances = append(s.resp.Instances, v)
			continue
		}
		if old := s.resp.Instances[i]; old.PID == v.PID {
			v.RAM, v.CPU, v.Children = old.RAM, old.CPU, old.Children
		}
		s.resp.Instances[i] = v
	}
	for _, name := range f.Removed {
		if i := s.index(name); i >= 0 {
			s.resp.Instances = append(s.resp.Instances[:i], s.resp.Instances[i+1:]...)
		}
	}
}

// stats applies a stats frame: fresh meters for what is running, none for
// what is not.
func (s *pumpState) stats(f control.Frame) {
	seen := map[string]control.StatView{}
	for _, st := range f.Stats {
		seen[st.Name] = st
	}
	for i := range s.resp.Instances {
		v := &s.resp.Instances[i]
		st, ok := seen[v.Name]
		if !ok || st.PID != v.PID {
			v.RAM, v.CPU, v.Children = 0, 0, nil
			continue
		}
		v.RAM, v.CPU, v.Children = st.RAM, st.CPU, st.Children
	}
}
