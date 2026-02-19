//go:build cgo && !race

package turns

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

func TestTurnStore_SearchArtifactsByEmbedding_VectorPathLibSQL(t *testing.T) {
	ctx := context.Background()
	storageRoot := t.TempDir()

	// Force libsql driver so this test validates the native vector SQL path.
	t.Setenv("AGENTCTL_V2_TURNS_DB_DRIVER", "libsql")
	t.Setenv("AGENTCTL_V2_TURNS_DB_PATH", filepath.Join(storageRoot, "turns_vector.libsql"))

	store, err := Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("Open(libsql) error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if !store.vectorEnabled.Load() {
		t.Skip("skipping: libsql vector capability unavailable in this test environment")
	}

	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-v1", SessionID: "run-vector"}); err != nil {
		t.Fatalf("SaveTurn(turn-v1) error = %v", err)
	}
	if err := store.SaveTurn(ctx, run.TurnRecord{ID: "turn-v2", SessionID: "run-vector"}); err != nil {
		t.Fatalf("SaveTurn(turn-v2) error = %v", err)
	}

	if err := store.SaveArtifact(ctx, Artifact{
		TurnID:          "turn-v1",
		ArtifactType:    ArtifactTypeEmbedding,
		ArtifactVersion: "v1",
		Summary:         "near query",
		Embedding:       []float32{1.0, 0.0, 0.0, 0.0},
	}); err != nil {
		t.Fatalf("SaveArtifact(turn-v1) error = %v", err)
	}
	if err := store.SaveArtifact(ctx, Artifact{
		TurnID:          "turn-v2",
		ArtifactType:    ArtifactTypeEmbedding,
		ArtifactVersion: "v1",
		Summary:         "far from query",
		Embedding:       []float32{0.0, 1.0, 0.0, 0.0},
	}); err != nil {
		t.Fatalf("SaveArtifact(turn-v2) error = %v", err)
	}

	result, err := store.SearchArtifactsByEmbedding(ctx, []float32{1.0, 0.0, 0.0, 0.0}, run.ArtifactSearchOptions{
		SessionID: "run-vector",
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("SearchArtifactsByEmbedding() error = %v", err)
	}
	if result.SearchPath != run.ArtifactSearchPathVector {
		if result.SearchPath == run.ArtifactSearchPathFallback &&
			result.VectorCapability == run.ArtifactVectorCapabilityDisabled {
			t.Skip("skipping: vector SQL unavailable at runtime; store downgraded to fallback path")
		}
		t.Fatalf("search_path=%q want %q", result.SearchPath, run.ArtifactSearchPathVector)
	}
	if result.VectorCapability != run.ArtifactVectorCapabilityEnabled {
		t.Fatalf("vector_capability=%q want %q", result.VectorCapability, run.ArtifactVectorCapabilityEnabled)
	}
	if len(result.Hits) != 2 {
		t.Fatalf("hits len=%d want 2", len(result.Hits))
	}
	if result.Hits[0].TurnID != "turn-v1" {
		t.Fatalf("top hit turn=%q want turn-v1", result.Hits[0].TurnID)
	}
	if result.Hits[0].Similarity < result.Hits[1].Similarity {
		t.Fatalf("hits not sorted by similarity desc: %.4f < %.4f", result.Hits[0].Similarity, result.Hits[1].Similarity)
	}
}
