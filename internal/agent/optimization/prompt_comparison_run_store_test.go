package optimization_test

import (
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
)

func TestPromptComparisonRunStore_SaveAndList(t *testing.T) {
	t.Parallel()

	store, err := optimization.OpenPromptComparisonRunStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open prompt comparison run store: %v", err)
	}
	defer store.Close() //nolint:errcheck

	saved, err := store.Save(context.Background(), optimization.PromptComparisonRun{
		WorkspaceID:    "ws-test",
		ArtifactDigest: "sha256:test-digest",
		Provider:       "lmstudio",
		BaseURL:        "http://localhost:1234/v1",
		Question:       "Summarize the issue",
		Context:        "Diff summary",
		ModelCount:     4,
		VariantCount:   2,
		SuccessCount:   8,
		FailureCount:   0,
	})
	if err != nil {
		t.Fatalf("save comparison run: %v", err)
	}

	got, err := store.Get(context.Background(), "ws-test", saved.ID)
	if err != nil {
		t.Fatalf("get comparison run: %v", err)
	}
	if got.ID != saved.ID {
		t.Fatalf("id=%q want %q", got.ID, saved.ID)
	}
	if got.ArtifactDigest != "sha256:test-digest" {
		t.Fatalf("artifact_digest=%q want sha256:test-digest", got.ArtifactDigest)
	}

	runs, err := store.List(context.Background(), "ws-test", 10)
	if err != nil {
		t.Fatalf("list comparison runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs)=%d want 1", len(runs))
	}
}

func TestPromptComparisonRunStore_ListUsesStableTieBreak(t *testing.T) {
	t.Parallel()

	store, err := optimization.OpenPromptComparisonRunStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open prompt comparison run store: %v", err)
	}
	defer store.Close() //nolint:errcheck

	createdAt := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"01A", "01B"} {
		if _, err := store.Save(context.Background(), optimization.PromptComparisonRun{
			ID:             id,
			WorkspaceID:    "ws-test",
			ArtifactDigest: "sha256:" + id,
			Provider:       "lmstudio",
			Question:       "Q",
			CreatedAt:      createdAt,
		}); err != nil {
			t.Fatalf("save comparison run %s: %v", id, err)
		}
	}

	runs, err := store.List(context.Background(), "ws-test", 10)
	if err != nil {
		t.Fatalf("list comparison runs: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("len(runs)=%d want 2", len(runs))
	}
	if runs[0].ID != "01B" || runs[1].ID != "01A" {
		t.Fatalf("unexpected order: %+v", runs)
	}
}
