package teams

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/joshka0/foxctl/internal/interfaces/chatadapter"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

// HandleInteraction processes Adaptive Card Action.Submit callbacks from Teams.
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

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to kill agent: %s", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return evt.Respond(fmt.Sprintf("Kill failed (%d): %s", resp.StatusCode, truncateForTeams(string(body))), nil)
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

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to restart agent: %s", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return evt.Respond(fmt.Sprintf("Retry failed (%d): %s", resp.StatusCode, truncateForTeams(string(body))), nil)
	}

	return evt.Respond(fmt.Sprintf("Retry requested for agent %s.\nagent_id: %s", chatadapter.TruncateRunes(agentID, 8), agentID), nil)
}

func (a *Adapter) handleDetails(ctx context.Context, evt chatadapter.InteractionEvent, agentID string) error {
	if a.daemonURL == "" {
		return evt.Respond("Daemon URL not configured", nil)
	}

	// Best-effort: if we have a session ID, show the structured summary.
	if v, ok := a.agentThreads.Load(agentID); ok {
		if st, ok := v.(agentThread); ok {
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
		return evt.Respond(fmt.Sprintf("Agent not found (%d): %s", resp.StatusCode, truncateForTeams(string(body))), nil)
	}

	var agent any
	if err := json.Unmarshal(body, &agent); err != nil {
		return evt.Respond(truncateForTeams(string(body)), nil)
	}
	pretty, _ := json.MarshalIndent(agent, "", "  ")
	return evt.Respond(truncateForTeams(string(pretty)), nil)
}

// handleInvoke processes an invoke activity from Teams (Adaptive Card Action.Submit).
func (a *Adapter) handleInvoke(ctx context.Context, serviceURL string, activity Activity, convKey string) error {
	a.mu.Lock()
	handler := a.intHandler
	a.mu.Unlock()
	if handler == nil {
		return nil
	}

	if a.botClient == nil {
		observability.Emit(ctx, observability.NewEvent("teams.invoke_uninitialized").
			WithComponent("teams").
			WithData("conversation_id", convKey).
			Error(fmt.Errorf("botClient is nil; Connect() not called"), 0))
		return fmt.Errorf("teams: botClient not initialized")
	}

	// Parse the invoke value: {action, agentID}.
	var payload struct {
		Action  string `json:"action"`
		AgentID string `json:"agentID"`
	}
	if len(activity.Value) > 0 {
		if err := json.Unmarshal(activity.Value, &payload); err != nil {
			observability.Emit(ctx, observability.NewEvent("teams.invoke_parse_failed").
				WithComponent("teams").
				WithData("conversation_id", convKey).
				Error(err, 0))
			return nil
		}
	}

	action := strings.TrimSpace(payload.Action)
	agentID := strings.TrimSpace(payload.AgentID)
	if action == "" || agentID == "" {
		return nil
	}

	customID := action + ":" + agentID
	rawConvID := strings.TrimSpace(activity.Conversation.ID)
	if rawConvID == "" {
		return nil
	}

	user := chatadapter.UserRef{
		ID:       strings.TrimSpace(activity.From.ID),
		Username: strings.TrimSpace(activity.From.Name),
	}
	if user.Username == "" {
		user.Username = user.ID
	}

	msgRef := chatadapter.MessageRef{
		ChannelID: convKey,
		MessageID: strings.TrimSpace(activity.ID),
	}

	respond := func(content string, _ []chatadapter.Embed) error {
		if strings.TrimSpace(content) == "" {
			return nil
		}
		out := Activity{Type: "message", Text: content}
		replyTo := strings.TrimSpace(activity.ID)
		if replyTo != "" {
			_, err := a.botClient.ReplyToActivity(ctx, serviceURL, rawConvID, replyTo, out)
			return err
		}
		_, err := a.botClient.SendActivity(ctx, serviceURL, rawConvID, out)
		return err
	}

	update := func(content string, _ []chatadapter.Embed, _ []chatadapter.Component) error {
		// Update the card that triggered the invoke.
		activityID := strings.TrimSpace(activity.ID)
		if activityID == "" {
			return nil
		}
		return a.botClient.UpdateActivity(ctx, serviceURL, rawConvID, activityID, Activity{Type: "message", Text: content})
	}

	evt := chatadapter.NewInteractionEvent("button", customID, user, convKey, "", msgRef, respond, update)
	return handler(ctx, evt)
}
