package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunAgentSmokeSubmitsCompanionAsk(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			_ = json.NewEncoder(w).Encode(ListAgentsResponse{})
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents/agent-smoke":
			_ = json.NewEncoder(w).Encode(GetAgentResponse{
				Agent: AgentRecord{
					ID:          "agent-smoke",
					Name:        "Local Fox",
					Role:        "coder",
					LLMProvider: "lmstudio",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/agents/agent-smoke/ask":
			var req AskAgentRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if req.Message != "ping" {
				t.Fatalf("Message = %q, want ping", req.Message)
			}
			_ = json.NewEncoder(w).Encode(AskAgentResponse{
				Reply:          "pong",
				ConversationID: "agent-smoke",
			})
		default:
			t.Fatalf("unexpected route: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	summary, err := RunAgentSmoke(context.Background(), SmokeAgentOptions{
		Options: Options{
			APIBaseURL: srv.URL,
			AgentID:    "agent-smoke",
		},
		Ask:     "ping",
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("RunAgentSmoke error: %v", err)
	}
	if summary.AskStatus != smokeStatusAccepted {
		t.Fatalf("AskStatus = %q, want %q", summary.AskStatus, smokeStatusAccepted)
	}
	if summary.Reply != "pong" {
		t.Fatalf("Reply = %q, want pong", summary.Reply)
	}
	if summary.AskAccepted != 1 || summary.AskErrors != 0 || summary.TimedOut {
		t.Fatalf("summary = %+v, want one accepted non-timeout ask", summary)
	}
}
