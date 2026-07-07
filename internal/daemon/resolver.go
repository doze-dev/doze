// The doze resolver: a tiny built-in DNS server answering
// *.doze → 127.0.0.1, so every stack's services get real hostnames
// (<service>.<stack>.doze) without touching /etc/hosts. The OS routes
// the suffix here via a one-time /etc/resolver/doze drop-in on macOS
// (see doctor); the answer is a constant, so whichever running daemon bound
// the port first serves every stack on the machine.
package daemon

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/doze-dev/doze/internal/config"
)

// DNSPort is the fixed local port the doze resolver listens on —
// referenced by the /etc/resolver/doze drop-in.
const DNSPort = 5323

// ResolverFile is the macOS resolver drop-in that routes *.doze queries
// to the built-in DNS server.
const ResolverFile = "/etc/resolver/doze"

// ResolverSetupHint is the one-time command that installs the drop-in.
const ResolverSetupHint = `sudo sh -c 'mkdir -p /etc/resolver && printf "nameserver 127.0.0.1\nport 5323\n" > /etc/resolver/doze'`

// serveDNS binds the resolver and answers until ctx ends, mapping each name to
// its IP via resolve (per-service loopback IPs; 127.0.0.1 default). Address in
// use is expected (another stack's daemon already serves) and reported as
// (false, nil).
func serveDNS(ctx context.Context, resolve func(string) net.IP) (bound bool, err error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: DNSPort})
	if err != nil {
		if isAddrInUse(err) {
			return false, nil
		}
		return false, err
	}
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return // closed on shutdown
			}
			if resp := answer(buf[:n], resolve); resp != nil {
				_, _ = conn.WriteToUDP(resp, addr)
			}
		}
	}()
	return true, nil
}

// answer builds the DNS response for one query: A records under doze
// resolve to their service's IP (via resolve); AAAA under doze gets an
// empty NOERROR (clients then use the A answer); anything else is NXDOMAIN.
// Nil for unparseable input.
func answer(q []byte, resolve func(string) net.IP) []byte {
	if len(q) < 12 {
		return nil
	}
	qd := binary.BigEndian.Uint16(q[4:6])
	if qd != 1 {
		return nil // one question per query, like every real client sends
	}
	name, qtype, qend, ok := parseQuestion(q, 12)
	if !ok {
		return nil
	}

	inZone := name == config.DomainSuffix || strings.HasSuffix(name, "."+config.DomainSuffix)
	const (
		typeA    = 1
		typeAAAA = 28
	)
	// Resolve the name to its service IP; only in-zone names we actually own get
	// an answer (unknown ones are NXDOMAIN, below).
	var ip net.IP
	if inZone && resolve != nil {
		if r := resolve(name); r != nil {
			ip = r.To4()
		}
	}
	known := ip != nil

	// Header: same ID; QR=1, RD echoed, RA=1; one question echoed back.
	resp := make([]byte, 0, qend+16)
	resp = append(resp, q[0], q[1])
	flags := uint16(0x8080) | (binary.BigEndian.Uint16(q[2:4]) & 0x0100) // QR|RA, echo RD
	rcode := uint16(0)
	answers := uint16(0)
	switch {
	case !inZone || !known:
		rcode = 3 // NXDOMAIN: not a name we serve
	case qtype == typeA:
		answers = 1
	} // in-zone A we own: answer; in-zone non-A (AAAA, …): empty NOERROR → client uses the A

	resp = binary.BigEndian.AppendUint16(resp, flags|rcode)
	resp = binary.BigEndian.AppendUint16(resp, 1)       // QDCOUNT
	resp = binary.BigEndian.AppendUint16(resp, answers) // ANCOUNT
	resp = binary.BigEndian.AppendUint16(resp, 0)       // NSCOUNT
	resp = binary.BigEndian.AppendUint16(resp, 0)       // ARCOUNT
	resp = append(resp, q[12:qend]...)                  // the question, verbatim

	if answers == 1 {
		resp = append(resp, 0xC0, 0x0C) // name: pointer to the question
		resp = binary.BigEndian.AppendUint16(resp, typeA)
		resp = binary.BigEndian.AppendUint16(resp, 1)  // IN
		resp = binary.BigEndian.AppendUint32(resp, 10) // TTL: short — stacks come and go
		resp = binary.BigEndian.AppendUint16(resp, 4)
		resp = append(resp, ip[0], ip[1], ip[2], ip[3])
	}
	return resp
}

// parseQuestion reads one question (QNAME + QTYPE/QCLASS) starting at off,
// returning the lowercase dotted name and the offset just past the question.
// QNAMEs may use compression pointers: macOS's mDNSResponder packs A and AAAA
// questions into one packet with the second name compressed to a pointer at
// the first — rejecting those silently drops real queries.
func parseQuestion(q []byte, off int) (name string, qtype uint16, end int, ok bool) {
	name, end, ok = parseName(q, off)
	if !ok || end+4 > len(q) {
		return "", 0, 0, false
	}
	return name, binary.BigEndian.Uint16(q[end : end+2]), end + 4, true
}

// parseName decodes a (possibly compressed) DNS name at off, returning the
// offset just past its encoding at the ORIGINAL position.
func parseName(q []byte, off int) (name string, end int, ok bool) {
	var labels []string
	pos, jumps := off, 0
	for {
		if pos >= len(q) {
			return "", 0, false
		}
		l := int(q[pos])
		switch {
		case l == 0:
			if end == 0 {
				end = pos + 1
			}
			return strings.Join(labels, "."), end, true
		case l&0xC0 == 0xC0: // compression pointer
			if pos+1 >= len(q) {
				return "", 0, false
			}
			if end == 0 {
				end = pos + 2
			}
			if jumps++; jumps > 8 {
				return "", 0, false // pointer loop
			}
			pos = int(binary.BigEndian.Uint16(q[pos:pos+2]) & 0x3FFF)
		default:
			if pos+1+l > len(q) {
				return "", 0, false
			}
			labels = append(labels, strings.ToLower(string(q[pos+1:pos+1+l])))
			pos += 1 + l
		}
	}
}

// ResolverConfigured reports whether the OS routes doze to the built-in
// resolver (macOS: the /etc/resolver drop-in exists and names our port).
func ResolverConfigured() bool {
	if runtime.GOOS != "darwin" {
		return false // linux setup is distro-specific; doctor explains
	}
	data, err := os.ReadFile(ResolverFile)
	if err != nil {
		return false
	}
	s := string(data)
	return strings.Contains(s, "127.0.0.1") && strings.Contains(s, "5323")
}

func isAddrInUse(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) {
		return strings.Contains(op.Err.Error(), "address already in use")
	}
	return strings.Contains(err.Error(), "address already in use")
}
