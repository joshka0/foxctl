//go:build cgo && !race

package sessions

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
		URL:       url,
		AuthToken: token,
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
		t.Logf("Total sessions: %d, Path: %s", stats.Count, stats.Path)
	})

	// Test List with raw query to debug
	t.Run("List", func(t *testing.T) {
		// First try a raw COUNT to verify data exists
		var count int64
		err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&count)
		if err != nil {
			t.Fatalf("Count query failed: %v", err)
		}
		t.Logf("Raw count: %d sessions", count)

		// Try raw select with just a few columns
		rows, err := store.db.QueryContext(ctx, "SELECT id, workspace_path, summary FROM sessions LIMIT 3")
		if err != nil {
			t.Fatalf("Raw query failed: %v", err)
		}
		defer rows.Close()

		var rawCount int
		for rows.Next() {
			var id, wp, summary string
			if err := rows.Scan(&id, &wp, &summary); err != nil {
				t.Logf("Scan error: %v", err)
				continue
			}
			rawCount++
			t.Logf("  Raw: %s - %s", id[:8], summary[:min(40, len(summary))])
		}
		t.Logf("Raw query returned %d rows", rawCount)

		// Now try the List method
		sessions, err := store.List(ctx, ListOptions{Limit: 3})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		t.Logf("List returned %d sessions", len(sessions))
	})

	// Test SearchSimilar (needs an embedding to test with)
	t.Run("SearchSimilar", func(t *testing.T) {
		// Use a 1024-dim test vector (Voyage default)
		testEmbedding := make([]float32, 1024)
		for i := range testEmbedding {
			testEmbedding[i] = 0.01
		}

		results, err := store.SearchSimilar(ctx, testEmbedding, 5)
		if err != nil {
			t.Fatalf("SearchSimilar failed: %v", err)
		}
		t.Logf("Found %d similar sessions", len(results))
		for _, r := range results {
			t.Logf("  - %s (similarity: %.4f)", r.Session.ID[:8], r.Similarity)
		}
	})
}

func TestTursoConnectionFailure(t *testing.T) {
	ctx := context.Background()

	// Invalid URL should fail to connect
	cfg := dbdriver.TursoConfig{
		URL:       "libsql://invalid-database.example.com",
		AuthToken: "invalid-token",
	}

	_, err := OpenTurso(ctx, cfg)
	if err == nil {
		t.Fatal("Expected error for invalid Turso URL, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestTursoConfigWithDimensions(t *testing.T) {
	url := os.Getenv("TURSO_DATABASE_URL")
	token := os.Getenv("TURSO_AUTH_TOKEN")

	if url == "" || token == "" {
		t.Skip("TURSO_DATABASE_URL and TURSO_AUTH_TOKEN not set, skipping test")
	}

	ctx := context.Background()

	// Test with explicit dimensions (1024 = Voyage default)
	cfg := dbdriver.TursoConfig{
		URL:              url,
		AuthToken:        token,
		VectorDimensions: 1024,
	}

	store, err := OpenTurso(ctx, cfg)
	if err != nil {
		t.Fatalf("OpenTurso with dimensions failed: %v", err)
	}
	defer store.Close()

	// Verify store has correct dimensions
	if store.vectorDimension != 1024 {
		t.Errorf("Expected vector dimension 1024, got %d", store.vectorDimension)
	}
}
