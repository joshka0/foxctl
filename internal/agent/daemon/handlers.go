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

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
)

func handleAsk(ctx context.Context, logger zerolog.Logger, msg agent.Message, dspyAgent agents.Agent, mailboxStore mailbox.Store, policy agent.Policy, optCtx *OptimizationContext) error {
	// 1. Parse payload envelope
	var env struct {
		Data agent.AskData `json:"data"`
	}
	if err := json.Unmarshal(msg.Payload, &env); err != nil {
		return fmt.Errorf("unmarshal ask payload: %w", err)
	}
	askData := env.Data

	// 2. Build prompt from ask
	prompt := fmt.Sprintf("Question: %s\nContext: %v", askData.Question, askData.Context)

	// 2a. Inject tool hints from optimization (if enabled)
	var hintsPrompt string
	if optCtx != nil && optCtx.Enabled && optCtx.Collector != nil {
		hints, err := optCtx.Collector.GetHints(ctx, optCtx.AgentRole, askData.Question)
		if err == nil && len(hints) > 0 {
			hintsPrompt = optCtx.Collector.FormatHintsForPrompt(hints)
			logger.Debug().Int("hint_count", len(hints)).Msg("injected tool hints from patterns")
		}
	}

	// 3. Execute DSPy turn
	// dspy-go agents.Agent.Execute takes map[string]any
	taskPrompt := prompt
	if hintsPrompt != "" {
		taskPrompt = prompt + "\n" + hintsPrompt
	}
	input := map[string]any{
		"task": taskPrompt,
	}

	// Apply turn timeout from policy
	timeout := 10 * time.Minute // default
	if policy.Timeout != "" {
		if d, err := time.ParseDuration(policy.Timeout); err == nil {
			timeout = d
		}
	}
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startTime := time.Now()
	resultMap, err := dspyAgent.Execute(turnCtx, input)
	durationMS := time.Since(startTime).Milliseconds()

	// Record pattern for optimization (success or failure)
	if optCtx != nil && optCtx.Enabled && optCtx.Collector != nil {
		success := err == nil
		// Note: In a full implementation, we'd extract actual tool names from the execution trace
		// For now, record a generic pattern based on the task context
		if recordErr := optCtx.Collector.RecordToolCall(ctx, optCtx.AgentRole, askData.Question, "dspy_ask", success, durationMS); recordErr != nil {
			logger.Warn().Err(recordErr).Msg("failed to record optimization pattern")
		}
	}

	if err != nil {
		return fmt.Errorf("dspy execution failed: %w", err)
	}

	// Extract result from ReAct output
	// The ReAct module returns: thought, action, observation, and our custom result field
	var result string
	if resultMap != nil {
		// Priority order: result (from our signature), answer, thought (agent's reasoning)
		if r, ok := resultMap["result"].(string); ok && r != "" {
			result = r
		} else if r, ok := resultMap["answer"].(string); ok && r != "" {
			result = r
		} else if r, ok := resultMap["output"].(string); ok && r != "" {
			result = r
		} else if r, ok := resultMap["thought"].(string); ok && r != "" {
			// Use thought field as fallback - this contains the agent's reasoning
			result = r
		} else {
			// Last resort: format the map but exclude internal ReAct fields
			cleaned := make(map[string]any)
			for k, v := range resultMap {
				// Skip internal ReAct control fields
				if k == "action" || k == "observation" || k == "conversation_context" {
					continue
				}
				cleaned[k] = v
			}
			if len(cleaned) > 0 {
				result = fmt.Sprintf("%v", cleaned)
			} else {
				result = "Task completed"
			}
		}
	}

	// 4. Build reply payload
	replyData := agent.ReplyData{
		AskID:  askData.AskID,
		Answer: map[string]any{"response": result},
	}
	replyEnv := envelope.OK("agent.reply", replyData)
	replyPayload, err := json.Marshal(replyEnv)
	if err != nil {
		return fmt.Errorf("marshal reply envelope: %w", err)
	}

	// 5. Send reply with correlation
	replyMsg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    msg.ToNS,   // we are the sender now
		ToNS:      msg.FromNS, // reply to asker
		Type:      agent.MessageTypeReply,
		TTLMS:     300000, // 5 min
		Headers:   map[string]string{"correlation": askData.AskID},
		Payload:   replyPayload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
	}
	return mailboxStore.Send(ctx, replyMsg)
}

func handleCmd(ctx context.Context, logger zerolog.Logger, msg agent.Message, dspyAgent agents.Agent, policy agent.Policy, optCtx *OptimizationContext) error {
	var env struct {
		Data agent.CmdData `json:"data"`
	}
	if err := json.Unmarshal(msg.Payload, &env); err != nil {
		return fmt.Errorf("unmarshal cmd payload: %w", err)
	}
	cmdData := env.Data

	switch cmdData.Action {
	case "run_skill":
		// Invoke skill via runner (future)
		return fmt.Errorf("run_skill not yet implemented")
	case "run_turn", "do_work":
		// Execute DSPy turn with cmdData.Args as context
		prompt := fmt.Sprintf("Command: %s\nArgs: %v", cmdData.Action, cmdData.Args)

		// Inject tool hints from optimization (if enabled)
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

		// Apply turn timeout from policy
		timeout := 10 * time.Minute // default
		if policy.Timeout != "" {
			if d, err := time.ParseDuration(policy.Timeout); err == nil {
				timeout = d
			}
		}
		turnCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		startTime := time.Now()
		_, err := dspyAgent.Execute(turnCtx, input)
		durationMS := time.Since(startTime).Milliseconds()

		// Record pattern for optimization (success or failure)
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

func handleEvent(ctx context.Context, logger zerolog.Logger, msg agent.Message) error {
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
// It executes the prompt via DSPy and sends console.reply with the response.
func handleConsoleAsk(ctx context.Context, logger zerolog.Logger, msg agent.Message, dspyAgent agents.Agent, mailboxStore mailbox.Store, policy agent.Policy, optCtx *OptimizationContext, cancelCtx *CancelContext) error {
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

	// 2. Create cancellable context
	var execCtx context.Context
	var cancel context.CancelFunc

	// Apply turn timeout from policy
	timeout := 10 * time.Minute // default
	if policy.Timeout != "" {
		if d, err := time.ParseDuration(policy.Timeout); err == nil {
			timeout = d
		}
	}
	execCtx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	// Register cancel func if we have a cancel context
	if cancelCtx != nil {
		cancelCtx.Register(askData.AskID, cancel)
		defer cancelCtx.Unregister(askData.AskID)
	}

	// 3. Build prompt
	prompt := askData.Prompt
	if len(askData.Context) > 0 {
		prompt = fmt.Sprintf("Context: %v\n\n%s", askData.Context, prompt)
	}

	// Inject tool hints from optimization (if enabled)
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

	// 4. Execute DSPy turn
	startTime := time.Now()
	resultMap, err := dspyAgent.Execute(execCtx, input)
	durationMS := time.Since(startTime).Milliseconds()

	// Record pattern for optimization
	if optCtx != nil && optCtx.Enabled && optCtx.Collector != nil {
		success := err == nil
		if recordErr := optCtx.Collector.RecordToolCall(ctx, optCtx.AgentRole, prompt, "console_ask", success, durationMS); recordErr != nil {
			logger.Warn().Err(recordErr).Msg("failed to record optimization pattern")
		}
	}

	// 5. Build response and status
	var response string
	var status string

	if err != nil {
		if ctx.Err() == context.Canceled || execCtx.Err() == context.Canceled {
			response = "Cancelled by user"
			status = "cancelled"
			logger.Info().Msg("console ask cancelled")
		} else if execCtx.Err() == context.DeadlineExceeded {
			response = "Request timed out"
			status = "error"
			logger.Warn().Msg("console ask timed out")
		} else {
			response = fmt.Sprintf("Error: %v", err)
			status = "error"
			logger.Error().Err(err).Msg("console ask failed")
		}
	} else {
		response = extractResult(resultMap)
		status = "ok"
		logger.Info().Int64("duration_ms", durationMS).Msg("console ask completed")
	}

	// 6. Build console.reply payload
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

	// 7. Send reply
	replyMsg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    msg.ToNS,
		ToNS:      msg.FromNS,
		Type:      agent.MessageTypeConsoleReply,
		TTLMS:     300000, // 5 min
		Headers:   map[string]string{"correlation": askData.AskID, "console_id": askData.ConsoleID},
		Payload:   replyPayload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
	}
	return mailboxStore.Send(ctx, replyMsg)
}

// handleConsoleCmd handles console.cmd messages (cancel, pause, etc.).
func handleConsoleCmd(ctx context.Context, logger zerolog.Logger, msg agent.Message, cancelCtx *CancelContext) error {
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
