package optdata

import (
	"testing"
	"time"
)

func TestBuildPromptPreferenceExamplesPairsSuccessfulAndFailedTrace(t *testing.T) {
	basePrompt := "Use helper tools carefully."
	records := []TrajectoryRecord{
		NewRecordBuilder(WithBuilderNow(func() time.Time { return time.Unix(1, 0).UTC() })).Build(BuildInput{
			RecordID: "fail",
			Prompt: PromptComponents{
				User:   "solve 2+2",
				System: basePrompt,
			},
			Execution: ExecutionMetadata{
				Runtime:      "rlm",
				Mode:         "repl",
				Model:        "small",
				Success:      false,
				ErrorMessage: "missing tool use",
			},
			Metrics: []MetricFeedback{{Name: "success", Value: 0, Goal: MetricGoalMaximize}},
		}),
		NewRecordBuilder(WithBuilderNow(func() time.Time { return time.Unix(2, 0).UTC() })).Build(BuildInput{
			RecordID: "pass",
			Prompt: PromptComponents{
				User:   "solve 2+2",
				System: basePrompt + "\nReturn only solution lines.",
			},
			Execution: ExecutionMetadata{
				Runtime: "rlm",
				Mode:    "repl",
				Model:   "small",
				Success: true,
			},
			Metrics: []MetricFeedback{{Name: "success", Value: 1, Goal: MetricGoalMaximize}},
		}),
	}

	examples := BuildPromptPreferenceExamples(records, PreferenceBuildOptions{
		AgentRole:       "rlm",
		Mode:            "gepa",
		TargetComponent: "system",
	})
	if len(examples) != 1 {
		t.Fatalf("len(examples)=%d want 1", len(examples))
	}
	example := examples[0]
	if example.Chosen.VariantID != "pass" {
		t.Fatalf("chosen=%q want pass", example.Chosen.VariantID)
	}
	if example.Rejected.VariantID != "fail" {
		t.Fatalf("rejected=%q want fail", example.Rejected.VariantID)
	}
	if example.Chosen.Prompt == example.Rejected.Prompt {
		t.Fatalf("expected different chosen/rejected prompts")
	}
	if example.Chosen.MeanScore <= example.Rejected.MeanScore {
		t.Fatalf("scores not ranked: chosen=%f rejected=%f", example.Chosen.MeanScore, example.Rejected.MeanScore)
	}
}

func TestBuildPromptPreferenceExamplesUsesSyntheticRejectedForSingleTrace(t *testing.T) {
	record := NewRecordBuilder(WithBuilderNow(func() time.Time { return time.Unix(3, 0).UTC() })).Build(BuildInput{
		RecordID: "only",
		Prompt: PromptComponents{
			User:   "solve",
			System: "Use tools.",
		},
		Execution: ExecutionMetadata{
			Runtime: "rlm",
			Mode:    "lambda-retrieval",
			Success: true,
		},
		Metrics: []MetricFeedback{{Name: "success", Value: 1}},
	})

	examples := BuildPromptPreferenceExamples([]TrajectoryRecord{record}, PreferenceBuildOptions{TargetComponent: "system"})
	if len(examples) != 1 {
		t.Fatalf("len(examples)=%d want 1", len(examples))
	}
	if examples[0].Rejected.VariantID != "only:baseline" {
		t.Fatalf("rejected=%q", examples[0].Rejected.VariantID)
	}
	if examples[0].Rejected.MeanScore >= examples[0].Chosen.MeanScore {
		t.Fatalf("synthetic rejected should score lower")
	}
}

func TestBuildPromptPreferenceExamplesUsesExecutionOutputText(t *testing.T) {
	record := NewRecordBuilder(WithBuilderNow(func() time.Time { return time.Unix(4, 0).UTC() })).Build(BuildInput{
		RecordID: "with-output",
		Prompt: PromptComponents{
			User:   "solve 2+2",
			System: "Use tools then respond in required format.",
		},
		Execution: ExecutionMetadata{
			Runtime:    "rlm",
			Mode:       "repl",
			Success:    true,
			OutputText: "solution = 4",
		},
		Metrics: []MetricFeedback{{Name: "success", Value: 1}},
	})

	examples := BuildPromptPreferenceExamples([]TrajectoryRecord{record}, PreferenceBuildOptions{TargetComponent: "system"})
	if len(examples) != 1 {
		t.Fatalf("len(examples)=%d want 1", len(examples))
	}
	if got := examples[0].Chosen.OutputsByModel[0].Output; got != "solution = 4" {
		t.Fatalf("chosen output=%q want solution = 4", got)
	}
	if got := examples[0].Rejected.OutputsByModel[0].Output; got == "solution = 4" {
		t.Fatalf("synthetic rejected output should not copy chosen output: %q", got)
	}
}
