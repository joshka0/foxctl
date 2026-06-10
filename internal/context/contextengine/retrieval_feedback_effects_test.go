package contextengine

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errInjectedFeedbackTest = errors.New("injected feedback effect failure")

func TestContextEventFromRetrievalFeedback_CorrectionUsesExistingSignal(t *testing.T) {
	t.Parallel()

	feedback := retrievalFeedbackForEffects("fb-correct", RetrievalFeedbackKindAnswerCorrected)
	feedback.UsedRefs = []EvidenceRef{{Type: RefTypeMemoryClaim, Ref: "claim-1"}}
	feedback.CorrectionStmt = "The durable fact should be rechecked"

	event, ok, err := ContextEventFromRetrievalFeedback(feedback)
	if err != nil {
		t.Fatalf("ContextEventFromRetrievalFeedback: %v", err)
	}
	if !ok {
		t.Fatalf("expected feedback to produce an event")
	}
	if event.Kind != EventKindAnswerCorrected {
		t.Fatalf("event.Kind=%q want %q", event.Kind, EventKindAnswerCorrected)
	}
	if event.Source != retrievalFeedbackEventSource {
		t.Fatalf("event.Source=%q want %q", event.Source, retrievalFeedbackEventSource)
	}
	if len(event.Refs) != 1 || !event.Refs[0].Equal(feedback.UsedRefs[0]) {
		t.Fatalf("event refs=%v want %v", event.Refs, feedback.UsedRefs)
	}
	if event.Data["feedback_id"] != feedback.ID {
		t.Fatalf("feedback_id data=%v want %q", event.Data["feedback_id"], feedback.ID)
	}
}

func TestContextEventFromRetrievalFeedback_NonLifecycleFeedbackIsInformational(t *testing.T) {
	t.Parallel()

	for _, kind := range []RetrievalFeedbackKind{
		RetrievalFeedbackKindEvidenceUsed,
		RetrievalFeedbackKindAnswerAccepted,
		RetrievalFeedbackKindRetrievalMissed,
		RetrievalFeedbackKindWrongFileRetrieved,
		RetrievalFeedbackKindGapCreated,
	} {
		t.Run(string(kind), func(t *testing.T) {
			feedback := retrievalFeedbackForEffects("fb-"+string(kind), kind)
			feedback.UsedRefs = []EvidenceRef{{Type: RefTypeMemoryClaim, Ref: "claim-1"}}

			event, ok, err := ContextEventFromRetrievalFeedback(feedback)
			if err != nil {
				t.Fatalf("ContextEventFromRetrievalFeedback: %v", err)
			}
			if ok {
				t.Fatalf("expected no lifecycle event, got %#v", event)
			}
		})
	}
}

func TestRetrievalFeedbackKindHasLifecycleImpact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind string
		want bool
	}{
		{string(RetrievalFeedbackKindEvidenceUsed), false},
		{string(RetrievalFeedbackKindAnswerAccepted), false},
		{string(RetrievalFeedbackKindAnswerCorrected), true},
		{string(RetrievalFeedbackKindRetrievalMissed), false},
		{string(RetrievalFeedbackKindWrongFileRetrieved), false},
		{string(RetrievalFeedbackKindStaleContextUsed), true},
		{string(RetrievalFeedbackKindGapCreated), false},
		{"bogus", false},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got := RetrievalFeedbackKindHasLifecycleImpact(RetrievalFeedbackKind(tt.kind))
			if got != tt.want {
				t.Fatalf("RetrievalFeedbackKindHasLifecycleImpact(%q)=%v want %v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestContextEventFromRetrievalFeedback_LifecycleFeedbackWithoutUsedRefsIsInformational(t *testing.T) {
	t.Parallel()

	for _, kind := range []RetrievalFeedbackKind{
		RetrievalFeedbackKindAnswerCorrected,
		RetrievalFeedbackKindStaleContextUsed,
	} {
		t.Run(string(kind), func(t *testing.T) {
			feedback := retrievalFeedbackForEffects("fb-empty-"+string(kind), kind)

			event, ok, err := ContextEventFromRetrievalFeedback(feedback)
			if err != nil {
				t.Fatalf("ContextEventFromRetrievalFeedback: %v", err)
			}
			if ok {
				t.Fatalf("expected no lifecycle event without used refs, got %#v", event)
			}
		})
	}
}

func TestRecordRetrievalFeedbackWithEffects_CorrectedDirectClaimNeedsRevalidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewMemoryStore()
	claim := memoryClaimForFeedbackEffects("claim-corrected", ClaimStatusCurrent)
	if _, err := store.UpsertClaim(ctx, claim); err != nil {
		t.Fatalf("upsert claim: %v", err)
	}

	feedback := retrievalFeedbackForEffects("fb-corrected", RetrievalFeedbackKindAnswerCorrected)
	feedback.UsedRefs = []EvidenceRef{{Type: RefTypeMemoryClaim, Ref: claim.ID}}
	feedback.CorrectionStmt = "The claim was contradicted by the user"

	if _, err := RecordRetrievalFeedbackWithEffects(ctx, store, feedback); err != nil {
		t.Fatalf("RecordRetrievalFeedbackWithEffects: %v", err)
	}

	got, err := store.GetClaim(ctx, claim.ID)
	if err != nil {
		t.Fatalf("get claim: %v", err)
	}
	if got.Status != ClaimStatusNeedsRevalidation {
		t.Fatalf("claim status=%q want %q", got.Status, ClaimStatusNeedsRevalidation)
	}

	target := EvidenceRef{Type: RefTypeMemoryClaim, Ref: claim.ID}
	markers, err := store.ListStaleness(ctx, StalenessFilter{
		WorkspaceID: claim.WorkspaceID,
		TargetRef:   &target,
		Status:      StalenessStatusNeedsRevalidation,
	})
	if err != nil {
		t.Fatalf("list staleness: %v", err)
	}
	if len(markers) != 1 {
		t.Fatalf("needs_revalidation markers=%d want 1", len(markers))
	}
}

func TestRecordRetrievalFeedbackWithEffects_CorrectedCandidateNeedsRevalidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewMemoryStore()
	claim := memoryClaimForFeedbackEffects("claim-corrected-candidate", ClaimStatusCandidate)
	if _, err := store.UpsertClaim(ctx, claim); err != nil {
		t.Fatalf("upsert claim: %v", err)
	}

	feedback := retrievalFeedbackForEffects("fb-corrected-candidate", RetrievalFeedbackKindAnswerCorrected)
	feedback.UsedRefs = []EvidenceRef{{Type: RefTypeMemoryClaim, Ref: claim.ID}}
	feedback.CorrectionStmt = "The candidate was contradicted before validation"

	if _, err := RecordRetrievalFeedbackWithEffects(ctx, store, feedback); err != nil {
		t.Fatalf("RecordRetrievalFeedbackWithEffects: %v", err)
	}

	got, err := store.GetClaim(ctx, claim.ID)
	if err != nil {
		t.Fatalf("get claim: %v", err)
	}
	if got.Status != ClaimStatusNeedsRevalidation {
		t.Fatalf("claim status=%q want %q", got.Status, ClaimStatusNeedsRevalidation)
	}
}

func TestRecordRetrievalFeedbackWithEffects_AcceptedDoesNotPromoteCandidate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewMemoryStore()
	claim := memoryClaimForFeedbackEffects("claim-candidate", ClaimStatusCandidate)
	if _, err := store.UpsertClaim(ctx, claim); err != nil {
		t.Fatalf("upsert claim: %v", err)
	}

	feedback := retrievalFeedbackForEffects("fb-accepted", RetrievalFeedbackKindAnswerAccepted)
	feedback.UsedRefs = []EvidenceRef{{Type: RefTypeMemoryClaim, Ref: claim.ID}}

	if _, err := RecordRetrievalFeedbackWithEffects(ctx, store, feedback); err != nil {
		t.Fatalf("RecordRetrievalFeedbackWithEffects: %v", err)
	}

	got, err := store.GetClaim(ctx, claim.ID)
	if err != nil {
		t.Fatalf("get claim: %v", err)
	}
	if got.Status != ClaimStatusCandidate {
		t.Fatalf("claim status=%q want %q", got.Status, ClaimStatusCandidate)
	}

	events, err := store.ListEvents(ctx, EventFilter{WorkspaceID: claim.WorkspaceID})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("accepted feedback should not append lifecycle events, got %d", len(events))
	}
}

func TestRecordRetrievalFeedbackWithEffects_StaleContextMarksOnlyUsedClaim(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewMemoryStore()
	used := memoryClaimForFeedbackEffects("claim-used", ClaimStatusCurrent)
	unrelated := memoryClaimForFeedbackEffects("claim-unrelated", ClaimStatusCurrent)
	if _, err := store.UpsertClaim(ctx, used); err != nil {
		t.Fatalf("upsert used claim: %v", err)
	}
	if _, err := store.UpsertClaim(ctx, unrelated); err != nil {
		t.Fatalf("upsert unrelated claim: %v", err)
	}

	feedback := retrievalFeedbackForEffects("fb-stale", RetrievalFeedbackKindStaleContextUsed)
	feedback.UsedRefs = []EvidenceRef{{Type: RefTypeMemoryClaim, Ref: used.ID}}

	if _, err := RecordRetrievalFeedbackWithEffects(ctx, store, feedback); err != nil {
		t.Fatalf("RecordRetrievalFeedbackWithEffects: %v", err)
	}

	gotUsed, err := store.GetClaim(ctx, used.ID)
	if err != nil {
		t.Fatalf("get used claim: %v", err)
	}
	if gotUsed.Status != ClaimStatusNeedsRevalidation {
		t.Fatalf("used claim status=%q want %q", gotUsed.Status, ClaimStatusNeedsRevalidation)
	}
	if gotUsed.Reason == "" || gotUsed.Reason == "user correction: event-retrieval-feedback-fb-stale" {
		t.Fatalf("used claim reason=%q, want stale feedback revalidation reason", gotUsed.Reason)
	}

	events, err := store.ListEvents(ctx, EventFilter{WorkspaceID: used.WorkspaceID})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%d want 1", len(events))
	}
	if events[0].Kind != EventKindMemoryClaimRevalidate {
		t.Fatalf("event kind=%q want %q", events[0].Kind, EventKindMemoryClaimRevalidate)
	}
	if events[0].Data["feedback_kind"] != string(RetrievalFeedbackKindStaleContextUsed) {
		t.Fatalf("event feedback_kind=%v want %q", events[0].Data["feedback_kind"], RetrievalFeedbackKindStaleContextUsed)
	}

	gotUnrelated, err := store.GetClaim(ctx, unrelated.ID)
	if err != nil {
		t.Fatalf("get unrelated claim: %v", err)
	}
	if gotUnrelated.Status != ClaimStatusCurrent {
		t.Fatalf("unrelated claim status=%q want %q", gotUnrelated.Status, ClaimStatusCurrent)
	}
}

func TestRecordRetrievalFeedbackWithEffects_RetryRecoversAfterPartialApply(t *testing.T) {
	ctx := context.Background()
	store := &failOnceStalenessStore{MemoryStore: NewMemoryStore()}
	claim := memoryClaimForFeedbackEffects("claim-retry", ClaimStatusCurrent)
	if _, err := store.UpsertClaim(ctx, claim); err != nil {
		t.Fatalf("upsert claim: %v", err)
	}

	feedback := retrievalFeedbackForEffects("fb-retry", RetrievalFeedbackKindAnswerCorrected)
	feedback.UsedRefs = []EvidenceRef{{Type: RefTypeMemoryClaim, Ref: claim.ID}}
	feedback.CorrectionStmt = "The first apply fails after feedback/event persistence"

	if _, err := RecordRetrievalFeedbackWithEffects(ctx, store, feedback); err == nil {
		t.Fatalf("first RecordRetrievalFeedbackWithEffects should fail")
	}
	if _, err := store.GetRetrievalFeedback(ctx, feedback.ID); err != nil {
		t.Fatalf("feedback should be persisted before retry: %v", err)
	}
	events, err := store.ListEvents(ctx, EventFilter{WorkspaceID: feedback.WorkspaceID})
	if err != nil {
		t.Fatalf("list events after first failure: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events after first failure=%d want 1", len(events))
	}

	if _, err := RecordRetrievalFeedbackWithEffects(ctx, store, feedback); err != nil {
		t.Fatalf("retry RecordRetrievalFeedbackWithEffects: %v", err)
	}
	got, err := store.GetClaim(ctx, claim.ID)
	if err != nil {
		t.Fatalf("get claim: %v", err)
	}
	if got.Status != ClaimStatusNeedsRevalidation {
		t.Fatalf("claim status=%q want %q", got.Status, ClaimStatusNeedsRevalidation)
	}
}

func TestRecordRetrievalFeedbackWithEffects_ClaimTransitionFailureIsReturnedAndRetryable(t *testing.T) {
	ctx := context.Background()
	store := &failOnceClaimTransitionStore{MemoryStore: NewMemoryStore()}
	claim := memoryClaimForFeedbackEffects("claim-transition-retry", ClaimStatusCurrent)
	if _, err := store.UpsertClaim(ctx, claim); err != nil {
		t.Fatalf("upsert claim: %v", err)
	}

	feedback := retrievalFeedbackForEffects("fb-transition-retry", RetrievalFeedbackKindAnswerCorrected)
	feedback.UsedRefs = []EvidenceRef{{Type: RefTypeMemoryClaim, Ref: claim.ID}}
	feedback.CorrectionStmt = "The claim transition write fails once after feedback/event persistence"

	if _, err := RecordRetrievalFeedbackWithEffects(ctx, store, feedback); err == nil {
		t.Fatalf("first RecordRetrievalFeedbackWithEffects should fail when claim transition persistence fails")
	}
	got, err := store.GetClaim(ctx, claim.ID)
	if err != nil {
		t.Fatalf("get claim after first failure: %v", err)
	}
	if got.Status != ClaimStatusCurrent {
		t.Fatalf("claim status after failed transition=%q want %q", got.Status, ClaimStatusCurrent)
	}

	if _, err := RecordRetrievalFeedbackWithEffects(ctx, store, feedback); err != nil {
		t.Fatalf("retry RecordRetrievalFeedbackWithEffects: %v", err)
	}
	got, err = store.GetClaim(ctx, claim.ID)
	if err != nil {
		t.Fatalf("get claim after retry: %v", err)
	}
	if got.Status != ClaimStatusNeedsRevalidation {
		t.Fatalf("claim status after retry=%q want %q", got.Status, ClaimStatusNeedsRevalidation)
	}
}

func TestSameContextEventHandlesStructuredData(t *testing.T) {
	event := ContextEvent{
		ID:          "event-structured",
		WorkspaceID: "ws-feedback",
		Kind:        EventKindAnswerCorrected,
		Source:      retrievalFeedbackEventSource,
		Data: map[string]any{
			"feedback_id": "fb-structured",
			"tags":        []string{"scope", "retry"},
			"metadata":    map[string]any{"source": "test"},
		},
	}
	if !sameContextEvent(event, event) {
		t.Fatal("same structured context event should compare equal")
	}
	changed := event
	changed.Data = map[string]any{
		"feedback_id": "fb-structured",
		"tags":        []string{"scope"},
		"metadata":    map[string]any{"source": "test"},
	}
	if sameContextEvent(event, changed) {
		t.Fatal("different structured context event data should not compare equal")
	}
}

func retrievalFeedbackForEffects(id string, kind RetrievalFeedbackKind) RetrievalFeedback {
	return RetrievalFeedback{
		ID:          id,
		WorkspaceID: "ws-feedback",
		EpisodeID:   "episode-feedback",
		Kind:        kind,
		Query:       "what should the agent remember",
		CreatedAt:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}
}

type failOnceStalenessStore struct {
	*MemoryStore
	failed bool
}

func (s *failOnceStalenessStore) UpsertStaleness(ctx context.Context, marker StalenessMarker) (StalenessMarker, error) {
	if !s.failed {
		s.failed = true
		return StalenessMarker{}, errInjectedFeedbackTest
	}
	return s.MemoryStore.UpsertStaleness(ctx, marker)
}

type failOnceClaimTransitionStore struct {
	*MemoryStore
	failed bool
}

func (s *failOnceClaimTransitionStore) UpsertClaim(ctx context.Context, claim MemoryClaim) (MemoryClaim, error) {
	if claim.Status == ClaimStatusNeedsRevalidation && !s.failed {
		s.failed = true
		return MemoryClaim{}, errInjectedFeedbackTest
	}
	return s.MemoryStore.UpsertClaim(ctx, claim)
}

func memoryClaimForFeedbackEffects(id string, status ClaimStatus) MemoryClaim {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	return MemoryClaim{
		ID:          id,
		WorkspaceID: "ws-feedback",
		ClaimType:   "fact",
		Status:      status,
		Summary:     "The agent should recheck explicit feedback",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
