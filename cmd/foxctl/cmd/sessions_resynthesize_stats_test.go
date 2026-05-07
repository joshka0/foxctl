package cmd

import (
	"context"
	"reflect"
	"testing"

	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
	"github.com/joshka0/foxctl/internal/v2/adapters/turso/turns"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func TestCollectResynthesizePersistedStats(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := turns.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("turns.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.SaveTurn(ctx, run.TurnRecord{
		ID:        "turn-1",
		SessionID: "session-1",
		TurnIndex: 1,
	}); err != nil {
		t.Fatalf("SaveTurn(turn-1) error = %v", err)
	}
	if err := store.SaveTurn(ctx, run.TurnRecord{
		ID:        "turn-2",
		SessionID: "session-1",
		TurnIndex: 2,
	}); err != nil {
		t.Fatalf("SaveTurn(turn-2) error = %v", err)
	}

	if err := store.SaveArtifact(ctx, turns.Artifact{
		TurnID:          "turn-1",
		ArtifactType:    turns.ArtifactTypeAnnotation,
		ArtifactVersion: "v1",
		Summary:         "annotation",
		ContentJSON:     []byte(`{"kind":"annotation"}`),
		MetadataJSON:    []byte(`{"source":"test"}`),
	}); err != nil {
		t.Fatalf("SaveArtifact(annotation) error = %v", err)
	}
	if err := store.SaveArtifact(ctx, turns.Artifact{
		TurnID:          "turn-2",
		ArtifactType:    turns.ArtifactTypeClassification,
		ArtifactVersion: "v1",
		Summary:         "classification",
		ContentJSON:     []byte(`{"kind":"classification"}`),
		MetadataJSON:    []byte(`{"source":"test"}`),
	}); err != nil {
		t.Fatalf("SaveArtifact(classification) error = %v", err)
	}

	if err := store.SaveEpisode(ctx, run.EpisodeRecord{
		SessionID:      "session-1",
		EpisodeVersion: "v1",
		BoundaryKey:    "episode-1",
		StartTurnID:    "turn-1",
		EndTurnID:      "turn-2",
		StartTurnIndex: 1,
		EndTurnIndex:   2,
		Topic:          "test episode",
	}); err != nil {
		t.Fatalf("SaveEpisode() error = %v", err)
	}

	if err := store.SaveNarrative(ctx, run.NarrativeRecord{
		SessionID:       "session-1",
		TurnID:          "turn-1",
		SourceTurnID:    "turn-2",
		SourceTurnIndex: 2,
		SourceTurnCount: 2,
		ArtifactVersion: "v1",
		Summary:         "narrative summary",
		Claims: []run.NarrativeClaim{
			{
				Text: "narrative claim",
				AnchorRefs: []string{
					turns.BuildArtifactRef("turn-1", turns.ArtifactTypeAnnotation, "v1"),
				},
			},
		},
	}); err != nil {
		t.Fatalf("SaveNarrative() error = %v", err)
	}

	stats, err := collectResynthesizePersistedStats(ctx, store, "session-1", "v1")
	if err != nil {
		t.Fatalf("collectResynthesizePersistedStats() error = %v", err)
	}

	if stats.Turns != 2 {
		t.Fatalf("stats.Turns=%d want 2", stats.Turns)
	}
	if stats.Artifacts != 3 {
		t.Fatalf("stats.Artifacts=%d want 3", stats.Artifacts)
	}
	if stats.Episodes != 1 {
		t.Fatalf("stats.Episodes=%d want 1", stats.Episodes)
	}
	if !stats.Narrative {
		t.Fatal("stats.Narrative=false want true")
	}
	if stats.ArtifactTypes[turns.ArtifactTypeAnnotation] != 1 {
		t.Fatalf("annotation count=%d want 1", stats.ArtifactTypes[turns.ArtifactTypeAnnotation])
	}
	if stats.ArtifactTypes[turns.ArtifactTypeClassification] != 1 {
		t.Fatalf("classification count=%d want 1", stats.ArtifactTypes[turns.ArtifactTypeClassification])
	}
	if stats.ArtifactTypes[turns.ArtifactTypeNarrative] != 1 {
		t.Fatalf("narrative count=%d want 1", stats.ArtifactTypes[turns.ArtifactTypeNarrative])
	}
}

func TestResolvePersistedSessionID_PrefersTurnSessionID(t *testing.T) {
	t.Parallel()

	parsed := sourceimport.ParsedSession{
		SessionID: "source-session",
		Turns: []run.TurnRecord{
			{ID: "turn-1", SessionID: "persisted-session"},
			{ID: "turn-2", SessionID: ""},
		},
	}
	if got := resolvePersistedSessionID(parsed); got != "persisted-session" {
		t.Fatalf("resolvePersistedSessionID()=%q want %q", got, "persisted-session")
	}
}

func TestResolvePersistedSessionID_FallsBackToSourceSessionID(t *testing.T) {
	t.Parallel()

	parsed := sourceimport.ParsedSession{
		SessionID: "source-session",
		Turns: []run.TurnRecord{
			{ID: "turn-1", SessionID: ""},
		},
	}
	if got := resolvePersistedSessionID(parsed); got != "source-session" {
		t.Fatalf("resolvePersistedSessionID()=%q want %q", got, "source-session")
	}
}

func TestResolveV2SessionCandidates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "raw session id includes source candidates",
			in:   "abc123",
			want: []string{"abc123", "source:codex:abc123", "source:claude:abc123"},
		},
		{
			name: "persisted id preserved as single candidate",
			in:   "source:codex:abc123",
			want: []string{"source:codex:abc123"},
		},
		{
			name: "blank",
			in:   "  ",
			want: nil,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveV2SessionCandidates(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resolveV2SessionCandidates(%q)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestLoadV2SessionDetail_FromSourceSessionID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := turns.Open(ctx, root)
	if err != nil {
		t.Fatalf("turns.Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	sessionID := "source:codex:session-xyz"
	if err := store.SaveTurn(ctx, run.TurnRecord{
		ID:        "turn-a",
		SessionID: sessionID,
		TurnIndex: 1,
		Prompt:    "user asked about migration",
		Iterations: []run.IterationRecord{
			{IterationIndex: 1, ToolCalls: []run.ToolCallRecord{{CallID: "tc-1", Name: "code_search"}}},
		},
		FinalOutput: run.MessageRef{Text: "first reply"},
	}); err != nil {
		t.Fatalf("SaveTurn(turn-a) error = %v", err)
	}
	if err := store.SaveTurn(ctx, run.TurnRecord{
		ID:        "turn-b",
		SessionID: sessionID,
		TurnIndex: 2,
		Prompt:    "follow-up question",
		FinalOutput: run.MessageRef{
			Text: "second reply",
		},
	}); err != nil {
		t.Fatalf("SaveTurn(turn-b) error = %v", err)
	}

	if err := store.SaveArtifact(ctx, turns.Artifact{
		TurnID:          "turn-a",
		ArtifactType:    turns.ArtifactTypeAnnotation,
		ArtifactVersion: "v1",
		Summary:         "annotation summary",
		ContentJSON:     []byte(`{"kind":"annotation"}`),
		MetadataJSON: []byte(`{
			"provider":"codex",
			"workspace":"/tmp/ws",
			"source_path":"/tmp/source.jsonl"
		}`),
	}); err != nil {
		t.Fatalf("SaveArtifact(annotation) error = %v", err)
	}
	if err := store.SaveEpisode(ctx, run.EpisodeRecord{
		SessionID:      sessionID,
		EpisodeVersion: "v1",
		BoundaryKey:    "episode-1",
		StartTurnID:    "turn-a",
		EndTurnID:      "turn-b",
		StartTurnIndex: 1,
		EndTurnIndex:   2,
		Topic:          "migration",
	}); err != nil {
		t.Fatalf("SaveEpisode() error = %v", err)
	}
	if err := store.SaveNarrative(ctx, run.NarrativeRecord{
		SessionID:       sessionID,
		TurnID:          "turn-a",
		SourceTurnID:    "turn-b",
		SourceTurnIndex: 2,
		SourceTurnCount: 2,
		ArtifactVersion: "v1",
		Summary:         "narrative summary",
		Claims: []run.NarrativeClaim{
			{
				Text:       "claim",
				AnchorRefs: []string{turns.BuildArtifactRef("turn-a", turns.ArtifactTypeAnnotation, "v1")},
			},
		},
	}); err != nil {
		t.Fatalf("SaveNarrative() error = %v", err)
	}

	detail, found, err := loadV2SessionDetail(ctx, root, "session-xyz")
	if err != nil {
		t.Fatalf("loadV2SessionDetail() error = %v", err)
	}
	if !found {
		t.Fatal("loadV2SessionDetail() found=false want true")
	}
	if !detail.V2 {
		t.Fatal("detail.V2=false want true")
	}
	if detail.ID != sessionID {
		t.Fatalf("detail.ID=%q want %q", detail.ID, sessionID)
	}
	if detail.SourceSessionID != "session-xyz" {
		t.Fatalf("detail.SourceSessionID=%q want session-xyz", detail.SourceSessionID)
	}
	if detail.SourceProvider != "codex" {
		t.Fatalf("detail.SourceProvider=%q want codex", detail.SourceProvider)
	}
	if detail.EpisodeCount != 1 {
		t.Fatalf("detail.EpisodeCount=%d want 1", detail.EpisodeCount)
	}
	if detail.ArtifactTypes[turns.ArtifactTypeAnnotation] != 1 {
		t.Fatalf("annotation count=%d want 1", detail.ArtifactTypes[turns.ArtifactTypeAnnotation])
	}
	if detail.ArtifactTypes[turns.ArtifactTypeNarrative] != 1 {
		t.Fatalf("narrative count=%d want 1", detail.ArtifactTypes[turns.ArtifactTypeNarrative])
	}
	if detail.WorkspacePath != "/tmp/ws" {
		t.Fatalf("detail.WorkspacePath=%q want /tmp/ws", detail.WorkspacePath)
	}
	if detail.RawJSONLPath != "/tmp/source.jsonl" {
		t.Fatalf("detail.RawJSONLPath=%q want /tmp/source.jsonl", detail.RawJSONLPath)
	}
	if detail.Summary == "" {
		t.Fatal("detail.Summary should not be empty")
	}
}

func TestSearchV2Sessions_ReturnsSessionFromEmbeddingArtifacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := turns.Open(ctx, root)
	if err != nil {
		t.Fatalf("turns.Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	const (
		query         = "find migration session"
		persistedID   = "source:codex:search-session"
		turnID        = "turn-search-1"
		workspacePath = "/tmp/search-workspace"
		sourcePath    = "/tmp/search-session.jsonl"
	)
	if err := store.SaveTurn(ctx, run.TurnRecord{
		ID:        turnID,
		SessionID: persistedID,
		TurnIndex: 1,
		Prompt:    "search prompt",
		FinalOutput: run.MessageRef{
			Text: "search final output",
		},
	}); err != nil {
		t.Fatalf("SaveTurn() error = %v", err)
	}

	embedder := sourceimport.NewHashEmbedder(store.VectorDimensions())
	embedded, err := embedder.Embed(ctx, query)
	if err != nil {
		t.Fatalf("embedder.Embed() error = %v", err)
	}
	if len(embedded.Vector) == 0 {
		t.Fatal("embedded vector is empty")
	}

	if err := store.SaveArtifact(ctx, turns.Artifact{
		TurnID:          turnID,
		ArtifactType:    turns.ArtifactTypeEmbedding,
		ArtifactVersion: "v1",
		Summary:         "embedded summary",
		ContentJSON:     []byte(`{"kind":"embedding"}`),
		MetadataJSON: []byte(`{
			"provider":"codex",
			"workspace":"` + workspacePath + `",
			"source_path":"` + sourcePath + `"
		}`),
		Embedding:      append([]float32(nil), embedded.Vector...),
		EmbeddingModel: "deterministic-hash-v1",
	}); err != nil {
		t.Fatalf("SaveArtifact(embedding) error = %v", err)
	}

	results, err := searchV2Sessions(ctx, root, query, 5)
	if err != nil {
		t.Fatalf("searchV2Sessions() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("searchV2Sessions() returned no results")
	}
	top := results[0]
	if top.ID != persistedID {
		t.Fatalf("top.ID=%q want %q", top.ID, persistedID)
	}
	if !top.V2 {
		t.Fatal("top.V2=false want true")
	}
	if top.SourceProvider != "codex" {
		t.Fatalf("top.SourceProvider=%q want codex", top.SourceProvider)
	}
}
