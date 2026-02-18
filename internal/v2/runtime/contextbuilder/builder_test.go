package contextbuilder_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

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

func TestContextBuilder_BuildTemporalDays(t *testing.T) {
	t.Parallel()

	reader := &fakeTurnReader{
		sessionTurns: []run.TurnRecord{
			{
				ID:        "turn-a",
				SessionID: "run-ctx",
				CreatedAt: time.Date(2026, time.February, 17, 10, 0, 0, 0, time.UTC),
				Command:   "ask",
				Prompt:    "first prompt",
				FinalOutput: run.MessageRef{
					ID:   "msg-a",
					Role: "assistant",
					Text: "first result",
				},
				Iterations: []run.IterationRecord{{IterationIndex: 1}},
			},
			{
				ID:        "turn-b",
				SessionID: "run-ctx",
				CreatedAt: time.Date(2026, time.February, 17, 13, 0, 0, 0, time.UTC),
				Command:   "run",
				FinalOutput: run.MessageRef{
					ID:   "msg-b",
					Role: "assistant",
					Text: "second result",
				},
			},
			{
				ID:        "turn-c",
				SessionID: "run-ctx",
				CreatedAt: time.Date(2026, time.February, 18, 9, 0, 0, 0, time.UTC),
				Command:   "ask",
				FinalOutput: run.MessageRef{
					ID:   "msg-c",
					Role: "assistant",
					Text: "latest result",
				},
			},
		},
	}

	builder := contextbuilder.New(reader)
	bundle, err := builder.BuildTemporal(context.Background(), contextbuilder.TemporalRequest{
		SessionID: "run-ctx",
		View:      contextbuilder.ViewDays,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("BuildTemporal() error = %v", err)
	}
	if bundle.View != contextbuilder.ViewDays {
		t.Fatalf("view=%q want %q", bundle.View, contextbuilder.ViewDays)
	}
	if len(bundle.Buckets) != 2 {
		t.Fatalf("bucket count=%d want 2", len(bundle.Buckets))
	}
	if bundle.Buckets[0].Key != "2026-02-18" {
		t.Fatalf("bucket[0].key=%q want 2026-02-18", bundle.Buckets[0].Key)
	}
	if !contains(bundle.ExpandableDates, "2026-02-17") || !contains(bundle.ExpandableDates, "2026-02-18") {
		t.Fatalf("expandable_dates=%v want both days", bundle.ExpandableDates)
	}
	if !contains(bundle.ExpandableRefs, "hour:2026-02-18T09") {
		t.Fatalf("expandable_refs=%v missing hour ref", bundle.ExpandableRefs)
	}
	if !contains(bundle.ExpandableRefs, "turn/turn-c") {
		t.Fatalf("expandable_refs=%v missing turn ref", bundle.ExpandableRefs)
	}
	if !strings.Contains(bundle.Content, "Temporal view: days") {
		t.Fatalf("content missing header: %q", bundle.Content)
	}
}

func TestContextBuilder_BuildTemporalWeeks(t *testing.T) {
	t.Parallel()

	reader := &fakeTurnReader{
		sessionTurns: []run.TurnRecord{
			{
				ID:        "turn-w1",
				SessionID: "run-week",
				CreatedAt: time.Date(2026, time.January, 30, 10, 0, 0, 0, time.UTC),
				Command:   "ask",
			},
			{
				ID:        "turn-w2",
				SessionID: "run-week",
				CreatedAt: time.Date(2026, time.February, 3, 9, 0, 0, 0, time.UTC),
				Command:   "run",
			},
		},
	}

	builder := contextbuilder.New(reader)
	bundle, err := builder.BuildTemporal(context.Background(), contextbuilder.TemporalRequest{
		SessionID: "run-week",
		View:      contextbuilder.ViewWeeks,
	})
	if err != nil {
		t.Fatalf("BuildTemporal() error = %v", err)
	}
	if len(bundle.Buckets) == 0 {
		t.Fatal("expected at least one week bucket")
	}
	if !strings.HasPrefix(bundle.Buckets[0].Key, "2026-W") {
		t.Fatalf("unexpected week bucket key %q", bundle.Buckets[0].Key)
	}
	if !containsPrefix(bundle.ExpandableRefs, "day:") {
		t.Fatalf("expandable_refs=%v missing day drill refs", bundle.ExpandableRefs)
	}
}

func TestContextBuilder_BuildTemporalRequiresTimelineReader(t *testing.T) {
	t.Parallel()

	reader := &readOnlyTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-only": {ID: "turn-only"},
		},
	}

	builder := contextbuilder.New(reader)
	_, err := builder.BuildTemporal(context.Background(), contextbuilder.TemporalRequest{
		SessionID: "run-only",
		View:      contextbuilder.ViewDays,
	})
	if !errors.Is(err, contextbuilder.ErrTemporalUnsupported) {
		t.Fatalf("BuildTemporal() error=%v want ErrTemporalUnsupported", err)
	}
}

type fakeTurnReader struct {
	turns        map[string]run.TurnRecord
	sessionTurns []run.TurnRecord
}

func (f *fakeTurnReader) GetTurn(_ context.Context, turnID string) (run.TurnRecord, error) {
	if turn, ok := f.turns[turnID]; ok {
		return turn.Clone(), nil
	}
	for _, turn := range f.sessionTurns {
		if turn.ID == turnID {
			return turn.Clone(), nil
		}
	}
	return run.TurnRecord{}, run.ErrTurnNotFound
}

func (f *fakeTurnReader) ListTurns(_ context.Context, sessionID string, opts run.TurnListOptions) ([]run.TurnRecord, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}

	filtered := make([]run.TurnRecord, 0, len(f.sessionTurns))
	for _, turn := range f.sessionTurns {
		if turn.SessionID != sessionID {
			continue
		}
		if !opts.Since.IsZero() && turn.CreatedAt.Before(opts.Since) {
			continue
		}
		if !opts.Until.IsZero() && turn.CreatedAt.After(opts.Until) {
			continue
		}
		filtered = append(filtered, turn.Clone())
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].ID < filtered[j].ID
		}
		if opts.Asc {
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	if opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}
	return filtered, nil
}

type readOnlyTurnReader struct {
	turns map[string]run.TurnRecord
}

func (r *readOnlyTurnReader) GetTurn(_ context.Context, turnID string) (run.TurnRecord, error) {
	if turn, ok := r.turns[turnID]; ok {
		return turn.Clone(), nil
	}
	return run.TurnRecord{}, run.ErrTurnNotFound
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func containsPrefix(items []string, prefix string) bool {
	for _, item := range items {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}
