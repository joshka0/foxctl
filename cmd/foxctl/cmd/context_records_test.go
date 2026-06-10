package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	contextstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	"github.com/spf13/cobra"
)

func TestContextRecordCommandFlags(t *testing.T) {
	tests := []struct {
		name  string
		cmd   func() *cobra.Command
		flags []string
	}{
		{
			name: "capture",
			cmd:  newCaptureCommand,
			flags: []string{
				"workspace", "task-id", "phase", "outcome", "summary",
				"evidence-ref", "file-touched", "observation", "tension", "next-action", "promotion-candidate", "dry-run",
			},
		},
		{
			name:  "observe",
			cmd:   newObserveCommand,
			flags: []string{"workspace", "statement", "confidence", "count", "project", "area", "evidence-ref", "dry-run"},
		},
		{
			name:  "tension",
			cmd:   newTensionCommand,
			flags: []string{"workspace", "kind", "statement", "impact", "status", "related-ref", "dry-run"},
		},
		{
			name: "retrieval-feedback",
			cmd:  newRetrievalFeedbackCommand,
			flags: []string{
				"workspace", "id", "episode-id", "kind", "query",
				"used-ref", "gap", "correction", "dry-run",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmd()
			for _, name := range tt.flags {
				if cmd.Flags().Lookup(name) == nil {
					t.Fatalf("missing flag %q", name)
				}
			}
		})
	}
}

func TestRetrievalFeedbackCommandAppliesClaimEffects(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	workspacePath := t.TempDir()
	workspaceID := ws.CanonicalID(workspacePath)

	store, err := contextstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open contextengine store: %v", err)
	}
	claim := contextengine.MemoryClaim{
		ID:          "claim-cli-feedback",
		WorkspaceID: workspaceID,
		ClaimType:   "semantic_fact",
		Status:      contextengine.ClaimStatusCurrent,
		Summary:     "CLI feedback should revalidate this claim.",
		CreatedAt:   fixedContextRecordsTestTime(),
		UpdatedAt:   fixedContextRecordsTestTime(),
	}
	if _, err := store.UpsertClaim(ctx, claim); err != nil {
		t.Fatalf("upsert claim: %v", err)
	}
	episode := contextengine.RetrievalEpisode{
		ID:          "episode-cli-feedback",
		WorkspaceID: workspaceID,
		Query:       "which claim should be revalidated",
		Lane:        contextengine.LaneMemory,
		CreatedAt:   fixedContextRecordsTestTime(),
	}
	if _, err := store.RecordRetrievalEpisode(ctx, episode); err != nil {
		t.Fatalf("record episode: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}

	out := &bytes.Buffer{}
	cmd := newRetrievalFeedbackCommand()
	cmd.SetContext(config.WithContext(ctx, cfg))
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--episode-id", episode.ID,
		"--kind", string(contextengine.RetrievalFeedbackKindAnswerCorrected),
		"--query", episode.Query,
		"--used-ref", "memory_claim:" + claim.ID,
		"--correction", "The claim is contradicted by fresh evidence.",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	env := decodeTestEnvelope(t, out.Bytes())
	if env.Status != "ok" {
		t.Fatalf("status=%q", env.Status)
	}
	data := envelopeDataMap(t, env)
	if data["applied"] != true || data["dry_run"] != false {
		t.Fatalf("unexpected output data: %#v", data)
	}
	feedback := nestedMap(t, data, "feedback")
	feedbackID, _ := feedback["id"].(string)
	if feedbackID == "" {
		t.Fatalf("feedback missing deterministic id: %#v", feedback)
	}

	store, err = contextstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("reopen contextengine store: %v", err)
	}
	defer store.Close()
	got, err := store.GetClaim(ctx, claim.ID)
	if err != nil {
		t.Fatalf("get claim: %v", err)
	}
	if got.Status != contextengine.ClaimStatusNeedsRevalidation {
		t.Fatalf("claim status=%q want %q", got.Status, contextengine.ClaimStatusNeedsRevalidation)
	}
	events, err := store.ListEvents(ctx, contextengine.EventFilter{
		WorkspaceID: workspaceID,
		Kind:        contextengine.EventKindAnswerCorrected,
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || events[0].Data["feedback_id"] != feedbackID {
		t.Fatalf("events=%#v want one derived feedback event", events)
	}
}

func TestRetrievalFeedbackCommandRetryIsIdempotentWithReorderedRefs(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	workspacePath := t.TempDir()
	workspaceID := ws.CanonicalID(workspacePath)

	store, err := contextstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open contextengine store: %v", err)
	}
	for _, claimID := range []string{"claim-cli-feedback-a", "claim-cli-feedback-b"} {
		claim := contextengine.MemoryClaim{
			ID:          claimID,
			WorkspaceID: workspaceID,
			ClaimType:   "semantic_fact",
			Status:      contextengine.ClaimStatusCurrent,
			Summary:     "CLI feedback should be retry safe.",
			CreatedAt:   fixedContextRecordsTestTime(),
			UpdatedAt:   fixedContextRecordsTestTime(),
		}
		if _, err := store.UpsertClaim(ctx, claim); err != nil {
			t.Fatalf("upsert claim %s: %v", claimID, err)
		}
	}
	episode := contextengine.RetrievalEpisode{
		ID:          "episode-cli-feedback-retry",
		WorkspaceID: workspaceID,
		Query:       "which claims should be revalidated on retry",
		Lane:        contextengine.LaneMemory,
		CreatedAt:   fixedContextRecordsTestTime(),
	}
	if _, err := store.RecordRetrievalEpisode(ctx, episode); err != nil {
		t.Fatalf("record episode: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}

	first := executeRetrievalFeedbackCommand(
		t, ctx, cfg,
		"--workspace", workspacePath,
		"--episode-id", episode.ID,
		"--kind", string(contextengine.RetrievalFeedbackKindAnswerCorrected),
		"--query", episode.Query,
		"--used-ref", "memory_claim:claim-cli-feedback-b",
		"--used-ref", "memory_claim:claim-cli-feedback-a",
		"--correction", "Both claims need revalidation.",
	)
	second := executeRetrievalFeedbackCommand(
		t, ctx, cfg,
		"--workspace", workspacePath,
		"--episode-id", episode.ID,
		"--kind", string(contextengine.RetrievalFeedbackKindAnswerCorrected),
		"--query", episode.Query,
		"--used-ref", "memory_claim:claim-cli-feedback-a",
		"--used-ref", "memory_claim:claim-cli-feedback-b",
		"--correction", "Both claims need revalidation.",
	)
	firstID, _ := nestedMap(t, first, "feedback")["id"].(string)
	secondID, _ := nestedMap(t, second, "feedback")["id"].(string)
	if firstID == "" || firstID != secondID {
		t.Fatalf("feedback ids first=%q second=%q", firstID, secondID)
	}

	store, err = contextstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("reopen contextengine store: %v", err)
	}
	defer store.Close()
	feedback, err := store.ListRetrievalFeedback(ctx, contextengine.RetrievalFeedbackFilter{
		WorkspaceID: workspaceID,
		EpisodeID:   episode.ID,
	})
	if err != nil {
		t.Fatalf("list feedback: %v", err)
	}
	if len(feedback) != 1 {
		t.Fatalf("feedback rows=%d want 1: %#v", len(feedback), feedback)
	}
	if len(feedback[0].UsedRefs) != 2 {
		t.Fatalf("stored refs=%#v want 2 refs", feedback[0].UsedRefs)
	}
	if got := contextengine.FormatEvidenceRef(feedback[0].UsedRefs[0]); got != "memory_claim:claim-cli-feedback-a" {
		t.Fatalf("first stored ref=%q want sorted claim a", got)
	}
	events, err := store.ListEvents(ctx, contextengine.EventFilter{
		WorkspaceID: workspaceID,
		Kind:        contextengine.EventKindAnswerCorrected,
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || events[0].Data["feedback_id"] != firstID {
		t.Fatalf("events=%#v want one idempotent feedback event", events)
	}
}

func TestRetrievalFeedbackCommandDryRunDoesNotPersist(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	workspacePath := t.TempDir()
	workspaceID := ws.CanonicalID(workspacePath)

	store, err := contextstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open contextengine store: %v", err)
	}
	claim := contextengine.MemoryClaim{
		ID:          "claim-cli-feedback-dry-run",
		WorkspaceID: workspaceID,
		ClaimType:   "semantic_fact",
		Status:      contextengine.ClaimStatusCurrent,
		Summary:     "Dry run should not change this claim.",
		CreatedAt:   fixedContextRecordsTestTime(),
		UpdatedAt:   fixedContextRecordsTestTime(),
	}
	if _, err := store.UpsertClaim(ctx, claim); err != nil {
		t.Fatalf("upsert claim: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}

	out := &bytes.Buffer{}
	cmd := newRetrievalFeedbackCommand()
	cmd.SetContext(config.WithContext(ctx, cfg))
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--episode-id", "episode-dry-run",
		"--kind", string(contextengine.RetrievalFeedbackKindAnswerCorrected),
		"--query", "dry run query",
		"--used-ref", "memory_claim:" + claim.ID,
		"--correction", "Preview only.",
		"--dry-run",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	env := decodeTestEnvelope(t, out.Bytes())
	data := envelopeDataMap(t, env)
	if data["applied"] != false || data["dry_run"] != true {
		t.Fatalf("unexpected dry-run output: %#v", data)
	}

	store, err = contextstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("reopen contextengine store: %v", err)
	}
	defer store.Close()
	got, err := store.GetClaim(ctx, claim.ID)
	if err != nil {
		t.Fatalf("get claim: %v", err)
	}
	if got.Status != contextengine.ClaimStatusCurrent {
		t.Fatalf("dry run changed claim status=%q", got.Status)
	}
	events, err := store.ListEvents(ctx, contextengine.EventFilter{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("dry run persisted events: %#v", events)
	}
}

func TestRetrievalFeedbackCommandDefaultKindIsInformational(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	workspacePath := t.TempDir()
	workspaceID := ws.CanonicalID(workspacePath)

	store, err := contextstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open contextengine store: %v", err)
	}
	claim := contextengine.MemoryClaim{
		ID:          "claim-cli-feedback-default",
		WorkspaceID: workspaceID,
		ClaimType:   "semantic_fact",
		Status:      contextengine.ClaimStatusCurrent,
		Summary:     "Default evidence-used feedback should not mutate lifecycle.",
		CreatedAt:   fixedContextRecordsTestTime(),
		UpdatedAt:   fixedContextRecordsTestTime(),
	}
	if _, err := store.UpsertClaim(ctx, claim); err != nil {
		t.Fatalf("upsert claim: %v", err)
	}
	episode := contextengine.RetrievalEpisode{
		ID:          "episode-cli-feedback-default",
		WorkspaceID: workspaceID,
		Query:       "which claim was cited",
		Lane:        contextengine.LaneMemory,
		CreatedAt:   fixedContextRecordsTestTime(),
	}
	if _, err := store.RecordRetrievalEpisode(ctx, episode); err != nil {
		t.Fatalf("record episode: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}

	out := &bytes.Buffer{}
	cmd := newRetrievalFeedbackCommand()
	cmd.SetContext(config.WithContext(ctx, cfg))
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--episode-id", episode.ID,
		"--query", episode.Query,
		"--used-ref", "memory_claim:" + claim.ID,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	env := decodeTestEnvelope(t, out.Bytes())
	data := envelopeDataMap(t, env)
	feedback := nestedMap(t, data, "feedback")
	if feedback["kind"] != string(contextengine.RetrievalFeedbackKindEvidenceUsed) {
		t.Fatalf("feedback.kind=%v want default evidence_used", feedback["kind"])
	}

	store, err = contextstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("reopen contextengine store: %v", err)
	}
	defer store.Close()
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
		t.Fatalf("default evidence_used feedback appended events: %#v", events)
	}
}

func TestRetrievalFeedbackCommandRejectsInvalidUsedRef(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()

	cmd := newRetrievalFeedbackCommand()
	cmd.SetContext(config.WithContext(ctx, cfg))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--workspace", t.TempDir(),
		"--episode-id", "episode-invalid-ref",
		"--kind", string(contextengine.RetrievalFeedbackKindAnswerCorrected),
		"--query", "query",
		"--used-ref", "memory-claim:bad-ref",
		"--correction", "Invalid refs should not silently become informational feedback.",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("execute succeeded with invalid used-ref")
	}
	if !strings.Contains(err.Error(), "--used-ref[0]") {
		t.Fatalf("error=%q want used-ref parse context", err)
	}
}

func executeRetrievalFeedbackCommand(t *testing.T, ctx context.Context, cfg config.Config, args ...string) map[string]any {
	t.Helper()
	out := &bytes.Buffer{}
	cmd := newRetrievalFeedbackCommand()
	cmd.SetContext(config.WithContext(ctx, cfg))
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute retrieval-feedback: %v", err)
	}
	env := decodeTestEnvelope(t, out.Bytes())
	if env.Status != "ok" {
		t.Fatalf("status=%q", env.Status)
	}
	return envelopeDataMap(t, env)
}

func fixedContextRecordsTestTime() time.Time {
	return time.Date(2026, 6, 3, 8, 0, 0, 0, time.UTC)
}
