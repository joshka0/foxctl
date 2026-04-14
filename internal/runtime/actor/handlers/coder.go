package handlers

import (
	"context"
	"fmt"

	"github.com/joshka0/foxctl/internal/runtime/actor"
)

// CoderHandler handles messages for coder role actors.
//
// Coders have full access to code manipulation tools:
// - File read/write operations
// - Code execution
// - Git operations
// - Testing and debugging
type CoderHandler struct {
	baseHandler
}

// NewCoderHandler creates a new coder handler.
func NewCoderHandler() *CoderHandler {
	return &CoderHandler{
		baseHandler: baseHandler{role: "coder"},
	}
}

// HandleAsk processes ask messages for coder role.
func (h *CoderHandler) HandleAsk(ctx context.Context, msg *actor.Message, agent AgentExecutor, mem *MemoryContext) (*actor.Message, error) {
	askData, err := parseAskData(msg.Body)
	if err != nil {
		return nil, fmt.Errorf("parse ask: %w", err)
	}

	// Build prompt based on ask kind
	var prompt string
	switch askData.Kind {
	case "context":
		prompt = fmt.Sprintf("Please provide context for: %s", askData.Question)
	case "code":
		prompt = fmt.Sprintf("Please write code for: %s", askData.Question)
	case "debug":
		prompt = fmt.Sprintf("Please help debug: %s", askData.Question)
	case "refactor":
		prompt = fmt.Sprintf("Please refactor: %s", askData.Question)
	default:
		prompt = askData.Question
	}

	// Add any context from the ask
	if askData.Context != nil {
		if additionalCtx, ok := askData.Context["additional"].(string); ok {
			prompt = fmt.Sprintf("%s\n\nAdditional context: %s", prompt, additionalCtx)
		}
	}

	// Run agent turn
	result, err := runAgentTurn(ctx, agent, prompt, mem)
	if err != nil {
		return nil, fmt.Errorf("run agent turn: %w", err)
	}

	// Build reply
	answer := map[string]any{
		"response": result,
		"role":     h.role,
	}

	return buildReplyMessage(askData.AskID, answer)
}

// HandleCmd processes command messages for coder role.
func (h *CoderHandler) HandleCmd(ctx context.Context, msg *actor.Message, agent AgentExecutor, mem *MemoryContext) (*actor.Message, error) {
	cmdData, err := parseCmdData(msg.Body)
	if err != nil {
		return nil, fmt.Errorf("parse cmd: %w", err)
	}

	switch cmdData.Action {
	case "run_turn":
		// Execute a coding turn
		prompt, _ := cmdData.Args["prompt"].(string)
		if prompt == "" {
			return nil, fmt.Errorf("run_turn requires prompt arg")
		}

		result, err := runAgentTurn(ctx, agent, prompt, mem)
		if err != nil {
			return nil, fmt.Errorf("run turn: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"result": result,
			"action": cmdData.Action,
		})

	case "run_skill":
		// Execute a specific skill
		skill := cmdData.Skill
		if skill == "" {
			return nil, fmt.Errorf("run_skill requires skill name")
		}

		prompt := fmt.Sprintf("Execute skill '%s' with args: %v", skill, cmdData.Args)
		result, err := runAgentTurn(ctx, agent, prompt, mem)
		if err != nil {
			return nil, fmt.Errorf("run skill: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"result": result,
			"skill":  skill,
		})

	case "write_code":
		// Generate code for a specific task
		task, _ := cmdData.Args["task"].(string)
		filepath, _ := cmdData.Args["filepath"].(string)

		prompt := fmt.Sprintf("Write code for: %s", task)
		if filepath != "" {
			prompt = fmt.Sprintf("%s\nTarget file: %s", prompt, filepath)
		}

		result, err := runAgentTurn(ctx, agent, prompt, mem)
		if err != nil {
			return nil, fmt.Errorf("write code: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"result":   result,
			"action":   cmdData.Action,
			"filepath": filepath,
		})

	case "do_work":
		// General work execution
		task, _ := cmdData.Args["task"].(string)
		if task == "" {
			return nil, fmt.Errorf("do_work requires task arg")
		}

		result, err := runAgentTurn(ctx, agent, task, mem)
		if err != nil {
			return nil, fmt.Errorf("do work: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"result": result,
			"action": cmdData.Action,
		})

	default:
		return nil, fmt.Errorf("unknown action: %s", cmdData.Action)
	}
}

// HandleEvent processes event messages for coder role.
func (h *CoderHandler) HandleEvent(ctx context.Context, msg *actor.Message, _ AgentExecutor, mem *MemoryContext) error {
	eventData, err := parseEventData(msg.Body)
	if err != nil {
		return fmt.Errorf("parse event: %w", err)
	}

	// Handle different event kinds
	switch eventData.Kind {
	case "file_changed":
		// Could trigger re-analysis of changed files
		// For now, just log to memory
		if mem != nil {
			mem.AppendTurn(ctx, "system", "File change event received: File changes detected in workspace")
		}

	case "test_failed":
		// Could trigger debugging
		if mem != nil {
			mem.AppendTurn(ctx, "system", "Test failure event received: Some tests have failed - may need investigation")
		}

	case "heartbeat":
		// Just acknowledge
		return nil
	}

	return nil
}
