package teams

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/interfaces/chatadapter"
	"github.com/joshka0/foxctl/internal/interfaces/web/sse"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

// agentThread tracks a root card message for a single agent.
type agentThread struct {
	RootActivityID string // Bot Framework activity ID of the root card
	Role           string
	Prompt         string
	SessionID      string
	LastIterEdit   time.Time
	LastSeen       time.Time
}

type agentRootIndexEntry struct {
	AgentID  string
	LastSeen time.Time
}

const (
	agentIndexTTL             = 24 * time.Hour
	agentIndexJanitorInterval = 30 * time.Minute
)

func agentKey(event observability.ActivityEvent) string {
	if v := strings.TrimSpace(event.AgentID); v != "" {
		return v
	}
	return strings.TrimSpace(event.SessionID)
}

func agentRootKey(convKey, activityID string) string {
	return convKey + ":" + activityID
}

// startEventListener subscribes to the SSE hub and routes agent activity events to Teams.
func (a *Adapter) startEventListener(ctx context.Context) {
	if a.sseHub == nil {
		observability.Emit(ctx, observability.NewEvent("teams.event_listener_skipped").
			WithComponent("teams").
			WithData("reason", "sseHub is nil").
			Success(0))
		return
	}

	client := &sse.Client{
		ID:   "teams-adapter",
		Send: make(chan []byte, 64),
	}

	a.mu.Lock()
	a.sseClient = client
	a.mu.Unlock()

	a.sseHub.Register(client)
	observability.Emit(ctx, observability.NewEvent("teams.event_listener_started").WithComponent("teams").Success(0))

	defer func() {
		a.sseHub.Unregister(client)
		observability.Emit(ctx, observability.NewEvent("teams.event_listener_stopped").WithComponent("teams").Success(0))
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-client.Send:
			if !ok {
				return
			}
			a.processSSEMessage(msg)
		}
	}
}

// processSSEMessage parses an SSE message and routes activity events.
func (a *Adapter) processSSEMessage(raw []byte) {
	const prefix = "data: "
	data := raw
	if len(data) > len(prefix) && string(data[:len(prefix)]) == prefix {
		data = data[len(prefix):]
	}
	for len(data) > 0 && (data[len(data)-1] == '\n' || data[len(data)-1] == '\r') {
		data = data[:len(data)-1]
	}

	var event sse.Event
	if err := json.Unmarshal(data, &event); err != nil {
		observability.Emit(a.ctx, observability.NewEvent("teams.sse_parse_failed").
			WithComponent("teams").
			WithData("stage", "event").
			Error(err, 0))
		return
	}
	if event.Type != "activity" {
		return
	}

	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		observability.Emit(a.ctx, observability.NewEvent("teams.sse_parse_failed").
			WithComponent("teams").
			WithData("stage", "marshal_data").
			Error(err, 0))
		return
	}

	var activity observability.ActivityEvent
	if err := json.Unmarshal(dataBytes, &activity); err != nil {
		observability.Emit(a.ctx, observability.NewEvent("teams.sse_parse_failed").
			WithComponent("teams").
			WithData("stage", "activity").
			Error(err, 0))
		return
	}

	a.routeActivityEvent(activity)
}

// routeActivityEvent dispatches an ActivityEvent to Teams messages.
func (a *Adapter) routeActivityEvent(event observability.ActivityEvent) {
	if !strings.HasPrefix(event.Operation, "agent.") {
		return
	}

	key := agentKey(event)
	if key == "" {
		return
	}

	// Compact activity feed (major lifecycle + errors only).
	if a.cfg.ActivityFeedConversationID != "" {
		if line := activityFeedLine(event); line != "" {
			if _, err := a.sendToConversation(a.ctx, a.cfg.ActivityFeedConversationID, Activity{Type: "message", Text: line}); err != nil {
				observability.Emit(a.ctx,
					observability.NewEvent("teams.activity_feed_failed").
						WithComponent("teams").
						WithData("op", event.Operation).
						Error(err, 0))
			}
		}
	}

	// Per-agent "thread" via a root card + edits in the agent conversation.
	if a.cfg.AgentConversationID == "" {
		return
	}

	state := a.ensureAgentRoot(event)
	if state.RootActivityID == "" {
		return
	}

	now := a.clock.Now()

	if sid := strings.TrimSpace(event.SessionID); sid != "" {
		state.SessionID = sid
	}
	state.LastSeen = now
	a.agentThreads.Store(key, state)

	switch event.Operation {
	case "agent.spawn":
		a.updateAgentRoot(event, state, "running", actionsStopDetails(key))
	case "agent.iteration":
		if now.Sub(state.LastIterEdit) < 1500*time.Millisecond {
			return
		}
		state.LastIterEdit = now
		state.LastSeen = now
		a.agentThreads.Store(key, state)
		a.updateAgentRoot(event, state, "running", actionsStopDetails(key))
	case "agent.complete":
		a.updateAgentRoot(event, state, "complete", actionsDetails(key))
		a.postAgentSummary(a.ctx, key, state)
	case "agent.kill":
		a.updateAgentRoot(event, state, "killed", actionsDetails(key))
	default:
		if event.Status == "error" {
			a.updateAgentRoot(event, state, "error", actionsRetryDetails(key))
		}
	}
}

// ensureAgentRoot creates or retrieves the root card for an agent.
func (a *Adapter) ensureAgentRoot(event observability.ActivityEvent) agentThread {
	if a.cfg.AgentConversationID == "" {
		return agentThread{}
	}

	key := agentKey(event)
	if key == "" {
		return agentThread{}
	}

	if v, ok := a.agentThreads.Load(key); ok {
		if st, ok := v.(agentThread); ok && st.RootActivityID != "" {
			return st
		}
	}

	role := chatadapter.GetDataString(event.Data, "role")
	prompt := chatadapter.GetDataString(event.Data, "prompt")
	sessionID := strings.TrimSpace(event.SessionID)

	body := agentRootCardText(event, "running")
	card := agentCard(key, role, "running", body, actionsStopDetails(key))

	activityID, err := a.sendToConversation(a.ctx, a.cfg.AgentConversationID, card)
	if err != nil {
		observability.Emit(a.ctx,
			observability.NewEvent("teams.agent_root_create_failed").
				WithComponent("teams").
				WithData("agent_id", key).
				Error(err, 0))
		return agentThread{}
	}

	now := a.clock.Now()
	st := agentThread{
		RootActivityID: activityID,
		Role:           role,
		Prompt:         prompt,
		SessionID:      sessionID,
		LastSeen:       now,
	}
	a.agentThreads.Store(key, st)

	convKey := a.cfg.AgentConversationID
	a.agentRootIdx.Store(agentRootKey(convKey, activityID), agentRootIndexEntry{AgentID: key, LastSeen: now})

	observability.Emit(a.ctx,
		observability.NewEvent("teams.agent_root_created").
			WithComponent("teams").
			WithData("agent_id", key).
			WithData("activity_id", activityID).
			Success(0))

	return st
}

// updateAgentRoot edits the root card with updated status and actions.
func (a *Adapter) updateAgentRoot(event observability.ActivityEvent, state agentThread, status string, actions []cardAction) {
	if state.RootActivityID == "" || a.cfg.AgentConversationID == "" {
		return
	}

	key := agentKey(event)
	body := agentRootCardText(event, status)
	card := agentCard(key, chatadapter.GetDataString(event.Data, "role"), status, body, actions)

	if err := a.updateInConversation(a.ctx, a.cfg.AgentConversationID, state.RootActivityID, card); err != nil {
		observability.Emit(a.ctx,
			observability.NewEvent("teams.agent_root_update_failed").
				WithComponent("teams").
				WithData("session_id", event.SessionID).
				WithData("op", event.Operation).
				Error(err, 0))
	}
}

// sendToConversation sends an activity to a conversation using a cached or stored serviceURL.
// Returns the activity ID of the sent message.
func (a *Adapter) sendToConversation(ctx context.Context, rawConvID string, activity Activity) (string, error) {
	if a.botClient == nil {
		return "", fmt.Errorf("sendToConversation: botClient is nil (Connect not called)")
	}

	serviceURL := a.resolveServiceURL(ctx, rawConvID)
	if serviceURL == "" {
		return "", fmt.Errorf("sendToConversation: could not resolve serviceURL for conversation %q", rawConvID)
	}

	rr, err := a.botClient.SendActivity(ctx, serviceURL, rawConvID, activity)
	if err != nil {
		return "", err
	}
	return rr.ID, nil
}

// updateInConversation updates an existing activity in a conversation.
func (a *Adapter) updateInConversation(ctx context.Context, rawConvID, activityID string, activity Activity) error {
	if a.botClient == nil {
		return fmt.Errorf("updateInConversation: botClient is nil (Connect not called)")
	}

	serviceURL := a.resolveServiceURL(ctx, rawConvID)
	if serviceURL == "" {
		return fmt.Errorf("updateInConversation: could not resolve serviceURL for conversation %q", rawConvID)
	}

	return a.botClient.UpdateActivity(ctx, serviceURL, rawConvID, activityID, activity)
}

// replyInConversation sends a reply to an existing activity.
func (a *Adapter) replyInConversation(ctx context.Context, rawConvID, replyToID string, activity Activity) (string, error) {
	if a.botClient == nil {
		return "", fmt.Errorf("replyInConversation: botClient is nil (Connect not called)")
	}

	serviceURL := a.resolveServiceURL(ctx, rawConvID)
	if serviceURL == "" {
		return "", fmt.Errorf("replyInConversation: could not resolve serviceURL for conversation %q", rawConvID)
	}

	rr, err := a.botClient.ReplyToActivity(ctx, serviceURL, rawConvID, replyToID, activity)
	if err != nil {
		return "", err
	}
	return rr.ID, nil
}

// resolveServiceURL finds the service URL for a conversation by checking cache then convRefStore.
func (a *Adapter) resolveServiceURL(ctx context.Context, rawConvID string) string {
	// Try direct lookup (rawConvID might be a convKey or raw ID).
	if v, ok := a.serviceURLs.Load(rawConvID); ok {
		if entry, ok := v.(serviceURLEntry); ok && entry.url != "" {
			return entry.url
		}
	}

	// Scan all cached entries for a matching rawConvID.
	var found string
	a.serviceURLs.Range(func(key, value any) bool {
		entry, ok := value.(serviceURLEntry)
		if ok && entry.rawConvID == rawConvID && entry.url != "" {
			found = entry.url
			return false
		}
		return true
	})
	if found != "" {
		return found
	}

	// Fallback to convRefStore.
	if a.convRefStore != nil {
		ref, err := a.convRefStore.Get(ctx, rawConvID)
		if err == nil && ref != nil && ref.ServiceURL != "" {
			a.serviceURLs.Store(rawConvID, serviceURLEntry{url: ref.ServiceURL, rawConvID: ref.RawConversationID})
			return ref.ServiceURL
		}
	}

	return ""
}

// agentIndexJanitor periodically evicts expired entries from agentThreads and agentRootIdx.
func (a *Adapter) agentIndexJanitor(ctx context.Context) {
	ticker := time.NewTicker(agentIndexJanitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.evictAgentIndexes(a.clock.Now())
		}
	}
}

func (a *Adapter) evictAgentIndexes(now time.Time) {
	cutoff := now.Add(-agentIndexTTL)

	a.agentThreads.Range(func(key any, value any) bool {
		st, ok := value.(agentThread)
		if !ok {
			a.agentThreads.Delete(key)
			return true
		}
		if st.LastSeen.IsZero() || st.LastSeen.Before(cutoff) {
			a.agentThreads.Delete(key)
		}
		return true
	})

	a.agentRootIdx.Range(func(key any, value any) bool {
		entry, ok := value.(agentRootIndexEntry)
		if !ok {
			a.agentRootIdx.Delete(key)
			return true
		}
		if entry.LastSeen.IsZero() || entry.LastSeen.Before(cutoff) {
			a.agentRootIdx.Delete(key)
		}
		return true
	})
}
