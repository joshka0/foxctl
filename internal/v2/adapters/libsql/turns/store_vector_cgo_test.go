//go:build cgo && !race

package turns

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func requireNativeVectorSQL() bool {
	switch os.Getenv("FOXCTL_V2_REQUIRE_NATIVE_VECTOR_SQL") {
	case "1", "true", "TRUE", "yes", "YES":
		return true
	default:
		return false
	}
}

func TestTurnStore_SearchArtifactsByEmbedding_VectorPathLibSQL(t *testing.T) {
	ctx := context.Background()
	storageRoot := t.TempDir()
	requireNative := requireNativeVectorSQL()

	// Force libsql driver so this test validates the native vector SQL path.
	t.Setenv("FOXCTL_V2_TURNS_DB_DRIVER", "libsql")
	t.Setenv("FOXCTL_V2_TURNS_DB_PATH", filepath.Join(storageRoot, "turns_vector.libsql"))
	t.Setenv("FOXCTL_V2_TURNS_VECTOR_SEARCH", "1")
	t.Setenv("FOXCTL_V2_TURNS_VECTOR_DIMS", "4")
	t.Setenv("FOXCTL_VECTOR_DIMS", "4")

	store, err := Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("Open(libsql) error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if !store.vectorEnabled.Load() {
		if requireNative {
			t.Fatalf("native vector SQL required, but store initialized without vector capability")
		}
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
			if requireNative {
				t.Fatalf("native vector SQL required, but search downgraded to fallback (vector_capability=%q)", result.VectorCapability)
			}
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

func TestTurnStore_Open_DefaultVectorDimensionsLocal(t *testing.T) {
	ctx := context.Background()
	storageRoot := t.TempDir()

	t.Setenv("FOXCTL_V2_TURNS_DB_DRIVER", "libsql")
	t.Setenv("FOXCTL_V2_TURNS_DB_PATH", filepath.Join(storageRoot, "turns_default_dims.libsql"))
	t.Setenv("FOXCTL_V2_TURNS_VECTOR_SEARCH", "1")
	t.Setenv("FOXCTL_V2_TURNS_VECTOR_DIMS", "")
	t.Setenv("FOXCTL_VECTOR_DIMS", "")

	store, err := Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("Open(libsql) error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if got := store.VectorDimensions(); got != defaultV2TurnsVectorDims {
		t.Fatalf("VectorDimensions() = %d, want %d", got, defaultV2TurnsVectorDims)
	}
}
