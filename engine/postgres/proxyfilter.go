package postgres

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/nerdmenot/doze/internal/engine"
)

// Postgres startup-phase request codes (appear in place of the protocol version).
const (
	sslRequestCode    = 80877103
	gssEncRequestCode = 80877104
	cancelRequestCode = 80877102
	protocolVersion3  = 196608
)

// Backend message types observed during the startup handshake.
const (
	msgBackendKeyData = 'K'
	msgReadyForQuery  = 'Z'
	msgErrorResponse  = 'E'
)

const (
	maxStartupLen      = 1 << 20
	maxHandshakeMsgLen = 1 << 20
)

// Preamble reads the Postgres startup preamble: it answers SSL/GSS negotiation
// (terminating TLS when configured), buffers the StartupMessage for replay, and
// fully handles a CancelRequest out-of-band. (engine.ProxyFilter)
func (Driver) Preamble(_ context.Context, client net.Conn, reg engine.CancelRegistry, opts engine.ProxyOpts) (engine.PreambleResult, error) {
	cur := client
	su, encrypted, err := readStartup(client, func(kind byte) (io.Reader, error) {
		// We only ever offer SSL termination, never GSSAPI encryption.
		if kind == 'S' && opts.TLS != nil {
			if _, werr := cur.Write([]byte{'S'}); werr != nil {
				return nil, werr
			}
			tc := tls.Server(cur, opts.TLS)
			if herr := tc.Handshake(); herr != nil {
				return nil, fmt.Errorf("TLS handshake: %w", herr)
			}
			cur = tc
			return tc, nil
		}
		_, werr := cur.Write([]byte{'N'})
		return cur, werr
	})
	if err != nil {
		_, _ = cur.Write(errorResponse("08P01", "doze: "+err.Error()))
		return engine.PreambleResult{}, err
	}
	if su.cancel != nil {
		routeCancel(reg, su.cancel)
		return engine.PreambleResult{Handled: true}, nil
	}
	if opts.RequireTLS && !encrypted && !opts.LocalUnix {
		_, _ = cur.Write(errorResponse("28000", "doze: SSL connection required"))
		return engine.PreambleResult{Handled: true}, nil
	}
	return engine.PreambleResult{Client: cur, Replay: su.raw}, nil
}

// Handshake watches the backend->client startup, swapping the backend's real
// cancellation key for a synthetic one registered in reg, so a later
// CancelRequest can be routed back. (engine.ProxyFilter)
func (Driver) Handshake(client net.Conn, backend *bufio.Reader, backendSocket string, reg engine.CancelRegistry) (bool, func(), error) {
	var synthetic []byte
	registered := false
	ready, err := forwardHandshake(client, backend, func(realPid, realSecret uint32) (uint32, uint32) {
		key := make([]byte, 8)
		binary.BigEndian.PutUint32(key[0:4], realPid)
		binary.BigEndian.PutUint32(key[4:8], realSecret)
		synthetic = reg.Register(engine.CancelTarget{BackendSocket: backendSocket, Key: key})
		registered = true
		return binary.BigEndian.Uint32(synthetic[0:4]), binary.BigEndian.Uint32(synthetic[4:8])
	})
	cleanup := func() {
		if registered {
			reg.Unregister(synthetic)
		}
	}
	if err != nil || !ready {
		cleanup()
		return false, func() {}, err
	}
	return true, cleanup, nil
}

// routeCancel forwards a client CancelRequest to the backend its synthetic key
// maps to, using the backend's real key.
func routeCancel(reg engine.CancelRegistry, c *cancelRequest) {
	key := make([]byte, 8)
	binary.BigEndian.PutUint32(key[0:4], c.pid)
	binary.BigEndian.PutUint32(key[4:8], c.secret)
	target, ok := reg.Lookup(key)
	if !ok || len(target.Key) != 8 {
		return
	}
	conn, err := net.DialTimeout("unix", target.BackendSocket, 5*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	realPid := binary.BigEndian.Uint32(target.Key[0:4])
	realSecret := binary.BigEndian.Uint32(target.Key[4:8])
	if _, err := conn.Write(buildCancelRequest(realPid, realSecret)); err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, conn)
}

// --- wire protocol parsing (ported from the old proxy) ---

type startup struct {
	raw      []byte
	database string
	user     string
	cancel   *cancelRequest
}

type cancelRequest struct {
	pid    uint32
	secret uint32
}

func readStartup(r io.Reader, negotiate func(kind byte) (io.Reader, error)) (su *startup, encrypted bool, err error) {
	for {
		header := make([]byte, 8)
		if _, err := io.ReadFull(r, header); err != nil {
			return nil, encrypted, fmt.Errorf("reading preamble: %w", err)
		}
		length := int(binary.BigEndian.Uint32(header[0:4]))
		code := binary.BigEndian.Uint32(header[4:8])

		switch code {
		case sslRequestCode, gssEncRequestCode:
			if length != 8 {
				return nil, encrypted, fmt.Errorf("malformed negotiation packet (len=%d)", length)
			}
			kind := byte('S')
			if code == gssEncRequestCode {
				kind = 'G'
			}
			next, nerr := negotiate(kind)
			if nerr != nil {
				return nil, encrypted, fmt.Errorf("encryption negotiation: %w", nerr)
			}
			if next != r {
				r = next
				encrypted = true
			}
			continue
		case cancelRequestCode:
			if length != 16 {
				return nil, encrypted, fmt.Errorf("malformed cancel request (len=%d)", length)
			}
			rest := make([]byte, 8)
			if _, err := io.ReadFull(r, rest); err != nil {
				return nil, encrypted, fmt.Errorf("reading cancel request: %w", err)
			}
			return &startup{cancel: &cancelRequest{
				pid:    binary.BigEndian.Uint32(rest[0:4]),
				secret: binary.BigEndian.Uint32(rest[4:8]),
			}}, encrypted, nil
		}

		if length < 8 || length > maxStartupLen {
			return nil, encrypted, fmt.Errorf("implausible startup length %d", length)
		}
		body := make([]byte, length-8)
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, encrypted, fmt.Errorf("reading startup body: %w", err)
		}
		raw := make([]byte, 0, length)
		raw = append(raw, header...)
		raw = append(raw, body...)

		su := &startup{raw: raw}
		if code == protocolVersion3 {
			su.database, su.user = parseParams(body)
		}
		if su.database == "" {
			su.database = su.user
		}
		if su.database == "" {
			return nil, encrypted, fmt.Errorf("startup message specified no database")
		}
		return su, encrypted, nil
	}
}

func forwardHandshake(dst io.Writer, src io.Reader, rewriteKey func(pid, secret uint32) (uint32, uint32)) (ready bool, err error) {
	header := make([]byte, 5)
	for {
		if _, err := io.ReadFull(src, header); err != nil {
			return false, err
		}
		msgType := header[0]
		length := binary.BigEndian.Uint32(header[1:5])
		if length < 4 || length-4 > maxHandshakeMsgLen {
			return false, fmt.Errorf("implausible message length %d", length)
		}
		payload := make([]byte, length-4)
		if _, err := io.ReadFull(src, payload); err != nil {
			return false, err
		}
		if msgType == msgBackendKeyData && len(payload) == 8 && rewriteKey != nil {
			realPid := binary.BigEndian.Uint32(payload[0:4])
			realSecret := binary.BigEndian.Uint32(payload[4:8])
			newPid, newSecret := rewriteKey(realPid, realSecret)
			binary.BigEndian.PutUint32(payload[0:4], newPid)
			binary.BigEndian.PutUint32(payload[4:8], newSecret)
		}
		if _, err := dst.Write(header); err != nil {
			return false, err
		}
		if _, err := dst.Write(payload); err != nil {
			return false, err
		}
		switch msgType {
		case msgReadyForQuery:
			return true, nil
		case msgErrorResponse:
			return false, nil
		}
	}
}

func buildCancelRequest(pid, secret uint32) []byte {
	b := make([]byte, 16)
	binary.BigEndian.PutUint32(b[0:4], 16)
	binary.BigEndian.PutUint32(b[4:8], cancelRequestCode)
	binary.BigEndian.PutUint32(b[8:12], pid)
	binary.BigEndian.PutUint32(b[12:16], secret)
	return b
}

func parseParams(body []byte) (database, user string) {
	parts := splitCStrings(body)
	for i := 0; i+1 < len(parts); i += 2 {
		switch parts[i] {
		case "database":
			database = parts[i+1]
		case "user":
			user = parts[i+1]
		}
	}
	return database, user
}

func splitCStrings(b []byte) []string {
	var out []string
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] == 0 {
			if i == start {
				break
			}
			out = append(out, string(b[start:i]))
			start = i + 1
		}
	}
	return out
}

// errorResponse builds a Postgres ErrorResponse so clients see a clean FATAL.
func errorResponse(code, message string) []byte {
	var payload []byte
	addField := func(typ byte, val string) {
		payload = append(payload, typ)
		payload = append(payload, val...)
		payload = append(payload, 0)
	}
	addField('S', "FATAL")
	addField('V', "FATAL")
	addField('C', code)
	addField('M', message)
	payload = append(payload, 0)

	msg := make([]byte, 1+4+len(payload))
	msg[0] = 'E'
	binary.BigEndian.PutUint32(msg[1:5], uint32(4+len(payload)))
	copy(msg[5:], payload)
	return msg
}
