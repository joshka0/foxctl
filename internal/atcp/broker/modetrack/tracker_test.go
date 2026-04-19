package modetrack

import (
	"sync"
	"testing"
	"time"
)

func TestTracker_BracketedPasteSetAndReset(t *testing.T) {
	tr := New()
	if tr.Snapshot().BracketedPaste {
		t.Fatal("default BracketedPaste should be false")
	}
	tr.Feed([]byte("\x1b[?2004h"))
	if !tr.Snapshot().BracketedPaste {
		t.Error("BracketedPaste did not enable on CSI ? 2004 h")
	}
	tr.Feed([]byte("\x1b[?2004l"))
	if tr.Snapshot().BracketedPaste {
		t.Error("BracketedPaste did not disable on CSI ? 2004 l")
	}
}

func TestTracker_AltScreen(t *testing.T) {
	for _, mode := range []string{"47", "1047", "1049"} {
		tr := New()
		tr.Feed([]byte("\x1b[?" + mode + "h"))
		if !tr.Snapshot().AltScreen {
			t.Errorf("AltScreen not enabled by ? %s h", mode)
		}
		tr.Feed([]byte("\x1b[?" + mode + "l"))
		if tr.Snapshot().AltScreen {
			t.Errorf("AltScreen not disabled by ? %s l", mode)
		}
	}
}

func TestTracker_ApplicationCursorKeys(t *testing.T) {
	tr := New()
	tr.Feed([]byte("\x1b[?1h"))
	if !tr.Snapshot().ApplicationCursorKeys {
		t.Error("ApplicationCursorKeys should be enabled")
	}
	tr.Feed([]byte("\x1b[?1l"))
	if tr.Snapshot().ApplicationCursorKeys {
		t.Error("ApplicationCursorKeys should be disabled")
	}
}

func TestTracker_CombinedSetCSI(t *testing.T) {
	// xterm commonly writes CSI ? 1049 ; 2004 h to enter an alt-screen TUI.
	tr := New()
	tr.Feed([]byte("\x1b[?1049;2004h"))
	snap := tr.Snapshot()
	if !snap.AltScreen || !snap.BracketedPaste {
		t.Errorf("combined set failed: %+v", snap)
	}
	tr.Feed([]byte("\x1b[?1049;2004l"))
	snap = tr.Snapshot()
	if snap.AltScreen || snap.BracketedPaste {
		t.Errorf("combined reset failed: %+v", snap)
	}
}

func TestTracker_IgnoresUnrelatedCSI(t *testing.T) {
	tr := New()
	// Plain SGR sequences, cursor motions: none should toggle modes.
	tr.Feed([]byte("\x1b[31mhello\x1b[0m world\x1b[2J\x1b[H"))
	if snap := tr.Snapshot(); snap.BracketedPaste || snap.AltScreen || snap.ApplicationCursorKeys {
		t.Errorf("SGR sequences polluted mode state: %+v", snap)
	}
}

func TestTracker_IgnoresNonPrivateHL(t *testing.T) {
	tr := New()
	// ANSI mode set (no '?') uses different mode numbers. 2004 as a non-private
	// mode means nothing — must not enable bracketed paste.
	tr.Feed([]byte("\x1b[2004h"))
	if tr.Snapshot().BracketedPaste {
		t.Error("non-private 2004h must not set BracketedPaste")
	}
}

func TestTracker_SplitAcrossFeeds(t *testing.T) {
	// Feed the sequence one byte at a time — state must persist.
	tr := New()
	seq := []byte("\x1b[?2004h")
	for _, b := range seq {
		tr.Feed([]byte{b})
	}
	if !tr.Snapshot().BracketedPaste {
		t.Error("split-byte feed failed to enable BracketedPaste")
	}
}

func TestTracker_OSCIgnored(t *testing.T) {
	tr := New()
	// OSC 0 ; title \x07 — setting window title should not affect modes.
	tr.Feed([]byte("\x1b]0;title\x07\x1b[?2004h"))
	if !tr.Snapshot().BracketedPaste {
		t.Error("OSC should be skipped so that later CSI still applies")
	}
}

// TestTracker_OSCPayloadNotMisparsed guards the bug where OSC state reverted
// to ground immediately, so bytes inside the OSC title that *looked like*
// CSI (e.g. "\x1b[?2004h") were incorrectly treated as mode toggles.
func TestTracker_OSCPayloadNotMisparsed(t *testing.T) {
	tr := New()
	// Window title payload deliberately contains a CSI-looking fragment.
	tr.Feed([]byte("\x1b]0;\x1b[?2004h malicious title\x07"))
	if tr.Snapshot().BracketedPaste {
		t.Error("CSI-like bytes inside an OSC payload must not toggle modes")
	}
}

// TestTracker_OSCWithStTerminator covers the ESC-\ String Terminator path.
func TestTracker_OSCWithStTerminator(t *testing.T) {
	tr := New()
	// OSC terminated by ST (ESC \) rather than BEL, followed by a real CSI.
	tr.Feed([]byte("\x1b]0;title\x1b\\\x1b[?2004h"))
	if !tr.Snapshot().BracketedPaste {
		t.Error("OSC ended by ST should let subsequent CSI apply")
	}
}

func TestTracker_MalformedCSIAborts(t *testing.T) {
	tr := New()
	// Bad byte in parameters should abort without leaking state.
	tr.Feed([]byte("\x1b[?20\x0004h"))
	if tr.Snapshot().BracketedPaste {
		t.Error("malformed CSI leaked into mode state")
	}
	// And a subsequent valid CSI should still work.
	tr.Feed([]byte("\x1b[?2004h"))
	if !tr.Snapshot().BracketedPaste {
		t.Error("tracker did not recover after malformed CSI")
	}
}

func TestTracker_OversizedCSIAborts(t *testing.T) {
	tr := New()
	var huge []byte
	huge = append(huge, 0x1B, '[', '?')
	for i := 0; i < 200; i++ {
		huge = append(huge, '1', ';')
	}
	huge = append(huge, '2', '0', '0', '4', 'h')
	tr.Feed(huge)
	// Oversized CSI should have been aborted long before reaching 'h'.
	if tr.Snapshot().BracketedPaste {
		t.Error("oversized CSI should abort; BracketedPaste unexpectedly true")
	}
}

func TestTracker_SubscriberReceivesChange(t *testing.T) {
	tr := New()
	ch, cancel := tr.Subscribe()
	defer cancel()

	tr.Feed([]byte("\x1b[?2004h"))
	select {
	case c := <-ch:
		if !c.Mode.BracketedPaste {
			t.Errorf("Change payload = %+v", c)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber received no change")
	}

	tr.Feed([]byte("hello")) // no mode change
	select {
	case c := <-ch:
		t.Errorf("unexpected change on plain text: %+v", c)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestTracker_SubscribeCancelClosesChannel(t *testing.T) {
	tr := New()
	ch, cancel := tr.Subscribe()
	cancel()
	select {
	case _, open := <-ch:
		if open {
			t.Error("channel should be closed after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not close channel")
	}
}

func TestTracker_LastSeenUpdates(t *testing.T) {
	tr := New()
	if !tr.LastSeen().IsZero() {
		t.Error("LastSeen should start at zero time")
	}
	tr.Feed([]byte("a"))
	if tr.LastSeen().IsZero() {
		t.Error("LastSeen should update after Feed")
	}
}

func TestTracker_ConcurrentReaders(t *testing.T) {
	tr := New()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = tr.Snapshot()
				_ = tr.LastSeen()
			}
		}()
	}
	for j := 0; j < 200; j++ {
		tr.Feed([]byte("\x1b[?2004h\x1b[?2004l"))
	}
	wg.Wait()
}

func TestTracker_FanoutDropsOnFullSubscriber(t *testing.T) {
	tr := New()
	ch, cancel := tr.Subscribe()
	defer cancel()
	// Do not consume ch: let it fill.
	for i := 0; i < 1000; i++ {
		tr.Feed([]byte("\x1b[?2004h\x1b[?2004l"))
	}
	// Feeds must not block; test passes if we got here.
	// The channel buffer is small but we drained nothing — at most buf entries.
	count := 0
drain:
	for {
		select {
		case <-ch:
			count++
		default:
			break drain
		}
	}
	if count == 0 {
		t.Error("expected at least one buffered change")
	}
}
