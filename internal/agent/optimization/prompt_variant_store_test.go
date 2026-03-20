package optimization_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
)

func TestPromptVariantStore_SaveAndGet(t *testing.T) {
	t.Parallel()

	store, err := optimization.OpenPromptVariantStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open prompt variant store: %v", err)
	}
	defer store.Close() //nolint:errcheck

	saved, err := store.Save(context.Background(), optimization.PromptVariant{
		WorkspaceID:    "ws-test",
		AgentRole:      "coder",
		TargetProfile:  "local_lmstudio",
		Mode:           "gepa",
		OriginalPrompt: "Original prompt",
		Prompt:         "Optimized prompt",
		OriginalScore:  0.45,
		OptimizedScore: 0.67,
		Improvement:    0.48,
		CandidateCount: 4,
		Metadata: map[string]any{
			"source": "test",
		},
	})
	if err != nil {
		t.Fatalf("save prompt variant: %v", err)
	}

	got, err := store.Get(context.Background(), "ws-test", saved.ID)
	if err != nil {
		t.Fatalf("get prompt variant: %v", err)
	}

	if got.ID != saved.ID {
		t.Fatalf("id=%q want %q", got.ID, saved.ID)
	}
	if got.AgentRole != "coder" {
		t.Fatalf("agent_role=%q want coder", got.AgentRole)
	}
	if got.TargetProfile != "local_lmstudio" {
		t.Fatalf("target_profile=%q want local_lmstudio", got.TargetProfile)
	}
	if got.Prompt != "Optimized prompt" {
		t.Fatalf("prompt=%q want Optimized prompt", got.Prompt)
	}
	if got.Metadata["source"] != "test" {
		t.Fatalf("metadata source=%v want test", got.Metadata["source"])
	}
}

func TestPromptVariantStore_ListByRole(t *testing.T) {
	t.Parallel()

	store, err := optimization.OpenPromptVariantStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open prompt variant store: %v", err)
	}
	defer store.Close() //nolint:errcheck

	inputs := []optimization.PromptVariant{
		{
			WorkspaceID:    "ws-test",
			AgentRole:      "coder",
			TargetProfile:  "local_lmstudio",
			Mode:           "gepa",
			OriginalPrompt: "Original coder",
			Prompt:         "Optimized coder",
		},
		{
			WorkspaceID:    "ws-test",
			AgentRole:      "planner",
			TargetProfile:  "openrouter_remote",
			Mode:           "copro",
			OriginalPrompt: "Original planner",
			Prompt:         "Optimized planner",
		},
	}
	for _, input := range inputs {
		if _, err := store.Save(context.Background(), input); err != nil {
			t.Fatalf("save prompt variant: %v", err)
		}
	}

	coderVariants, err := store.List(context.Background(), "ws-test", "coder", 10)
	if err != nil {
		t.Fatalf("list coder variants: %v", err)
	}
	if len(coderVariants) != 1 {
		t.Fatalf("len(coderVariants)=%d want 1", len(coderVariants))
	}
	if coderVariants[0].AgentRole != "coder" {
		t.Fatalf("agent_role=%q want coder", coderVariants[0].AgentRole)
	}
}

func TestPromptVariantStore_ResolveLatestCompatible(t *testing.T) {
	t.Parallel()

	store, err := optimization.OpenPromptVariantStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open prompt variant store: %v", err)
	}
	defer store.Close() //nolint:errcheck

	inputs := []optimization.PromptVariant{
		{
			WorkspaceID:    "ws-test",
			AgentRole:      "coder",
			TargetProfile:  "generic",
			Mode:           "gepa",
			OriginalPrompt: "base",
			Prompt:         "generic prompt",
			CreatedAt:      time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
		},
		{
			WorkspaceID:    "ws-test",
			AgentRole:      "coder",
			TargetProfile:  "local_lmstudio",
			Mode:           "gepa",
			OriginalPrompt: "base",
			Prompt:         "local prompt",
			CreatedAt:      time.Date(2026, 3, 20, 12, 0, 1, 0, time.UTC),
		},
	}
	for _, input := range inputs {
		if _, err := store.Save(context.Background(), input); err != nil {
			t.Fatalf("save prompt variant: %v", err)
		}
	}

	local, err := store.ResolveLatestCompatible(context.Background(), "ws-test", "coder", "local_lmstudio")
	if err != nil {
		t.Fatalf("ResolveLatestCompatible(local): %v", err)
	}
	if local.Prompt != "local prompt" {
		t.Fatalf("local prompt=%q want local prompt", local.Prompt)
	}

	jido, err := store.ResolveLatestCompatible(context.Background(), "ws-test", "coder", "jido_openrouter")
	if err != nil {
		t.Fatalf("ResolveLatestCompatible(jido): %v", err)
	}
	if jido.Prompt != "generic prompt" {
		t.Fatalf("fallback prompt=%q want generic prompt", jido.Prompt)
	}
}

func TestPromptVariantStore_ResolveLatestCompatibleRejectsIncompatibleFallback(t *testing.T) {
	t.Parallel()

	store, err := optimization.OpenPromptVariantStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open prompt variant store: %v", err)
	}
	defer store.Close() //nolint:errcheck

	if _, err := store.Save(context.Background(), optimization.PromptVariant{
		WorkspaceID:    "ws-test",
		AgentRole:      "coder",
		TargetProfile:  "local_lmstudio",
		Mode:           "gepa",
		OriginalPrompt: "base",
		Prompt:         "local prompt",
	}); err != nil {
		t.Fatalf("save prompt variant: %v", err)
	}

	_, err = store.ResolveLatestCompatible(context.Background(), "ws-test", "coder", "jido_openrouter")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err=%v want sql.ErrNoRows", err)
	}
}
