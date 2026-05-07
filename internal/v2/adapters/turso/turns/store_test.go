package turns

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/v2/core/run"
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

func TestDefaultV2TurnsVectorDimensions_Precedence(t *testing.T) {
	t.Setenv("FOXCTL_V2_TURNS_VECTOR_DIMS", "")
	t.Setenv("FOXCTL_VECTOR_DIMS", "")
	if hasVectorDimsOverride() {
		t.Fatalf("hasVectorDimsOverride() = true, want false")
	}
	if got := defaultV2TurnsVectorDimensions(); got != defaultV2TurnsVectorDims {
		t.Fatalf("defaultV2TurnsVectorDimensions() = %d, want %d", got, defaultV2TurnsVectorDims)
	}

	t.Setenv("FOXCTL_VECTOR_DIMS", "1024")
	if !hasVectorDimsOverride() {
		t.Fatalf("hasVectorDimsOverride() = false, want true")
	}
	if got := defaultV2TurnsVectorDimensions(); got != 1024 {
		t.Fatalf("defaultV2TurnsVectorDimensions() with FOXCTL_VECTOR_DIMS = %d, want 1024", got)
	}

	t.Setenv("FOXCTL_V2_TURNS_VECTOR_DIMS", "1536")
	if got := defaultV2TurnsVectorDimensions(); got != 1536 {
		t.Fatalf("defaultV2TurnsVectorDimensions() with FOXCTL_V2_TURNS_VECTOR_DIMS = %d, want 1536", got)
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

func TestTurnStore_SaveAndListEpisodes_IdempotentBoundaryKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_episodes.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	base := time.Date(2026, time.February, 22, 10, 0, 0, 0, time.UTC)
	store.SetNowForTest(func() time.Time { return base })

	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-ep-1", SessionID: "run-ep"}); err != nil {
		t.Fatalf("SaveTurn(turn-ep-1) error = %v", err)
	}
	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-ep-2", SessionID: "run-ep"}); err != nil {
		t.Fatalf("SaveTurn(turn-ep-2) error = %v", err)
	}

	first := run.EpisodeRecord{
		ID:             "episode-001",
		SessionID:      "run-ep",
		EpisodeVersion: "v1",
		BoundaryKey:    "boundary:turn-ep-2",
		StartTurnID:    "turn-ep-1",
		EndTurnID:      "turn-ep-2",
		StartTurnIndex: 1,
		EndTurnIndex:   2,
		Topic:          "episode skeleton",
		Summary:        "first summary",
		SalienceScore:  1.7,
		IsLandmark:     true,
		AnchorRefs:     []string{"anchor:2", "anchor:1", "anchor:1"},
	}
	if err := store.SaveEpisode(ctx, first); err != nil {
		t.Fatalf("SaveEpisode(first) error = %v", err)
	}

	update := first
	update.ID = "episode-002" // should not replace existing id on conflict
	update.Summary = "updated summary"
	update.SalienceScore = -0.3
	update.AnchorRefs = []string{"anchor:3", "anchor:1"}
	if err := store.SaveEpisode(ctx, update); err != nil {
		t.Fatalf("SaveEpisode(update) error = %v", err)
	}

	list, err := store.ListEpisodes(ctx, "run-ep", run.EpisodeListOptions{Asc: true})
	if err != nil {
		t.Fatalf("ListEpisodes() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("episodes len=%d want 1", len(list))
	}
	got := list[0]
	if got.ID != "episode-001" {
		t.Fatalf("episode id=%q want episode-001", got.ID)
	}
	if got.Summary != "updated summary" {
		t.Fatalf("summary=%q want updated summary", got.Summary)
	}
	if got.SalienceScore != 0 {
		t.Fatalf("salience_score=%.2f want 0", got.SalienceScore)
	}
	if len(got.AnchorRefs) != 2 || got.AnchorRefs[0] != "anchor:1" || got.AnchorRefs[1] != "anchor:3" {
		t.Fatalf("anchor refs=%v want [anchor:1 anchor:3]", got.AnchorRefs)
	}

	landmarks, err := store.ListEpisodes(ctx, "run-ep", run.EpisodeListOptions{LandmarkOnly: true})
	if err != nil {
		t.Fatalf("ListEpisodes(landmark) error = %v", err)
	}
	if len(landmarks) != 1 || !landmarks[0].IsLandmark {
		t.Fatalf("landmark filter unexpected output: %+v", landmarks)
	}
}

func TestTurnStore_GetEpisode_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_episode_not_found.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	_, err = store.GetEpisode(ctx, "missing-episode")
	if !errors.Is(err, run.ErrEpisodeNotFound) {
		t.Fatalf("GetEpisode() error=%v want run.ErrEpisodeNotFound", err)
	}
}

func TestTurnStore_SaveEpisode_AutoIDBoundaryHashAvoidsCollisions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_episode_id_hash.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-eh-1", SessionID: "run-eh"}); err != nil {
		t.Fatalf("SaveTurn(turn-eh-1) error = %v", err)
	}
	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-eh-2", SessionID: "run-eh"}); err != nil {
		t.Fatalf("SaveTurn(turn-eh-2) error = %v", err)
	}

	one := run.EpisodeRecord{
		SessionID:      "run-eh",
		EpisodeVersion: "v1",
		BoundaryKey:    "boundary/turn-eh-2",
		StartTurnID:    "turn-eh-1",
		EndTurnID:      "turn-eh-2",
	}
	two := run.EpisodeRecord{
		SessionID:      "run-eh",
		EpisodeVersion: "v1",
		BoundaryKey:    "boundary turn-eh-2",
		StartTurnID:    "turn-eh-1",
		EndTurnID:      "turn-eh-2",
	}

	if err := store.SaveEpisode(ctx, one); err != nil {
		t.Fatalf("SaveEpisode(one) error = %v", err)
	}
	if err := store.SaveEpisode(ctx, two); err != nil {
		t.Fatalf("SaveEpisode(two) error = %v", err)
	}

	list, err := store.ListEpisodes(ctx, "run-eh", run.EpisodeListOptions{Asc: true})
	if err != nil {
		t.Fatalf("ListEpisodes() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("episodes len=%d want 2", len(list))
	}
	if list[0].ID == list[1].ID {
		t.Fatalf("episode ids must be distinct for distinct boundary keys: %q", list[0].ID)
	}
}

func TestTurnStore_ListEpisodes_DeterministicOrderByCreatedAtThenID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_episode_ordering.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	for _, turnID := range []string{"turn-ord-1", "turn-ord-2", "turn-ord-3"} {
		if err := store.SaveTurn(ctx, run.TurnRecord{ID: turnID, SessionID: "run-ord"}); err != nil {
			t.Fatalf("SaveTurn(%s) error = %v", turnID, err)
		}
	}

	base := time.Date(2026, time.February, 22, 15, 0, 0, 0, time.UTC)
	episodes := []run.EpisodeRecord{
		{
			ID:             "ep-b",
			SessionID:      "run-ord",
			EpisodeVersion: "v1",
			BoundaryKey:    "boundary:b",
			StartTurnID:    "turn-ord-1",
			EndTurnID:      "turn-ord-1",
			StartTurnIndex: 1,
			EndTurnIndex:   1,
			CreatedAt:      base,
			UpdatedAt:      base,
		},
		{
			ID:             "ep-a",
			SessionID:      "run-ord",
			EpisodeVersion: "v1",
			BoundaryKey:    "boundary:a",
			StartTurnID:    "turn-ord-2",
			EndTurnID:      "turn-ord-2",
			StartTurnIndex: 2,
			EndTurnIndex:   2,
			CreatedAt:      base, // same timestamp as ep-b; ID tie-breaker should sort ep-a first.
			UpdatedAt:      base,
		},
		{
			ID:             "ep-c",
			SessionID:      "run-ord",
			EpisodeVersion: "v1",
			BoundaryKey:    "boundary:c",
			StartTurnID:    "turn-ord-3",
			EndTurnID:      "turn-ord-3",
			StartTurnIndex: 3,
			EndTurnIndex:   3,
			CreatedAt:      base.Add(1 * time.Minute),
			UpdatedAt:      base.Add(1 * time.Minute),
		},
	}
	for _, episode := range episodes {
		if err := store.SaveEpisode(ctx, episode); err != nil {
			t.Fatalf("SaveEpisode(%s) error = %v", episode.ID, err)
		}
	}

	asc, err := store.ListEpisodes(ctx, "run-ord", run.EpisodeListOptions{Asc: true, Limit: 10})
	if err != nil {
		t.Fatalf("ListEpisodes(asc) error = %v", err)
	}
	if len(asc) != 3 {
		t.Fatalf("asc len=%d want 3", len(asc))
	}
	if asc[0].ID != "ep-a" || asc[1].ID != "ep-b" || asc[2].ID != "ep-c" {
		t.Fatalf("asc order=%q,%q,%q want ep-a,ep-b,ep-c", asc[0].ID, asc[1].ID, asc[2].ID)
	}

	desc, err := store.ListEpisodes(ctx, "run-ord", run.EpisodeListOptions{Asc: false, Limit: 10})
	if err != nil {
		t.Fatalf("ListEpisodes(desc) error = %v", err)
	}
	if len(desc) != 3 {
		t.Fatalf("desc len=%d want 3", len(desc))
	}
	if desc[0].ID != "ep-c" || desc[1].ID != "ep-b" || desc[2].ID != "ep-a" {
		t.Fatalf("desc order=%q,%q,%q want ep-c,ep-b,ep-a", desc[0].ID, desc[1].ID, desc[2].ID)
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
	store.vectorDimensions = 3

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

func TestTurnStore_SaveNarrative_RejectsUncitedClaims(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_narrative_invalid.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	if err := store.SaveTurn(ctx, run.TurnRecord{
		ID:        "turn-n-invalid-1",
		SessionID: "run-n-invalid",
		TurnIndex: 1,
	}); err != nil {
		t.Fatalf("SaveTurn() error = %v", err)
	}

	err = store.SaveNarrative(ctx, run.NarrativeRecord{
		SessionID:       "run-n-invalid",
		ArtifactVersion: "v1",
		Summary:         "invalid narrative",
		Claims: []run.NarrativeClaim{
			{
				Text:       "missing anchors should be rejected",
				AnchorRefs: nil,
			},
		},
	})
	if !errors.Is(err, ErrInvalidNarrativeClaims) {
		t.Fatalf("SaveNarrative() error=%v want ErrInvalidNarrativeClaims", err)
	}
}

func TestTurnStore_SaveAndGetNarrative_IdempotentSessionScoped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_narrative_roundtrip.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	base := time.Date(2026, time.February, 23, 18, 0, 0, 0, time.UTC)
	store.SetNowForTest(func() time.Time { return base })

	turnsInSession := []run.TurnRecord{
		{ID: "turn-n-1", SessionID: "run-narrative", TurnIndex: 1},
		{ID: "turn-n-2", SessionID: "run-narrative", TurnIndex: 2},
	}
	for _, turn := range turnsInSession {
		if err := store.SaveTurn(ctx, turn); err != nil {
			t.Fatalf("SaveTurn(%s) error = %v", turn.ID, err)
		}
	}

	first := run.NarrativeRecord{
		SessionID:       "run-narrative",
		TurnID:          "turn-n-1",
		ArtifactVersion: "v1",
		Summary:         "first summary",
		Claims: []run.NarrativeClaim{
			{
				Text:       "first claim",
				AnchorRefs: []string{"turn/turn-n-1"},
			},
		},
		AnchorRefs:      []string{"turn/turn-n-1"},
		SourceTurnID:    "turn-n-1",
		SourceTurnIndex: 1,
		SourceTurnCount: 2,
		UpdatedAt:       base,
	}
	if err := store.SaveNarrative(ctx, first); err != nil {
		t.Fatalf("SaveNarrative(first) error = %v", err)
	}

	second := run.NarrativeRecord{
		SessionID:       "run-narrative",
		TurnID:          "turn-n-2",
		ArtifactVersion: "v1",
		Summary:         "updated summary",
		Claims: []run.NarrativeClaim{
			{
				Text:       "updated claim",
				AnchorRefs: []string{"turn/turn-n-2", "turn/turn-n-2/artifact/annotation/v1"},
			},
		},
		AnchorRefs:      []string{"turn/turn-n-2", "turn/turn-n-2/artifact/annotation/v1"},
		SourceTurnID:    "turn-n-2",
		SourceTurnIndex: 2,
		SourceTurnCount: 2,
		UpdatedAt:       base.Add(5 * time.Minute),
	}
	if err := store.SaveNarrative(ctx, second); err != nil {
		t.Fatalf("SaveNarrative(second) error = %v", err)
	}

	got, err := store.GetNarrative(ctx, "run-narrative", "v1")
	if err != nil {
		t.Fatalf("GetNarrative() error = %v", err)
	}
	if got.Summary != "updated summary" {
		t.Fatalf("summary=%q want updated summary", got.Summary)
	}
	if len(got.Claims) != 1 || got.Claims[0].Text != "updated claim" {
		t.Fatalf("claims=%+v want updated claim", got.Claims)
	}
	if got.SourceTurnID != "turn-n-2" {
		t.Fatalf("source_turn_id=%q want turn-n-2", got.SourceTurnID)
	}
	if got.SourceTurnIndex != 2 {
		t.Fatalf("source_turn_index=%d want 2", got.SourceTurnIndex)
	}
	if got.SourceTurnCount != 2 {
		t.Fatalf("source_turn_count=%d want 2", got.SourceTurnCount)
	}

	var narrativeRows int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM v2_turn_artifacts a
		JOIN v2_turns t ON t.id = a.turn_id
		WHERE t.session_id = $1 AND a.artifact_type = $2 AND a.artifact_version = $3
	`, "run-narrative", ArtifactTypeNarrative, "v1").Scan(&narrativeRows); err != nil {
		t.Fatalf("count narrative rows: %v", err)
	}
	if narrativeRows != 1 {
		t.Fatalf("narrative rows=%d want 1", narrativeRows)
	}
}

func TestTurnStore_SaveNarrative_RejectsTurnIDFromOtherSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_narrative_session_guard.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-a-1", SessionID: "run-a", TurnIndex: 1}); err != nil {
		t.Fatalf("SaveTurn(turn-a-1) error = %v", err)
	}
	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-b-1", SessionID: "run-b", TurnIndex: 1}); err != nil {
		t.Fatalf("SaveTurn(turn-b-1) error = %v", err)
	}

	err = store.SaveNarrative(ctx, run.NarrativeRecord{
		SessionID:       "run-a",
		TurnID:          "turn-b-1",
		ArtifactVersion: "v1",
		Summary:         "cross-session should fail",
		Claims: []run.NarrativeClaim{
			{
				Text:       "claim",
				AnchorRefs: []string{"turn/turn-a-1"},
			},
		},
		AnchorRefs: []string{"turn/turn-a-1"},
	})
	if err == nil {
		t.Fatalf("SaveNarrative() error=nil want session mismatch error")
	}
	if !strings.Contains(err.Error(), "not in session") {
		t.Fatalf("SaveNarrative() error=%v want session mismatch", err)
	}
}

func TestTurnStore_GetNarrative_PrefersCanonicalFirstTurnArtifact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_narrative_canonical_read.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	base := time.Date(2026, time.February, 24, 12, 0, 0, 0, time.UTC)
	store.SetNowForTest(func() time.Time { return base })

	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-c-1", SessionID: "run-c", TurnIndex: 1}); err != nil {
		t.Fatalf("SaveTurn(turn-c-1) error = %v", err)
	}
	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-c-2", SessionID: "run-c", TurnIndex: 2}); err != nil {
		t.Fatalf("SaveTurn(turn-c-2) error = %v", err)
	}

	legacyContent := narrativeContent{
		Summary: "legacy duplicate",
		Claims: []run.NarrativeClaim{
			{
				Text:       "legacy claim",
				AnchorRefs: []string{"turn/turn-c-2"},
			},
		},
		AnchorRefs:      []string{"turn/turn-c-2"},
		SourceTurnID:    "turn-c-2",
		SourceTurnIndex: 2,
		SourceTurnCount: 2,
	}
	if err := store.SaveArtifact(ctx, Artifact{
		TurnID:          "turn-c-2",
		ArtifactType:    ArtifactTypeNarrative,
		ArtifactVersion: "v1",
		Summary:         "legacy duplicate",
		ContentJSON:     mustMarshalJSON(legacyContent),
		MetadataJSON:    mustMarshalJSON(map[string]any{"session_id": "run-c"}),
		CreatedAt:       base.Add(10 * time.Minute),
		UpdatedAt:       base.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("SaveArtifact(legacy duplicate) error = %v", err)
	}

	canonicalContent := narrativeContent{
		Summary: "canonical summary",
		Claims: []run.NarrativeClaim{
			{
				Text:       "canonical claim",
				AnchorRefs: []string{"turn/turn-c-1"},
			},
		},
		AnchorRefs:      []string{"turn/turn-c-1"},
		SourceTurnID:    "turn-c-1",
		SourceTurnIndex: 1,
		SourceTurnCount: 2,
	}
	if err := store.SaveArtifact(ctx, Artifact{
		TurnID:          "turn-c-1",
		ArtifactType:    ArtifactTypeNarrative,
		ArtifactVersion: "v1",
		Summary:         "canonical summary",
		ContentJSON:     mustMarshalJSON(canonicalContent),
		MetadataJSON:    mustMarshalJSON(map[string]any{"session_id": "run-c"}),
		CreatedAt:       base,
		UpdatedAt:       base,
	}); err != nil {
		t.Fatalf("SaveArtifact(canonical) error = %v", err)
	}

	got, err := store.GetNarrative(ctx, "run-c", "v1")
	if err != nil {
		t.Fatalf("GetNarrative() error = %v", err)
	}
	if got.TurnID != "turn-c-1" {
		t.Fatalf("turn_id=%q want turn-c-1", got.TurnID)
	}
	if got.Summary != "canonical summary" {
		t.Fatalf("summary=%q want canonical summary", got.Summary)
	}
}

func TestTurnStore_SearchArtifactsByEmbedding_VectorAndFilters(t *testing.T) {
	ctx := context.Background()
	storageRoot := t.TempDir()
	requireNative := requireNativeVectorSQL()

	t.Setenv("FOXCTL_V2_TURNS_DB_DRIVER", "turso")
	t.Setenv("FOXCTL_V2_TURNS_DB_PATH", filepath.Join(storageRoot, "turn_artifact_search.turso"))
	t.Setenv("FOXCTL_V2_TURNS_VECTOR_SEARCH", "1")
	t.Setenv("FOXCTL_V2_TURNS_VECTOR_DIMS", "3")
	t.Setenv("FOXCTL_VECTOR_DIMS", "3")

	store, err := Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("Open(turso) error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if !store.vectorEnabled.Load() {
		if requireNative {
			t.Fatal("native vector SQL required, but store initialized without vector capability")
		}
		t.Skip("skipping: turso vector capability unavailable in this test environment")
	}

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

	results, err := store.SearchArtifactsByEmbedding(ctx, []float32{1.0, 0.0, 0.0}, run.ArtifactSearchOptions{
		SessionID: "run-s1",
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("SearchArtifactsByEmbedding() error = %v", err)
	}
	if results.SearchPath != run.ArtifactSearchPathVector {
		t.Fatalf("search path=%q want %q", results.SearchPath, run.ArtifactSearchPathVector)
	}
	if results.VectorCapability != run.ArtifactVectorCapabilityEnabled {
		t.Fatalf("vector capability=%q want %q", results.VectorCapability, run.ArtifactVectorCapabilityEnabled)
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

func TestTurnStore_SearchArtifactsByEmbedding_WorkingContextFallbackLadder(t *testing.T) {
	ctx := context.Background()
	storageRoot := t.TempDir()
	requireNative := requireNativeVectorSQL()

	t.Setenv("FOXCTL_V2_TURNS_DB_DRIVER", "turso")
	t.Setenv("FOXCTL_V2_TURNS_DB_PATH", filepath.Join(storageRoot, "turn_artifact_working_context.turso"))
	t.Setenv("FOXCTL_V2_TURNS_VECTOR_SEARCH", "1")
	t.Setenv("FOXCTL_V2_TURNS_VECTOR_DIMS", "3")
	t.Setenv("FOXCTL_VECTOR_DIMS", "3")

	store, err := Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("Open(turso) error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if !store.vectorEnabled.Load() {
		if requireNative {
			t.Fatal("native vector SQL required, but store initialized without vector capability")
		}
		t.Skip("skipping: turso vector capability unavailable in this test environment")
	}

	for _, turn := range []run.TurnRecord{
		{ID: "turn-wc-1", SessionID: "run-wc"},
		{ID: "turn-wc-2", SessionID: "run-wc"},
		{ID: "turn-wc-3", SessionID: "run-wc"},
		{ID: "turn-wc-4", SessionID: "run-other"},
	} {
		if err := store.SaveTurn(ctx, turn); err != nil {
			t.Fatalf("SaveTurn(%s) error = %v", turn.ID, err)
		}
	}

	saveArtifact := func(turnID, summary string, embedding []float32, metadata string) {
		t.Helper()
		if err := store.SaveArtifact(ctx, Artifact{
			TurnID:          turnID,
			ArtifactType:    ArtifactTypeEmbedding,
			ArtifactVersion: "v1",
			Summary:         summary,
			Embedding:       embedding,
			MetadataJSON:    []byte(metadata),
		}); err != nil {
			t.Fatalf("SaveArtifact(%s) error = %v", turnID, err)
		}
	}

	saveArtifact(
		"turn-wc-1",
		"auth legacy",
		[]float32{1.0, 0.0, 0.0},
		`{"workspace_id":"ws-main","labels":["auth","decision"],"salience":0.30,"active_files":["legacy/auth.go"]}`,
	)
	saveArtifact(
		"turn-wc-2",
		"auth critical",
		[]float32{0.95, 0.05, 0.0},
		`{"workspace_id":"ws-main","labels":["Auth"],"salience":0.90,"active_files":["internal/auth/service.go"]}`,
	)
	saveArtifact(
		"turn-wc-3",
		"build context",
		[]float32{0.90, 0.10, 0.0},
		`{"workspace":"ws-main","labels":["build"],"salience":0.70,"active_files":["internal/build/pipeline.go"]}`,
	)
	saveArtifact(
		"turn-wc-4",
		"other session",
		[]float32{1.0, 0.0, 0.0},
		`{"workspace_id":"ws-other","labels":["auth"],"salience":0.95,"active_files":["internal/auth/service.go"]}`,
	)

	strict, err := store.SearchArtifactsByEmbedding(ctx, []float32{1.0, 0.0, 0.0}, run.ArtifactSearchOptions{
		SessionID: "run-wc",
		Limit:     5,
		Working: run.WorkingContext{
			WorkspaceID:    "ws-main",
			ActiveFiles:    []string{"internal/auth/service.go"},
			RequiredLabels: []string{"auth"},
			MinSalience:    0.80,
		},
	})
	if err != nil {
		t.Fatalf("SearchArtifactsByEmbedding(strict) error = %v", err)
	}
	if !strict.WorkingApplied || strict.FallbackLevel != 0 {
		t.Fatalf("strict working metadata applied=%v level=%d want true/0", strict.WorkingApplied, strict.FallbackLevel)
	}
	if strict.EligibleCount != 1 {
		t.Fatalf("strict eligible_count=%d want 1", strict.EligibleCount)
	}
	if len(strict.Hits) != 1 || strict.Hits[0].TurnID != "turn-wc-2" {
		t.Fatalf("strict hits=%v want turn-wc-2 only", strict.Hits)
	}

	relaxLabels, err := store.SearchArtifactsByEmbedding(ctx, []float32{1.0, 0.0, 0.0}, run.ArtifactSearchOptions{
		SessionID: "run-wc",
		Limit:     5,
		Working: run.WorkingContext{
			WorkspaceID:    "ws-main",
			RequiredLabels: []string{"security"}, // no strict label hit
			MinSalience:    0.80,
		},
	})
	if err != nil {
		t.Fatalf("SearchArtifactsByEmbedding(relaxLabels) error = %v", err)
	}
	if relaxLabels.FallbackLevel != 1 {
		t.Fatalf("relaxLabels fallback_level=%d want 1", relaxLabels.FallbackLevel)
	}
	if len(relaxLabels.Hits) == 0 || relaxLabels.Hits[0].TurnID != "turn-wc-2" {
		t.Fatalf("relaxLabels top hit=%v want turn-wc-2", relaxLabels.Hits)
	}

	relaxSalience, err := store.SearchArtifactsByEmbedding(ctx, []float32{1.0, 0.0, 0.0}, run.ArtifactSearchOptions{
		SessionID: "run-wc",
		Limit:     5,
		Working: run.WorkingContext{
			WorkspaceID:    "ws-main",
			RequiredLabels: []string{"security"},
			MinSalience:    0.95, // none satisfy strict or level1 salience
		},
	})
	if err != nil {
		t.Fatalf("SearchArtifactsByEmbedding(relaxSalience) error = %v", err)
	}
	if relaxSalience.FallbackLevel != 2 {
		t.Fatalf("relaxSalience fallback_level=%d want 2", relaxSalience.FallbackLevel)
	}
	if len(relaxSalience.Hits) < 2 {
		t.Fatalf("relaxSalience expected multiple hits after salience relaxation, got=%v", relaxSalience.Hits)
	}
	// Soft rerank should prefer higher salience auth hit even with near-identical similarity.
	if relaxSalience.Hits[0].TurnID != "turn-wc-2" {
		t.Fatalf("relaxSalience top hit=%q want turn-wc-2", relaxSalience.Hits[0].TurnID)
	}

	temporalOnly, err := store.SearchArtifactsByEmbedding(ctx, []float32{1.0, 0.0, 0.0}, run.ArtifactSearchOptions{
		SessionID: "run-wc",
		Limit:     5,
		Working: run.WorkingContext{
			WorkspaceID:    "ws-missing",
			RequiredLabels: []string{"security"},
			MinSalience:    0.99,
		},
	})
	if err != nil {
		t.Fatalf("SearchArtifactsByEmbedding(temporalOnly) error = %v", err)
	}
	if temporalOnly.FallbackLevel != 3 {
		t.Fatalf("temporalOnly fallback_level=%d want 3", temporalOnly.FallbackLevel)
	}
	if len(temporalOnly.Hits) != 0 {
		t.Fatalf("temporalOnly hits=%v want none", temporalOnly.Hits)
	}
}

func TestTurnStore_SaveArtifact_EmbeddingDimensionsPolicy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_artifact_dims.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	store.vectorDimensions = 4
	store.vectorEnabled.Store(true)

	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-dims", SessionID: "run-dims"}); err != nil {
		t.Fatalf("SaveTurn() error = %v", err)
	}

	err = store.SaveArtifact(ctx, Artifact{
		TurnID:          "turn-dims",
		ArtifactType:    ArtifactTypeEmbedding,
		ArtifactVersion: "v1",
		Embedding:       []float32{1.0, 0.0, 0.0},
	})
	if !errors.Is(err, ErrInvalidEmbeddingDimensions) {
		t.Fatalf("SaveArtifact() error=%v want ErrInvalidEmbeddingDimensions", err)
	}

	// When vector mode is disabled, embeddings remain available through embedding_json
	// and strict vector dimensions are not enforced.
	store.vectorEnabled.Store(false)
	if err := store.SaveArtifact(ctx, Artifact{
		TurnID:          "turn-dims",
		ArtifactType:    ArtifactTypeEmbedding,
		ArtifactVersion: "v1",
		Embedding:       []float32{1.0, 0.0, 0.0},
	}); err != nil {
		t.Fatalf("SaveArtifact(vector disabled) error = %v", err)
	}
}

func TestTurnStore_SearchArtifactsByEmbedding_RejectsDimensionMismatchWhenVectorEnabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "turn_artifact_query_dims.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, db.Close)
	store.vectorDimensions = 4
	store.vectorEnabled.Store(true)

	_, err = store.SearchArtifactsByEmbedding(ctx, []float32{1.0, 0.0, 0.0}, run.ArtifactSearchOptions{Limit: 3})
	if !errors.Is(err, ErrInvalidEmbeddingDimensions) {
		t.Fatalf("SearchArtifactsByEmbedding() error=%v want ErrInvalidEmbeddingDimensions", err)
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
