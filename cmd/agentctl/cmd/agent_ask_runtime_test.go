package cmd

import (
	"testing"
	"time"
)

func TestResolvedAskDispatcherMode(t *testing.T) {
	t.Setenv(envAskDispatcherMode, "")
	if got := resolvedAskDispatcherMode(""); got != askDispatchModeMailbox {
		t.Fatalf("default mode=%q want %q", got, askDispatchModeMailbox)
	}

	t.Setenv(envAskDispatcherMode, askDispatchModeJido)
	if got := resolvedAskDispatcherMode(""); got != askDispatchModeJido {
		t.Fatalf("env mode=%q want %q", got, askDispatchModeJido)
	}

	if got := resolvedAskDispatcherMode(askDispatchModeMailbox); got != askDispatchModeMailbox {
		t.Fatalf("override mode=%q want %q", got, askDispatchModeMailbox)
	}
}

func TestParseDurationMillisEnv(t *testing.T) {
	t.Setenv("TEST_TIMEOUT_MS", "")
	if got := parseDurationMillisEnv("TEST_TIMEOUT_MS", 3*time.Second); got != 3*time.Second {
		t.Fatalf("timeout=%v want 3s", got)
	}

	t.Setenv("TEST_TIMEOUT_MS", "2500")
	if got := parseDurationMillisEnv("TEST_TIMEOUT_MS", 3*time.Second); got != 2500*time.Millisecond {
		t.Fatalf("timeout=%v want 2500ms", got)
	}

	t.Setenv("TEST_TIMEOUT_MS", "abc")
	if got := parseDurationMillisEnv("TEST_TIMEOUT_MS", 3*time.Second); got != 3*time.Second {
		t.Fatalf("timeout=%v want fallback 3s", got)
	}
}
