package telegram

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/observability"
)

func TestAgentKey_PrefersAgentID(t *testing.T) {
	ev := observability.ActivityEvent{
		AgentID:   "agent-123",
		SessionID: "sess-456",
	}
	if got := agentKey(ev); got != "agent-123" {
		t.Fatalf("expected agent id, got %q", got)
	}
}

func TestAgentKey_FallsBackToSessionID(t *testing.T) {
	ev := observability.ActivityEvent{
		SessionID: "sess-456",
	}
	if got := agentKey(ev); got != "sess-456" {
		t.Fatalf("expected session id fallback, got %q", got)
	}
}
