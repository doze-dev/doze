// The history ring buffer: rolling RAM/CPU/connection samples behind the
// detail card's charts.

package tui

// history holds rolling samples for an instance's charts.
type history struct {
	ram   []float64
	cpu   []float64
	conns []float64
}

func (h *history) push(ram, cpu, conns float64) {
	h.ram = pushCap(h.ram, ram)
	h.cpu = pushCap(h.cpu, cpu)
	h.conns = pushCap(h.conns, conns)
}

func pushCap(s []float64, v float64) []float64 {
	s = append(s, v)
	if len(s) > histLen {
		s = s[len(s)-histLen:]
	}
	return s
}
