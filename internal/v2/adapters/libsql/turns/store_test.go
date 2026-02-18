package turns

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

func TestTurnStore_SaveAndGetTurn_RoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turns.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.February, 18, 21, 0, 0, 0, time.UTC)
	store := NewStore(db, db.Close)
	store.SetNowForTest(func() time.Time { return now })

	want := run.TurnRecord{
		ID:            "turn-001",
		SessionID:     "run-001",
		TurnIndex:     1,
		TraceID:       "trace-001",
		RootSpanID:    "span-root-001",
		CorrelationID: "corr-001",
		CausationID:   "cause-001",
		RequestID:     "req-001",
		ActorID:       "actor-001",
		Command:       "run",
		Prompt:        "find all TODO items",
		Iterations: []run.IterationRecord{
			{
				TurnID:         "turn-001",
				IterationIndex: 1,
				TraceID:        "trace-001",
				SpanID:         "span-iter-1",
				ParentSpanID:   "span-root-001",
				Message: run.MessageRef{
					ID:   "msg-iter-1",
					Role: "assistant",
					Text: "calling tools",
				},
				ToolCalls: []run.ToolCallRecord{
					{
						CallID:         "tc-1-1",
						IterationIndex: 1,
						Name:           "code_search",
						ArgsJSON:       []byte(`{"pattern":"TODO","path":"."}`),
						Status:         "ok",
						ResultRef: run.ArtifactRef{
							ID:   "artifact-tc-1-1",
							Kind: "tool_result",
							Text: "found 3 matches",
						},
					},
				},
			},
			{
				TurnID:         "turn-001",
				IterationIndex: 2,
				TraceID:        "trace-001",
				SpanID:         "span-iter-2",
				ParentSpanID:   "span-root-001",
				Message: run.MessageRef{
					ID:   "msg-iter-2",
					Role: "assistant",
					Text: "done",
				},
			},
		},
		FinalOutput: run.MessageRef{
			ID:   "msg-final",
			Role: "assistant",
			Text: "There are 3 TODOs.",
		},
	}

	if err := store.SaveTurn(ctx, want); err != nil {
		t.Fatalf("SaveTurn() error = %v", err)
	}

	got, err := store.GetTurn(ctx, "turn-001")
	if err != nil {
		t.Fatalf("GetTurn() error = %v", err)
	}

	if got.ID != want.ID || got.SessionID != want.SessionID || got.TraceID != want.TraceID {
		t.Fatalf("turn root mismatch: got %+v", got)
	}
	if got.FinalOutput.Text != want.FinalOutput.Text {
		t.Fatalf("final_output.text=%q want %q", got.FinalOutput.Text, want.FinalOutput.Text)
	}
	if len(got.Iterations) != 2 {
		t.Fatalf("iterations len=%d want 2", len(got.Iterations))
	}
	if len(got.Iterations[0].ToolCalls) != 1 {
		t.Fatalf("tool calls len=%d want 1", len(got.Iterations[0].ToolCalls))
	}
	if got.Iterations[0].ToolCalls[0].CallID != "tc-1-1" {
		t.Fatalf("call_id=%q want tc-1-1", got.Iterations[0].ToolCalls[0].CallID)
	}
	if !got.CreatedAt.Equal(now) || !got.UpdatedAt.Equal(now) {
		t.Fatalf("timestamps created=%s updated=%s want=%s", got.CreatedAt, got.UpdatedAt, now)
	}
}

func TestTurnStore_SaveTurn_ReplacesPriorLineage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turns_replace.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)

	first := run.TurnRecord{
		ID:        "turn-002",
		SessionID: "run-002",
		Iterations: []run.IterationRecord{
			{
				IterationIndex: 1,
				ToolCalls: []run.ToolCallRecord{
					{CallID: "tc-1-1", Name: "fs_read_file"},
				},
			},
			{
				IterationIndex: 2,
				ToolCalls: []run.ToolCallRecord{
					{CallID: "tc-2-1", Name: "code_search"},
				},
			},
		},
	}
	if err := store.SaveTurn(ctx, first); err != nil {
		t.Fatalf("SaveTurn(first) error = %v", err)
	}

	second := run.TurnRecord{
		ID:        "turn-002",
		SessionID: "run-002",
		Iterations: []run.IterationRecord{
			{
				IterationIndex: 1,
				ToolCalls: []run.ToolCallRecord{
					{CallID: "tc-1-1", Name: "think"},
				},
			},
		},
		FinalOutput: run.MessageRef{ID: "msg-final", Role: "assistant", Text: "done"},
	}
	if err := store.SaveTurn(ctx, second); err != nil {
		t.Fatalf("SaveTurn(second) error = %v", err)
	}

	got, err := store.GetTurn(ctx, "turn-002")
	if err != nil {
		t.Fatalf("GetTurn() error = %v", err)
	}
	if len(got.Iterations) != 1 {
		t.Fatalf("iterations len=%d want 1", len(got.Iterations))
	}
	if len(got.Iterations[0].ToolCalls) != 1 {
		t.Fatalf("tool calls len=%d want 1", len(got.Iterations[0].ToolCalls))
	}
	if got.Iterations[0].ToolCalls[0].Name != "think" {
		t.Fatalf("tool call name=%q want think", got.Iterations[0].ToolCalls[0].Name)
	}
}

func TestTurnStore_SaveArtifact_IdempotentAndStableRefLookup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_artifacts.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	// Force vector path in sqlite test to verify graceful fallback to non-vector writes.
	store.vectorEnabled.Store(true)

	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-003", SessionID: "run-003"}); err != nil {
		t.Fatalf("SaveTurn() error = %v", err)
	}

	first := Artifact{
		TurnID:          "turn-003",
		ArtifactType:    ArtifactTypeEmbedding,
		ArtifactVersion: "v1",
		Summary:         "initial embedding",
		ContentJSON:     []byte(`{"chunks":2}`),
		MetadataJSON:    []byte(`{"source":"enricher"}`),
		Embedding:       []float32{0.1, 0.2, 0.3},
		EmbeddingModel:  "test-embed",
	}
	if err := store.SaveArtifact(ctx, first); err != nil {
		t.Fatalf("SaveArtifact(first) error = %v", err)
	}

	update := first
	update.Summary = "updated embedding"
	update.MetadataJSON = []byte(`{"source":"enricher","rev":2}`)
	if err := store.SaveArtifact(ctx, update); err != nil {
		t.Fatalf("SaveArtifact(update) error = %v", err)
	}

	got, err := store.GetArtifact(ctx, "turn-003", ArtifactTypeEmbedding, "v1")
	if err != nil {
		t.Fatalf("GetArtifact() error = %v", err)
	}
	if got.Summary != "updated embedding" {
		t.Fatalf("summary=%q want updated embedding", got.Summary)
	}
	if got.Ref != BuildArtifactRef("turn-003", ArtifactTypeEmbedding, "v1") {
		t.Fatalf("ref=%q unexpected", got.Ref)
	}
	if len(got.Embedding) != 3 {
		t.Fatalf("embedding length=%d want 3", len(got.Embedding))
	}

	byRef, err := store.GetArtifactByRef(ctx, "turn/turn-003/artifact/embedding/v1")
	if err != nil {
		t.Fatalf("GetArtifactByRef() error = %v", err)
	}
	if byRef.Summary != got.Summary {
		t.Fatalf("artifact by ref summary=%q want %q", byRef.Summary, got.Summary)
	}

	list, err := store.ListArtifacts(ctx, "turn-003")
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("artifacts len=%d want 1", len(list))
	}
}

func TestTurnStore_SaveArtifact_RejectsInvalidType(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_artifact_invalid.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	err = store.SaveArtifact(ctx, Artifact{
		TurnID:          "turn-004",
		ArtifactType:    "unknown",
		ArtifactVersion: "v1",
	})
	if !errors.Is(err, ErrInvalidArtifactType) {
		t.Fatalf("SaveArtifact() error=%v want ErrInvalidArtifactType", err)
	}
}

func TestParseArtifactRef(t *testing.T) {
	t.Parallel()

	turnID, artifactType, artifactVersion, err := ParseArtifactRef("turn/turn-xyz/artifact/learning/v2")
	if err != nil {
		t.Fatalf("ParseArtifactRef() error = %v", err)
	}
	if turnID != "turn-xyz" || artifactType != "learning" || artifactVersion != "v2" {
		t.Fatalf("unexpected parse values: %q %q %q", turnID, artifactType, artifactVersion)
	}

	_, _, _, err = ParseArtifactRef("turn/turn-xyz/iter/1")
	if !errors.Is(err, ErrInvalidArtifactRef) {
		t.Fatalf("ParseArtifactRef(invalid) error=%v want ErrInvalidArtifactRef", err)
	}
}
