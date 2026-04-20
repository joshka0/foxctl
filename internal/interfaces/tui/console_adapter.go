package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ConsoleAdapter provides typed calls for /api/console/sessions surfaces.
type ConsoleAdapter struct {
	client *APIClient
}

type ConsoleSession struct {
	ID           string    `json:"id"`
	Workspace    string    `json:"workspace"`
	Profile      string    `json:"profile"`
	Created      time.Time `json:"created"`
	MessageCount int       `json:"message_count"`
	ClientCount  int       `json:"client_count"`
}

type ConsoleMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	Timestamp  int64             `json:"timestamp"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	ToolCalls  []json.RawMessage `json:"tool_calls,omitempty"`
	Metadata   json.RawMessage   `json:"metadata,omitempty"`
}

type ConsoleInFlightState struct {
	CorrelationID string
	Raw           json.RawMessage
}

func (s *ConsoleInFlightState) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*s = ConsoleInFlightState{}
		return nil
	}

	s.Raw = append(s.Raw[:0], data...)

	var correlationID string
	if err := json.Unmarshal(data, &correlationID); err == nil {
		s.CorrelationID = strings.TrimSpace(correlationID)
		return nil
	}

	var payload struct {
		CorrelationID string `json:"correlation_id"`
		ID            string `json:"id"`
		InFlight      string `json:"inflight"`
	}
	if err := json.Unmarshal(data, &payload); err == nil {
		s.CorrelationID = firstNonEmpty(payload.CorrelationID, payload.ID, payload.InFlight)
	}

	return nil
}

type ListConsoleSessionsResponse struct {
	Sessions []ConsoleSession `json:"sessions"`
	Count    int              `json:"count"`
}

type GetConsoleSessionResponse struct {
	Session  ConsoleSession       `json:"session"`
	Messages []ConsoleMessage     `json:"messages"`
	InFlight ConsoleInFlightState `json:"inflight"`
}

type CreateConsoleSessionRequest struct {
	Workspace    string `json:"workspace,omitempty"`
	Profile      string `json:"profile,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	LLMProvider  string `json:"llm_provider,omitempty"`
	LLMModel     string `json:"llm_model,omitempty"`
}

type CreateConsoleSessionResponse struct {
	Session ConsoleSession `json:"session"`
}

type AskConsoleSessionRequest struct {
	Content       string `json:"content"`
	CorrelationID string `json:"correlation_id,omitempty"`
	LLMProvider   string `json:"llm_provider,omitempty"`
	LLMModel      string `json:"llm_model,omitempty"`
}

type AskConsoleSessionResponse struct {
	OK            bool   `json:"ok"`
	CorrelationID string `json:"correlation_id"`
	Message       string `json:"message,omitempty"`
}

type CancelConsoleSessionRequest struct {
	CorrelationID string `json:"correlation_id,omitempty"`
}

type CancelConsoleSessionResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

func NewConsoleAdapter(client *APIClient) (*ConsoleAdapter, error) {
	if client == nil {
		return nil, errors.New("api client is required")
	}
	return &ConsoleAdapter{client: client}, nil
}

func (a *ConsoleAdapter) ListSessions(ctx context.Context, workspace string) (ListConsoleSessionsResponse, error) {
	if a == nil || a.client == nil {
		return ListConsoleSessionsResponse{}, errors.New("console adapter is not configured")
	}

	path := "/api/console/sessions"
	if workspace = strings.TrimSpace(workspace); workspace != "" {
		values := url.Values{}
		values.Set("workspace", workspace)
		path += "?" + values.Encode()
	}

	var response ListConsoleSessionsResponse
	if err := a.client.RequestJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return ListConsoleSessionsResponse{}, fmt.Errorf("list console sessions: %w", err)
	}
	return response, nil
}

func (a *ConsoleAdapter) GetSession(ctx context.Context, sessionID string) (GetConsoleSessionResponse, error) {
	if a == nil || a.client == nil {
		return GetConsoleSessionResponse{}, errors.New("console adapter is not configured")
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return GetConsoleSessionResponse{}, errors.New("session id is required")
	}

	var response GetConsoleSessionResponse
	path := "/api/console/sessions/" + url.PathEscape(sessionID)
	if err := a.client.RequestJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return GetConsoleSessionResponse{}, fmt.Errorf("get console session %q: %w", sessionID, err)
	}
	return response, nil
}

func (a *ConsoleAdapter) CreateSession(ctx context.Context, req CreateConsoleSessionRequest) (CreateConsoleSessionResponse, error) {
	if a == nil || a.client == nil {
		return CreateConsoleSessionResponse{}, errors.New("console adapter is not configured")
	}

	var response CreateConsoleSessionResponse
	if err := a.client.RequestJSON(ctx, http.MethodPost, "/api/console/sessions", req, &response); err != nil {
		return CreateConsoleSessionResponse{}, fmt.Errorf("create console session: %w", err)
	}
	return response, nil
}

func (a *ConsoleAdapter) AskSession(ctx context.Context, sessionID string, req AskConsoleSessionRequest) (AskConsoleSessionResponse, error) {
	if a == nil || a.client == nil {
		return AskConsoleSessionResponse{}, errors.New("console adapter is not configured")
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return AskConsoleSessionResponse{}, errors.New("session id is required")
	}

	var response AskConsoleSessionResponse
	path := "/api/console/sessions/" + url.PathEscape(sessionID) + "/ask"
	if err := a.client.RequestJSON(ctx, http.MethodPost, path, req, &response); err != nil {
		return AskConsoleSessionResponse{}, fmt.Errorf("ask console session %q: %w", sessionID, err)
	}
	return response, nil
}

func (a *ConsoleAdapter) CancelSession(ctx context.Context, sessionID string, req CancelConsoleSessionRequest) (CancelConsoleSessionResponse, error) {
	if a == nil || a.client == nil {
		return CancelConsoleSessionResponse{}, errors.New("console adapter is not configured")
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return CancelConsoleSessionResponse{}, errors.New("session id is required")
	}

	var response CancelConsoleSessionResponse
	path := "/api/console/sessions/" + url.PathEscape(sessionID) + "/cancel"
	if err := a.client.RequestJSON(ctx, http.MethodPost, path, req, &response); err != nil {
		return CancelConsoleSessionResponse{}, fmt.Errorf("cancel console session %q: %w", sessionID, err)
	}
	return response, nil
}
