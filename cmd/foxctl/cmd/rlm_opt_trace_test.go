package cmd

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/rlm/optdata"
)

func TestBuildRLMOptimizerTraceRecordStoresExecutionOutputText(t *testing.T) {
	t.Parallel()

	record := buildRLMOptimizerTraceRecord(rlmOptimizerTraceInput{
		Mode:        "repl",
		InputPrompt: "solve",
		Task:        rlm.Task{Prompt: "Return solution = <value>.", RunID: "run-1"},
		Result: rlm.Result{
			Answer: "solution = 4",
			Metadata: map[string]any{
				"run_id": "trace-1",
			},
		},
		RecordedAt: time.Unix(5, 0).UTC(),
	})

	if got := strings.TrimSpace(record.Execution.OutputText); got != "solution = 4" {
		t.Fatalf("execution.output_text=%q want solution = 4", got)
	}
}

func TestBuildRLMOptimizerTraceRecordAttributesFeedbackComponents(t *testing.T) {
	t.Parallel()

	record := buildRLMOptimizerTraceRecord(rlmOptimizerTraceInput{
		Mode:        "repl",
		InputPrompt: "solve",
		Task:        rlm.Task{Prompt: "Return solution = <value>.", RunID: "run-2"},
		Result: rlm.Result{
			Answer: "solution = 4",
			Metadata: map[string]any{
				"run_id":    "trace-2",
				"llm_error": "schema mismatch",
				"output_sanitization": map[string]any{
					"raw_text": "<think>hidden</think>\nsolution = 4",
				},
			},
		},
		RunErr: errors.New("runner timeout"),
	})

	if !hasFeedbackComponent(record.Feedback, optdata.ComponentRuntimeError) {
		t.Fatalf("missing runtime error component feedback: %+v", record.Feedback)
	}
	if !hasFeedbackComponent(record.Feedback, optdata.ComponentOutputSanitization) {
		t.Fatalf("missing output sanitization component feedback: %+v", record.Feedback)
	}
	for _, item := range record.Feedback {
		if strings.TrimSpace(item.Component) == optdata.ComponentTaskPrompt {
			t.Fatalf("unexpected task-prompt component attribution: %+v", item)
		}
	}
}

func hasFeedbackComponent(feedback []optdata.PromptFeedback, component string) bool {
	component = strings.TrimSpace(component)
	for _, item := range feedback {
		if strings.TrimSpace(item.Component) == component {
			return true
		}
	}
	return false
}
