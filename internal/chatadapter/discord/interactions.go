package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jkatigb/agentctl/internal/chatadapter"
)

// HandleInteraction processes button clicks from Discord.
// CustomID format: "action:sessionID" (e.g., "stop:abc-123").
func (a *Adapter) HandleInteraction(ctx context.Context, evt chatadapter.InteractionEvent) error {
	parts := strings.SplitN(evt.CustomID, ":", 2)
	if len(parts) != 2 {
		return evt.Respond("Unknown interaction", nil)
	}

	action, sessionID := parts[0], parts[1]

	switch action {
	case "stop":
		return a.handleStop(ctx, evt, sessionID)
	case "retry":
		return a.handleRetry(ctx, evt, sessionID)
	case "details":
		return a.handleDetails(ctx, evt, sessionID)
	default:
		return evt.Respond(fmt.Sprintf("Unknown action: %s", action), nil)
	}
}

// handleStop sends a kill request to the daemon and updates the embed.
func (a *Adapter) handleStop(ctx context.Context, evt chatadapter.InteractionEvent, sessionID string) error {
	if a.daemonURL == "" {
		return evt.Respond("Daemon URL not configured", nil)
	}

	url := fmt.Sprintf("%s/api/agents/%s/daemon/kill", a.daemonURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to create request: %s", err), nil)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to kill agent: %s", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return evt.Respond(fmt.Sprintf("Kill failed (%d): %s", resp.StatusCode, truncate(string(body), 200)), nil)
	}

	// Update the original message to show killed state
	return evt.Update(
		"",
		[]chatadapter.Embed{{
			Title:       "Agent Killed",
			Description: fmt.Sprintf("Session `%s` was stopped by %s", truncate(sessionID, 36), evt.User.Username),
			Color:       colorKilled,
		}},
		nil, // remove buttons
	)
}

// handleRetry sends a start request to the daemon.
func (a *Adapter) handleRetry(ctx context.Context, evt chatadapter.InteractionEvent, sessionID string) error {
	if a.daemonURL == "" {
		return evt.Respond("Daemon URL not configured", nil)
	}

	url := fmt.Sprintf("%s/api/agents/%s/daemon/start", a.daemonURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to create request: %s", err), nil)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to restart agent: %s", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return evt.Respond(fmt.Sprintf("Retry failed (%d): %s", resp.StatusCode, truncate(string(body), 200)), nil)
	}

	return evt.Respond(fmt.Sprintf("Retrying agent `%s`...", truncate(sessionID, 36)), nil)
}

// handleDetails fetches agent info and responds ephemerally.
func (a *Adapter) handleDetails(ctx context.Context, evt chatadapter.InteractionEvent, sessionID string) error {
	if a.daemonURL == "" {
		return evt.Respond("Daemon URL not configured", nil)
	}

	url := fmt.Sprintf("%s/api/agents/%s", a.daemonURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to create request: %s", err), nil)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to fetch agent: %s", err), nil)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to read response: %s", err), nil)
	}

	if resp.StatusCode >= 400 {
		return evt.Respond(fmt.Sprintf("Agent not found (%d): %s", resp.StatusCode, truncate(string(body), 200)), nil)
	}

	// Parse and format agent info
	var agent map[string]any
	if err := json.Unmarshal(body, &agent); err != nil {
		return evt.Respond(fmt.Sprintf("```json\n%s\n```", truncate(string(body), 1800)), nil)
	}

	fields := []chatadapter.Field{}
	for _, key := range []string{"id", "role", "status", "prompt", "session_id", "iterations", "max_iterations"} {
		if v, ok := agent[key]; ok && v != nil {
			fields = append(fields, chatadapter.Field{
				Name:   key,
				Value:  fmt.Sprintf("`%v`", v),
				Inline: true,
			})
		}
	}

	return evt.Respond("", []chatadapter.Embed{{
		Title:  "Agent Details",
		Color:  colorInfo,
		Fields: fields,
	}})
}
