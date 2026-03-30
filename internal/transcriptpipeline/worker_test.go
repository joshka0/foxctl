package transcriptpipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBoundArtifactText_ClipsLargeInputs(t *testing.T) {
	input := strings.Repeat("abcd", 200000)
	got := BoundArtifactText(input, 100000)
	if got == "" {
		t.Fatal("expected bounded text")
	}
	if len(got) >= len(input) {
		t.Fatal("expected clipped text")
	}
}

func TestBoundArtifactText_PreservesSmallInputs(t *testing.T) {
	input := "small artifact"
	if got := BoundArtifactText(input, 100000); got != input {
		t.Fatalf("got=%q want %q", got, input)
	}
}

func TestRunLLMTaskWithFallbackModel_UsesFallbackWhenPrimaryFails(t *testing.T) {
	t.Parallel()

	calls := make([]string, 0, 2)
	result, ok := RunLLMTaskWithFallbackModel(
		context.Background(),
		WorkerConfig{Provider: "lmstudio", Model: "bridge-model", BaseURL: "http://localhost:1234/v1"},
		WorkerConfig{Provider: "lmstudio", Model: "main-model", BaseURL: "http://localhost:1234/v1"},
		Task{Stage: StageBridge, InputKind: "classified_claim_doctrine_seed_frame", SystemPrompt: "sys", ArtifactText: "artifact"},
		func(result Result) bool { return strings.TrimSpace(result.OutputText) != "" },
		func(_ context.Context, cfg WorkerConfig, _ Task) (Result, error) {
			calls = append(calls, cfg.Model)
			if cfg.Model == "bridge-model" {
				return Result{}, errors.New("model unavailable")
			}
			return Result{ModelID: "lmstudio:main-model", OutputText: `{"claims":[{"text":"ok","kind":"workflow_rule","durability":"durable"}]}`}, nil
		},
	)
	if !ok {
		t.Fatal("expected fallback run accepted")
	}
	if result.ModelID != "lmstudio:main-model" {
		t.Fatalf("model_id=%q want fallback model", result.ModelID)
	}
	if len(calls) != 2 || calls[0] != "bridge-model" || calls[1] != "main-model" {
		t.Fatalf("calls=%v want [bridge-model main-model]", calls)
	}
}

func TestRunLLMTaskWithFallbackModel_UsesFallbackWhenPrimaryOutputRejected(t *testing.T) {
	t.Parallel()

	calls := make([]string, 0, 2)
	result, ok := RunLLMTaskWithFallbackModel(
		context.Background(),
		WorkerConfig{Provider: "lmstudio", Model: "distill-model", BaseURL: "http://localhost:1234/v1"},
		WorkerConfig{Provider: "lmstudio", Model: "main-model", BaseURL: "http://localhost:1234/v1"},
		Task{Stage: StageDistill, InputKind: "classified_claim_doctrine", SystemPrompt: "sys", ArtifactText: "artifact"},
		func(result Result) bool { return strings.Contains(result.OutputText, `"claims":[{`) },
		func(_ context.Context, cfg WorkerConfig, _ Task) (Result, error) {
			calls = append(calls, cfg.Model)
			if cfg.Model == "distill-model" {
				return Result{ModelID: "lmstudio:distill-model", OutputText: `{"claims":[]}`}, nil
			}
			return Result{ModelID: "lmstudio:main-model", OutputText: `{"claims":[{"text":"ok"}]}`}, nil
		},
	)
	if !ok {
		t.Fatal("expected fallback output accepted")
	}
	if result.ModelID != "lmstudio:main-model" {
		t.Fatalf("model_id=%q want fallback model", result.ModelID)
	}
	if len(calls) != 2 || calls[0] != "distill-model" || calls[1] != "main-model" {
		t.Fatalf("calls=%v want [distill-model main-model]", calls)
	}
}
