package postgres

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

// buildStartup encodes a v3 StartupMessage with the given parameters.
func buildStartup(params ...string) []byte {
	var payload []byte
	for _, s := range params {
		payload = append(payload, s...)
		payload = append(payload, 0)
	}
	payload = append(payload, 0) // terminator
	msg := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(msg[0:4], uint32(8+len(payload)))
	binary.BigEndian.PutUint32(msg[4:8], protocolVersion3)
	copy(msg[8:], payload)
	return msg
}

func declineSame(r io.Reader) func(byte) (io.Reader, error) {
	return func(byte) (io.Reader, error) { return r, nil }
}

func TestReadStartupParsesDatabase(t *testing.T) {
	r := bytes.NewReader(buildStartup("user", "u", "database", "shop"))
	su, enc, err := readStartup(r, declineSame(r))
	if err != nil {
		t.Fatal(err)
	}
	if enc || su.cancel != nil {
		t.Fatalf("unexpected enc/cancel: %v %v", enc, su.cancel)
	}
	if su.database != "shop" || su.user != "u" {
		t.Errorf("database=%q user=%q", su.database, su.user)
	}
}

func TestReadStartupDefaultsDatabaseToUser(t *testing.T) {
	r := bytes.NewReader(buildStartup("user", "alice"))
	su, _, err := readStartup(r, declineSame(r))
	if err != nil {
		t.Fatal(err)
	}
	if su.database != "alice" {
		t.Errorf("database should default to user: %q", su.database)
	}
}

func TestReadStartupSSLThenStartup(t *testing.T) {
	ssl := make([]byte, 8)
	binary.BigEndian.PutUint32(ssl[0:4], 8)
	binary.BigEndian.PutUint32(ssl[4:8], sslRequestCode)
	stream := append(ssl, buildStartup("database", "shop")...)
	r := bytes.NewReader(stream)

	negotiated := false
	su, _, err := readStartup(r, func(kind byte) (io.Reader, error) {
		negotiated = true
		if kind != 'S' {
			t.Errorf("kind = %q, want S", kind)
		}
		return r, nil // decline (no upgrade), continue reading
	})
	if err != nil {
		t.Fatal(err)
	}
	if !negotiated {
		t.Error("negotiate was not called for SSLRequest")
	}
	if su.database != "shop" {
		t.Errorf("database = %q", su.database)
	}
}

func TestReadCancelRequest(t *testing.T) {
	msg := make([]byte, 16)
	binary.BigEndian.PutUint32(msg[0:4], 16)
	binary.BigEndian.PutUint32(msg[4:8], cancelRequestCode)
	binary.BigEndian.PutUint32(msg[8:12], 4242)
	binary.BigEndian.PutUint32(msg[12:16], 99)
	r := bytes.NewReader(msg)
	su, _, err := readStartup(r, declineSame(r))
	if err != nil {
		t.Fatal(err)
	}
	if su.cancel == nil || su.cancel.pid != 4242 || su.cancel.secret != 99 {
		t.Fatalf("cancel = %+v", su.cancel)
	}
}

func backendMsg(typ byte, payload []byte) []byte {
	msg := make([]byte, 5+len(payload))
	msg[0] = typ
	binary.BigEndian.PutUint32(msg[1:5], uint32(4+len(payload)))
	copy(msg[5:], payload)
	return msg
}

func TestForwardHandshakeRewritesKey(t *testing.T) {
	key := make([]byte, 8)
	binary.BigEndian.PutUint32(key[0:4], 1111) // real pid
	binary.BigEndian.PutUint32(key[4:8], 2222) // real secret
	stream := append(backendMsg(msgBackendKeyData, key), backendMsg(msgReadyForQuery, []byte{'I'})...)

	var out bytes.Buffer
	var gotReal [2]uint32
	ready, err := forwardHandshake(&out, bytes.NewReader(stream), func(pid, secret uint32) (uint32, uint32) {
		gotReal = [2]uint32{pid, secret}
		return 7777, 8888 // synthetic
	})
	if err != nil || !ready {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	if gotReal != [2]uint32{1111, 2222} {
		t.Errorf("rewrite saw real key %v", gotReal)
	}
	// The forwarded BackendKeyData must carry the synthetic key.
	fwd := out.Bytes()
	if fwd[0] != msgBackendKeyData {
		t.Fatalf("first forwarded msg type = %q", fwd[0])
	}
	pid := binary.BigEndian.Uint32(fwd[5:9])
	secret := binary.BigEndian.Uint32(fwd[9:13])
	if pid != 7777 || secret != 8888 {
		t.Errorf("forwarded key = (%d,%d), want synthetic (7777,8888)", pid, secret)
	}
}
