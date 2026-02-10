package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/jkatigb/agentctl/internal/chatadapter"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/web/sse"
)

type agentThread struct {
	RootMessageID int
	Role          string
	Prompt        string
	SessionID     string
	LastIterEdit  time.Time
	LastSeen      time.Time
}

func agentKey(event observability.ActivityEvent) string {
	// AgentID (WideEvent.AgentID) is the stable agent identifier (and is what the
	// web API expects for /api/agents/{id}/...).
	if v := strings.TrimSpace(event.AgentID); v != "" {
		return v
	}
	// Fallback: runtime session ID.
	return strings.TrimSpace(event.SessionID)
}

// startEventListener subscribes to the SSE hub and routes agent activity events to Telegram.
func (a *Adapter) startEventListener(ctx context.Context) {
	client := &sse.Client{
		ID:   "telegram-adapter",
		Send: make(chan []byte, 64),
	}

	a.mu.Lock()
	a.sseClient = client
	a.mu.Unlock()

	a.sseHub.Register(client)
	observability.Emit(ctx, observability.NewEvent("telegram.event_listener_started").WithComponent("telegram").Success(0))

	defer func() {
		a.sseHub.Unregister(client)
		observability.Emit(ctx, observability.NewEvent("telegram.event_listener_stopped").WithComponent("telegram").Success(0))
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
		return
	}
	if event.Type != "activity" {
		return
	}

	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return
	}

	var activity observability.ActivityEvent
	if err := json.Unmarshal(dataBytes, &activity); err != nil {
		return
	}

	a.routeActivityEvent(activity)
}

// routeActivityEvent dispatches an ActivityEvent to Telegram messages.
func (a *Adapter) routeActivityEvent(event observability.ActivityEvent) {
	if !strings.HasPrefix(event.Operation, "agent.") {
		return
	}

	key := agentKey(event)
	if key == "" {
		return
	}

	// Compact activity feed (major lifecycle + errors only; iteration is too spammy for Telegram).
	if a.cfg.ActivityChatID != 0 {
		if line := telegramActivityFeedLine(event); line != "" {
			if _, err := a.sendMessage(a.cfg.ActivityChatID, 0, line, nil); err != nil {
				observability.Emit(a.ctx,
					observability.NewEvent("telegram.activity_feed_failed").
						WithComponent("telegram").
						WithData("op", event.Operation).
						Error(err, 0))
			}
		}
	}

	// Per-agent "thread" via a root message + edits/replies in the agent chat.
	if a.cfg.AgentChatID == 0 {
		return
	}

	state := a.ensureAgentRoot(event)
	if state.RootMessageID == 0 {
		return
	}

	// Keep last-seen session ID for "details" / summary lookup.
	if sid := strings.TrimSpace(event.SessionID); sid != "" {
		state.SessionID = sid
	}
	state.LastSeen = time.Now()
	a.agentThreads.Store(key, state)

	switch event.Operation {
	case "agent.spawn":
		// Root message already created; ensure it contains the latest role/prompt.
		a.updateAgentRoot(event, state, "running", telegramKeyboardStopDetails(key))
	case "agent.iteration":
		// Throttle edits; iterations can be frequent.
		if time.Since(state.LastIterEdit) < 1500*time.Millisecond {
			return
		}
		state.LastIterEdit = time.Now()
		state.LastSeen = time.Now()
		a.agentThreads.Store(key, state)
		a.updateAgentRoot(event, state, "running", telegramKeyboardStopDetails(key))
	case "agent.complete":
		a.updateAgentRoot(event, state, "complete", telegramKeyboardDetails(key))
		a.postAgentSummary(a.ctx, key, state)
	case "agent.kill":
		a.updateAgentRoot(event, state, "killed", telegramKeyboardDetails(key))
	default:
		if event.Status == "error" {
			a.updateAgentRoot(event, state, "error", telegramKeyboardRetryDetails(key))
		}
	}
}

func (a *Adapter) ensureAgentRoot(event observability.ActivityEvent) agentThread {
	if a.cfg.AgentChatID == 0 {
		return agentThread{}
	}

	key := agentKey(event)
	if key == "" {
		return agentThread{}
	}

	if v, ok := a.agentThreads.Load(key); ok {
		if st, ok := v.(agentThread); ok && st.RootMessageID != 0 {
			return st
		}
	}

	role := chatadapter.GetDataString(event.Data, "role")
	prompt := chatadapter.GetDataString(event.Data, "prompt")
	sessionID := strings.TrimSpace(event.SessionID)

	content := telegramAgentRootText(event, "running")
	kb := telegramKeyboardStopDetails(key)

	msgID, err := a.sendMessage(a.cfg.AgentChatID, 0, content, kb)
	if err != nil {
		observability.Emit(a.ctx,
			observability.NewEvent("telegram.agent_root_create_failed").
				WithComponent("telegram").
				WithData("agent_id", key).
				Error(err, 0))
		return agentThread{}
	}

	st := agentThread{
		RootMessageID: msgID,
		Role:          role,
		Prompt:        prompt,
		SessionID:     sessionID,
		LastIterEdit:  time.Time{},
		LastSeen:      time.Now(),
	}
	a.agentThreads.Store(key, st)
	a.agentRootIdx.Store(agentRootKey(a.cfg.AgentChatID, msgID), agentRootIndexEntry{AgentID: key, LastSeen: time.Now()})

	observability.Emit(a.ctx,
		observability.NewEvent("telegram.agent_root_created").
			WithComponent("telegram").
			WithData("agent_id", key).
			WithData("message_id", msgID).
			Success(0))

	return st
}

func (a *Adapter) updateAgentRoot(event observability.ActivityEvent, state agentThread, status string, keyboard *tgbotapi.InlineKeyboardMarkup) {
	if state.RootMessageID == 0 || a.cfg.AgentChatID == 0 {
		return
	}

	content := telegramAgentRootText(event, status)
	if err := a.editMessage(a.cfg.AgentChatID, state.RootMessageID, content, keyboard); err != nil {
		observability.Emit(a.ctx,
			observability.NewEvent("telegram.agent_root_update_failed").
				WithComponent("telegram").
				WithData("session_id", event.SessionID).
				WithData("op", event.Operation).
				Error(err, 0))
	}
}

func telegramActivityFeedLine(event observability.ActivityEvent) string {
	idShort := chatadapter.TruncateRunes(agentKey(event), 8)
	switch event.Operation {
	case "agent.spawn":
		role := chatadapter.GetDataString(event.Data, "role")
		if role != "" {
			return fmt.Sprintf("spawn %s role=%s", idShort, role)
		}
		return fmt.Sprintf("spawn %s", idShort)
	case "agent.complete":
		return fmt.Sprintf("complete %s %s", idShort, chatadapter.FormatDuration(event.DurationMS))
	case "agent.kill":
		return fmt.Sprintf("killed %s", idShort)
	default:
		if event.Status == "error" {
			errMsg := strings.TrimSpace(event.ErrorMessage)
			if errMsg == "" {
				errMsg = chatadapter.GetDataString(event.Data, "error")
			}
			errMsg = chatadapter.TruncateRunes(errMsg, 120)
			if errMsg != "" {
				return fmt.Sprintf("error %s %s", idShort, errMsg)
			}
			return fmt.Sprintf("error %s", idShort)
		}
		return ""
	}
}

func telegramAgentRootText(event observability.ActivityEvent, status string) string {
	agentID := strings.TrimSpace(event.AgentID)
	sessionID := strings.TrimSpace(event.SessionID)

	displayID := agentID
	if displayID == "" {
		displayID = sessionID
	}
	displayShort := chatadapter.TruncateRunes(displayID, 8)

	role := chatadapter.GetDataString(event.Data, "role")
	prompt := chatadapter.GetDataString(event.Data, "prompt")
	iteration := chatadapter.GetDataString(event.Data, "iteration")
	toolName := chatadapter.GetDataString(event.Data, "tool_name")
	errMsg := strings.TrimSpace(event.ErrorMessage)
	if errMsg == "" {
		errMsg = chatadapter.GetDataString(event.Data, "error")
	}

	var b strings.Builder
	b.WriteString("Agent ")
	b.WriteString(displayShort)
	if role != "" {
		b.WriteString(" (")
		b.WriteString(role)
		b.WriteString(")")
	}
	b.WriteString("\n")
	b.WriteString("status: ")
	b.WriteString(status)
	b.WriteString("\n")

	if status == "running" && iteration != "" {
		b.WriteString("iteration: ")
		b.WriteString(iteration)
		if toolName != "" {
			b.WriteString(" tool=")
			b.WriteString(toolName)
		}
		b.WriteString("\n")
	}

	if status == "error" && errMsg != "" {
		b.WriteString("error: ")
		b.WriteString(chatadapter.TruncateRunes(errMsg, 400))
		b.WriteString("\n")
	}

	// Only include prompt on spawn (or when still empty); it can be large/noisy.
	if event.Operation == "agent.spawn" && prompt != "" {
		b.WriteString("\n")
		b.WriteString("prompt: ")
		b.WriteString(chatadapter.TruncateRunes(prompt, 600))
		b.WriteString("\n")
	}

	if agentID != "" {
		b.WriteString("\n")
		b.WriteString("agent_id: ")
		b.WriteString(agentID)
		b.WriteString("\n")
	}
	if sessionID != "" {
		b.WriteString("\n")
		b.WriteString("session_id: ")
		b.WriteString(sessionID)
		b.WriteString("\n")
	}

	return truncateForTelegram(strings.TrimSpace(b.String()))
}

func telegramKeyboardStopDetails(agentID string) *tgbotapi.InlineKeyboardMarkup {
	row := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Stop", "stop:"+agentID),
		tgbotapi.NewInlineKeyboardButtonData("Details", "details:"+agentID),
	)
	kb := tgbotapi.NewInlineKeyboardMarkup(row)
	return &kb
}

func telegramKeyboardRetryDetails(agentID string) *tgbotapi.InlineKeyboardMarkup {
	row := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Retry", "retry:"+agentID),
		tgbotapi.NewInlineKeyboardButtonData("Details", "details:"+agentID),
	)
	kb := tgbotapi.NewInlineKeyboardMarkup(row)
	return &kb
}

func telegramKeyboardDetails(agentID string) *tgbotapi.InlineKeyboardMarkup {
	row := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Details", "details:"+agentID),
	)
	kb := tgbotapi.NewInlineKeyboardMarkup(row)
	return &kb
}
