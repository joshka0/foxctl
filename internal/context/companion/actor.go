package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/runtime/actor"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/rs/zerolog"
)

// CompanionActor implements the Actor interface for RLM companion agents.
// It receives messages via the mailbox and routes them through the companion
// service for stateless RLM processing.
type CompanionActor struct {
	config     actor.Config
	service    *Service
	boardStore blackboard.BoardStore
	logger     zerolog.Logger

	mu    sync.RWMutex
	state actor.State

	CreatedAt time.Time
}

// CompanionActorConfig holds configuration for creating a CompanionActor.
type CompanionActorConfig struct {
	// Namespace is the mailbox namespace this actor listens to.
	// Example: "companion:philosopher", "companion:assistant"
	Namespace string

	// Service is the companion service for processing messages.
	Service *Service

	// BoardStore is used to send responses back.
	BoardStore blackboard.BoardStore

	// WorkspaceID scopes board messages.
	WorkspaceID string

	// Logger for structured logging.
	Logger zerolog.Logger
}

// NewCompanionActor creates a new companion actor.
// NewCompanionActor creates a companion actor with mailbox configuration.
//
// Index:
// - Purpose: Build a companion actor backed by a companion service
// - Flow: validate config → build actor config → return actor
// - FailureModes: missing namespace/service/board store
// - Related: CompanionActor.OnMailReceived, CompanionActor.Start
// - Keywords: companion_actor, mailbox, namespace, service, board_store
func NewCompanionActor(cfg CompanionActorConfig) (*CompanionActor, error) {
	if cfg.Namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	if cfg.Service == nil {
		return nil, fmt.Errorf("service is required")
	}
	if cfg.BoardStore == nil {
		return nil, fmt.Errorf("board store is required")
	}

	return &CompanionActor{
		config: actor.Config{
			ID:           cfg.Namespace,
			Namespace:    cfg.Namespace,
			Role:         "companion",
			LeaseTimeout: 2 * time.Minute,
			MaxRetries:   3,
			Metadata: map[string]any{
				"workspace_id": cfg.WorkspaceID,
			},
		},
		service:    cfg.Service,
		boardStore: cfg.BoardStore,
		logger:     cfg.Logger,
		state:      actor.StateIdle,
		CreatedAt:  time.Now(),
	}, nil
}

// ID returns the actor's unique identifier.
func (a *CompanionActor) ID() string {
	return a.config.ID
}

// Namespace returns the mailbox namespace.
func (a *CompanionActor) Namespace() string {
	return a.config.Namespace
}

// Start initializes the actor.
func (a *CompanionActor) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.state = actor.StateIdle
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	a.logger.Info().
		Str("namespace", a.config.Namespace).
		Msg("Companion actor started")
	return nil
}

// Stop shuts down the actor.
func (a *CompanionActor) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.state = actor.StateStopped
	a.logger.Info().
		Str("namespace", a.config.Namespace).
		Msg("Companion actor stopped")
	return nil
}

// State returns the current actor state.
func (a *CompanionActor) State() actor.State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// SetState updates the actor state.
func (a *CompanionActor) SetState(state actor.State) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state = state
}

// MailboxMessage is the expected payload format for mailbox messages.
type MailboxMessage struct {
	// Message is the user's message text.
	Message string `json:"message"`

	// Personality overrides the default personality (optional).
	Personality string `json:"personality,omitempty"`

	// ReplyTo is the actor/recipient to send the response to.
	// If empty, replies to the sender.
	ReplyTo string `json:"reply_to,omitempty"`
}

// OnMailReceived handles incoming mailbox messages.
// It routes the message through the companion service and posts the response.
// OnMailReceived handles mailbox messages and replies with companion output.
//
// Index:
// - Purpose: Process mailbox messages and respond via blackboard
// - Flow: parse payload → build chat request → call service → post response
// - SideEffects: calls companion service; posts to blackboard
// - FailureModes: parse errors, chat errors, response post errors
// - Related: Service.Chat, CompanionActor.postResponse
// - Keywords: companion_mail, mailbox, chat_request, blackboard, response
func (a *CompanionActor) OnMailReceived(ctx context.Context, msg *actor.Message) error {
	a.SetState(actor.StateProcessing)
	defer a.SetState(actor.StateIdle)

	a.logger.Debug().
		Str("msg_id", msg.ID).
		Str("from", msg.FromNS).
		Str("subject", msg.Subject).
		Msg("Companion actor received message")

	// Parse the message payload
	var payload MailboxMessage
	if err := json.Unmarshal(msg.Body, &payload); err != nil {
		// If not JSON, treat the body as plain text message
		payload.Message = string(msg.Body)
	} else if payload.Message == "" {
		// JSON parsed successfully but Message field is empty.
		// This is intentional: valid JSON like {"personality":"friendly"} should NOT
		// be treated as a chat message. Return an error so callers can handle it.
		return fmt.Errorf("parsed JSON has no message field")
	}

	if payload.Message == "" {
		return fmt.Errorf("empty message")
	}

	// Use namespace as conversation ID for persistent context
	// This means all messages to "companion:philosopher" share context
	conversationID := a.config.Namespace

	// Call the companion service
	chatReq := ChatRequest{
		ConversationID: conversationID,
		Message:        payload.Message,
		Personality:    payload.Personality,
	}

	resp, err := a.service.Chat(ctx, chatReq)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("conversation_id", conversationID).
			Msg("Companion chat failed")

		replyTo := normalizeReplyTo(payload.ReplyTo)
		// Post error response
		return a.postResponse(ctx, msg, replyTo, nil, err)
	}

	a.logger.Debug().
		Str("conversation_id", conversationID).
		Int("context_queries", resp.ContextQueries).
		Int64("duration_ms", resp.DurationMS).
		Msg("Companion chat completed")

	replyTo := normalizeReplyTo(payload.ReplyTo)
	// Post success response
	return a.postResponse(ctx, msg, replyTo, resp, nil)
}

// postResponse sends the response back via the board store.
func (a *CompanionActor) postResponse(ctx context.Context, originalMsg *actor.Message, replyTo string, resp *ChatResponse, chatErr error) error {
	// Determine recipient
	recipient := replyTo
	if recipient == "" {
		recipient = originalMsg.FromNS
	}
	if recipient == "" {
		recipient = "*" // Broadcast if no sender
	}

	workspaceID, _ := a.config.Metadata["workspace_id"].(string)

	// Build response message
	var subject, body string
	var kind agent.BoardMessageKind

	if chatErr != nil {
		subject = fmt.Sprintf("Error from %s", a.config.Namespace)
		body = fmt.Sprintf("Error processing message: %v", chatErr)
		kind = agent.BoardMessageKindAlert
	} else {
		subject = fmt.Sprintf("Reply from %s", a.config.Namespace)
		body = resp.Response

		// Add metadata as JSON footer
		metadata := map[string]any{
			"context_queries": resp.ContextQueries,
			"tools_used":      resp.ToolsUsed,
			"duration_ms":     resp.DurationMS,
			"token_usage":     resp.TokenUsage,
		}
		if metaJSON, err := json.MarshalIndent(metadata, "", "  "); err == nil {
			body += fmt.Sprintf("\n\n---\n```json\n%s\n```", string(metaJSON))
		}
		kind = agent.BoardMessageKindInfo
	}

	// Normalize namespace to avoid double-prefixing (a.config.Namespace may already have "companion:" prefix)
	ns := strings.TrimPrefix(a.config.Namespace, "companion:")

	boardMsg := &agent.BoardMessage{
		WorkspaceID: workspaceID,
		Sender:      fmt.Sprintf("actor:%s", ns),
		Recipient:   recipient,
		Kind:        kind,
		Priority:    3, // Normal priority
		Subject:     subject,
		Body:        body,
	}

	if err := a.boardStore.SendMessage(ctx, boardMsg); err != nil {
		a.logger.Error().
			Err(err).
			Str("recipient", recipient).
			Msg("Failed to post companion response")
		return err
	}

	a.logger.Debug().
		Str("msg_id", boardMsg.ID).
		Str("recipient", recipient).
		Msg("Posted companion response")

	return nil
}

// OnTimeout handles timer events (not used for companion actors).
func (a *CompanionActor) OnTimeout(ctx context.Context, timer actor.TimerEvent) error {
	// Companion actors don't use timers
	return nil
}

// OnError handles actor errors and returns a directive.
func (a *CompanionActor) OnError(ctx context.Context, err error) actor.Directive {
	a.logger.Error().
		Err(err).
		Str("namespace", a.config.Namespace).
		Msg("Companion actor error")

	// Resume on recoverable errors
	return actor.DirectiveResume
}

// CompanionActorFactory creates a factory function for registering with the supervisor.
func CompanionActorFactory(service *Service, boardStore blackboard.BoardStore, workspaceID string, logger zerolog.Logger) func(cfg actor.Config) (actor.Actor, error) {
	return func(cfg actor.Config) (actor.Actor, error) {
		return NewCompanionActor(CompanionActorConfig{
			Namespace:   cfg.Namespace,
			Service:     service,
			BoardStore:  boardStore,
			WorkspaceID: workspaceID,
			Logger:      logger.With().Str("actor", cfg.Namespace).Logger(),
		})
	}
}

func normalizeReplyTo(replyTo string) string {
	return strings.TrimSpace(replyTo)
}
