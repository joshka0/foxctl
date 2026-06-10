package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/rlm"
	contextstore "github.com/joshka0/foxctl/internal/storage/contextengine"
)

func TestRecordRLMAnswerFeedbackNoFlagsNoops(t *testing.T) {
	t.Parallel()

	record, err := recordRLMAnswerFeedback(context.Background(), nil, t.TempDir(), rlm.Task{}, rlm.Result{
		Metadata: map[string]any{
			"answer_used_evidence_refs": []string{"memory_claim:claim-ignored"},
		},
	}, rlmAnswerFeedbackOptions{})
	if err != nil {
		t.Fatalf("recordRLMAnswerFeedback: %v", err)
	}
	if record != nil {
		t.Fatalf("record=%#v want nil when no answer feedback flags are set", record)
	}
}

func TestRecordRLMAnswerFeedbackRequiresKindEpisodeAndRefSource(t *testing.T) {
	t.Parallel()

	_, err := recordRLMAnswerFeedback(context.Background(), nil, t.TempDir(), rlm.Task{}, rlm.Result{
		Metadata: map[string]any{
			"answer_used_evidence_refs": []string{"memory_claim:claim-1"},
		},
	}, rlmAnswerFeedbackOptions{EpisodeID: "episode-1"})
	if err == nil {
		t.Fatal("expected error without --answer-feedback-kind")
	}
	if !strings.Contains(err.Error(), "--answer-feedback-kind is required") {
		t.Fatalf("error=%q", err)
	}

	_, err = recordRLMAnswerFeedback(context.Background(), nil, t.TempDir(), rlm.Task{}, rlm.Result{
		Metadata: map[string]any{
			"answer_used_evidence_refs": []string{"memory_claim:claim-1"},
		},
	}, rlmAnswerFeedbackOptions{Kind: string(contextengine.RetrievalFeedbackKindAnswerAccepted)})
	if err == nil {
		t.Fatal("expected error without --answer-feedback-episode-id")
	}
	if !strings.Contains(err.Error(), "--answer-feedback-episode-id is required") {
		t.Fatalf("error=%q", err)
	}

	_, err = recordRLMAnswerFeedback(context.Background(), nil, t.TempDir(), rlm.Task{}, rlm.Result{
		Metadata: map[string]any{
			"answer_used_evidence_refs": []string{"memory_claim:claim-1"},
		},
	}, rlmAnswerFeedbackOptions{
		EpisodeID: "episode-1",
		Kind:      string(contextengine.RetrievalFeedbackKindAnswerAccepted),
	})
	if err == nil {
		t.Fatal("expected error without explicit ref source")
	}
	if !strings.Contains(err.Error(), "--answer-feedback-used-ref or --answer-feedback-use-answer-refs is required") {
		t.Fatalf("error=%q", err)
	}
}

func TestRecordRLMAnswerFeedbackRejectsInvalidKindAndRefs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	store, err := contextstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open contextengine store: %v", err)
	}
	defer store.Close()

	_, err = recordRLMAnswerFeedback(ctx, store, t.TempDir(), rlm.Task{}, rlm.Result{}, rlmAnswerFeedbackOptions{
		EpisodeID:     "episode-invalid-kind",
		Kind:          "bogus",
		UsedRefs:      []string{"memory_claim:claim-1"},
		UseAnswerRefs: false,
	})
	if err == nil {
		t.Fatal("expected invalid kind error")
	}
	if !strings.Contains(err.Error(), "--answer-feedback-kind must be a valid retrieval feedback kind") {
		t.Fatalf("error=%q", err)
	}

	_, err = recordRLMAnswerFeedback(ctx, store, t.TempDir(), rlm.Task{Prompt: "query"}, rlm.Result{
		Metadata: map[string]any{
			"answer_used_evidence_refs": []string{"not-a-ref"},
		},
	}, rlmAnswerFeedbackOptions{
		EpisodeID:     "episode-invalid-ref",
		Kind:          string(contextengine.RetrievalFeedbackKindAnswerAccepted),
		UseAnswerRefs: true,
	})
	if err == nil {
		t.Fatal("expected invalid answer-used ref error")
	}
	if !strings.Contains(err.Error(), "--answer-feedback-used-ref[0]") {
		t.Fatalf("error=%q want answer-feedback-used-ref parse context", err)
	}
	feedback, err := store.ListRetrievalFeedback(ctx, contextengine.RetrievalFeedbackFilter{})
	if err != nil {
		t.Fatalf("list feedback: %v", err)
	}
	if len(feedback) != 0 {
		t.Fatalf("feedback rows=%#v want none after invalid inputs", feedback)
	}
}

func TestRecordRLMAnswerFeedbackRejectsAnswerRefsForLifecycleKinds(t *testing.T) {
	t.Parallel()

	for _, kind := range []contextengine.RetrievalFeedbackKind{
		contextengine.RetrievalFeedbackKindAnswerCorrected,
		contextengine.RetrievalFeedbackKindStaleContextUsed,
	} {
		t.Run(string(kind), func(t *testing.T) {
			_, err := recordRLMAnswerFeedback(context.Background(), nil, t.TempDir(), rlm.Task{}, rlm.Result{
				Metadata: map[string]any{
					"answer_used_evidence_refs": []string{"memory_claim:claim-1"},
				},
			}, rlmAnswerFeedbackOptions{
				EpisodeID:     "episode-1",
				Kind:          string(kind),
				UseAnswerRefs: true,
			})
			if err == nil {
				t.Fatal("expected lifecycle kind to reject answer-derived refs")
			}
			if !strings.Contains(err.Error(), "--answer-feedback-used-ref is required for lifecycle-impacting answer feedback kind") {
				t.Fatalf("error=%q", err)
			}
		})
	}
}

func TestRecordRLMAnswerFeedbackCanPersistInformationalAnswerRefs(t *testing.T) {
	t.Parallel()

	for _, kind := range []contextengine.RetrievalFeedbackKind{
		contextengine.RetrievalFeedbackKindEvidenceUsed,
		contextengine.RetrievalFeedbackKindAnswerAccepted,
		contextengine.RetrievalFeedbackKindGapCreated,
	} {
		t.Run(string(kind), func(t *testing.T) {
			ctx := context.Background()
			cfg := config.Config{}
			cfg.Storage.Root = t.TempDir()
			workspacePath := t.TempDir()
			workspaceID := ws.CanonicalID(workspacePath)

			store, err := contextstore.Open(ctx, cfg.Storage.Root)
			if err != nil {
				t.Fatalf("open contextengine store: %v", err)
			}
			defer store.Close()

			record, err := recordRLMAnswerFeedback(ctx, store, workspacePath, rlm.Task{Prompt: "what was used"}, rlm.Result{
				EvidenceRefs: []string{"memory_claim:bootstrap-claim"},
				Metadata: map[string]any{
					"tool_surfaced_evidence_refs": []string{"memory_claim:retrieved-only"},
					"answer_used_evidence_refs":   []string{"memory_claim:answer-cited", "memory_claim:answer-cited"},
				},
			}, rlmAnswerFeedbackOptions{
				EpisodeID:     "episode-answer-used-informational-" + string(kind),
				Kind:          string(kind),
				GapStmt:       "The answer exposed a missing fact.",
				UseAnswerRefs: true,
			})
			if err != nil {
				t.Fatalf("recordRLMAnswerFeedback: %v", err)
			}
			if record == nil || !record.Enabled || !record.Applied || record.Source != "answer_used_evidence_refs" || record.Feedback == nil {
				t.Fatalf("record=%#v want applied informational answer refs", record)
			}
			feedback, err := store.ListRetrievalFeedback(ctx, contextengine.RetrievalFeedbackFilter{WorkspaceID: workspaceID})
			if err != nil {
				t.Fatalf("list feedback: %v", err)
			}
			if len(feedback) != 1 {
				t.Fatalf("feedback rows=%#v want one informational row", feedback)
			}
			if len(feedback[0].UsedRefs) != 1 || contextengine.FormatEvidenceRef(feedback[0].UsedRefs[0]) != "memory_claim:answer-cited" {
				t.Fatalf("used refs=%#v want unique answer-cited ref", feedback[0].UsedRefs)
			}
			events, err := store.ListEvents(ctx, contextengine.EventFilter{WorkspaceID: workspaceID})
			if err != nil {
				t.Fatalf("list events: %v", err)
			}
			if len(events) != 0 {
				t.Fatalf("informational answer feedback appended lifecycle events: %#v", events)
			}
		})
	}
}

func TestRecordRLMAnswerFeedbackDoesNotPersistWithoutAnswerUsedRefs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	workspacePath := t.TempDir()
	workspaceID := ws.CanonicalID(workspacePath)

	store, err := contextstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open contextengine store: %v", err)
	}
	defer store.Close()

	record, err := recordRLMAnswerFeedback(ctx, store, workspacePath, rlm.Task{Prompt: "what was used"}, rlm.Result{
		EvidenceRefs: []string{"memory_claim:bootstrap-claim", "   "},
		Metadata: map[string]any{
			"tool_surfaced_evidence_refs": []string{"memory_claim:retrieved-only", "not-a-ref"},
			"answer_used_evidence_refs":   []string{" ", ""},
		},
	}, rlmAnswerFeedbackOptions{
		EpisodeID:     "episode-no-answer-used-refs",
		Kind:          string(contextengine.RetrievalFeedbackKindAnswerAccepted),
		UseAnswerRefs: true,
	})
	if err != nil {
		t.Fatalf("recordRLMAnswerFeedback: %v", err)
	}
	if record == nil || !record.Enabled || record.Applied || record.Reason != "no_answer_feedback_refs" {
		t.Fatalf("record=%#v want no-op with explicit reason", record)
	}
	feedback, err := store.ListRetrievalFeedback(ctx, contextengine.RetrievalFeedbackFilter{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatalf("list feedback: %v", err)
	}
	if len(feedback) != 0 {
		t.Fatalf("feedback rows=%#v want none", feedback)
	}
}

func TestRecordRLMAnswerFeedbackDryRunDoesNotPersist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	workspacePath := t.TempDir()
	workspaceID := ws.CanonicalID(workspacePath)

	store, err := contextstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open contextengine store: %v", err)
	}
	defer store.Close()
	claim := contextengine.MemoryClaim{
		ID:          "claim-rlm-dry-run",
		WorkspaceID: workspaceID,
		ClaimType:   "semantic_fact",
		Status:      contextengine.ClaimStatusCurrent,
		Summary:     "Dry-run answer feedback should not change this claim.",
	}
	if _, err := store.UpsertClaim(ctx, claim); err != nil {
		t.Fatalf("upsert claim: %v", err)
	}

	record, err := recordRLMAnswerFeedback(ctx, store, workspacePath, rlm.Task{Prompt: "query"}, rlm.Result{}, rlmAnswerFeedbackOptions{
		EpisodeID:      "episode-dry-run",
		Kind:           string(contextengine.RetrievalFeedbackKindAnswerCorrected),
		UsedRefs:       []string{"memory_claim:" + claim.ID},
		CorrectionStmt: "Preview correction.",
		DryRun:         true,
	})
	if err != nil {
		t.Fatalf("recordRLMAnswerFeedback: %v", err)
	}
	if record == nil || !record.Enabled || !record.DryRun || record.Applied || record.Feedback == nil {
		t.Fatalf("record=%#v want dry-run feedback preview", record)
	}
	feedback, err := store.ListRetrievalFeedback(ctx, contextengine.RetrievalFeedbackFilter{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatalf("list feedback: %v", err)
	}
	if len(feedback) != 0 {
		t.Fatalf("dry-run feedback rows=%#v want none", feedback)
	}
	got, err := store.GetClaim(ctx, claim.ID)
	if err != nil {
		t.Fatalf("get claim: %v", err)
	}
	if got.Status != contextengine.ClaimStatusCurrent {
		t.Fatalf("claim status=%q want current", got.Status)
	}
	events, err := store.ListEvents(ctx, contextengine.EventFilter{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("dry-run events=%#v want none", events)
	}
}

func TestRecordRLMAnswerFeedbackRetryIsIdempotentWithReorderedRefs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	workspacePath := t.TempDir()
	workspaceID := ws.CanonicalID(workspacePath)
	store, err := contextstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open contextengine store: %v", err)
	}
	defer store.Close()
	for _, claimID := range []string{"claim-a", "claim-b"} {
		claim := contextengine.MemoryClaim{
			ID:          claimID,
			WorkspaceID: workspaceID,
			ClaimType:   "semantic_fact",
			Status:      contextengine.ClaimStatusCurrent,
			Summary:     "Retry feedback should remain idempotent.",
		}
		if _, err := store.UpsertClaim(ctx, claim); err != nil {
			t.Fatalf("upsert claim %s: %v", claimID, err)
		}
	}
	task := rlm.Task{Prompt: "query"}
	opts := rlmAnswerFeedbackOptions{
		EpisodeID:      "episode-retry",
		Kind:           string(contextengine.RetrievalFeedbackKindAnswerCorrected),
		Query:          "query",
		UsedRefs:       []string{"memory_claim:claim-b", "memory_claim:claim-a"},
		CorrectionStmt: "Both claims need revalidation.",
	}
	first, err := recordRLMAnswerFeedback(ctx, store, workspacePath, task, rlm.Result{}, opts)
	if err != nil {
		t.Fatalf("first recordRLMAnswerFeedback: %v", err)
	}
	opts.UsedRefs = []string{"memory_claim:claim-a", "memory_claim:claim-b"}
	second, err := recordRLMAnswerFeedback(ctx, store, workspacePath, task, rlm.Result{}, opts)
	if err != nil {
		t.Fatalf("second recordRLMAnswerFeedback: %v", err)
	}
	if first.Feedback == nil || second.Feedback == nil || first.Feedback.ID != second.Feedback.ID {
		t.Fatalf("feedback IDs first=%v second=%v", first.Feedback, second.Feedback)
	}
	feedback, err := store.ListRetrievalFeedback(ctx, contextengine.RetrievalFeedbackFilter{EpisodeID: opts.EpisodeID})
	if err != nil {
		t.Fatalf("list feedback: %v", err)
	}
	if len(feedback) != 1 {
		t.Fatalf("feedback rows=%#v want one idempotent row", feedback)
	}
	if len(feedback[0].UsedRefs) != 2 {
		t.Fatalf("stored refs=%#v want two refs", feedback[0].UsedRefs)
	}
	if got := contextengine.FormatEvidenceRef(feedback[0].UsedRefs[0]); got != "memory_claim:claim-a" {
		t.Fatalf("first stored ref=%q want sorted claim-a", got)
	}
	if got := contextengine.FormatEvidenceRef(feedback[0].UsedRefs[1]); got != "memory_claim:claim-b" {
		t.Fatalf("second stored ref=%q want sorted claim-b", got)
	}
	events, err := store.ListEvents(ctx, contextengine.EventFilter{
		WorkspaceID: workspaceID,
		Kind:        contextengine.EventKindAnswerCorrected,
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || first.Feedback == nil || events[0].Data["feedback_id"] != first.Feedback.ID {
		t.Fatalf("events=%#v want one idempotent feedback event for %v", events, first.Feedback)
	}
}
