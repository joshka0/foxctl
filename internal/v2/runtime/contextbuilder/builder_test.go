package contextbuilder_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/v2/core/run"
	"github.com/jkatigb/agentctl/internal/v2/runtime/contextbuilder"
)

func TestContextBuilder_ResolveWholeTurnRef(t *testing.T) {
	t.Parallel()

	reader := &fakeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-ctx-1": {
				ID:            "turn-ctx-1",
				Prompt:        "summarize",
				CorrelationID: "trace-ctx-1",
				Iterations: []run.IterationRecord{
					{
						TurnID:         "turn-ctx-1",
						IterationIndex: 1,
						Message: run.MessageRef{
							ID:   "msg-iter-1",
							Role: "assistant",
							Text: "iteration output",
						},
						ToolCalls: []run.ToolCallRecord{
							{
								CallID: "tc-1-1",
								Name:   "fs_read",
								ResultRef: run.ArtifactRef{
									ID:   "artifact-tc-1-1",
									Kind: "tool_result",
									Text: "file contents",
								},
							},
						},
					},
				},
				FinalOutput: run.MessageRef{
					ID:   "msg-final",
					Role: "assistant",
					Text: "final answer",
				},
			},
		},
	}

	builder := contextbuilder.New(reader)
	bundle, err := builder.Build(context.Background(), contextbuilder.Request{
		Ref: "turn/turn-ctx-1",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if bundle.Kind != contextbuilder.RefWholeTurn {
		t.Fatalf("kind=%q want %q", bundle.Kind, contextbuilder.RefWholeTurn)
	}
	if bundle.TurnID != "turn-ctx-1" {
		t.Fatalf("turn_id=%q want turn-ctx-1", bundle.TurnID)
	}
	if bundle.Content == "" {
		t.Fatal("expected non-empty content")
	}
	if !strings.Contains(bundle.Content, "Iteration 1") {
		t.Fatalf("content missing iteration block: %q", bundle.Content)
	}
	if !strings.Contains(bundle.Content, "Final: final answer") {
		t.Fatalf("content missing final output: %q", bundle.Content)
	}
}

func TestContextBuilder_ResolveSliceRef(t *testing.T) {
	t.Parallel()

	reader := &fakeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-ctx-2": {
				ID: "turn-ctx-2",
				FinalOutput: run.MessageRef{
					ID:   "msg-final",
					Role: "assistant",
					Text: "abcdefghij",
				},
			},
		},
	}

	builder := contextbuilder.New(reader)
	bundle, err := builder.Build(context.Background(), contextbuilder.Request{
		Ref: "turn/turn-ctx-2#msg:msg-final:2-7",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if bundle.Kind != contextbuilder.RefSlice {
		t.Fatalf("kind=%q want %q", bundle.Kind, contextbuilder.RefSlice)
	}
	if bundle.Content != "cdefg" {
		t.Fatalf("slice=%q want cdefg", bundle.Content)
	}
}

type fakeTurnReader struct {
	turns map[string]run.TurnRecord
}

func (f *fakeTurnReader) GetTurn(_ context.Context, turnID string) (run.TurnRecord, error) {
	if turn, ok := f.turns[turnID]; ok {
		return turn.Clone(), nil
	}
	return run.TurnRecord{}, run.ErrTurnNotFound
}
