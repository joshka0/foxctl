package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/joshka0/foxctl/internal/storage/contextbuffer"
	"github.com/joshka0/foxctl/internal/storage/mailbox"
	"github.com/oklog/ulid/v2"
)

// ActionSkillRunner executes skills by name for action processing.
// This is separate from the SkillRunner struct used for hook skill execution.
type ActionSkillRunner interface {
	// RunSkill executes a skill with the given arguments and returns the result.
	RunSkill(ctx context.Context, skill string, args json.RawMessage) (string, error)
}

// ActionExecutor processes hook output actions.
type ActionExecutor interface {
	// Execute processes all actions from a hook output.
	// Returns the collected inject_context text (if any) and any error.
	Execute(ctx context.Context, actions []Action, input Input) (injectedContext string, err error)
}

// ExecutorConfig configures the action executor.
type ExecutorConfig struct {
	// SkillRunner for run_skill actions. Optional - skill actions are skipped if nil.
	SkillRunner ActionSkillRunner

	// ContextBuffer for enqueue_context actions. Optional - actions are skipped if nil.
	ContextBuffer contextbuffer.Store

	// MailboxStore for send_mailbox actions. Optional - actions are skipped if nil.
	MailboxStore mailbox.Store

	// BoardStore for bb_post/bb_claim actions. Optional - actions are skipped if nil.
	BoardStore blackboard.BoardStore

	// Logger for action execution. Uses slog.Default() if nil.
	Logger *slog.Logger

	// FailOpen controls error handling. If true (default), action errors are logged
	// but don't stop processing. If false, first error stops execution.
	FailOpen bool
}

// DefaultExecutor implements ActionExecutor using configured stores.
type DefaultExecutor struct {
	config ExecutorConfig
	logger *slog.Logger
}

// NewExecutor creates a new action executor with the given configuration.
// NewExecutor creates a new hook action executor.
//
// Index:
// - Purpose: Initialize a default action executor with configured stores
// - Flow: select logger → store config → return executor
// - Related: DefaultExecutor.Execute
// - Keywords: hook_executor, action_executor, fail_open, logger
func NewExecutor(cfg ExecutorConfig) *DefaultExecutor {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &DefaultExecutor{
		config: cfg,
		logger: logger.With("component", "hook-executor"),
	}
}

// Execute processes all actions and returns any injected context.
// Execute processes actions from hook outputs.
//
// Index:
// - Purpose: Execute hook actions and return injected context
// - Flow: iterate actions → dispatch handlers → collect injected text → return
// - SideEffects: runs skills; writes context buffer; sends mailbox; posts to blackboard
// - FailureModes: action errors (fail-open or fail-closed depending on config)
// - Related: executeRunSkill, executeEnqueueContext, executeSendMailbox, executeBBPost
// - Keywords: hook_action, run_skill, enqueue_context, send_mailbox, bb_post
func (e *DefaultExecutor) Execute(ctx context.Context, actions []Action, input Input) (string, error) {
	if len(actions) == 0 {
		return "", nil
	}

	var injectedContext string

	for i, action := range actions {
		e.logger.Debug("executing action",
			"index", i,
			"type", action.Type,
			"session_id", input.SessionID,
		)

		var err error
		switch action.Type {
		case ActionRunSkill:
			err = e.executeRunSkill(ctx, action, input)

		case ActionInjectContext:
			// Collect inject_context text - caller handles actual injection
			if action.Text != "" {
				if injectedContext != "" {
					injectedContext += "\n\n"
				}
				injectedContext += action.Text
			}

		case ActionEnqueueContext:
			err = e.executeEnqueueContext(ctx, action, input)

		case ActionSendMailbox:
			err = e.executeSendMailbox(ctx, action, input)

		case ActionBBPost:
			err = e.executeBBPost(ctx, action, input)

		case ActionBBClaim:
			err = e.executeBBClaim(ctx, action, input)

		default:
			e.logger.Warn("unknown action type", "type", action.Type)
			continue
		}

		if err != nil {
			e.logger.Warn("action execution failed",
				"type", action.Type,
				"error", err,
			)
			if !e.config.FailOpen {
				return injectedContext, fmt.Errorf("action %s failed: %w", action.Type, err)
			}
		}
	}

	return injectedContext, nil
}

// executeRunSkill handles run_skill actions.
func (e *DefaultExecutor) executeRunSkill(ctx context.Context, action Action, input Input) error {
	if e.config.SkillRunner == nil {
		e.logger.Debug("skipping run_skill: no skill runner configured")
		return nil
	}

	if action.Skill == "" {
		return fmt.Errorf("run_skill: skill name required")
	}

	e.logger.Debug("running skill",
		"skill", action.Skill,
		"session_id", input.SessionID,
	)

	result, err := e.config.SkillRunner.RunSkill(ctx, action.Skill, action.Args)
	if err != nil {
		return fmt.Errorf("run_skill %s: %w", action.Skill, err)
	}

	e.logger.Debug("skill completed",
		"skill", action.Skill,
		"result_len", len(result),
	)

	return nil
}

// executeEnqueueContext handles enqueue_context actions.
func (e *DefaultExecutor) executeEnqueueContext(ctx context.Context, action Action, input Input) error {
	if e.config.ContextBuffer == nil {
		e.logger.Debug("skipping enqueue_context: no context buffer configured")
		return nil
	}

	if action.Text == "" {
		return fmt.Errorf("enqueue_context: text required")
	}

	// Set defaults
	priority := action.Priority
	if priority < 1 || priority > 3 {
		priority = 2 // Normal priority
	}

	ttl := time.Duration(action.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 60 * time.Second
	}

	source := action.Source
	if source == "" {
		source = "hook"
	}

	params := contextbuffer.EnqueueParams{
		WorkspaceID: input.WorkspaceID,
		SessionID:   input.SessionID,
		Source:      source,
		Text:        action.Text,
		Priority:    priority,
		TTL:         ttl,
		Dedupe:      action.Dedupe,
	}

	entry, err := e.config.ContextBuffer.Enqueue(ctx, params)
	if err != nil {
		return fmt.Errorf("enqueue_context: %w", err)
	}

	e.logger.Debug("enqueued context",
		"entry_id", entry.ID,
		"source", source,
		"session_id", input.SessionID,
	)

	return nil
}

// executeSendMailbox handles send_mailbox actions.
func (e *DefaultExecutor) executeSendMailbox(ctx context.Context, action Action, input Input) error {
	if e.config.MailboxStore == nil {
		e.logger.Debug("skipping send_mailbox: no mailbox store configured")
		return nil
	}

	if action.ToNS == "" {
		return fmt.Errorf("send_mailbox: to_ns required")
	}

	msgType := agent.MessageType(action.MessageType)
	if action.MessageType == "" {
		msgType = agent.MessageTypeCmd
	}

	ttlMS := action.TTLMS
	if ttlMS <= 0 {
		ttlMS = 300000 // 5 minutes default
	}

	msg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    input.ActorID,
		ToNS:      action.ToNS,
		Type:      msgType,
		TTLMS:     ttlMS,
		Headers:   action.Headers,
		Payload:   action.Payload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
		SessionID: input.SessionID,
		Workspace: input.WorkspaceRoot,
	}

	if err := e.config.MailboxStore.Send(ctx, msg); err != nil {
		return fmt.Errorf("send_mailbox: %w", err)
	}

	e.logger.Debug("sent mailbox message",
		"message_id", msg.ID,
		"to_ns", action.ToNS,
		"type", msgType,
	)

	return nil
}

// executeBBPost handles bb_post actions.
func (e *DefaultExecutor) executeBBPost(ctx context.Context, action Action, input Input) error {
	if e.config.BoardStore == nil {
		e.logger.Debug("skipping bb_post: no board store configured")
		return nil
	}

	// Convert action to BoardMessage
	msg := &agent.BoardMessage{
		ID:          ulid.Make().String(),
		WorkspaceID: input.WorkspaceID,
		TaskID:      input.SessionID,
		Stream:      action.Topic,
		Sender:      input.ActorID,
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindInfo,
		Priority:    agent.DefaultPriority,
		Subject:     action.Topic,
		Body:        string(action.BBPayload),
		CreatedAt:   time.Now(),
	}

	if err := e.config.BoardStore.SendMessage(ctx, msg); err != nil {
		return fmt.Errorf("bb_post: %w", err)
	}

	e.logger.Debug("posted to blackboard",
		"message_id", msg.ID,
		"topic", action.Topic,
	)

	return nil
}

// executeBBClaim handles bb_claim actions.
func (e *DefaultExecutor) executeBBClaim(ctx context.Context, action Action, input Input) error {
	if e.config.BoardStore == nil {
		e.logger.Debug("skipping bb_claim: no board store configured")
		return nil
	}

	if action.Topic == "" || action.RecordID == "" {
		return fmt.Errorf("bb_claim: topic and record_id required")
	}

	// Mark the message as read/claimed
	_, err := e.config.BoardStore.MarkRead(ctx, input.WorkspaceID, input.ActorID, []string{action.RecordID})
	if err != nil {
		return fmt.Errorf("bb_claim: %w", err)
	}

	e.logger.Debug("claimed blackboard record",
		"record_id", action.RecordID,
		"topic", action.Topic,
	)

	return nil
}

// NopExecutor is a no-op executor for testing or when no action processing is needed.
type NopExecutor struct{}

// Execute returns immediately without processing any actions.
func (NopExecutor) Execute(ctx context.Context, actions []Action, input Input) (string, error) {
	return "", nil
}

var (
	_ ActionExecutor = (*DefaultExecutor)(nil)
	_ ActionExecutor = NopExecutor{}
)
