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
	ID              string `json:"id"`
	Name            string `json:"name,omitempty"`
	Slug            string `json:"slug,omitempty"`
	Role            string `json:"role,omitempty"`
	State           string `json:"state,omitempty"`
	ParentID        string `json:"parent_id,omitempty"`
	Namespace       string `json:"ns,omitempty"`
	ExecMode        string `json:"exec_mode,omitempty"`
	LLMProvider     string `json:"llm_provider,omitempty"`
	LLMModel        string `json:"llm_model,omitempty"`
	WorkspaceRoot   string `json:"workspace_root,omitempty"`
	WorkspaceSource string `json:"workspace_source,omitempty"`
}

type ListAgentsResponse struct {
	Agents []AgentRecord `json:"agents"`
	Total  int           `json:"total"`
}

type GetAgentResponse struct {
	Agent AgentRecord `json:"agent"`
}

type AskAgentRequest struct {
	Message string `json:"message"`
}

type AskAgentResponse struct {
	Reply          string `json:"reply"`
	ConversationID string `json:"conversation_id"`
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

func (a *AgentAdapter) AskAgent(ctx context.Context, agentID string, req AskAgentRequest) (AskAgentResponse, error) {
	if a == nil || a.client == nil {
		return AskAgentResponse{}, errors.New("agent adapter is not configured")
	}

	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return AskAgentResponse{}, errors.New("agent id is required")
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return AskAgentResponse{}, errors.New("message is required")
	}

	var response AskAgentResponse
	path := "/api/agents/" + url.PathEscape(agentID) + "/ask"
	if err := a.client.RequestJSON(ctx, http.MethodPost, path, req, &response); err != nil {
		return AskAgentResponse{}, fmt.Errorf("ask agent %q: %w", agentID, err)
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
