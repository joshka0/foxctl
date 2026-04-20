package termcaps

import (
	"bytes"
	"testing"
)

// TestOSC10Query_BEL locks the exact response bytes. Changing either
// side would risk breaking clients that pattern-match the reply.
func TestOSC10Query_BEL(t *testing.T) {
	r := New()
	got := r.Feed([]byte("\x1b]10;?\x07"))
	if len(got) != 1 {
		t.Fatalf("want 1 response, got %d: %q", len(got), got)
	}
	want := []byte("\x1b]10;rgb:ffff/ffff/ffff\x1b\\")
	if !bytes.Equal(got[0], want) {
		t.Errorf("OSC10 reply = %q, want %q", got[0], want)
	}
	if r.Stats().OSCColor != 1 {
		t.Errorf("OSCColor stat = %d, want 1", r.Stats().OSCColor)
	}
}

// TestOSC10Query_ST exercises the ST-terminated form (ESC '\') which is
// what real xterm emits. Codex uses this form.
func TestOSC10Query_ST(t *testing.T) {
	r := New()
	got := r.Feed([]byte("\x1b]10;?\x1b\\"))
	if len(got) != 1 || !bytes.Equal(got[0], []byte("\x1b]10;rgb:ffff/ffff/ffff\x1b\\")) {
		t.Errorf("OSC10 ST reply = %q", got)
	}
}

func TestOSC11Query(t *testing.T) {
	r := New()
	got := r.Feed([]byte("\x1b]11;?\x07"))
	if len(got) != 1 || !bytes.Contains(got[0], []byte("11;rgb:0000/0000/0000")) {
		t.Errorf("OSC11 reply = %q", got)
	}
}

func TestOSC12Query(t *testing.T) {
	r := New()
	got := r.Feed([]byte("\x1b]12;?\x07"))
	if len(got) != 1 || !bytes.Contains(got[0], []byte("12;rgb:ffff/ffff/ffff")) {
		t.Errorf("OSC12 reply = %q", got)
	}
}

func TestDSRStatus(t *testing.T) {
	r := New()
	got := r.Feed([]byte("\x1b[5n"))
	if len(got) != 1 || !bytes.Equal(got[0], []byte("\x1b[0n")) {
		t.Errorf("DSR 5 reply = %q, want ESC[0n", got)
	}
}

func TestDSRCursor(t *testing.T) {
	r := New()
	got := r.Feed([]byte("\x1b[6n"))
	if len(got) != 1 || !bytes.Equal(got[0], []byte("\x1b[1;1R")) {
		t.Errorf("DSR 6 reply = %q, want ESC[1;1R", got)
	}
}

func TestDA1_Bare(t *testing.T) {
	r := New()
	got := r.Feed([]byte("\x1b[c"))
	if len(got) != 1 || !bytes.Equal(got[0], []byte("\x1b[?1;2c")) {
		t.Errorf("DA1 reply = %q", got)
	}
}

func TestDA1_ExplicitZero(t *testing.T) {
	r := New()
	got := r.Feed([]byte("\x1b[0c"))
	if len(got) != 1 {
		t.Fatalf("DA1 explicit 0 got %d responses", len(got))
	}
}

func TestDA2_Bare(t *testing.T) {
	r := New()
	got := r.Feed([]byte("\x1b[>c"))
	if len(got) != 1 || !bytes.Equal(got[0], []byte("\x1b[>41;420;0c")) {
		t.Errorf("DA2 reply = %q", got)
	}
}

func TestKittyKeyboardQuery(t *testing.T) {
	r := New()
	got := r.Feed([]byte("\x1b[?u"))
	if len(got) != 1 || !bytes.Equal(got[0], []byte("\x1b[?0u")) {
		t.Errorf("kitty kbd ? reply = %q, want ESC[?0u", got)
	}
}

// TestKittyKeyboardPush_NoResponse locks the invariant that CSI > 7 u
// (push flags) produces NO response — it's a state change, not a query.
// Before this distinction, we could have emitted ESC[?0u on every
// boot-time push which confuses clients.
func TestKittyKeyboardPush_NoResponse(t *testing.T) {
	r := New()
	got := r.Feed([]byte("\x1b[>7u"))
	if len(got) != 0 {
		t.Errorf("push flags should not produce response, got %q", got)
	}
	if r.Stats().UnknownIgnored != 1 {
		t.Errorf("UnknownIgnored = %d, want 1 (the push flag sequence)", r.Stats().UnknownIgnored)
	}
}

// TestPartialSequenceAcrossFeeds verifies state persists: a query split
// across two Feed calls is still recognised. This matters because PTY
// reads return at arbitrary boundaries.
func TestPartialSequenceAcrossFeeds(t *testing.T) {
	r := New()
	if got := r.Feed([]byte("\x1b[")); len(got) != 0 {
		t.Fatalf("partial CSI start emitted response: %q", got)
	}
	if got := r.Feed([]byte("5")); len(got) != 0 {
		t.Fatalf("partial CSI param emitted response: %q", got)
	}
	got := r.Feed([]byte("n"))
	if len(got) != 1 || !bytes.Equal(got[0], []byte("\x1b[0n")) {
		t.Errorf("split DSR reply = %q", got)
	}
}

// TestMultipleQueriesInOneFeed locks that N queries in a single Feed
// produce N responses in order. Realistic case for codex boot: OSC 10 +
// kitty push + DA1 + DA2 all arrive in one buffered write.
func TestMultipleQueriesInOneFeed(t *testing.T) {
	r := New()
	input := []byte("\x1b]10;?\x07\x1b[>7u\x1b[c\x1b[>c")
	got := r.Feed(input)
	if len(got) != 3 {
		t.Fatalf("want 3 responses (OSC10, DA1, DA2; push has none), got %d: %q", len(got), got)
	}
	if !bytes.Contains(got[0], []byte("10;rgb")) {
		t.Errorf("first response should be OSC10: %q", got[0])
	}
	if !bytes.Equal(got[1], []byte("\x1b[?1;2c")) {
		t.Errorf("second response should be DA1: %q", got[1])
	}
	if !bytes.Equal(got[2], []byte("\x1b[>41;420;0c")) {
		t.Errorf("third response should be DA2: %q", got[2])
	}
}

// TestGroundTextPassesThroughSilently confirms the hot path does
// nothing for normal text. This is the common case — most PTY bytes are
// application output, not queries.
func TestGroundTextPassesThroughSilently(t *testing.T) {
	r := New()
	got := r.Feed([]byte("hello world\n"))
	if len(got) != 0 {
		t.Errorf("plain text should produce no responses, got %q", got)
	}
	if r.Stats() != (Stats{}) {
		t.Errorf("stats should be zero, got %+v", r.Stats())
	}
}

// TestMalformedSequenceDoesNotHang confirms a hostile child that opens
// CSI and never terminates cannot pin us in an in-progress state: we
// abort after maxBuf parameter bytes.
func TestMalformedSequenceDoesNotHang(t *testing.T) {
	r := New()
	// Build a sequence with way more parameter bytes than maxBuf allows,
	// then a normal query after the abort. We want to confirm the
	// normal query still gets answered.
	payload := []byte("\x1b[")
	for i := 0; i < maxBuf+10; i++ {
		payload = append(payload, '0')
	}
	// No final byte on the long one — parser should abort. Then issue a
	// real DSR.
	payload = append(payload, "\x1b[5n"...)
	got := r.Feed(payload)
	if len(got) != 1 || !bytes.Equal(got[0], []byte("\x1b[0n")) {
		t.Errorf("after malformed abort, DSR reply = %q", got)
	}
}

// TestSGRAndCursorMovementIgnored proves we don't reply to SGR (CSI m)
// or cursor movement (CSI H / CSI A / etc.). These flood during any
// TUI render and a response to each would pump garbage back into the
// child.
func TestSGRAndCursorMovementIgnored(t *testing.T) {
	r := New()
	got := r.Feed([]byte("\x1b[1;32m\x1b[1;1H\x1b[2J\x1b[3A"))
	if len(got) != 0 {
		t.Errorf("SGR/cursor should be ignored, got %q", got)
	}
	// Should increment UnknownIgnored (4 finished sequences).
	if r.Stats().UnknownIgnored != 4 {
		t.Errorf("UnknownIgnored = %d, want 4", r.Stats().UnknownIgnored)
	}
}
