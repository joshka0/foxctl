package rlm

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeToolExecutor struct{}

func (fakeToolExecutor) Execute(_ context.Context, name string, _ json.RawMessage) (map[string]any, error) {
	switch name {
	case "search_repo":
		return map[string]any{
			"results": []map[string]any{{"path": "internal/auth/handler.go"}},
		}, nil
	case "search_vault":
		return map[string]any{
			"results": []map[string]any{{"path": "notes/repo/auth.md"}},
		}, nil
	case "search_scenes":
		return map[string]any{
			"results": []map[string]any{{"handle": "conversation:abc"}},
		}, nil
	case "subcall":
		return map[string]any{
			"result": Result{
				Answer:       "child summary",
				EvidenceRefs: []string{"path:child.go"},
			},
		}, nil
	default:
		return map[string]any{"results": []map[string]any{}}, nil
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
		Tools:         []Tool{{Name: "search_repo", ReadOnly: true}},
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

func TestInspectRunnerUsesSubcallWhenHandlesAreLarge(t *testing.T) {
	t.Parallel()

	runner := InspectRunner{Tools: fakeToolExecutor{}}
	result, err := runner.Run(context.Background(), Task{
		Prompt:      "inspect auth flow",
		MaxDepth:    1,
		MaxSubcalls: 1,
	}, Environment{
		RepoHandles:     []string{"path:a.go", "path:b.go"},
		VaultHandles:    []string{"note:a.md"},
		ArtifactHandles: []string{"trajectory:t1"},
		Tools:           []Tool{{Name: "search_repo", ReadOnly: true}, {Name: "subcall", ReadOnly: true}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Subcalls != 1 {
		t.Fatalf("subcalls=%d", result.Subcalls)
	}
	found := false
	for _, ref := range result.EvidenceRefs {
		if ref == "path:child.go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected child evidence in %v", result.EvidenceRefs)
	}
}
