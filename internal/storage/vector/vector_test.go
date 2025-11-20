//go:build vector && cgo

package vector

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestVectorStore(t *testing.T) {
	// Create a temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create test table
	_, err = db.Exec(`
		CREATE TABLE test_memory (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			workspace TEXT NOT NULL,
			summary TEXT,
			embedding BLOB,
			UNIQUE(name, workspace)
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Create vector store
	store, err := NewStore(db, "test_memory")
	if err != nil {
		t.Fatalf("Failed to create vector store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Insert test data
	testData := []struct {
		id        string
		name      string
		workspace string
		summary   string
		embedding []float32
	}{
		{
			id:        "1",
			name:      "doc1",
			workspace: "test",
			summary:   "Document about cats",
			embedding: []float32{0.1, 0.2, 0.3, 0.4},
		},
		{
			id:        "2",
			name:      "doc2",
			workspace: "test",
			summary:   "Document about dogs",
			embedding: []float32{0.2, 0.3, 0.4, 0.5},
		},
		{
			id:        "3",
			name:      "doc3",
			workspace: "test",
			summary:   "Document about birds",
			embedding: []float32{0.9, 0.8, 0.7, 0.6},
		},
	}

	for _, td := range testData {
		_, err := db.Exec(
			"INSERT INTO test_memory (id, name, workspace, summary, embedding) VALUES (?, ?, ?, ?, ?)",
			td.id, td.name, td.workspace, td.summary, serializeFloat32(td.embedding),
		)
		if err != nil {
			t.Fatalf("Failed to insert test data: %v", err)
		}
	}

	// Initialize vectors with L2 distance
	err = store.InitializeVectors(ctx, 4, "L2")
	if err != nil {
		t.Fatalf("Failed to initialize vectors: %v", err)
	}

	// Quantize for better performance
	err = store.Quantize(ctx)
	if err != nil {
		t.Fatalf("Failed to quantize: %v", err)
	}

	// Test vector search - search for embeddings similar to doc1
	queryEmbedding := []float32{0.1, 0.2, 0.3, 0.4}
	results, err := store.Search(ctx, SearchOptions{
		Embedding: queryEmbedding,
		Limit:     2,
		Workspace: "test",
	})
	if err != nil {
		t.Fatalf("Failed to search: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Expected at least one result")
	}

	// The closest result should be doc1 itself
	if results[0].Name != "doc1" {
		t.Errorf("Expected first result to be 'doc1', got '%s'", results[0].Name)
	}

	// The distance should be very small (nearly zero)
	if results[0].Distance > 0.001 {
		t.Errorf("Expected distance to be near 0, got %f", results[0].Distance)
	}

	t.Logf("Search results:")
	for i, r := range results {
		t.Logf("  %d. %s (distance: %f)", i+1, r.Name, r.Distance)
	}
}

func TestSaveEmbedding(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create test table
	_, err = db.Exec(`
		CREATE TABLE test_memory (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			workspace TEXT NOT NULL,
			summary TEXT,
			embedding BLOB,
			UNIQUE(name, workspace)
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Insert initial entry without embedding
	_, err = db.Exec(
		"INSERT INTO test_memory (id, name, workspace, summary) VALUES (?, ?, ?, ?)",
		"1", "doc1", "test", "Test document",
	)
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	store, err := NewStore(db, "test_memory")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Save embedding
	embedding := []float32{0.1, 0.2, 0.3, 0.4}
	err = store.SaveEmbedding(ctx, "1", "test", "doc1", embedding)
	if err != nil {
		t.Fatalf("Failed to save embedding: %v", err)
	}

	// Verify embedding was saved
	var blob []byte
	err = db.QueryRow("SELECT embedding FROM test_memory WHERE id = ?", "1").Scan(&blob)
	if err != nil {
		t.Fatalf("Failed to query embedding: %v", err)
	}

	if blob == nil {
		t.Fatal("Embedding is NULL")
	}

	retrieved := deserializeFloat32(blob)
	if len(retrieved) != len(embedding) {
		t.Fatalf("Expected %d dimensions, got %d", len(embedding), len(retrieved))
	}

	for i := range embedding {
		if retrieved[i] != embedding[i] {
			t.Errorf("Dimension %d: expected %f, got %f", i, embedding[i], retrieved[i])
		}
	}
}

func TestVectorEnabled(t *testing.T) {
	if !Enabled {
		t.Error("Vector support should be enabled in this build")
	}
}

func TestSerializeDeserialize(t *testing.T) {
	original := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	blob := serializeFloat32(original)

	if len(blob) != len(original)*4 {
		t.Errorf("Expected blob size %d, got %d", len(original)*4, len(blob))
	}

	retrieved := deserializeFloat32(blob)
	if len(retrieved) != len(original) {
		t.Fatalf("Expected %d dimensions, got %d", len(original), len(retrieved))
	}

	for i := range original {
		if retrieved[i] != original[i] {
			t.Errorf("Dimension %d: expected %f, got %f", i, original[i], retrieved[i])
		}
	}
}

func TestExtensionPath(t *testing.T) {
	path, err := extensionPath()
	if err != nil {
		t.Fatalf("Failed to get extension path: %v", err)
	}

	// Check that the path points to a file that exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Logf("Warning: Extension not found at %s (this is expected if running tests without building the extension)", path)
	} else {
		t.Logf("Extension path: %s", path)
	}
}
