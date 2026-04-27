package vtscreen

import "testing"

func TestScreen_CursorPositionAndCR(t *testing.T) {
	s := New(12, 20)
	s.Feed([]byte("\x1b[10;10Hhello\rworld"))
	snap := s.Snapshot()
	if got := snap.Lines[9]; got != "world    hello" {
		t.Fatalf("line 10 = %q, want %q", got, "world    hello")
	}
	if snap.Cursor.Row != 9 || snap.Cursor.Col != 5 {
		t.Fatalf("cursor = %+v, want row=9 col=5", snap.Cursor)
	}
}

func TestScreen_EraseDisplayAndLine(t *testing.T) {
	s := New(3, 10)
	s.Feed([]byte("abcdef\x1b[1;3H\x1b[K"))
	snap := s.Snapshot()
	if got := snap.Lines[0]; got != "ab" {
		t.Fatalf("line after erase-line = %q, want ab", got)
	}
	s.Feed([]byte("\x1b[2J"))
	snap = s.Snapshot()
	for i, line := range snap.Lines {
		if line != "" {
			t.Fatalf("line %d after erase-display = %q, want empty", i, line)
		}
	}
}

func TestScreen_AltScreenPreservesPrimary(t *testing.T) {
	s := New(3, 12)
	s.Feed([]byte("primary"))
	s.Feed([]byte("\x1b[?1049hALT"))
	alt := s.Snapshot()
	if !alt.AltScreen || alt.Lines[0] != "ALT" {
		t.Fatalf("alt snapshot = %+v", alt)
	}
	s.Feed([]byte("\x1b[?1049l"))
	primary := s.Snapshot()
	if primary.AltScreen {
		t.Fatal("expected primary screen after alt exit")
	}
	if primary.Lines[0] != "primary" {
		t.Fatalf("primary line = %q, want primary", primary.Lines[0])
	}
}

func TestScreen_UTF8Printable(t *testing.T) {
	s := New(2, 10)
	s.Feed([]byte("⛬ ok"))
	snap := s.Snapshot()
	if snap.Lines[0] != "⛬ ok" {
		t.Fatalf("line = %q, want unicode text", snap.Lines[0])
	}
}
