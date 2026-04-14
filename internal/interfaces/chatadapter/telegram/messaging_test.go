package telegram

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestTruncateForTelegram(t *testing.T) {
	// Short string - no truncation
	short := "hello"
	if truncateForTelegram(short) != short {
		t.Fatalf("short string should not be truncated")
	}

	// Exactly 4096 chars - no truncation
	exact := strings.Repeat("a", 4096)
	if len([]rune(truncateForTelegram(exact))) != 4096 {
		t.Fatalf("4096-char string should not be truncated")
	}

	// Over 4096 chars - truncated with "..."
	long := strings.Repeat("b", 5000)
	got := truncateForTelegram(long)
	if len([]rune(got)) != 4096 {
		t.Fatalf("expected truncated length 4096, got %d", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected trailing '...', got %q", got[len(got)-3:])
	}
}

func TestTruncateForTelegramWithSuffix(t *testing.T) {
	if got := truncateForTelegramWithSuffix("hello", "..."); got != "hello..." {
		t.Fatalf("unexpected value: %q", got)
	}

	long := strings.Repeat("x", 5000)
	got := truncateForTelegramWithSuffix(long, "...")
	if len([]rune(got)) != 4096 {
		t.Fatalf("expected 4096 runes, got %d", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected suffix, got %q", got[len(got)-3:])
	}
}

func TestDispatchWithLimit_DropsWhenSaturated(t *testing.T) {
	a := New(config.TelegramSettings{MaxConcurrentMessages: 1}, "http://localhost:8090", nil)
	a.ctx, a.cancel = context.WithCancel(context.Background())
	defer a.cancel()

	var ran atomic.Bool

	// Occupy the semaphore with a long-running handler.
	a.msgSem <- struct{}{}
	defer func() { <-a.msgSem }()

	a.dispatchWithLimit("telegram.test", 1, func(ctx context.Context) error {
		ran.Store(true)
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	time.Sleep(20 * time.Millisecond)
	if ran.Load() {
		t.Fatalf("expected handler to be dropped when saturated")
	}
}
