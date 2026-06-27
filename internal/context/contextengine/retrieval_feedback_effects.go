package contextengine

import (
	"context"
	"fmt"
	"reflect"
)

const retrievalFeedbackEventSource = "retrieval_feedback"

// RetrievalFeedbackEffectStore is the existing context store surface needed to
// persist feedback and apply any lifecycle impact it implies.
type RetrievalFeedbackEffectStore interface {
	InvalidationStore
	ContextEventAppendStore
	RecordRetrievalFeedback(ctx context.Context, feedback RetrievalFeedback) (RetrievalFeedback, error)
	GetRetrievalFeedback(ctx context.Context, id string) (RetrievalFeedback, error)
}

// ContextEventAppendStore is the event stream surface needed for idempotent
// event recording. It stays separate from InvalidationStore so ApplyInvalidation
// does not require event-stream reads it does not use.
type ContextEventAppendStore interface {
	AppendEvent(ctx context.Context, event ContextEvent) (ContextEvent, error)
	ListEvents(ctx context.Context, filter EventFilter) ([]ContextEvent, error)
}

// RetrievalFeedbackApplyStore is the narrow store surface needed to adapt
// recorded retrieval feedback into a context event and apply its lifecycle
// effects.
type RetrievalFeedbackApplyStore interface {
	InvalidationStore
	ContextEventAppendStore
}

// RecordRetrievalFeedbackWithEffects records feedback and applies any lifecycle
// impact that feedback explicitly carries through UsedRefs.
func RecordRetrievalFeedbackWithEffects(ctx context.Context, store RetrievalFeedbackEffectStore, feedback RetrievalFeedback, opts ...ComputeImpactOption) (RetrievalFeedback, error) {
	if err := feedback.Validate(); err != nil {
		return RetrievalFeedback{}, fmt.Errorf("record retrieval feedback with effects: %w", err)
	}

	recorded, err := recordRetrievalFeedbackOnce(ctx, store, feedback)
	if err != nil {
		return RetrievalFeedback{}, err
	}
	if err := ApplyRetrievalFeedback(ctx, store, recorded, opts...); err != nil {
		return RetrievalFeedback{}, err
	}
	return recorded, nil
}

// ApplyRetrievalFeedback applies the lifecycle impact for already-recorded
// retrieval feedback. Feedback without an explicit used ref is informational.
func ApplyRetrievalFeedback(ctx context.Context, store RetrievalFeedbackApplyStore, feedback RetrievalFeedback, opts ...ComputeImpactOption) error {
	event, ok, err := ContextEventFromRetrievalFeedback(feedback)
	if err != nil {
		return fmt.Errorf("apply retrieval feedback: %w", err)
	}
	if !ok {
		return nil
	}

	if err := appendContextEventOnce(ctx, store, event); err != nil {
		return fmt.Errorf("apply retrieval feedback: %w", err)
	}
	if err := ApplyInvalidation(ctx, store, event, opts...); err != nil {
		return fmt.Errorf("apply retrieval feedback: apply event %s: %w", event.ID, err)
	}
	return nil
}

// RetrievalFeedbackKindHasLifecycleImpact reports whether feedback of kind can
// produce claim lifecycle effects when it carries explicit UsedRefs.
func RetrievalFeedbackKindHasLifecycleImpact(kind RetrievalFeedbackKind) bool {
	_, ok := feedbackImpactEventKind(kind)
	return ok
}

func recordRetrievalFeedbackOnce(ctx context.Context, store RetrievalFeedbackEffectStore, feedback RetrievalFeedback) (RetrievalFeedback, error) {
	recorded, err := store.RecordRetrievalFeedback(ctx, feedback)
	if err == nil {
		return recorded, nil
	}

	existing, getErr := store.GetRetrievalFeedback(ctx, feedback.ID)
	if getErr != nil {
		return RetrievalFeedback{}, fmt.Errorf("record retrieval feedback with effects: record: %w", err)
	}
	if !sameRetrievalFeedback(existing, feedback) {
		return RetrievalFeedback{}, fmt.Errorf("record retrieval feedback with effects: feedback %q already exists with different content", feedback.ID)
	}
	return existing, nil
}

func appendContextEventOnce(ctx context.Context, store ContextEventAppendStore, event ContextEvent) error {
	_, appendErr := store.AppendEvent(ctx, event)
	if appendErr == nil {
		return nil
	}

	events, err := store.ListEvents(ctx, EventFilter{
		ID:          event.ID,
		WorkspaceID: event.WorkspaceID,
		Limit:       1,
	})
	if err != nil {
		return fmt.Errorf("list events after append failure %s: %w", event.ID, err)
	}
	for _, existing := range events {
		if existing.ID != event.ID {
			continue
		}
		if !sameContextEvent(existing, event) {
			return fmt.Errorf("event %q already exists with different content", event.ID)
		}
		return nil
	}
	return fmt.Errorf("append event %s: %w", event.ID, appendErr)
}

// ContextEventFromRetrievalFeedback adapts feedback with explicit claim use
// into the existing event/impact lifecycle path.
func ContextEventFromRetrievalFeedback(feedback RetrievalFeedback) (ContextEvent, bool, error) {
	if err := feedback.Validate(); err != nil {
		return ContextEvent{}, false, err
	}
	if len(feedback.UsedRefs) == 0 {
		return ContextEvent{}, false, nil
	}

	kind, ok := feedbackImpactEventKind(feedback.Kind)
	if !ok {
		return ContextEvent{}, false, nil
	}

	data := map[string]any{
		"feedback_id":   feedback.ID,
		"feedback_kind": string(feedback.Kind),
		"episode_id":    feedback.EpisodeID,
		"query":         feedback.Query,
	}
	if feedback.CorrectionStmt != "" {
		data["correction"] = feedback.CorrectionStmt
	}
	if feedback.GapStmt != "" {
		data["gap"] = feedback.GapStmt
	}

	return ContextEvent{
		ID:          "event-retrieval-feedback-" + feedback.ID,
		WorkspaceID: feedback.WorkspaceID,
		Kind:        kind,
		Source:      retrievalFeedbackEventSource,
		Refs:        feedback.UsedRefs,
		Data:        data,
		CreatedAt:   feedback.CreatedAt,
	}, true, nil
}

func feedbackImpactEventKind(kind RetrievalFeedbackKind) (ContextEventKind, bool) {
	switch kind {
	case RetrievalFeedbackKindAnswerCorrected:
		return EventKindAnswerCorrected, true
	case RetrievalFeedbackKindStaleContextUsed:
		return EventKindMemoryClaimRevalidate, true
	case RetrievalFeedbackKindAnswerAccepted:
		return EventKindMemoryClaimPromoted, true
	default:
		return "", false
	}
}

func sameRetrievalFeedback(existing, incoming RetrievalFeedback) bool {
	if existing.ID != incoming.ID ||
		existing.WorkspaceID != incoming.WorkspaceID ||
		existing.EpisodeID != incoming.EpisodeID ||
		existing.Kind != incoming.Kind ||
		existing.Query != incoming.Query ||
		existing.GapStmt != incoming.GapStmt ||
		existing.CorrectionStmt != incoming.CorrectionStmt ||
		!sameEvidenceRefs(existing.UsedRefs, incoming.UsedRefs) {
		return false
	}
	if !incoming.CreatedAt.IsZero() && !existing.CreatedAt.Equal(incoming.CreatedAt) {
		return false
	}
	return true
}

func sameContextEvent(existing, incoming ContextEvent) bool {
	return existing.ID == incoming.ID &&
		existing.WorkspaceID == incoming.WorkspaceID &&
		existing.Kind == incoming.Kind &&
		existing.Source == incoming.Source &&
		existing.TaskID == incoming.TaskID &&
		existing.SessionID == incoming.SessionID &&
		sameEvidenceRefs(existing.Refs, incoming.Refs) &&
		sameEventData(existing.Data, incoming.Data)
}

func sameEvidenceRefs(left, right []EvidenceRef) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !left[i].Equal(right[i]) {
			return false
		}
	}
	return true
}

func sameEventData(left, right map[string]any) bool {
	return reflect.DeepEqual(left, right)
}
