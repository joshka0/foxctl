package cmd

import (
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
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

func TestResolvedSpawnExecutionLayer(t *testing.T) {
	t.Setenv(envAskDispatcherMode, "")
	if got := resolvedSpawnExecutionLayer(""); got != agent.ExecutionLayerClassic {
		t.Fatalf("default layer=%q want %q", got, agent.ExecutionLayerClassic)
	}

	t.Setenv(envAskDispatcherMode, askDispatchModeJido)
	if got := resolvedSpawnExecutionLayer(""); got != agent.ExecutionLayerJido {
		t.Fatalf("env layer=%q want %q", got, agent.ExecutionLayerJido)
	}

	if got := resolvedSpawnExecutionLayer(askDispatchModeMailbox); got != agent.ExecutionLayerClassic {
		t.Fatalf("override layer=%q want %q", got, agent.ExecutionLayerClassic)
	}
}
