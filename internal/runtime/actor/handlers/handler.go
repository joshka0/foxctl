package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jkatigb/agentctl/internal/runtime/actor"
	"github.com/jkatigb/agentctl/internal/runtime/actor/memory"
	agentdomain "github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
)

// AgentExecutor abstracts agent execution for handlers.
type AgentExecutor interface {
	Execute(ctx context.Context, input map[string]any) (map[string]any, error)
}

// Handler defines the interface for role-specific message handlers.
type Handler interface {
	// Role returns the role this handler is designed for.
	Role() string

	// HandleAsk processes an ask message and returns a reply.
	HandleAsk(ctx context.Context, msg *actor.Message, agent AgentExecutor, mem *MemoryContext) (*actor.Message, error)

	// HandleCmd processes a command message.
	HandleCmd(ctx context.Context, msg *actor.Message, agent AgentExecutor, mem *MemoryContext) (*actor.Message, error)

	// HandleEvent processes an event message.
	HandleEvent(ctx context.Context, msg *actor.Message, agent AgentExecutor, mem *MemoryContext) error
}

// Registry holds registered handlers by role.
type Registry struct {
	handlers map[string]Handler
}

// NewRegistry creates a new handler registry with default handlers.
func NewRegistry() *Registry {
	r := &Registry{
		handlers: make(map[string]Handler),
	}

	// Register default handlers
	r.Register(NewCoderHandler())
	r.Register(NewPlannerHandler())
	r.Register(NewReviewerHandler())

	return r
}

// Register adds a handler to the registry.
func (r *Registry) Register(h Handler) {
	r.handlers[h.Role()] = h
}

// Get returns the handler for a role, or nil if not found.
func (r *Registry) Get(role string) Handler {
	return r.handlers[role]
}

// baseHandler provides common functionality for all handlers.
type baseHandler struct {
	role string
}

func (h *baseHandler) Role() string {
	return h.role
}

// parseAskData extracts AskData from a message body.
func parseAskData(body []byte) (agentdomain.AskData, error) {
	var env struct {
		Data agentdomain.AskData `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return agentdomain.AskData{}, fmt.Errorf("parse ask data: %w", err)
	}
	return env.Data, nil
}

// parseCmdData extracts CmdData from a message body.
func parseCmdData(body []byte) (agentdomain.CmdData, error) {
	var env struct {
		Data agentdomain.CmdData `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return agentdomain.CmdData{}, fmt.Errorf("parse cmd data: %w", err)
	}
	return env.Data, nil
}

// parseEventData extracts EventData from a message body.
func parseEventData(body []byte) (agentdomain.EventData, error) {
	var env struct {
		Data agentdomain.EventData `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return agentdomain.EventData{}, fmt.Errorf("parse event data: %w", err)
	}
	return env.Data, nil
}

// buildReplyMessage creates a reply message from an ask.
func buildReplyMessage(askID string, answer map[string]any) (*actor.Message, error) {
	replyData := agentdomain.ReplyData{
		AskID:  askID,
		Answer: answer,
	}

	replyEnv := envelope.OK("agent.reply", replyData)
	replyPayload, err := json.Marshal(replyEnv)
	if err != nil {
		return nil, fmt.Errorf("marshal reply envelope: %w", err)
	}

	return &actor.Message{
		Subject: "agent.reply",
		Body:    replyPayload,
	}, nil
}

// MemoryContext provides simplified memory access for handlers.
// This wraps ShortTermMemory with a pre-bound actorID.
type MemoryContext struct {
	mem     *memory.ShortTermMemory
	actorID string
}

// NewMemoryContext creates a handler memory context.
func NewMemoryContext(mem *memory.ShortTermMemory, actorID string) *MemoryContext {
	if mem == nil {
		return nil
	}
	return &MemoryContext{mem: mem, actorID: actorID}
}

// GetContext returns formatted context for the LLM.
func (m *MemoryContext) GetContext(ctx context.Context) string {
	if m == nil || m.mem == nil {
		return ""
	}
	context, _ := m.mem.GetContext(ctx, m.actorID)
	return context
}

// AppendTurn records a conversation turn.
func (m *MemoryContext) AppendTurn(ctx context.Context, role, content string) {
	if m == nil || m.mem == nil {
		return
	}
	_ = m.mem.AppendTurn(ctx, m.actorID, memory.Turn{
		Role:    role,
		Content: content,
	})
}

// runAgentTurn executes a single agent turn with the given prompt.
func runAgentTurn(ctx context.Context, agent AgentExecutor, prompt string, mem *MemoryContext) (string, error) {
	// Add memory context if available
	fullPrompt := prompt
	if mem != nil {
		memContext := mem.GetContext(ctx)
		if memContext != "" {
			fullPrompt = fmt.Sprintf("Context from previous turns:\n%s\n\nCurrent request:\n%s", memContext, prompt)
		}
	}

	// Execute the agent
	result, err := agent.Execute(ctx, map[string]any{
		"question": fullPrompt,
	})
	if err != nil {
		return "", fmt.Errorf("agent execute: %w", err)
	}

	// Extract result string
	resultStr := extractAgentResult(result)

	// Update memory with this turn
	if mem != nil {
		mem.AppendTurn(ctx, "user", prompt)
		mem.AppendTurn(ctx, "assistant", resultStr)
	}

	return resultStr, nil
}

// extractAgentResult extracts a string result from agent output.
func extractAgentResult(result map[string]any) string {
	// Try common result fields
	for _, key := range []string{"result", "answer", "output", "response", "thought"} {
		if val, ok := result[key]; ok {
			if s, ok := val.(string); ok && s != "" {
				return s
			}
		}
	}

	// Fallback to JSON representation
	if len(result) > 0 {
		b, _ := json.Marshal(result)
		return string(b)
	}

	return "Task completed"
}
