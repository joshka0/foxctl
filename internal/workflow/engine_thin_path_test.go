package workflow

import (
	"context"
	"fmt"
	"testing"
)

// stubStepExecutor is an in-process stand-in for skillExecutor.
// It lets the thin-path test run without invoking any agentctl binary.
type stubStepExecutor struct {
	executeFn func(ctx context.Context, step *Step, input map[string]any) (*StepResult, error)
}

func (s *stubStepExecutor) Execute(ctx context.Context, step *Step, input map[string]any) (*StepResult, error) {
	return s.executeFn(ctx, step, input)
}

// TestScheduler_ThinPathProof exercises the full primitive chain:
//
//	Builder → Workflow → DAG → Scheduler → ExecutionContext → TemplateEngine → WorkflowResult
//
// Two steps are wired in sequence: "fetch" produces data, "process" templates
// that data into its own input. The stub executor eliminates any dependency on
// the agentctl binary, proving the workflow primitive boundaries in isolation.
//
// This is the M4 thin-path proof for story 01KNQ6MHTHN6KZJ3WGGVHVTK1T.
func TestScheduler_ThinPathProof(t *testing.T) {
	// Build a two-step workflow where "process" templates "fetch" output.
	wf, err := NewBuilder("m4-thin-path").
		Step("fetch", "test/fetch", map[string]any{
			"url": "https://example.com",
		}).
		Step("process", "test/process", map[string]any{
			"content": "{{.fetch.data.body}}",
		}).DependsOn("fetch").
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var executionOrder []string
	var capturedProcessInput map[string]any

	executor := &stubStepExecutor{
		executeFn: func(_ context.Context, step *Step, input map[string]any) (*StepResult, error) {
			executionOrder = append(executionOrder, step.ID)
			switch step.ID {
			case "fetch":
				return &StepResult{
					StepID: step.ID,
					Status: StepCompleted,
					Data:   map[string]any{"body": "fetched-content"},
				}, nil
			case "process":
				capturedProcessInput = input
				return &StepResult{
					StepID: step.ID,
					Status: StepCompleted,
					Data:   map[string]any{"summary": "processed"},
				}, nil
			default:
				return nil, fmt.Errorf("unexpected step: %s", step.ID)
			}
		},
	}

	dag, err := NewDAG(wf.Steps)
	if err != nil {
		t.Fatalf("NewDAG: %v", err)
	}

	result, err := NewScheduler(dag, executor).Run(context.Background(), NewExecutionContext(nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// DAG ordering: fetch must precede process.
	if len(executionOrder) != 2 || executionOrder[0] != "fetch" || executionOrder[1] != "process" {
		t.Errorf("wrong execution order: %v", executionOrder)
	}

	// Template interpolation: process.input.content must carry fetch's output.
	if content, _ := capturedProcessInput["content"].(string); content != "fetched-content" {
		t.Errorf("template interpolation failed: process.input.content = %v", capturedProcessInput["content"])
	}

	// Result collection: both steps complete and are present in WorkflowResult.
	if result.Status != StepCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}
	if len(result.Steps) != 2 {
		t.Errorf("expected 2 step results, got %d", len(result.Steps))
	}
	fetchResult, ok := result.Steps["fetch"]
	if !ok || fetchResult.Status != StepCompleted {
		t.Errorf("fetch step missing or not completed: %+v", fetchResult)
	}
	processResult, ok := result.Steps["process"]
	if !ok || processResult.Status != StepCompleted {
		t.Errorf("process step missing or not completed: %+v", processResult)
	}
}
