package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	tooling "github.com/jkatigb/agentctl/internal/tooling"
	"github.com/oklog/ulid/v2"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errspkg "github.com/jkatigb/agentctl/internal/platform/errors"
)

// registerAgentTools registers agent lifecycle tools.
func (r *Registry) registerAgentTools() error {
	cfg := SpawnToolConfig{
		CallerActorID:       r.config.ActorID,
		CallerDepth:         r.config.Depth,
		CallerMaxDepth:      r.config.MaxDepth,
		CallerLocalMaxDepth: r.config.LocalMaxDepth,
		EpicID:              r.config.EpicID,
		MailSender:          r.spawnMailSender,
	}
	if err := r.RegisterSpawnTool(cfg); err != nil {
		return fmt.Errorf("register agent.spawn: %w", err)
	}

	// Register agent.ask tool for inter-agent communication
	if err := r.registerAgentAskTool(); err != nil {
		return fmt.Errorf("register agent.ask: %w", err)
	}

	return nil
}

// registerAgentAskTool registers the agent.ask tool for inter-agent messaging.
func (r *Registry) registerAgentAskTool() error {
	askTool := tooling.NewFuncTool(
		"agent.ask",
		"Send a question to another agent and optionally wait for their response. "+
			"Use this for inter-agent collaboration when you need information or assistance from a specialist agent.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"target_agent": {
					Type:        "string",
					Description: "Target agent by slug, name, or ID (e.g., 'researcher', 'Luna', or '01KFC87QQRAYJGBH8X11J499W8')",
					Required:    true,
				},
				"question": {
					Type:        "string",
					Description: "The question or request to send to the target agent",
					Required:    true,
				},
				"kind": {
					Type:        "string",
					Description: "Question kind: context, secret, approval, toolhint, or other (default: other)",
				},
				"context": {
					Type:        "object",
					Description: "Additional context to pass to the target agent",
				},
				"wait_for_reply": {
					Type:        "boolean",
					Description: "If true, wait for the agent's response (default: true, max 60s timeout)",
				},
				"conversation_id": {
					Type:        "string",
					Description: "Conversation ID for memory continuity (optional)",
				},
			},
		},
		r.wrapWithTelemetry("agent.ask", r.agentAsk),
	)
	return r.tools.Register(askTool)
}

const (
	defaultAgentAskTimeout = 60 * time.Second
	agentAskPollInterval   = 500 * time.Millisecond
)

// agentAsk implements the agent.ask tool.
func (r *Registry) agentAsk(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openMailboxStore == nil {
		return errorResult("mailbox store not configured"), nil
	}

	targetAgent, ok := args["target_agent"].(string)
	if !ok || targetAgent == "" {
		return errorResult("target_agent is required"), nil
	}

	question, ok := args["question"].(string)
	if !ok || question == "" {
		return errorResult("question is required"), nil
	}

	kind := "other"
	if k, ok := args["kind"].(string); ok && k != "" {
		kind = k
	}

	contextData := make(map[string]any)
	if c, ok := args["context"].(map[string]any); ok {
		contextData = c
	}

	waitForReply := true
	if w, ok := args["wait_for_reply"].(bool); ok {
		waitForReply = w
	}

	conversationID := ""
	if c, ok := args["conversation_id"].(string); ok {
		conversationID = c
	}

	// Resolve target agent by slug, name, or ID
	targetNS := targetAgent
	resolvedName := ""
	if r.openAgentsStore != nil {
		agentStore, err := r.openAgentsStore(ctx)
		if err == nil {
			defer func() { errspkg.Ignore(agentStore.Close(), "close agents store") }()
			resolved, err := agentStore.Resolve(ctx, targetAgent)
			if err == nil {
				targetNS = resolved.ID // Use the resolved agent's ID
				resolvedName = resolved.Name
				if resolved.Slug != "" {
					resolvedName = resolved.Slug
				}
			}
			// If resolution fails, fall back to using targetAgent as-is
		}
	}

	store, err := r.openMailboxStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open mailbox store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close mailbox store") }()

	// Build ask data
	askID := ulid.Make().String()
	askData := agent.AskData{
		AskID:          askID,
		Kind:           kind,
		Question:       question,
		Context:        contextData,
		ConversationID: conversationID,
	}

	env := envelope.OK("agent.ask", askData)
	payload, err := json.Marshal(env)
	if err != nil {
		return errorResult(fmt.Sprintf("marshal ask payload: %v", err)), nil
	}

	// Build and send message
	msg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    r.config.ActorID,
		ToNS:      targetNS,
		Type:      agent.MessageTypeAsk,
		TTLMS:     int64(defaultAgentAskTimeout.Milliseconds()),
		Headers:   map[string]string{"ask_id": askID},
		Payload:   payload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
		SessionID: r.config.SessionID,
		Workspace: r.config.WorkspaceID,
		AgentID:   r.config.ActorID,
	}

	if err := store.Send(ctx, msg); err != nil {
		return errorResult(fmt.Sprintf("send ask message: %v", err)), nil
	}

	if !waitForReply {
		result := map[string]any{
			"status":       "sent",
			"ask_id":       askID,
			"target_agent": targetNS,
			"message_id":   msg.ID,
			"note":         "Ask sent. Reply will arrive in your mailbox.",
		}
		if resolvedName != "" {
			result["resolved_as"] = resolvedName
		}
		return successResult(result), nil
	}

	// Wait for reply
	reply, err := r.waitForAgentReply(ctx, store, askID)
	if err != nil {
		result := map[string]any{
			"status":       "timeout",
			"ask_id":       askID,
			"target_agent": targetNS,
			"error":        err.Error(),
		}
		if resolvedName != "" {
			result["resolved_as"] = resolvedName
		}
		return successResult(result), nil
	}

	result := map[string]any{
		"status":       "replied",
		"ask_id":       askID,
		"target_agent": targetNS,
		"reply":        reply,
	}
	if resolvedName != "" {
		result["resolved_as"] = resolvedName
	}
	return successResult(result), nil
}

// waitForAgentReply polls the mailbox for a reply to the given ask ID.
func (r *Registry) waitForAgentReply(ctx context.Context, store interface {
	List(ctx context.Context, agentNS string, limit int) ([]agent.Message, error)
	Ack(ctx context.Context, messageID string) error
}, askID string,
) (map[string]any, error) {
	deadline := time.Now().Add(defaultAgentAskTimeout)
	ticker := time.NewTicker(agentAskPollInterval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for reply (ask_id: %s)", askID)
		}

		messages, err := store.List(ctx, r.config.ActorID, 50)
		if err != nil {
			return nil, fmt.Errorf("list mailbox: %w", err)
		}

		for _, msg := range messages {
			if msg.Type != agent.MessageTypeReply {
				continue
			}
			// Check both ask_id and correlation headers for compatibility
			headerAskID := msg.Headers["ask_id"]
			if headerAskID == "" {
				headerAskID = msg.Headers["correlation"]
			}
			if headerAskID != askID {
				continue
			}

			// Found matching reply - ack and parse
			if err := store.Ack(ctx, msg.ID); err != nil {
				return nil, fmt.Errorf("ack reply: %w", err)
			}

			var env envelope.Envelope
			if err := json.Unmarshal(msg.Payload, &env); err != nil {
				return nil, fmt.Errorf("decode reply: %w", err)
			}

			// Extract answer from reply data
			if replyData, ok := env.Data.(map[string]any); ok {
				if answer, ok := replyData["answer"].(map[string]any); ok {
					return answer, nil
				}
				return replyData, nil
			}

			return map[string]any{"raw": env.Data}, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
