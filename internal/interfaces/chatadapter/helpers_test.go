package chatadapter

import (
	"strings"
	"testing"
)

func TestTruncateRunesWithEllipsis(t *testing.T) {
	// Short string - no truncation.
	if got := TruncateRunesWithEllipsis("hello", 10); got != "hello" {
		t.Fatalf("unexpected value: %q", got)
	}

	// Exactly at limit - no truncation.
	exact := strings.Repeat("a", 5)
	if got := TruncateRunesWithEllipsis(exact, 5); got != exact {
		t.Fatalf("expected no truncation, got %q", got)
	}

	// Over limit - truncated with "...".
	long := strings.Repeat("b", 20)
	got := TruncateRunesWithEllipsis(long, 10)
	if len([]rune(got)) != 10 {
		t.Fatalf("expected 10 runes, got %d", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected suffix '...', got %q", got)
	}
}

func TestTruncateRunesWithSuffix(t *testing.T) {
	if got := TruncateRunesWithSuffix("hello", "...", 20); got != "hello..." {
		t.Fatalf("unexpected value: %q", got)
	}

	long := strings.Repeat("x", 5000)
	got := TruncateRunesWithSuffix(long, "...", 2000)
	if len([]rune(got)) != 2000 {
		t.Fatalf("expected 2000 runes, got %d", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected suffix, got %q", got[len(got)-3:])
	}
}
