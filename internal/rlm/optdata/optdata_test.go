package optdata

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordBuilderBuildUsesInjectedClockAndCopiesInput(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.April, 10, 11, 12, 13, 0, time.UTC)
	target := 0.9
	passed := true

	input := BuildInput{
		RecordID: "record-1",
		Prompt: PromptComponents{
			Objective: "Summarize architecture tradeoffs",
			System:    "You are a planner.",
			User:      "Compare option A vs B.",
			ContextBlocks: []PromptContextBlock{
				{Name: "top-of-mind", Source: "aca", Content: "context payload"},
			},
			ToolDefinitions: []PromptToolDefinition{
				{Name: "code_search", Description: "semantic search"},
			},
		},
		Execution: ExecutionMetadata{
			Runtime:      "rlm",
			Mode:         "lambda-retrieval",
			Model:        "openrouter/aurora-alpha",
			RunID:        "run-1",
			NodeID:       "node-1",
			EvidenceRefs: []string{"artifact:sha256:abc"},
		},
		Metrics: []MetricFeedback{
			{Name: "accuracy", Value: 0.82, Goal: MetricGoalMaximize, Target: &target, Passed: &passed},
		},
		Labels: map[string]string{
			"split": "dev",
		},
	}

	builder := NewRecordBuilder(WithBuilderNow(func() time.Time { return base }))
	record := builder.Build(input)

	if record.RecordType != RecordTypeTrajectoryV1 {
		t.Fatalf("record.RecordType = %q, want %q", record.RecordType, RecordTypeTrajectoryV1)
	}
	if record.SchemaVersion != 1 {
		t.Fatalf("record.SchemaVersion = %d, want 1", record.SchemaVersion)
	}
	if !record.CreatedAt.Equal(base) {
		t.Fatalf("record.CreatedAt = %s, want %s", record.CreatedAt, base)
	}

	input.Prompt.ContextBlocks[0].Content = "mutated"
	input.Execution.EvidenceRefs[0] = "mutated"
	input.Labels["split"] = "train"
	*input.Metrics[0].Target = 0.1
	*input.Metrics[0].Passed = false

	if got := record.Prompt.ContextBlocks[0].Content; got != "context payload" {
		t.Fatalf("record prompt mutated: got %q", got)
	}
	if got := record.Execution.EvidenceRefs[0]; got != "artifact:sha256:abc" {
		t.Fatalf("record execution mutated: got %q", got)
	}
	if got := record.Labels["split"]; got != "dev" {
		t.Fatalf("record labels mutated: got %q", got)
	}
	if got := *record.Metrics[0].Target; got != 0.9 {
		t.Fatalf("record metric target mutated: got %f", got)
	}
	if got := *record.Metrics[0].Passed; got != true {
		t.Fatalf("record metric passed mutated: got %t", got)
	}
}

func TestBuildAndParseTrajectoryRecordsJSONL(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.April, 11, 9, 8, 7, 0, time.UTC)
	records := []TrajectoryRecord{
		{
			RecordType:    RecordTypeTrajectoryV1,
			SchemaVersion: 1,
			RecordID:      "record-a",
			CreatedAt:     base,
			Prompt: PromptComponents{
				System: "system prompt",
				User:   "user prompt",
			},
			Execution: ExecutionMetadata{
				Runtime: "rlm",
				Mode:    "repl",
				Model:   "model-a",
				Success: true,
			},
		},
		{
			RecordType:    RecordTypeTrajectoryV1,
			SchemaVersion: 1,
			RecordID:      "record-b",
			CreatedAt:     base.Add(2 * time.Second),
			Prompt: PromptComponents{
				System: "system prompt v2",
				User:   "user prompt v2",
			},
			Execution: ExecutionMetadata{
				Runtime:      "rlm",
				Mode:         "lambda-retrieval",
				Model:        "model-b",
				Success:      false,
				ErrorCode:    "budget_exhausted",
				EvidenceRefs: []string{"path:internal/rlm/runtime/trajectory.go"},
			},
			Metrics: []MetricFeedback{
				{Name: "accuracy", Value: 0.32},
			},
		},
	}

	body, err := BuildTrajectoryRecordsJSONL(records)
	if err != nil {
		t.Fatalf("BuildTrajectoryRecordsJSONL: %v", err)
	}

	parsed, err := ParseTrajectoryRecordsJSONL(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("ParseTrajectoryRecordsJSONL: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("len(parsed) = %d, want 2", len(parsed))
	}
	if parsed[1].RecordID != "record-b" {
		t.Fatalf("parsed[1].RecordID = %q, want record-b", parsed[1].RecordID)
	}
	if !parsed[0].CreatedAt.Equal(base) {
		t.Fatalf("parsed[0].CreatedAt = %s, want %s", parsed[0].CreatedAt, base)
	}
	if parsed[1].Execution.ErrorCode != "budget_exhausted" {
		t.Fatalf("parsed[1].Execution.ErrorCode = %q, want budget_exhausted", parsed[1].Execution.ErrorCode)
	}
}

func TestAppendAndLoadTrajectoryRecordsFile(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.April, 12, 8, 0, 0, 0, time.UTC)
	tick := 0
	builder := NewRecordBuilder(WithBuilderNow(func() time.Time {
		at := base.Add(time.Duration(tick) * time.Second)
		tick++
		return at
	}))

	path := filepath.Join(t.TempDir(), "trajectory_records.jsonl")
	first := builder.Build(BuildInput{
		RecordID: "record-1",
		Prompt: PromptComponents{
			User: "first request",
		},
		Execution: ExecutionMetadata{
			Runtime: "rlm",
			Model:   "model-a",
			Success: true,
		},
	})
	second := builder.Build(BuildInput{
		RecordID: "record-2",
		Prompt: PromptComponents{
			User: "second request",
		},
		Execution: ExecutionMetadata{
			Runtime: "rlm",
			Model:   "model-b",
			Success: false,
		},
	})

	if err := AppendTrajectoryRecordFile(path, first); err != nil {
		t.Fatalf("AppendTrajectoryRecordFile(first): %v", err)
	}
	if err := AppendTrajectoryRecordsFile(path, []TrajectoryRecord{second}); err != nil {
		t.Fatalf("AppendTrajectoryRecordsFile(second): %v", err)
	}

	loaded, err := LoadTrajectoryRecordsFile(path)
	if err != nil {
		t.Fatalf("LoadTrajectoryRecordsFile: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("len(loaded) = %d, want 2", len(loaded))
	}
	if loaded[0].RecordID != "record-1" || loaded[1].RecordID != "record-2" {
		t.Fatalf("loaded record order mismatch: got %q, %q", loaded[0].RecordID, loaded[1].RecordID)
	}
}

func TestParseTrajectoryRecordsJSONLLargeLine(t *testing.T) {
	t.Parallel()

	large := strings.Repeat("x", 70_000)
	record := TrajectoryRecord{
		RecordType:    RecordTypeTrajectoryV1,
		SchemaVersion: 1,
		RecordID:      "record-large",
		CreatedAt:     time.Date(2026, time.April, 12, 10, 0, 0, 0, time.UTC),
		Prompt: PromptComponents{
			User: large,
		},
		Execution: ExecutionMetadata{
			Runtime: "rlm",
			Model:   "model-large",
			Success: true,
		},
	}

	body, err := BuildTrajectoryRecordsJSONL([]TrajectoryRecord{record})
	if err != nil {
		t.Fatalf("BuildTrajectoryRecordsJSONL: %v", err)
	}
	parsed, err := ParseTrajectoryRecordsJSONL(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("ParseTrajectoryRecordsJSONL(large): %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("len(parsed) = %d, want 1", len(parsed))
	}
	if parsed[0].Prompt.User != large {
		t.Fatalf("parsed[0].Prompt.User len = %d, want %d", len(parsed[0].Prompt.User), len(large))
	}
}

func TestWriteTrajectoryRecordsJSONLRejectsInvalidRecord(t *testing.T) {
	t.Parallel()

	_, err := BuildTrajectoryRecordsJSONL([]TrajectoryRecord{
		{
			SchemaVersion: 1,
			CreatedAt:     time.Date(2026, time.April, 12, 10, 0, 0, 0, time.UTC),
			Execution: ExecutionMetadata{
				Runtime: "rlm",
			},
		},
	})
	if err == nil {
		t.Fatalf("BuildTrajectoryRecordsJSONL() error = nil, want invalid record error")
	}
	if !strings.Contains(err.Error(), "record type is required") {
		t.Fatalf("BuildTrajectoryRecordsJSONL() error = %v, want record type validation", err)
	}
}
