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

func TestTurnStore_ListTurns_BySessionAndTime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turns_list.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	base := time.Date(2026, time.February, 18, 12, 0, 0, 0, time.UTC)
	store.SetNowForTest(func() time.Time { return base })

	mustSave := func(turn run.TurnRecord) {
		t.Helper()
		if err := store.SaveTurn(ctx, turn); err != nil {
			t.Fatalf("SaveTurn(%s) error = %v", turn.ID, err)
		}
	}

	mustSave(run.TurnRecord{
		ID:        "turn-101",
		SessionID: "run-list-1",
		CreatedAt: base.Add(-2 * time.Hour),
		UpdatedAt: base.Add(-2 * time.Hour),
	})
	mustSave(run.TurnRecord{
		ID:        "turn-102",
		SessionID: "run-list-1",
		CreatedAt: base.Add(-1 * time.Hour),
		UpdatedAt: base.Add(-1 * time.Hour),
	})
	mustSave(run.TurnRecord{
		ID:        "turn-201",
		SessionID: "run-list-2",
		CreatedAt: base,
		UpdatedAt: base,
	})

	asc, err := store.ListTurns(ctx, "run-list-1", run.TurnListOptions{Asc: true})
	if err != nil {
		t.Fatalf("ListTurns(asc) error = %v", err)
	}
	if len(asc) != 2 {
		t.Fatalf("asc len=%d want 2", len(asc))
	}
	if asc[0].ID != "turn-101" || asc[1].ID != "turn-102" {
		t.Fatalf("asc ids = [%s,%s] want [turn-101,turn-102]", asc[0].ID, asc[1].ID)
	}

	descLimited, err := store.ListTurns(ctx, "run-list-1", run.TurnListOptions{Limit: 1, Asc: false})
	if err != nil {
		t.Fatalf("ListTurns(desc limited) error = %v", err)
	}
	if len(descLimited) != 1 || descLimited[0].ID != "turn-102" {
		t.Fatalf("desc limited unexpected ids = %+v", descLimited)
	}

	sinceFiltered, err := store.ListTurns(ctx, "run-list-1", run.TurnListOptions{
		Asc:   true,
		Since: base.Add(-90 * time.Minute),
	})
	if err != nil {
		t.Fatalf("ListTurns(since) error = %v", err)
	}
	if len(sinceFiltered) != 1 || sinceFiltered[0].ID != "turn-102" {
		t.Fatalf("since filtered unexpected ids = %+v", sinceFiltered)
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

func TestTurnStore_SearchArtifactsByEmbedding_FallbackAndFilters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_artifact_search.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	base := time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC)
	store.SetNowForTest(func() time.Time { return base })

	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-s1-a", SessionID: "run-s1"}); err != nil {
		t.Fatalf("SaveTurn(turn-s1-a) error = %v", err)
	}
	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-s1-b", SessionID: "run-s1"}); err != nil {
		t.Fatalf("SaveTurn(turn-s1-b) error = %v", err)
	}
	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-s2-a", SessionID: "run-s2"}); err != nil {
		t.Fatalf("SaveTurn(turn-s2-a) error = %v", err)
	}

	if err := store.SaveArtifact(ctx, Artifact{
		TurnID:          "turn-s1-a",
		ArtifactType:    ArtifactTypeEmbedding,
		ArtifactVersion: "v1",
		Summary:         "s1 embedding primary",
		Embedding:       []float32{1.0, 0.0, 0.0},
	}); err != nil {
		t.Fatalf("SaveArtifact(turn-s1-a embedding) error = %v", err)
	}
	if err := store.SaveArtifact(ctx, Artifact{
		TurnID:          "turn-s1-b",
		ArtifactType:    ArtifactTypeAnnotation,
		ArtifactVersion: "v1",
		Summary:         "s1 annotation secondary",
		Embedding:       []float32{0.7, 0.3, 0.0},
	}); err != nil {
		t.Fatalf("SaveArtifact(turn-s1-b annotation) error = %v", err)
	}
	if err := store.SaveArtifact(ctx, Artifact{
		TurnID:          "turn-s2-a",
		ArtifactType:    ArtifactTypeEmbedding,
		ArtifactVersion: "v1",
		Summary:         "s2 embedding other session",
		Embedding:       []float32{0.0, 1.0, 0.0},
	}); err != nil {
		t.Fatalf("SaveArtifact(turn-s2-a embedding) error = %v", err)
	}

	// Force vector-query path on sqlite to verify graceful downgrade and fallback search.
	store.vectorEnabled.Store(true)

	results, err := store.SearchArtifactsByEmbedding(ctx, []float32{1.0, 0.0, 0.0}, run.ArtifactSearchOptions{
		SessionID: "run-s1",
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("SearchArtifactsByEmbedding() error = %v", err)
	}
	if results.SearchPath != run.ArtifactSearchPathFallback {
		t.Fatalf("search path=%q want %q", results.SearchPath, run.ArtifactSearchPathFallback)
	}
	if results.VectorCapability != run.ArtifactVectorCapabilityDisabled {
		t.Fatalf("vector capability=%q want %q", results.VectorCapability, run.ArtifactVectorCapabilityDisabled)
	}
	if store.vectorEnabled.Load() {
		t.Fatal("vectorEnabled should be disabled after unsupported vector search fallback")
	}
	if len(results.Hits) != 2 {
		t.Fatalf("results len=%d want 2", len(results.Hits))
	}
	if results.Hits[0].TurnID != "turn-s1-a" {
		t.Fatalf("top result turn=%q want turn-s1-a", results.Hits[0].TurnID)
	}
	if results.Hits[0].Similarity < results.Hits[1].Similarity {
		t.Fatalf("results not sorted by similarity desc: %.4f < %.4f", results.Hits[0].Similarity, results.Hits[1].Similarity)
	}

	typed, err := store.SearchArtifactsByEmbedding(ctx, []float32{1.0, 0.0, 0.0}, run.ArtifactSearchOptions{
		SessionID:     "run-s1",
		ArtifactTypes: []string{ArtifactTypeAnnotation},
		Limit:         5,
	})
	if err != nil {
		t.Fatalf("SearchArtifactsByEmbedding(annotation) error = %v", err)
	}
	if len(typed.Hits) != 1 {
		t.Fatalf("typed results len=%d want 1", len(typed.Hits))
	}
	if typed.Hits[0].ArtifactType != ArtifactTypeAnnotation {
		t.Fatalf("typed result artifact_type=%q want %q", typed.Hits[0].ArtifactType, ArtifactTypeAnnotation)
	}

	invalidType, err := store.SearchArtifactsByEmbedding(ctx, []float32{1.0, 0.0, 0.0}, run.ArtifactSearchOptions{
		ArtifactTypes: []string{"bogus"},
	})
	if !errors.Is(err, ErrInvalidArtifactType) {
		t.Fatalf("invalid type error=%v want ErrInvalidArtifactType", err)
	}
	if len(invalidType.Hits) != 0 {
		t.Fatalf("invalid type results=%v want empty", invalidType.Hits)
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
