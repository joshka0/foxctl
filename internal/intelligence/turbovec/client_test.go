package turbovec

import (
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
