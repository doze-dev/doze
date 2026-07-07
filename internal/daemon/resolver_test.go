package daemon

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
)

// loopbackResolver answers every doze name with 127.0.0.1 (membership),
// nil for anything else.
func loopbackResolver(name string) net.IP {
	n := strings.ToLower(name)
	if n == "doze" || strings.HasSuffix(n, ".doze") {
		return net.IPv4(127, 0, 0, 1)
	}
	return nil
}

func buildQuery(name string, qtype uint16) []byte {
	q := make([]byte, 0, 64)
	q = append(q, 0xAB, 0xCD, 0x01, 0x00) // id, RD
	q = binary.BigEndian.AppendUint16(q, 1)
	q = append(q, 0, 0, 0, 0, 0, 0)
	for _, label := range splitLabels(name) {
		q = append(q, byte(len(label)))
		q = append(q, label...)
	}
	q = append(q, 0)
	q = binary.BigEndian.AppendUint16(q, qtype)
	q = binary.BigEndian.AppendUint16(q, 1)
	return q
}

func splitLabels(name string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			out = append(out, name[start:i])
			start = i + 1
		}
	}
	return out
}

func TestResolverAnswers(t *testing.T) {
	// A record under doze → 127.0.0.1.
	resp := answer(buildQuery("orders-pg.demo.doze", 1), loopbackResolver)
	if resp == nil {
		t.Fatal("no response for in-zone A query")
	}
	if got := binary.BigEndian.Uint16(resp[6:8]); got != 1 {
		t.Fatalf("ANCOUNT = %d, want 1", got)
	}
	if rcode := resp[3] & 0x0F; rcode != 0 {
		t.Fatalf("rcode = %d, want NOERROR", rcode)
	}
	ip := resp[len(resp)-4:]
	if ip[0] != 127 || ip[3] != 1 {
		t.Fatalf("answer IP = %v, want 127.0.0.1", ip)
	}

	// AAAA in-zone: empty NOERROR (client falls back to the A record).
	resp = answer(buildQuery("orders-pg.demo.doze", 28), loopbackResolver)
	if got := binary.BigEndian.Uint16(resp[6:8]); got != 0 {
		t.Fatalf("AAAA ANCOUNT = %d, want 0", got)
	}
	if rcode := resp[3] & 0x0F; rcode != 0 {
		t.Fatalf("AAAA rcode = %d, want NOERROR", rcode)
	}

	// Out-of-zone: NXDOMAIN.
	resp = answer(buildQuery("example.com", 1), loopbackResolver)
	if rcode := resp[3] & 0x0F; rcode != 3 {
		t.Fatalf("out-of-zone rcode = %d, want NXDOMAIN", rcode)
	}

	// Case-insensitive.
	resp = answer(buildQuery("API.Demo.DOZE", 1), loopbackResolver)
	if got := binary.BigEndian.Uint16(resp[6:8]); got != 1 {
		t.Fatalf("mixed-case ANCOUNT = %d, want 1", got)
	}

	// Garbage doesn't crash or answer.
	if answer([]byte{1, 2, 3}, loopbackResolver) != nil {
		t.Fatal("garbage should yield no response")
	}
}
