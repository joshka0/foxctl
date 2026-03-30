package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

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
// - Purpose: Execute an ask request via companion service and reply
// - Flow: parse payload → resolve conversation → execute engine → build reply → send mailbox response
// - SideEffects: LLM calls; memory reads/writes; mailbox send
// - FailureModes: payload decode errors, execution errors, reply marshal/send errors
// - Related: handleCmd, handleConsoleAsk
// - Keywords: agent.ask, agent.reply, mailbox, companion
func handleAsk(ctx context.Context, logger zerolog.Logger, msg agent.Message, companionSvc ChatService, mailboxStore mailbox.Store, policy agent.Policy, optCtx *OptimizationContext, agentID string, agentRole string) error {
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

	if companionSvc == nil {
		return fmt.Errorf("companion service not configured")
	}

	// Inject tool hints from optimization (if enabled)
	question := askData.Question
	originalQuestion := askData.Question
	requireContextQuery := false
	execMode := agent.ExecutionMode("")
	if shouldEnforceResearchExecution(agentRole, originalQuestion) {
		question = injectResearchExecutionContract(question)
		requireContextQuery = true
		execMode = agent.ModeReactive
	}
	logger.Debug().
		Str("agent_role", agentRole).
		Bool("require_context_query", requireContextQuery).
		Str("exec_mode_override", string(execMode)).
		Msg("ask execution policy")

	if optCtx != nil && optCtx.Enabled && optCtx.Collector != nil {
		hints, err := optCtx.Collector.GetHints(ctx, optCtx.AgentRole, askData.Question)
		if err == nil && len(hints) > 0 {
			hintsPrompt := optCtx.Collector.FormatHintsForPrompt(hints)
			question = question + "\n" + hintsPrompt
			logger.Debug().Int("hint_count", len(hints)).Msg("injected tool hints from patterns")
		}
	}

	logger.Debug().Str("conversation_id", conversationID).Msg("using companion service for ask")

	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startTime := time.Now()
	var requireContextQueryPtr *bool
	if requireContextQuery {
		requireContextQueryPtr = &requireContextQuery
	}
	chatReq := companion.ChatRequest{
		ConversationID:      conversationID,
		Message:             question,
		Context:             askData.Context,
		ResponseSchema:      askData.ResponseSchema,
		ResponseKeys:        askData.ResponseKeys,
		RequireContextQuery: requireContextQueryPtr,
	}
	if execMode != "" {
		chatReq.ExecMode = execMode
	}
	resp, chatErr := companionSvc.Chat(turnCtx, chatReq)
	durationMS = time.Since(startTime).Milliseconds()

	if chatErr != nil {
		return fmt.Errorf("companion chat failed: %w", chatErr)
	}
	if resp == nil {
		return fmt.Errorf("companion chat returned nil response")
	}
	result = resp.Response

	logger.Debug().
		Int("context_queries", resp.ContextQueries).
		Int64("duration_ms", durationMS).
		Msg("companion chat completed")

	// 6. Build reply payload
	answer := map[string]any{"response": result}
	if len(askData.ResponseSchema) > 0 {
		var payload any
		if err := json.Unmarshal([]byte(result), &payload); err == nil {
			answer["response_json"] = payload
		}
	}
	if resp.Presence != nil {
		answer["presence"] = resp.Presence
	}
	if resp.Tone != nil {
		answer["tone"] = resp.Tone
	}
	replyData := agent.ReplyData{
		AskID:  askData.AskID,
		Answer: answer,
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
		VisibleAt: time.Now().UnixMilli(),
		Timestamp: time.Now().UnixMilli(),
	}
	return mailboxStore.Send(ctx, replyMsg)
}

func shouldEnforceResearchExecution(agentRole, question string) bool {
	role := strings.ToLower(strings.TrimSpace(agentRole))
	switch role {
	case "researcher", "semantic_scout", "dag_scout", "symbol_scout", "annotation_scout", "memory_fact_scout", "memory_timeline_scout", "aca_context_scout":
		// Skip strict enforcement for very short conversational asks.
		q := strings.ToLower(strings.TrimSpace(question))
		if q == "" {
			return false
		}
		if len(q) <= 24 {
			shortConversational := []string{
				"hello", "hi", "hey", "thanks", "thank you",
				"how are you", "who are you", "what can you do",
			}
			for _, phrase := range shortConversational {
				if q == phrase {
					return false
				}
			}
		}
		return true
	default:
		return false
	}
}

func injectResearchExecutionContract(question string) string {
	contractHeader := "RESEARCH EXECUTION CONTRACT (must follow):"
	if strings.Contains(question, contractHeader) {
		return question
	}
	contract := []string{
		contractHeader,
		"1. First call one discovery tool: context_search OR smart_search OR repo_index_search OR code.search.",
		"2. Then call one source tool: fs_read_file OR fs.read_file OR code_search OR code.search OR repo_index_dag_grep.",
		"3. Ground major claims with concrete file references (`path:line`).",
		"4. If evidence is missing, state it explicitly under 'Gaps'.",
		"5. Final format: Findings, Evidence, Gaps, Next Steps.",
	}
	return strings.TrimSpace(question) + "\n\n" + strings.Join(contract, "\n")
}

// handleCmd processes agent.cmd messages and executes requested actions.
//
// Index:
// - Purpose: Execute command requests via companion service
// - Flow: parse payload → build prompt → inject hints → execute engine → record patterns
// - SideEffects: LLM calls; optimization pattern recording
// - FailureModes: payload decode errors, unknown actions, execution errors
// - Related: handleAsk, handleConsoleCmd
// - Keywords: agent.cmd, run_turn, do_work, companion
func handleCmd(ctx context.Context, logger zerolog.Logger, msg agent.Message, companionSvc ChatService, policy agent.Policy, optCtx *OptimizationContext, agentID string) error {
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

		if companionSvc == nil {
			return fmt.Errorf("companion service not configured")
		}

		turnCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		startTime := time.Now()
		_, err := companionSvc.Chat(turnCtx, companion.ChatRequest{
			ConversationID: conversationID,
			Message:        taskPrompt,
		})
		durationMS := time.Since(startTime).Milliseconds()

		if optCtx != nil && optCtx.Enabled && optCtx.Collector != nil {
			success := err == nil
			if recordErr := optCtx.Collector.RecordToolCall(ctx, optCtx.AgentRole, prompt, "companion_cmd", success, durationMS); recordErr != nil {
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
// It executes the prompt via companion service and sends console.reply with the response.
//
// Index:
// - Purpose: Execute console ask prompts with cancellation support
// - Flow: parse payload → setup cancel context → execute engine → build reply → send mailbox response
// - SideEffects: LLM calls; memory reads/writes; mailbox send; cancel registry updates
// - FailureModes: payload decode errors, execution errors, reply marshal/send errors
// - Related: handleConsoleCmd, handleAsk
// - Keywords: console.ask, console.reply, cancellation, companion
func handleConsoleAsk(ctx context.Context, logger zerolog.Logger, msg agent.Message, companionSvc ChatService, mailboxStore mailbox.Store, policy agent.Policy, optCtx *OptimizationContext, cancelCtx *CancelContext, agentID string) error {
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

	if companionSvc == nil {
		return fmt.Errorf("companion service not configured")
	}

	prompt := askData.Prompt
	if optCtx != nil && optCtx.Enabled && optCtx.Collector != nil {
		hints, err := optCtx.Collector.GetHints(ctx, optCtx.AgentRole, prompt)
		if err == nil && len(hints) > 0 {
			hintsPrompt := optCtx.Collector.FormatHintsForPrompt(hints)
			prompt = prompt + "\n" + hintsPrompt
			logger.Debug().Int("hint_count", len(hints)).Msg("injected tool hints from patterns")
		}
	}

	logger.Debug().Str("conversation_id", conversationID).Msg("using companion service for console ask")

	startTime := time.Now()
	resp, chatErr := companionSvc.Chat(execCtx, companion.ChatRequest{
		ConversationID: conversationID,
		Message:        prompt,
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
	if resp != nil && resp.Presence != nil {
		replyData.Presence = resp.Presence
	}
	if resp != nil && resp.Tone != nil {
		replyData.Tone = resp.Tone
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
