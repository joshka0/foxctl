package transcriptpipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/quick"
	"unicode"
	"unicode/utf8"
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

func TestBoundArtifactTextFallbackPreservesUTF8(t *testing.T) {
	t.Parallel()

	input := strings.Repeat("é", 5000)
	got := BoundArtifactText(input, 1)
	if !utf8.ValidString(got) {
		t.Fatalf("BoundArtifactText() produced invalid UTF-8: %q", got)
	}
	if got == "" {
		t.Fatal("BoundArtifactText() returned empty output for non-empty input")
	}
	if utf8.RuneCountInString(got) >= utf8.RuneCountInString(input) {
		t.Fatalf("BoundArtifactText() rune count=%d want less than input", utf8.RuneCountInString(got))
	}
}

func TestTranscriptTruncateInlinePropertyNormalizesAndBoundsGeneratedText(t *testing.T) {
	t.Parallel()

	property := func(input string, rawLimit uint8) bool {
		limit := int(rawLimit)
		got := truncateInline(input, limit)
		if !utf8.ValidString(got) {
			t.Logf("truncateInline(%q, %d) produced invalid UTF-8: %q", input, limit, got)
			return false
		}
		normalized := normalizeTranscriptInlineForTest(input)
		if limit <= 0 {
			return got == normalized
		}
		if utf8.RuneCountInString(got) > limit {
			t.Logf("truncateInline(%q, %d) produced %d runes", input, limit, utf8.RuneCountInString(got))
			return false
		}
		if utf8.RuneCountInString(normalized) <= limit {
			return got == normalized
		}
		if limit > 1 && !strings.HasSuffix(got, "…") {
			t.Logf("truncated output missing ellipsis: %q", got)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("truncateInline property failed: %v", err)
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

func normalizeTranscriptInlineForTest(input string) string {
	fields := strings.FieldsFunc(strings.TrimSpace(input), unicode.IsSpace)
	return strings.Join(fields, " ")
}
