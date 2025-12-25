//go:build cgo && !race

package memory

import (
	"context"
	"os"
	"testing"

	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

func TestTursoStoreIntegration(t *testing.T) {
	url := os.Getenv("TURSO_DATABASE_URL")
	token := os.Getenv("TURSO_AUTH_TOKEN")

	if url == "" || token == "" {
		t.Skip("TURSO_DATABASE_URL and TURSO_AUTH_TOKEN not set, skipping Turso integration test")
	}

	ctx := context.Background()
	cfg := dbdriver.TursoConfig{
		URL:              url,
		AuthToken:        token,
		VectorDimensions: 3072,
	}

	store, err := OpenTurso(ctx, cfg)
	if err != nil {
		t.Fatalf("OpenTurso failed: %v", err)
	}
	defer store.Close()

	// Test Stats
	t.Run("Stats", func(t *testing.T) {
		stats, err := store.Stats(ctx)
		if err != nil {
			t.Fatalf("Stats failed: %v", err)
		}
		t.Logf("Total memories: %d, Path: %s", stats.Named, stats.Path)
	})

	// Test SaveWithEmbedding and SearchSimilar
	t.Run("VectorOperations", func(t *testing.T) {
		// Create test embedding with correct dimensions
		testEmbedding := make([]float32, 3072)
		for i := range testEmbedding {
			testEmbedding[i] = float32(i) * 0.001
		}

		// Save entry with embedding
		entry := NamedEntry{
			Name:      "test-vector-entry",
			Type:      "test",
			Workspace: "test-ws",
			Summary:   "Test entry for vector search",
			Result:    []byte(`{"test": true}`),
		}

		saved, err := store.SaveWithEmbedding(ctx, entry, testEmbedding, "test-model")
		if err != nil {
			t.Fatalf("SaveWithEmbedding failed: %v", err)
		}
		t.Logf("Saved entry: %s", saved.ID)

		// Search for similar entries
		results, err := store.SearchSimilar(ctx, "test-ws", testEmbedding, 5)
		if err != nil {
			t.Fatalf("SearchSimilar failed: %v", err)
		}
		t.Logf("Found %d similar entries", len(results))

		// Clean up
		if err := store.Delete(ctx, entry.Name, entry.Workspace); err != nil {
			t.Logf("Cleanup warning: %v", err)
		}
	})
}

func TestTursoConfigWithDimensions(t *testing.T) {
	url := os.Getenv("TURSO_DATABASE_URL")
	token := os.Getenv("TURSO_AUTH_TOKEN")

	if url == "" || token == "" {
		t.Skip("TURSO_DATABASE_URL and TURSO_AUTH_TOKEN not set, skipping test")
	}

	ctx := context.Background()

	// Test with explicit dimensions
	cfg := dbdriver.TursoConfig{
		URL:              url,
		AuthToken:        token,
		VectorDimensions: 3072,
	}

	store, err := OpenTurso(ctx, cfg)
	if err != nil {
		t.Fatalf("OpenTurso with dimensions failed: %v", err)
	}
	defer store.Close()

	// Verify store has correct dimensions
	if store.vectorDimension != 3072 {
		t.Errorf("Expected vector dimension 3072, got %d", store.vectorDimension)
	}
}

func TestTursoEmbeddingDimensionMismatch(t *testing.T) {
	url := os.Getenv("TURSO_DATABASE_URL")
	token := os.Getenv("TURSO_AUTH_TOKEN")

	if url == "" || token == "" {
		t.Skip("TURSO_DATABASE_URL and TURSO_AUTH_TOKEN not set, skipping test")
	}

	ctx := context.Background()
	cfg := dbdriver.TursoConfig{
		URL:              url,
		AuthToken:        token,
		VectorDimensions: 3072,
	}

	store, err := OpenTurso(ctx, cfg)
	if err != nil {
		t.Fatalf("OpenTurso failed: %v", err)
	}
	defer store.Close()

	entry := NamedEntry{
		Name:      "test-mismatch-entry",
		Type:      "test",
		Workspace: "test-ws",
		Summary:   "Test entry with wrong dimensions",
		Result:    []byte(`{"test": true}`),
	}

	t.Run("WrongDimensionsSaveWithEmbedding", func(t *testing.T) {
		// Create embedding with wrong dimensions (768 instead of 3072)
		wrongEmbedding := make([]float32, 768)
		for i := range wrongEmbedding {
			wrongEmbedding[i] = 0.01
		}

		_, err := store.SaveWithEmbedding(ctx, entry, wrongEmbedding, "wrong-model")
		if err == nil {
			t.Fatal("Expected error for dimension mismatch, got nil")
		}
		if !contains(err.Error(), "dimension mismatch") {
			t.Errorf("Expected dimension mismatch error, got: %v", err)
		}
		t.Logf("Got expected error: %v", err)
	})

	t.Run("ZeroDimensionsSaveWithEmbedding", func(t *testing.T) {
		emptyEmbedding := make([]float32, 0)
		_, err := store.SaveWithEmbedding(ctx, entry, emptyEmbedding, "empty-model")
		if err == nil {
			t.Fatal("Expected error for empty embedding, got nil")
		}
		t.Logf("Got expected error: %v", err)
	})
}

func TestTursoConnectionFailure(t *testing.T) {
	ctx := context.Background()

	// Invalid URL should fail to connect
	cfg := dbdriver.TursoConfig{
		URL:              "libsql://invalid-database.example.com",
		AuthToken:        "invalid-token",
		VectorDimensions: 3072,
	}

	_, err := OpenTurso(ctx, cfg)
	if err == nil {
		t.Fatal("Expected error for invalid Turso URL, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestTursoDefaultDimensions(t *testing.T) {
	url := os.Getenv("TURSO_DATABASE_URL")
	token := os.Getenv("TURSO_AUTH_TOKEN")

	if url == "" || token == "" {
		t.Skip("TURSO_DATABASE_URL and TURSO_AUTH_TOKEN not set, skipping test")
	}

	ctx := context.Background()

	// Test with zero dimensions (should default to 3072)
	cfg := dbdriver.TursoConfig{
		URL:              url,
		AuthToken:        token,
		VectorDimensions: 0, // Should default to 3072
	}

	store, err := OpenTurso(ctx, cfg)
	if err != nil {
		t.Fatalf("OpenTurso with default dimensions failed: %v", err)
	}
	defer store.Close()

	// Verify store defaulted to 3072
	if store.vectorDimension != 3072 {
		t.Errorf("Expected default vector dimension 3072, got %d", store.vectorDimension)
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && len(substr) > 0 &&
			findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
