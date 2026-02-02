package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/agents"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/companion"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
)

// handleAsk processes agent.ask messages and sends agent.reply results.
//
// Index:
// - Purpose: Execute an ask request via companion service or DSPy and reply
// - Flow: parse payload → resolve conversation → execute engine → build reply → send mailbox response
// - SideEffects: LLM calls; memory reads/writes; mailbox send
// - FailureModes: payload decode errors, execution errors, reply marshal/send errors
// - Related: handleCmd, handleConsoleAsk
// - Keywords: agent.ask, agent.reply, mailbox, companion, dspy
func handleAsk(ctx context.Context, logger zerolog.Logger, msg agent.Message, dspyAgent agents.Agent, companionSvc *companion.Service, mailboxStore mailbox.Store, policy agent.Policy, optCtx *OptimizationContext, companionMemory *companion.ConversationMemory, agentID string) error {
	// 1. Parse payload envelope
	var env struct {
		Data agent.AskData `json:"data"`
	}
	if err := json.Unmarshal(msg.Payload, &env); err != nil {
		return fmt.Errorf("unmarshal ask payload: %w", err)
	}
	askData := env.Data

	// 2. Derive conversation ID - use explicit ID if provided, else use agentID
	// This ensures memory continuity across all callers (CLI, API, etc.)
	conversationID := askData.ConversationID
	if conversationID == "" {
		conversationID = agentID
	}

	var result string
	var durationMS int64
	var err error

	timeout := 10 * time.Minute // default
	if policy.Timeout != "" {
		if d, err := time.ParseDuration(policy.Timeout); err == nil {
			timeout = d
		}
	}

	// Use companion service (LLMChatEngine) if available, else fall back to DSPy
	if companionSvc != nil {
		// Use LLMChatEngine via companion.Service
		logger.Debug().Str("conversation_id", conversationID).Msg("using companion service for ask")

		turnCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		startTime := time.Now()
		resp, chatErr := companionSvc.Chat(turnCtx, companion.ChatRequest{
			ConversationID: conversationID,
			Message:        askData.Question,
			Context:        askData.Context,
		})
		durationMS = time.Since(startTime).Milliseconds()

		if chatErr != nil {
			return fmt.Errorf("companion chat failed: %w", chatErr)
		}
		result = resp.Response

		logger.Debug().
			Int("context_queries", resp.ContextQueries).
			Int64("duration_ms", durationMS).
			Msg("companion chat completed")
	} else if dspyAgent != nil {
		// Fall back to DSPy ReAct
		// 3. Inject memory context if available
		var memoryContext string
		if companionMemory != nil {
			memCtx, err := companionMemory.GetContext(ctx, conversationID)
			if err != nil {
				logger.Warn().Err(err).Msg("failed to get memory context")
			} else if memCtx != "" {
				memoryContext = memCtx
				logger.Debug().Int("context_len", len(memCtx)).Msg("injected memory context")
			}

			// Store user turn BEFORE processing (valid context regardless of outcome)
			if err := companionMemory.AppendTurn(ctx, companion.ConversationTurn{
				ConversationID: conversationID,
				Role:           "user",
				Content:        askData.Question,
			}); err != nil {
				logger.Warn().Err(err).Msg("failed to store user turn")
			}
		}

		// 4. Build prompt from ask with memory context
		prompt := fmt.Sprintf("Question: %s\nContext: %v", askData.Question, askData.Context)
		if memoryContext != "" {
			prompt = fmt.Sprintf("## Conversation Memory\n%s\n\n---\n\n%s", memoryContext, prompt)
		}

		// 4a. Inject tool hints from optimization (if enabled)
		var hintsPrompt string
		if optCtx != nil && optCtx.Enabled && optCtx.Collector != nil {
			hints, err := optCtx.Collector.GetHints(ctx, optCtx.AgentRole, askData.Question)
			if err == nil && len(hints) > 0 {
				hintsPrompt = optCtx.Collector.FormatHintsForPrompt(hints)
				logger.Debug().Int("hint_count", len(hints)).Msg("injected tool hints from patterns")
			}
		}

		// 5. Execute DSPy turn
		taskPrompt := prompt
		if hintsPrompt != "" {
			taskPrompt = prompt + "\n" + hintsPrompt
		}
		input := map[string]any{
			"task": taskPrompt,
		}

		// Apply turn timeout from policy
		turnCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		startTime := time.Now()
		resultMap, execErr := dspyAgent.Execute(turnCtx, input)
		durationMS = time.Since(startTime).Milliseconds()

		// Record pattern for optimization (success or failure)
		if optCtx != nil && optCtx.Enabled && optCtx.Collector != nil {
			success := execErr == nil
			if recordErr := optCtx.Collector.RecordToolCall(ctx, optCtx.AgentRole, askData.Question, "dspy_ask", success, durationMS); recordErr != nil {
				logger.Warn().Err(recordErr).Msg("failed to record optimization pattern")
			}
		}

		if execErr != nil {
			return fmt.Errorf("dspy execution failed: %w", execErr)
		}

		// Extract result from ReAct output
		result = extractResult(resultMap)

		// Store assistant turn on success (user turn was already stored)
		if companionMemory != nil && result != "" {
			if err := companionMemory.AppendTurn(ctx, companion.ConversationTurn{
				ConversationID: conversationID,
				Role:           "assistant",
				Content:        result,
			}); err != nil {
				logger.Warn().Err(err).Msg("failed to store assistant turn")
			}
		}
	} else {
		return fmt.Errorf("no agent engine available (neither companion service nor dspy agent)")
	}

	// 6. Build reply payload
	replyData := agent.ReplyData{
		AskID:  askData.AskID,
		Answer: map[string]any{"response": result},
	}
	replyEnv := envelope.OK("agent.reply", replyData)
	replyPayload, err := json.Marshal(replyEnv)
	if err != nil {
		return fmt.Errorf("marshal reply envelope: %w", err)
	}

	// 7. Send reply with correlation
	replyMsg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    msg.ToNS,   // we are the sender now
		ToNS:      msg.FromNS, // reply to asker
		Type:      agent.MessageTypeReply,
		TTLMS:     300000, // 5 min
		Headers:   map[string]string{"correlation": askData.AskID, "ask_id": askData.AskID},
		Payload:   replyPayload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
	}
	return mailboxStore.Send(ctx, replyMsg)
}

// handleCmd processes agent.cmd messages and executes requested actions.
//
// Index:
// - Purpose: Execute command requests via companion service or DSPy
// - Flow: parse payload → build prompt → inject hints → execute engine → record patterns
// - SideEffects: LLM calls; optimization pattern recording
// - FailureModes: payload decode errors, unknown actions, execution errors
// - Related: handleAsk, handleConsoleCmd
// - Keywords: agent.cmd, run_turn, do_work, companion, dspy
func handleCmd(ctx context.Context, logger zerolog.Logger, msg agent.Message, dspyAgent agents.Agent, companionSvc *companion.Service, policy agent.Policy, optCtx *OptimizationContext, agentID string) error {
	var env struct {
		Data agent.CmdData `json:"data"`
	}
	if err := json.Unmarshal(msg.Payload, &env); err != nil {
		return fmt.Errorf("unmarshal cmd payload: %w", err)
	}
	cmdData := env.Data

	timeout := 10 * time.Minute // default
	if policy.Timeout != "" {
		if d, err := time.ParseDuration(policy.Timeout); err == nil {
			timeout = d
		}
	}

	conversationID := agentID
	if conversationID == "" {
		conversationID = msg.ToNS
	}

	switch cmdData.Action {
	case "run_skill":
		// Invoke skill via runner (future)
		return fmt.Errorf("run_skill not yet implemented")
	case "run_turn", "do_work":
		prompt := fmt.Sprintf("Command: %s\nArgs: %v", cmdData.Action, cmdData.Args)
		if cmdData.Skill != "" {
			prompt = fmt.Sprintf("Command: %s\nSkill: %s\nArgs: %v", cmdData.Action, cmdData.Skill, cmdData.Args)
		}

		var hintsPrompt string
		if optCtx != nil && optCtx.Enabled && optCtx.Collector != nil {
			hints, err := optCtx.Collector.GetHints(ctx, optCtx.AgentRole, prompt)
			if err == nil && len(hints) > 0 {
				hintsPrompt = optCtx.Collector.FormatHintsForPrompt(hints)
				logger.Debug().Int("hint_count", len(hints)).Msg("injected tool hints from patterns")
			}
		}

		taskPrompt := prompt
		if hintsPrompt != "" {
			taskPrompt = prompt + "\n" + hintsPrompt
		}

		if companionSvc != nil {
			turnCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			if _, chatErr := companionSvc.Chat(turnCtx, companion.ChatRequest{
				ConversationID: conversationID,
				Message:        taskPrompt,
			}); chatErr != nil {
				return fmt.Errorf("companion chat failed: %w", chatErr)
			}
			return nil
		}
		if dspyAgent == nil {
			return fmt.Errorf("no agent engine available")
		}

		input := map[string]any{
			"task": taskPrompt,
		}

		turnCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		startTime := time.Now()
		_, err := dspyAgent.Execute(turnCtx, input)
		durationMS := time.Since(startTime).Milliseconds()

		if optCtx != nil && optCtx.Enabled && optCtx.Collector != nil {
			success := err == nil
			if recordErr := optCtx.Collector.RecordToolCall(ctx, optCtx.AgentRole, prompt, "dspy_cmd", success, durationMS); recordErr != nil {
				logger.Warn().Err(recordErr).Msg("failed to record optimization pattern")
			}
		}

		return err
	default:
		return fmt.Errorf("unknown action: %s", cmdData.Action)
	}
}

func handleEvent(_ context.Context, logger zerolog.Logger, msg agent.Message) error {
	var env struct {
		Data agent.EventData `json:"data"`
	}
	if err := json.Unmarshal(msg.Payload, &env); err != nil {
		return fmt.Errorf("unmarshal event payload: %w", err)
	}
	eventData := env.Data

	// MVP: just log it
	logger.Info().
		Str("event_id", eventData.EventID).
		Str("kind", eventData.Kind).
		Int("job_count", eventData.JobCount).
		Msg("received agent event")

	// Future: propagate to overseer, update metrics, etc.
	return nil
}

func backoffDuration(attempt int) time.Duration {
	base := 5 * time.Second
	// cap attempt at 5 to avoid overflow and excessive wait (max 160s)
	if attempt > 5 {
		attempt = 5
	}
	return base * time.Duration(1<<attempt)
}

// handleConsoleAsk handles console.ask messages from interactive console sessions.
// It executes the prompt via companion service or DSPy and sends console.reply with the response.
//
// Index:
// - Purpose: Execute console ask prompts with cancellation support
// - Flow: parse payload → setup cancel context → execute engine → build reply → send mailbox response
// - SideEffects: LLM calls; memory reads/writes; mailbox send; cancel registry updates
// - FailureModes: payload decode errors, execution errors, reply marshal/send errors
// - Related: handleConsoleCmd, handleAsk
// - Keywords: console.ask, console.reply, cancellation, companion, dspy
func handleConsoleAsk(ctx context.Context, logger zerolog.Logger, msg agent.Message, dspyAgent agents.Agent, companionSvc *companion.Service, mailboxStore mailbox.Store, policy agent.Policy, optCtx *OptimizationContext, cancelCtx *CancelContext, companionMemory *companion.ConversationMemory, agentID string) error {
	// 1. Parse payload envelope
	var env struct {
		Data agent.ConsoleAskData `json:"data"`
	}
	if err := json.Unmarshal(msg.Payload, &env); err != nil {
		return fmt.Errorf("unmarshal console ask payload: %w", err)
	}
	askData := env.Data

	logger = logger.With().
		Str("ask_id", askData.AskID).
		Str("console_id", askData.ConsoleID).
		Logger()

	logger.Info().Msg("handling console.ask")

	// 2. Derive conversation ID - use agentID for memory continuity
	// Console sessions share memory with the agent by default
	conversationID := agentID

	// 3. Create cancellable context
	var execCtx context.Context
	var cancel context.CancelFunc

	timeout := 10 * time.Minute
	if policy.Timeout != "" {
		if d, err := time.ParseDuration(policy.Timeout); err == nil {
			timeout = d
		}
	}
	execCtx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	if cancelCtx != nil {
		cancelCtx.Register(askData.AskID, cancel)
		defer cancelCtx.Unregister(askData.AskID)
	}

	var response string
	var status string
	var durationMS int64

	// Use companion service (LLMChatEngine) if available, else fall back to DSPy
	if companionSvc != nil {
		logger.Debug().Str("conversation_id", conversationID).Msg("using companion service for console ask")

		startTime := time.Now()
		resp, chatErr := companionSvc.Chat(execCtx, companion.ChatRequest{
			ConversationID: conversationID,
			Message:        askData.Prompt,
			Context:        askData.Context,
		})
		durationMS = time.Since(startTime).Milliseconds()

		if chatErr != nil {
			if execCtx.Err() == context.Canceled {
				response = "Cancelled by user"
				status = "cancelled"
			} else if execCtx.Err() == context.DeadlineExceeded {
				response = "Request timed out"
				status = "error"
			} else {
				response = fmt.Sprintf("Error: %v", chatErr)
				status = "error"
			}
		} else {
			response = resp.Response
			status = "ok"
			logger.Debug().
				Int("context_queries", resp.ContextQueries).
				Int64("duration_ms", durationMS).
				Msg("companion console ask completed")
		}
	} else if dspyAgent != nil {
		// Fall back to DSPy ReAct
		var memoryContext string
		if companionMemory != nil {
			memCtx, err := companionMemory.GetContext(ctx, conversationID)
			if err != nil {
				logger.Warn().Err(err).Msg("failed to get memory context")
			} else if memCtx != "" {
				memoryContext = memCtx
				logger.Debug().Int("context_len", len(memCtx)).Msg("injected memory context")
			}

			if err := companionMemory.AppendTurn(ctx, companion.ConversationTurn{
				ConversationID: conversationID,
				Role:           "user",
				Content:        askData.Prompt,
			}); err != nil {
				logger.Warn().Err(err).Msg("failed to store user turn")
			}
		}

		prompt := askData.Prompt
		if len(askData.Context) > 0 {
			prompt = fmt.Sprintf("Context: %v\n\n%s", askData.Context, prompt)
		}
		if memoryContext != "" {
			prompt = fmt.Sprintf("## Conversation Memory\n%s\n\n---\n\n%s", memoryContext, prompt)
		}

		var hintsPrompt string
		if optCtx != nil && optCtx.Enabled && optCtx.Collector != nil {
			hints, err := optCtx.Collector.GetHints(ctx, optCtx.AgentRole, prompt)
			if err == nil && len(hints) > 0 {
				hintsPrompt = optCtx.Collector.FormatHintsForPrompt(hints)
				logger.Debug().Int("hint_count", len(hints)).Msg("injected tool hints from patterns")
			}
		}

		taskPrompt := prompt
		if hintsPrompt != "" {
			taskPrompt = prompt + "\n" + hintsPrompt
		}
		input := map[string]any{
			"task": taskPrompt,
		}

		startTime := time.Now()
		resultMap, err := dspyAgent.Execute(execCtx, input)
		durationMS = time.Since(startTime).Milliseconds()

		if optCtx != nil && optCtx.Enabled && optCtx.Collector != nil {
			success := err == nil
			if recordErr := optCtx.Collector.RecordToolCall(ctx, optCtx.AgentRole, prompt, "console_ask", success, durationMS); recordErr != nil {
				logger.Warn().Err(recordErr).Msg("failed to record optimization pattern")
			}
		}

		if err != nil {
			if ctx.Err() == context.Canceled || execCtx.Err() == context.Canceled {
				response = "Cancelled by user"
				status = "cancelled"
			} else if execCtx.Err() == context.DeadlineExceeded {
				response = "Request timed out"
				status = "error"
			} else {
				response = fmt.Sprintf("Error: %v", err)
				status = "error"
			}
		} else {
			response = extractResult(resultMap)
			status = "ok"
		}

		if companionMemory != nil && status == "ok" && response != "" {
			if err := companionMemory.AppendTurn(ctx, companion.ConversationTurn{
				ConversationID: conversationID,
				Role:           "assistant",
				Content:        response,
			}); err != nil {
				logger.Warn().Err(err).Msg("failed to store assistant turn")
			}
		}
	} else {
		return fmt.Errorf("no agent engine available")
	}

	logger.Info().Str("status", status).Int64("duration_ms", durationMS).Msg("console ask completed")

	// Build console.reply payload
	replyData := agent.ConsoleReplyData{
		AskID:    askData.AskID,
		Response: response,
		Status:   status,
		Metrics: map[string]any{
			"duration_ms": durationMS,
		},
	}
	replyEnv := envelope.OK("console.reply", replyData)
	replyPayload, err := json.Marshal(replyEnv)
	if err != nil {
		return fmt.Errorf("marshal console reply envelope: %w", err)
	}

	replyMsg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    msg.ToNS,
		ToNS:      msg.FromNS,
		Type:      agent.MessageTypeConsoleReply,
		TTLMS:     300000,
		Headers:   map[string]string{"correlation": askData.AskID, "console_id": askData.ConsoleID},
		Payload:   replyPayload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
	}
	return mailboxStore.Send(ctx, replyMsg)
}

// handleConsoleCmd handles console.cmd messages (cancel, pause, etc.).
//
// Index:
// - Purpose: Apply console command actions such as cancel
// - Flow: parse payload → select action → cancel request if applicable
// - SideEffects: cancels in-flight requests; logs warnings
// - FailureModes: payload decode errors, missing cancel target
// - Related: CancelContext.Cancel, handleConsoleAsk
// - Keywords: console.cmd, cancel, pause, resume, cancel_context
func handleConsoleCmd(_ context.Context, logger zerolog.Logger, msg agent.Message, cancelCtx *CancelContext) error {
	var env struct {
		Data agent.ConsoleCmdData `json:"data"`
	}
	if err := json.Unmarshal(msg.Payload, &env); err != nil {
		return fmt.Errorf("unmarshal console cmd payload: %w", err)
	}
	cmdData := env.Data

	logger = logger.With().
		Str("cmd_id", cmdData.CmdID).
		Str("action", cmdData.Action).
		Logger()

	switch cmdData.Action {
	case "cancel":
		if cancelCtx != nil && cmdData.AskID != "" {
			if cancelCtx.Cancel(cmdData.AskID) {
				logger.Info().Str("ask_id", cmdData.AskID).Msg("cancelled console ask")
			} else {
				logger.Warn().Str("ask_id", cmdData.AskID).Msg("no active ask to cancel")
			}
		}
	case "pause", "resume":
		// TODO: Implement pause/resume if needed
		logger.Warn().Msg("pause/resume not yet implemented")
	default:
		logger.Warn().Msg("unknown console command action")
	}

	return nil
}

// extractResult extracts the result string from DSPy execution output.
func extractResult(resultMap map[string]any) string {
	if resultMap == nil {
		return "Task completed"
	}

	// Priority order: result, answer, output, thought
	if r, ok := resultMap["result"].(string); ok && r != "" {
		return r
	}
	if r, ok := resultMap["answer"].(string); ok && r != "" {
		return r
	}
	if r, ok := resultMap["output"].(string); ok && r != "" {
		return r
	}
	if r, ok := resultMap["thought"].(string); ok && r != "" {
		return r
	}

	// Last resort: format the map but exclude internal ReAct fields
	cleaned := make(map[string]any)
	for k, v := range resultMap {
		if k == "action" || k == "observation" || k == "conversation_context" {
			continue
		}
		cleaned[k] = v
	}
	if len(cleaned) > 0 {
		return fmt.Sprintf("%v", cleaned)
	}
	return "Task completed"
}

// CancelContext manages cancellation functions for in-flight requests.
type CancelContext struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// NewCancelContext creates a new CancelContext.
//
// Index:
// - Purpose: Initialize cancellation registry for console requests
// - Flow: allocate map → return context
// - Related: CancelContext.Register, CancelContext.Cancel
// - Keywords: cancel_context, console, cancel_registry
func NewCancelContext() *CancelContext {
	return &CancelContext{
		cancels: make(map[string]context.CancelFunc),
	}
}

// Register registers a cancel function for an ask ID.
func (c *CancelContext) Register(askID string, cancel context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancels[askID] = cancel
}

// Unregister removes a cancel function for an ask ID.
func (c *CancelContext) Unregister(askID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cancels, askID)
}

// Cancel cancels the request with the given ask ID.
// Returns true if the request was found and cancelled.
func (c *CancelContext) Cancel(askID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel, ok := c.cancels[askID]; ok {
		cancel()
		return true
	}
	return false
}
