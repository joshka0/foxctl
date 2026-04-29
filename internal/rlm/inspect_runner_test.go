package rlm

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeToolExecutor struct{}

func (fakeToolExecutor) Execute(_ context.Context, name string, _ json.RawMessage) (map[string]any, error) {
	switch name {
	case "retrieve_mixed":
		return map[string]any{
			"nodes": []map[string]any{
				{"ref": "path:internal/auth/handler.go"},
				{"ref": "note:notes/repo/auth.md"},
				{"ref": "memory_claim:abc"},
			},
		}, nil
	default:
		return map[string]any{"nodes": []map[string]any{}}, nil
	}
}

func TestInspectRunnerBuildsAnswerAndEvidence(t *testing.T) {
	t.Parallel()

	runner := InspectRunner{Tools: fakeToolExecutor{}}
	result, err := runner.Run(context.Background(), Task{
		Prompt: "inspect auth flow",
	}, Environment{
		TopOfMind:     map[string]any{"objective": "trace auth", "phase": "analyze"},
		LatestHandoff: map[string]any{"summary": "Collected auth evidence."},
		VaultHandles:  []string{"note:notes/repo/auth.md"},
		SceneHandles:  []string{"conversation:abc"},
		Tools:         []Tool{{Name: "retrieve_mixed", ReadOnly: true}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Answer == "" {
		t.Fatalf("expected non-empty answer")
	}
	if result.Iterations != 1 {
		t.Fatalf("iterations=%d", result.Iterations)
	}
	if len(result.EvidenceRefs) == 0 {
		t.Fatalf("expected evidence refs")
	}
}
