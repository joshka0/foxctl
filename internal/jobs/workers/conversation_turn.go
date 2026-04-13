package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/context/companion"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const conversationTurnKind = "conversation.turn"

// ConversationTurnArgs contains arguments for a conversation turn job.
// DedupeKey is "{conversationID}:{activityID}" for River's UniqueOpts.
// Only stable fields go in args; mutable metadata (serviceURL, etc.) should
// be looked up from persistent conversation refs at execution time.
type ConversationTurnArgs struct {
	DedupeKey      string          `json:"dedupe_key"`
	ConversationID string          `json:"conversation_id"`
	ActivityID     string          `json:"activity_id"`
	Content        string          `json:"content"`
	PrincipalJSON  json.RawMessage `json:"principal"`
	Platform       string          `json:"platform"`
	ChannelID      string          `json:"channel_id"`
	ReplyTo        string          `json:"reply_to,omitempty"`
}

func (ConversationTurnArgs) Kind() string { return conversationTurnKind }

func (a ConversationTurnArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: "conversations",
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByQueue: true,
			ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStateRunning},
		},
	}
}

// NewConversationTurnArgs constructs args with a properly formatted DedupeKey.
func NewConversationTurnArgs(
	conversationID, activityID, content string,
	principalJSON json.RawMessage,
	platform, channelID, replyTo string,
) ConversationTurnArgs {
	return ConversationTurnArgs{
		DedupeKey:      conversationID + ":" + activityID,
		ConversationID: conversationID,
		ActivityID:     activityID,
		Content:        content,
		PrincipalJSON:  principalJSON,
		Platform:       platform,
		ChannelID:      channelID,
		ReplyTo:        replyTo,
	}
}

// TurnProcessor processes a conversation turn and returns the reply text.
type TurnProcessor interface {
	ProcessTurn(ctx context.Context, conversationID, content string, principalJSON json.RawMessage) (string, error)
}

// ReplyDeliverer sends a reply back to the originating platform.
type ReplyDeliverer interface {
	DeliverReply(ctx context.Context, conversationKey, replyText, replyTo string) error
}

// ConversationTurnWorker processes conversation turns via River jobs.
type ConversationTurnWorker struct {
	river.WorkerDefaults[ConversationTurnArgs]

	Processor TurnProcessor
	Deliverer ReplyDeliverer
	TurnLock  companion.Locker

	// TurnTimeout is the per-turn processing timeout. Defaults to 5 minutes.
	TurnTimeout time.Duration
}

func (w *ConversationTurnWorker) Work(ctx context.Context, job *river.Job[ConversationTurnArgs]) error {
	if w == nil {
		return fmt.Errorf("jobs: conversation turn worker is nil")
	}
	if job == nil {
		return fmt.Errorf("jobs: conversation turn job is required")
	}
	if w.Processor == nil {
		return fmt.Errorf("jobs: turn processor is required")
	}
	if w.Deliverer == nil {
		return fmt.Errorf("jobs: reply deliverer is required")
	}

	args := job.Args
	conversationID := strings.TrimSpace(args.ConversationID)
	if conversationID == "" {
		return fmt.Errorf("jobs: conversation_id is required")
	}
	content := strings.TrimSpace(args.Content)
	if content == "" {
		return fmt.Errorf("jobs: content is required")
	}
	channelID := strings.TrimSpace(args.ChannelID)
	replyTo := strings.TrimSpace(args.ReplyTo)

	timeout := w.TurnTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	event := observability.NewEvent("jobs.conversation_turn").
		WithComponent(observability.ComponentJob).
		WithData("conversation_id", conversationID).
		WithData("platform", strings.TrimSpace(args.Platform)).
		WithData("activity_id", strings.TrimSpace(args.ActivityID))

	var (
		unlock func()
		err    error
	)
	if w.TurnLock != nil {
		unlock, err = w.TurnLock.Lock(ctx, conversationID)
		if err != nil {
			wrappedErr := fmt.Errorf("jobs: turn lock: %w", err)
			observability.Emit(ctx, event.Error(wrappedErr, 0))
			return wrappedErr
		}
		defer unlock()
	}

	start := time.Now()
	reply, err := w.Processor.ProcessTurn(ctx, conversationID, content, args.PrincipalJSON)
	if err != nil {
		wrappedErr := fmt.Errorf("jobs: process turn: %w", err)
		observability.Emit(ctx, event.Error(wrappedErr, time.Since(start)))
		return wrappedErr
	}

	if err := w.Deliverer.DeliverReply(ctx, channelID, reply, replyTo); err != nil {
		wrappedErr := fmt.Errorf("jobs: deliver reply: %w", err)
		observability.Emit(ctx, event.Error(wrappedErr, time.Since(start)))
		return wrappedErr
	}

	observability.Emit(ctx, event.
		WithData("reply_len", len(reply)).
		Success(time.Since(start)))
	return nil
}

var _ river.Worker[ConversationTurnArgs] = (*ConversationTurnWorker)(nil)
