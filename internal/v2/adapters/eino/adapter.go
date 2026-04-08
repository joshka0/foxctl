package eino

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/jkatigb/agentctl/internal/engine"
)

// EinoEngineAdapter bridges Eino's adk.Agent to agentctl's engine.AgentEngine.
//
// This is a spike adapter — it is only instantiated when AGENTCTL_ENGINE_BACKEND=eino
// is set. The default LLMChatEngine path is unaffected when the gate is off.
//
// Scope: the adapter converts engine.EngineInput messages to adk.AgentInput,
// drains the AsyncIterator of AgentEvents, and collects the final assistant text
// into engine.EngineOutput. Tool-call bridging is out of scope for the spike.
type EinoEngineAdapter struct {
	agent adk.Agent
}

// NewEinoEngineAdapter wraps an adk.Agent behind the engine.AgentEngine interface.
func NewEinoEngineAdapter(agent adk.Agent) (*EinoEngineAdapter, error) {
	if agent == nil {
		return nil, fmt.Errorf("eino engine adapter requires a non-nil adk.Agent")
	}
	return &EinoEngineAdapter{agent: agent}, nil
}

// Run implements engine.AgentEngine by delegating to the wrapped adk.Agent.
func (a *EinoEngineAdapter) Run(ctx context.Context, input engine.EngineInput) (engine.EngineOutput, error) {
	msgs, err := convertMessages(input)
	if err != nil {
		return engine.EngineOutput{}, fmt.Errorf("eino adapter: convert messages: %w", err)
	}

	iter := a.agent.Run(ctx, &adk.AgentInput{
		Messages:        msgs,
		EnableStreaming: false,
	})

	return drainIterator(iter)
}

// convertMessages maps engine.Message slice to Eino schema.Message slice.
func convertMessages(input engine.EngineInput) ([]*schema.Message, error) {
	out := make([]*schema.Message, 0, len(input.Messages)+1)

	if strings.TrimSpace(input.SystemPrompt) != "" {
		out = append(out, &schema.Message{
			Role:    schema.System,
			Content: input.SystemPrompt,
		})
	}

	for i, m := range input.Messages {
		role, err := toEinoRole(m.Role)
		if err != nil {
			return nil, fmt.Errorf("message[%d]: %w", i, err)
		}
		out = append(out, &schema.Message{
			Role:    role,
			Content: m.Content,
		})
	}
	return out, nil
}

func toEinoRole(r string) (schema.RoleType, error) {
	switch r {
	case engine.RoleUser:
		return schema.User, nil
	case engine.RoleAssistant:
		return schema.Assistant, nil
	case engine.RoleSystem:
		return schema.System, nil
	case engine.RoleTool:
		return schema.Tool, nil
	default:
		return "", fmt.Errorf("unknown engine role %q", r)
	}
}

// drainIterator consumes all AgentEvents from the iterator and assembles
// the final EngineOutput. Tool-call bridging is not implemented in the spike;
// only the final assistant text is collected.
func drainIterator(iter *adk.AsyncIterator[*adk.AgentEvent]) (engine.EngineOutput, error) {
	if iter == nil {
		return engine.EngineOutput{}, fmt.Errorf("eino adapter: nil event iterator")
	}

	var assistantParts []string
	var firstErr error

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			if firstErr == nil {
				firstErr = event.Err
			}
			continue
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		msg, err := event.Output.MessageOutput.GetMessage()
		if err != nil || msg == nil {
			continue
		}
		if msg.Role == schema.Assistant && strings.TrimSpace(msg.Content) != "" {
			assistantParts = append(assistantParts, msg.Content)
		}
	}

	if firstErr != nil {
		return engine.EngineOutput{}, fmt.Errorf("eino adapter: agent event error: %w", firstErr)
	}

	return engine.EngineOutput{
		AssistantText: strings.Join(assistantParts, ""),
		StopReason:    engine.StopReasonEndTurn,
	}, nil
}
