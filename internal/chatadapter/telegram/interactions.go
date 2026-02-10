package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jkatigb/agentctl/internal/chatadapter"
)

// HandleInteraction processes inline keyboard callback queries from Telegram.
// CustomID format: "action:agentID" (e.g., "stop:01J...").
func (a *Adapter) HandleInteraction(ctx context.Context, evt chatadapter.InteractionEvent) error {
	parts := strings.SplitN(evt.CustomID, ":", 2)
	if len(parts) != 2 {
		return evt.Respond("Unknown interaction", nil)
	}

	action, agentID := parts[0], strings.TrimSpace(parts[1])
	if agentID == "" {
		return evt.Respond("Unknown interaction (missing agent id)", nil)
	}

	switch action {
	case "stop":
		return a.handleStop(ctx, evt, agentID)
	case "retry":
		return a.handleRetry(ctx, evt, agentID)
	case "details":
		return a.handleDetails(ctx, evt, agentID)
	default:
		return evt.Respond(fmt.Sprintf("Unknown action: %s", action), nil)
	}
}

func (a *Adapter) handleStop(ctx context.Context, evt chatadapter.InteractionEvent, agentID string) error {
	if a.daemonURL == "" {
		return evt.Respond("Daemon URL not configured", nil)
	}

	url := fmt.Sprintf("%s/api/agents/%s/daemon/kill", strings.TrimRight(a.daemonURL, "/"), agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to create request: %s", err), nil)
	}

	if a.httpClient == nil {
		return evt.Respond("Internal error: HTTP client not configured", nil)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to kill agent: %s", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return evt.Respond(fmt.Sprintf("Kill failed (%d): %s", resp.StatusCode, truncateForTelegram(string(body))), nil)
	}

	return evt.Respond(fmt.Sprintf("Stop requested for agent %s.\nagent_id: %s", chatadapter.TruncateRunes(agentID, 8), agentID), nil)
}

func (a *Adapter) handleRetry(ctx context.Context, evt chatadapter.InteractionEvent, agentID string) error {
	if a.daemonURL == "" {
		return evt.Respond("Daemon URL not configured", nil)
	}

	url := fmt.Sprintf("%s/api/agents/%s/daemon/start", strings.TrimRight(a.daemonURL, "/"), agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to create request: %s", err), nil)
	}

	if a.httpClient == nil {
		return evt.Respond("Internal error: HTTP client not configured", nil)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to restart agent: %s", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return evt.Respond(fmt.Sprintf("Retry failed (%d): %s", resp.StatusCode, truncateForTelegram(string(body))), nil)
	}

	return evt.Respond(fmt.Sprintf("Retry requested for agent %s.\nagent_id: %s", chatadapter.TruncateRunes(agentID, 8), agentID), nil)
}

func (a *Adapter) handleDetails(ctx context.Context, evt chatadapter.InteractionEvent, agentID string) error {
	if a.daemonURL == "" {
		return evt.Respond("Daemon URL not configured", nil)
	}

	// Best-effort: if we have a last-seen session ID for this agent, show the
	// structured session summary (much more useful than raw agent JSON).
	if v, ok := a.agentThreads.Load(agentID); ok {
		if st, ok := v.(agentThread); ok {
			st.LastSeen = a.clock.Now()
			a.agentThreads.Store(agentID, st)
			if sid := strings.TrimSpace(st.SessionID); sid != "" {
				if sess, err := a.fetchSessionSummary(ctx, sid); err == nil {
					return evt.Respond(formatSessionSummaryText(agentID, sess), nil)
				}
			}
		}
	}

	url := fmt.Sprintf("%s/api/agents/%s", strings.TrimRight(a.daemonURL, "/"), agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to create request: %s", err), nil)
	}

	if a.httpClient == nil {
		return evt.Respond("Internal error: HTTP client not configured", nil)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to fetch agent: %s", err), nil)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to read response: %s", err), nil)
	}

	if resp.StatusCode >= 400 {
		return evt.Respond(fmt.Sprintf("Agent not found (%d): %s", resp.StatusCode, truncateForTelegram(string(body))), nil)
	}

	// Best-effort parse and pretty print.
	var agent any
	if err := json.Unmarshal(body, &agent); err != nil {
		return evt.Respond(truncateForTelegram(string(body)), nil)
	}
	pretty, _ := json.MarshalIndent(agent, "", "  ")
	return evt.Respond(truncateForTelegram(string(pretty)), nil)
}
