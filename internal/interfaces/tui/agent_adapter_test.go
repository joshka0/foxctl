package tui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentAdapterListAndGet(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			if got := r.URL.Query().Get("limit"); got != "25" {
				t.Fatalf("limit query = %q, want %q", got, "25")
			}
			_ = json.NewEncoder(w).Encode(ListAgentsResponse{
				Agents: []AgentRecord{
					{
						ID:          "agent-1",
						Name:        "Pascal",
						Role:        "researcher",
						State:       "running",
						ExecMode:    "proactive",
						LLMProvider: "lmstudio",
						LLMModel:    "gpt-5.3-codex-high",
					},
				},
				Total: 1,
			})
			return

		case r.Method == http.MethodGet && r.URL.Path == "/api/agents/agent-1":
			_ = json.NewEncoder(w).Encode(GetAgentResponse{
				Agent: AgentRecord{
					ID:        "agent-1",
					Name:      "Pascal",
					Role:      "researcher",
					State:     "running",
					ParentID:  "agent-root",
					Namespace: "ws-main",
				},
			})
			return
		}

		t.Fatalf("unexpected route: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}
	adapter, err := NewAgentAdapter(client)
	if err != nil {
		t.Fatalf("NewAgentAdapter error: %v", err)
	}

	listResp, err := adapter.ListAgents(context.Background(), 25)
	if err != nil {
		t.Fatalf("ListAgents error: %v", err)
	}
	if listResp.Total != 1 {
		t.Fatalf("Total = %d, want 1", listResp.Total)
	}
	if len(listResp.Agents) != 1 {
		t.Fatalf("len(Agents) = %d, want 1", len(listResp.Agents))
	}
	if listResp.Agents[0].ID != "agent-1" {
		t.Fatalf("Agents[0].ID = %q, want %q", listResp.Agents[0].ID, "agent-1")
	}

	getResp, err := adapter.GetAgent(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("GetAgent error: %v", err)
	}
	if getResp.Agent.Name != "Pascal" {
		t.Fatalf("Agent.Name = %q, want %q", getResp.Agent.Name, "Pascal")
	}
}

func TestAgentAdapterReturnsNon2xxError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "backend unavailable", http.StatusBadGateway)
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}
	adapter, err := NewAgentAdapter(client)
	if err != nil {
		t.Fatalf("NewAgentAdapter error: %v", err)
	}

	_, err = adapter.ListAgents(context.Background(), 0)
	if err == nil {
		t.Fatal("ListAgents error = nil, want non-2xx error")
	}

	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error type = %T, want wrapped *HTTPStatusError", err)
	}
	if statusErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("StatusCode = %d, want %d", statusErr.StatusCode, http.StatusBadGateway)
	}
}

func TestAgentAdapterRejectsEmptyAgentID(t *testing.T) {
	t.Parallel()

	adapter := &AgentAdapter{client: &APIClient{}}
	_, err := adapter.GetAgent(context.Background(), " ")
	if err == nil {
		t.Fatal("GetAgent error = nil, want validation error")
	}
}
