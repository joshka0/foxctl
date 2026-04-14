package teams

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

// agentAskResponse matches the daemon's POST /api/agents/{id}/ask response.
// The daemon returns {reply, conversation_id} directly (not a canonical envelope).
type agentAskResponse struct {
	Reply          string `json:"reply"`
	ConversationID string `json:"conversation_id"`

	// Canonical envelope fallback: if the daemon wraps in {data: {...}}, we check here.
	Data json.RawMessage `json:"data,omitempty"`
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

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if readErr != nil {
		return "", fmt.Errorf("read response: %w", readErr)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("ask failed (HTTP %d): %s", resp.StatusCode, truncateForTeams(string(raw)))
	}

	var parsed agentAskResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	// If reply is empty but a canonical envelope data field is present, try unwrapping.
	if strings.TrimSpace(parsed.Reply) == "" && len(parsed.Data) > 0 {
		var nested agentAskResponse
		if err := json.Unmarshal(parsed.Data, &nested); err == nil && strings.TrimSpace(nested.Reply) != "" {
			return nested.Reply, nil
		}
	}

	if strings.TrimSpace(parsed.Reply) == "" {
		return "", fmt.Errorf("empty reply")
	}
	return parsed.Reply, nil
}

// dispatchAgentAsk routes a reply-to-agent-card message to the daemon ask endpoint.
func (a *Adapter) dispatchAgentAsk(ctx context.Context, serviceURL string, activity Activity, convKey string, agentID string, text string) {
	agentID = strings.TrimSpace(agentID)
	text = strings.TrimSpace(text)
	if agentID == "" || text == "" {
		return
	}

	rawConvID := strings.TrimSpace(activity.Conversation.ID)
	if rawConvID == "" {
		return
	}

	a.dispatchWithLimit(ctx, "teams.agent_ask", convKey, func(askCtx context.Context) error {
		a.ShowTyping(askCtx, convKey)

		reply, err := a.askAgent(askCtx, agentID, text)
		if err != nil {
			errMsg := "Ask failed: " + err.Error()
			out := Activity{Type: "message", Text: errMsg}
			replyTo := strings.TrimSpace(activity.ID)
			if replyTo != "" {
				_, _ = a.botClient.ReplyToActivity(askCtx, serviceURL, rawConvID, replyTo, out)
			} else {
				_, _ = a.botClient.SendActivity(askCtx, serviceURL, rawConvID, out)
			}
			return err
		}

		out := Activity{Type: "message", Text: truncateForTeams(reply)}
		replyTo := strings.TrimSpace(activity.ID)
		if replyTo != "" {
			rr, err := a.botClient.ReplyToActivity(askCtx, serviceURL, rawConvID, replyTo, out)
			if err == nil && rr.ID != "" {
				a.agentRootIdx.Store(agentRootKey(convKey, rr.ID), agentRootIndexEntry{AgentID: agentID, LastSeen: a.clock.Now()})
			}
		} else {
			rr, err := a.botClient.SendActivity(askCtx, serviceURL, rawConvID, out)
			if err == nil && rr.ID != "" {
				a.agentRootIdx.Store(agentRootKey(convKey, rr.ID), agentRootIndexEntry{AgentID: agentID, LastSeen: a.clock.Now()})
			}
		}
		return nil
	})
}
