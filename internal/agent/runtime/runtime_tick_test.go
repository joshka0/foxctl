package runtime

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
)

func TestScheduledThinkPrompt_TickMode(t *testing.T) {
	t.Parallel()

	got := scheduledThinkPrompt(agent.ModeTick)
	if got == "" {
		t.Fatal("scheduledThinkPrompt(agent.ModeTick) returned empty prompt")
	}
	if want := "tick mode"; !containsFold(got, want) {
		t.Fatalf("scheduledThinkPrompt(agent.ModeTick)=%q want substring %q", got, want)
	}
}

func TestScheduledTickInterval_DefaultsToMinute(t *testing.T) {
	t.Parallel()

	if got := scheduledTickInterval(0); got != 60*time.Second {
		t.Fatalf("scheduledTickInterval(0)=%s want 1m", got)
	}
}

func containsFold(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (stringContainsFold(haystack, needle))
}

func stringContainsFold(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if equalFoldASCII(haystack[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		aa := a[i]
		bb := b[i]
		if aa >= 'A' && aa <= 'Z' {
			aa += 'a' - 'A'
		}
		if bb >= 'A' && bb <= 'Z' {
			bb += 'a' - 'A'
		}
		if aa != bb {
			return false
		}
	}
	return true
}
