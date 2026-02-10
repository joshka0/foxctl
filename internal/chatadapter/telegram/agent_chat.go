package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type agentAskRequest struct {
	Message string `json:"message"`
}

type agentAskResponse struct {
	Reply          string `json:"reply"`
	ConversationID string `json:"conversation_id"`
}

func (a *Adapter) askAgent(ctx context.Context, agentID string, message string) (string, error) {
	if strings.TrimSpace(a.daemonURL) == "" {
		return "", fmt.Errorf("daemon URL not configured")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", fmt.Errorf("missing agent id")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("missing message")
	}

	body, err := json.Marshal(agentAskRequest{Message: message})
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	url := fmt.Sprintf("%s/api/agents/%s/ask", strings.TrimRight(a.daemonURL, "/"), agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ask agent: %w", err)
	}
	defer resp.Body.Close()

	// Limit response size to avoid unbounded memory usage on error bodies.
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if readErr != nil {
		return "", fmt.Errorf("read response: %w", readErr)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("ask failed (HTTP %d): %s", resp.StatusCode, truncateForTelegram(string(raw)))
	}

	var parsed agentAskResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if strings.TrimSpace(parsed.Reply) == "" {
		return "", fmt.Errorf("empty reply")
	}
	return parsed.Reply, nil
}
