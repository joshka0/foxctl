package conversationsettings

import (
	"context"
	"errors"
	"testing"
)

func TestStore_PatchAndGet(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	convID := "conv-1"

	_, err = store.Get(ctx, convID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	provider := "openrouter"
	model := "openai/gpt-4o-mini"
	execMode := "story"
	tools := []string{"rlm_context_query", " rlm_context_query ", "rlm_context_put", ""}

	got, err := store.Patch(ctx, convID, Patch{
		LLMProvider: &provider,
		LLMModel:    &model,
		ExecMode:    &execMode,
		ToolsAllow:  &tools,
	})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if got.ConversationID != convID {
		t.Fatalf("ConversationID: got %q", got.ConversationID)
	}
	if got.LLMProvider != provider {
		t.Fatalf("LLMProvider: got %q", got.LLMProvider)
	}
	if got.LLMModel != model {
		t.Fatalf("LLMModel: got %q", got.LLMModel)
	}
	if got.ExecMode != execMode {
		t.Fatalf("ExecMode: got %q", got.ExecMode)
	}
	if len(got.ToolsAllow) != 2 || got.ToolsAllow[0] != "rlm_context_put" || got.ToolsAllow[1] != "rlm_context_query" {
		t.Fatalf("ToolsAllow: got %#v", got.ToolsAllow)
	}
	if got.UpdatedAt == "" {
		t.Fatalf("UpdatedAt: expected non-empty")
	}

	roundTrip, err := store.Get(ctx, convID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if roundTrip.LLMProvider != provider || roundTrip.LLMModel != model || roundTrip.ExecMode != execMode {
		t.Fatalf("roundTrip mismatch: %#v", roundTrip)
	}
	if len(roundTrip.ToolsAllow) != 2 || roundTrip.ToolsAllow[0] != "rlm_context_put" || roundTrip.ToolsAllow[1] != "rlm_context_query" {
		t.Fatalf("roundTrip ToolsAllow: got %#v", roundTrip.ToolsAllow)
	}
}

func TestStore_PatchInvalidExecMode(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	convID := "conv-1"
	execMode := "weird"
	_, err = store.Patch(ctx, convID, Patch{ExecMode: &execMode})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
}
