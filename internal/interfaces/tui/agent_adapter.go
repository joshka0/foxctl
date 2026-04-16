package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// AgentAdapter provides typed calls for /api/agents surfaces.
type AgentAdapter struct {
	client *APIClient
}

type AgentRecord struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Role        string `json:"role,omitempty"`
	State       string `json:"state,omitempty"`
	ParentID    string `json:"parent_id,omitempty"`
	Namespace   string `json:"ns,omitempty"`
	ExecMode    string `json:"exec_mode,omitempty"`
	LLMProvider string `json:"llm_provider,omitempty"`
	LLMModel    string `json:"llm_model,omitempty"`
}

type ListAgentsResponse struct {
	Agents []AgentRecord `json:"agents"`
	Total  int           `json:"total"`
}

type GetAgentResponse struct {
	Agent AgentRecord `json:"agent"`
}

func NewAgentAdapter(client *APIClient) (*AgentAdapter, error) {
	if client == nil {
		return nil, errors.New("api client is required")
	}
	return &AgentAdapter{client: client}, nil
}

func (a *AgentAdapter) ListAgents(ctx context.Context, limit int) (ListAgentsResponse, error) {
	if a == nil || a.client == nil {
		return ListAgentsResponse{}, errors.New("agent adapter is not configured")
	}

	path := "/api/agents"
	if limit > 0 {
		values := url.Values{}
		values.Set("limit", strconv.Itoa(limit))
		path += "?" + values.Encode()
	}

	var response ListAgentsResponse
	if err := a.client.RequestJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return ListAgentsResponse{}, fmt.Errorf("list agents: %w", err)
	}
	return response, nil
}

func (a *AgentAdapter) GetAgent(ctx context.Context, agentID string) (GetAgentResponse, error) {
	if a == nil || a.client == nil {
		return GetAgentResponse{}, errors.New("agent adapter is not configured")
	}

	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return GetAgentResponse{}, errors.New("agent id is required")
	}

	var response GetAgentResponse
	path := "/api/agents/" + url.PathEscape(agentID)
	if err := a.client.RequestJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return GetAgentResponse{}, fmt.Errorf("get agent %q: %w", agentID, err)
	}
	return response, nil
}
