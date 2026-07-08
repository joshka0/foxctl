package turbovec

import (
	"net"
	"path/filepath"
	"testing"
)

func TestEncodeName(t *testing.T) {
	name := "test-workspace"
	b := encodeName(name)
	if len(b) < 2 {
		t.Fatal("too short")
	}
	nameLen := int(b[0])<<8 | int(b[1])
	if nameLen != len(name) {
		t.Fatalf("nameLen = %d, want %d", nameLen, len(name))
	}
	if string(b[2:]) != name {
		t.Fatalf("name = %q, want %q", string(b[2:]), name)
	}
}

func TestFrameRoundtrip(t *testing.T) {
	// We can't test the actual socket without the server running,
	// but we can test the frame encode/decode.
	// This is tested via the integration test when turbovecd is available.
	t.Skip("requires turbovecd sidecar")
}

// A failed roundtrip must drop the connection so it can't be reused in a
// desynced state, and Connected() must reflect that. This underpins the read
// deadline that prevents a wedged sidecar from hanging callers forever.
func TestClient_DropsConnectionOnFailure(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "t.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	// Server accepts then immediately closes — the client's read gets EOF.
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_ = c.Close()
		}
	}()

	client, err := Dial(sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if !client.Connected() {
		t.Fatal("expected Connected() true after dial")
	}
	if err := client.Ping(); err == nil {
		t.Fatal("expected Ping error on closed server")
	}
	if client.Connected() {
		t.Fatal("expected Connected() false after failed roundtrip (conn dropped)")
	}
	// Further roundtrips fail fast rather than using a dead/desynced conn.
	if err := client.Ping(); err == nil {
		t.Fatal("expected Ping error after connection dropped")
	}
}
