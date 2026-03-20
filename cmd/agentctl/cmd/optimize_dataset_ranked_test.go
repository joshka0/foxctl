package cmd

import (
	"context"
	"testing"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
)

func TestBuildPromptPreferenceExamples(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	casRoot := t.TempDir()

	variantStore, err := optimization.OpenPromptVariantStore(ctx, root)
	if err != nil {
		t.Fatalf("open prompt variant store: %v", err)
	}
	defer variantStore.Close() //nolint:errcheck

	runStore, err := optimization.OpenPromptComparisonRunStore(ctx, root)
	if err != nil {
		t.Fatalf("open prompt comparison run store: %v", err)
	}
	defer runStore.Close() //nolint:errcheck

	chosen, err := variantStore.Save(ctx, optimization.PromptVariant{
		WorkspaceID:    "/tmp/ws",
		AgentRole:      "coder",
		Mode:           "gepa",
		OriginalPrompt: "Original A",
		Prompt:         "Chosen prompt",
	})
	if err != nil {
		t.Fatalf("save chosen variant: %v", err)
	}
	rejected, err := variantStore.Save(ctx, optimization.PromptVariant{
		WorkspaceID:    "/tmp/ws",
		AgentRole:      "coder",
		Mode:           "copro",
		OriginalPrompt: "Original B",
		Prompt:         "Rejected prompt",
	})
	if err != nil {
		t.Fatalf("save rejected variant: %v", err)
	}

	report := map[string]any{
		"question": "Summarize the bug",
		"context":  "Diff summary",
		"scoring": map[string]any{
			"pass_threshold": 0.8,
		},
		"ranking": []map[string]any{
			{
				"variant_id":     chosen.ID,
				"agent_role":     "coder",
				"mode":           "gepa",
				"mean_score":     0.95,
				"worst_score":    0.90,
				"pass_count":     4,
				"score_variance": 0.001,
			},
			{
				"variant_id":     rejected.ID,
				"agent_role":     "coder",
				"mode":           "copro",
				"mean_score":     0.40,
				"worst_score":    0.30,
				"pass_count":     1,
				"score_variance": 0.100,
			},
		},
		"results": []map[string]any{
			{"variant_id": chosen.ID, "model": "m1", "output": "good", "score": 0.95, "passed": true},
			{"variant_id": rejected.ID, "model": "m1", "output": "bad", "score": 0.40, "passed": false},
		},
	}
	artifact, err := persistPromptComparisonArtifact(ctx, casRoot, report)
	if err != nil {
		t.Fatalf("persist report: %v", err)
	}

	run, err := runStore.Save(ctx, optimization.PromptComparisonRun{
		WorkspaceID:    "/tmp/ws",
		ArtifactDigest: artifact,
		Provider:       "lmstudio",
		Question:       "Summarize the bug",
		Context:        "Diff summary",
		ModelCount:     1,
		VariantCount:   2,
		SuccessCount:   2,
	})
	if err != nil {
		t.Fatalf("save comparison run: %v", err)
	}

	examples, err := buildPromptPreferenceExamples(ctx, []optimization.PromptComparisonRun{run}, variantStore, casRoot, 0.05, "run")
	if err != nil {
		t.Fatalf("buildPromptPreferenceExamples: %v", err)
	}
	if len(examples) != 1 {
		t.Fatalf("len(examples)=%d want 1", len(examples))
	}
	if examples[0].Chosen.VariantID != chosen.ID {
		t.Fatalf("chosen=%q want %q", examples[0].Chosen.VariantID, chosen.ID)
	}
	if examples[0].Rejected.VariantID != rejected.ID {
		t.Fatalf("rejected=%q want %q", examples[0].Rejected.VariantID, rejected.ID)
	}
}

func TestBuildPromptPreferenceExamplesCaseGranularity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	casRoot := t.TempDir()

	variantStore, err := optimization.OpenPromptVariantStore(ctx, root)
	if err != nil {
		t.Fatalf("open prompt variant store: %v", err)
	}
	defer variantStore.Close() //nolint:errcheck

	runStore, err := optimization.OpenPromptComparisonRunStore(ctx, root)
	if err != nil {
		t.Fatalf("open prompt comparison run store: %v", err)
	}
	defer runStore.Close() //nolint:errcheck

	chosen, err := variantStore.Save(ctx, optimization.PromptVariant{
		WorkspaceID:    "/tmp/ws",
		AgentRole:      "coder",
		Mode:           "gepa",
		OriginalPrompt: "Original A",
		Prompt:         "Chosen prompt",
	})
	if err != nil {
		t.Fatalf("save chosen variant: %v", err)
	}
	rejected, err := variantStore.Save(ctx, optimization.PromptVariant{
		WorkspaceID:    "/tmp/ws",
		AgentRole:      "coder",
		Mode:           "gepa",
		OriginalPrompt: "Original B",
		Prompt:         "Rejected prompt",
	})
	if err != nil {
		t.Fatalf("save rejected variant: %v", err)
	}

	report := map[string]any{
		"question": "eval-dataset:2-cases",
		"context":  "",
		"eval_cases": []map[string]any{
			{"id": "case-1", "question": "Q1", "context": "C1", "target_response": "A1", "category": "coder_impl"},
			{"id": "case-2", "question": "Q2", "context": "C2", "target_response": "A2", "category": "coder_impl"},
		},
		"ranking": []map[string]any{
			{"variant_id": chosen.ID, "agent_role": "coder", "mode": "gepa", "mean_score": 0.9, "worst_score": 0.8, "pass_count": 2},
			{"variant_id": rejected.ID, "agent_role": "coder", "mode": "gepa", "mean_score": 0.4, "worst_score": 0.3, "pass_count": 0},
		},
		"results": []map[string]any{
			{"variant_id": chosen.ID, "eval_case_id": "case-1", "model": "m1", "output": "good one", "score": 0.91, "passed": true},
			{"variant_id": rejected.ID, "eval_case_id": "case-1", "model": "m1", "output": "bad one", "score": 0.30, "passed": false},
			{"variant_id": chosen.ID, "eval_case_id": "case-2", "model": "m1", "output": "good two", "score": 0.22, "passed": false},
			{"variant_id": rejected.ID, "eval_case_id": "case-2", "model": "m1", "output": "bad two", "score": 0.81, "passed": true},
		},
	}
	artifact, err := persistPromptComparisonArtifact(ctx, casRoot, report)
	if err != nil {
		t.Fatalf("persist report: %v", err)
	}

	run, err := runStore.Save(ctx, optimization.PromptComparisonRun{
		WorkspaceID:    "/tmp/ws",
		ArtifactDigest: artifact,
		Provider:       "lmstudio",
		Question:       "eval-dataset:2-cases",
		ModelCount:     1,
		VariantCount:   2,
		SuccessCount:   4,
	})
	if err != nil {
		t.Fatalf("save comparison run: %v", err)
	}

	examples, err := buildPromptPreferenceExamples(ctx, []optimization.PromptComparisonRun{run}, variantStore, casRoot, 0.05, "case")
	if err != nil {
		t.Fatalf("buildPromptPreferenceExamples(case): %v", err)
	}
	if len(examples) != 2 {
		t.Fatalf("len(examples)=%d want 2", len(examples))
	}
	if examples[0].Metadata.Granularity != "case" {
		t.Fatalf("granularity=%q want case", examples[0].Metadata.Granularity)
	}
	if examples[0].Input.EvalCaseID == "" {
		t.Fatal("expected eval_case_id in case-granularity example")
	}
	if examples[0].Input.TargetResponse == "" {
		t.Fatal("expected target_response in case-granularity example")
	}
	if examples[0].Chosen.VariantID == examples[1].Chosen.VariantID {
		t.Fatal("expected per-case ranking to allow different chosen variants")
	}
}

func TestApplyPromptPreferenceExportDryRun(t *testing.T) {
	t.Parallel()

	plan, err := planPromptPreferenceExport(
		"/tmp/ws",
		nil,
		[]optimization.PromptPreferenceExample{{
			RecordType: "prompt_preference",
			Input:      optimization.PromptPreferenceInput{Question: "Q"},
			Chosen: optimization.PromptPreferenceCandidate{
				VariantID: "a",
				AgentRole: "coder",
				Mode:      "gepa",
				Prompt:    "A",
			},
			Rejected: optimization.PromptPreferenceCandidate{
				VariantID: "b",
				AgentRole: "coder",
				Mode:      "gepa",
				Prompt:    "B",
			},
		}},
		0.05,
		"run",
		"/tmp/out.jsonl",
		true,
	)
	if err != nil {
		t.Fatalf("planPromptPreferenceExport() error = %v", err)
	}

	data, artifact, err := applyPromptPreferenceExport(context.Background(), t.TempDir(), plan, true)
	if err != nil {
		t.Fatalf("applyPromptPreferenceExport() error = %v", err)
	}
	if artifact != "" {
		t.Fatalf("artifact=%q want empty", artifact)
	}
	if dryRun, ok := data["dry_run"].(bool); !ok || !dryRun {
		t.Fatalf("dry_run=%v want true", data["dry_run"])
	}
	if candidate, ok := data["artifact_digest_candidate"].(string); !ok || candidate == "" {
		t.Fatalf("artifact_digest_candidate=%v want non-empty", data["artifact_digest_candidate"])
	}
	if wouldWrite, ok := data["would_write_file"].(bool); !ok || !wouldWrite {
		t.Fatalf("would_write_file=%v want true", data["would_write_file"])
	}
	if wouldWriteCAS, ok := data["would_write_cas"].(bool); !ok || !wouldWriteCAS {
		t.Fatalf("would_write_cas=%v want true", data["would_write_cas"])
	}
}
