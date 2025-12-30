//go:build cgo && !race

package dbdriver

import (
	"context"
	"os"
	"testing"
	"time"
)

// skipIfNoTurso skips the test if Turso credentials are not available
func skipIfNoTurso(t *testing.T) (url, token string) {
	t.Helper()

	url = os.Getenv("TURSO_DATABASE_URL")
	token = os.Getenv("TURSO_AUTH_TOKEN")

	if url == "" || token == "" {
		t.Skip("Skipping Turso test: TURSO_DATABASE_URL and TURSO_AUTH_TOKEN not set")
	}

	return url, token
}

func TestTursoConnection(t *testing.T) {
	url, token := skipIfNoTurso(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := TursoConfig{
		URL:                url,
		AuthToken:          token,
		EnableVectorSearch: false, // Test basic connection first
	}

	db, err := openTurso(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("Failed to open Turso connection: %v", err)
	}
	defer db.Close()

	// Test ping
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("Failed to ping Turso: %v", err)
	}

	// Verify driver type
	if dt := db.GetDriverType(); dt != DriverTurso {
		t.Errorf("Expected driver type %s, got %s", DriverTurso, dt)
	}

	t.Log("Turso connection successful")
}

func TestTursoVectorSupport(t *testing.T) {
	url, token := skipIfNoTurso(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := TursoConfig{
		URL:                url,
		AuthToken:          token,
		EnableVectorSearch: true,
		VectorDimensions:   4, // Small dimensions for testing
	}

	db, err := openTurso(ctx, cfg, nil)
	if err != nil {
		// Vector support might not be enabled in the Turso group
		if os.Getenv("TURSO_VECTOR_ENABLED") != "1" {
			t.Skipf("Skipping: Vector support not available or not enabled (set TURSO_VECTOR_ENABLED=1 if your Turso group supports vectors): %v", err)
		}
		t.Fatalf("Failed to open Turso with vector support: %v", err)
	}
	defer db.Close()

	// Verify vector search is enabled
	if !db.IsVectorSearchEnabled() {
		t.Error("Expected vector search to be enabled")
	}

	// Verify dimensions via type assertion
	if vd, ok := db.(interface{ GetVectorDimensions() int }); ok {
		if dims := vd.GetVectorDimensions(); dims != 4 {
			t.Errorf("Expected 4 dimensions, got %d", dims)
		}
	} else {
		t.Error("DB does not implement GetVectorDimensions")
	}

	t.Log("Turso vector support verified")
}

func TestTursoVectorHelper(t *testing.T) {
	url, token := skipIfNoTurso(t)

	if os.Getenv("TURSO_VECTOR_ENABLED") != "1" {
		t.Skip("Skipping: Set TURSO_VECTOR_ENABLED=1 to run vector helper tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := TursoConfig{
		URL:                url,
		AuthToken:          token,
		EnableVectorSearch: true,
		VectorDimensions:   4,
	}

	db, err := openTurso(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("Failed to open Turso: %v", err)
	}
	defer db.Close()

	// Create VectorHelper
	vh, err := NewVectorHelper(db)
	if err != nil {
		t.Fatalf("Failed to create VectorHelper: %v", err)
	}

	// Verify dimensions
	if dims := vh.GetDimensions(); dims != 4 {
		t.Errorf("Expected 4 dimensions, got %d", dims)
	}

	// Create test table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS vector_test (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			embedding F32_BLOB(4)
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Clean up on exit
	defer func() {
		_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS vector_test")
	}()

	// Insert test data using VectorHelper expressions
	testData := []struct {
		id        int
		name      string
		embedding Vector
	}{
		{1, "cat", Vector{0.1, 0.2, 0.3, 0.4}},
		{2, "dog", Vector{0.2, 0.3, 0.4, 0.5}},
		{3, "bird", Vector{0.9, 0.8, 0.7, 0.6}},
	}

	for _, td := range testData {
		query := "INSERT OR REPLACE INTO vector_test (id, name, embedding) VALUES (?, ?, " + vh.VectorExpression(td.embedding) + ")"
		_, err := db.ExecContext(ctx, query, td.id, td.name)
		if err != nil {
			t.Fatalf("Failed to insert test data: %v", err)
		}
	}

	// Test vector search - find similar to "cat"
	queryVec := Vector{0.1, 0.2, 0.3, 0.4}
	distExpr := vh.CosineSimilarity("embedding", queryVec)

	rows, err := db.QueryContext(ctx, "SELECT name, "+distExpr+" as distance FROM vector_test ORDER BY distance LIMIT 2")
	if err != nil {
		t.Fatalf("Failed to search: %v", err)
	}
	defer rows.Close()

	var results []struct {
		name     string
		distance float64
	}

	for rows.Next() {
		var r struct {
			name     string
			distance float64
		}
		if err := rows.Scan(&r.name, &r.distance); err != nil {
			t.Fatalf("Failed to scan row: %v", err)
		}
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("Row iteration error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Expected at least one result")
	}

	// The closest result should be "cat" itself
	if results[0].name != "cat" {
		t.Errorf("Expected first result to be 'cat', got '%s'", results[0].name)
	}

	// Distance should be very small (cosine distance of identical vectors is 0)
	if results[0].distance > 0.01 {
		t.Errorf("Expected distance near 0, got %f", results[0].distance)
	}

	t.Logf("Vector search results: %+v", results)
}

func TestVectorTypeConversions(t *testing.T) {
	// Test Vector string representation
	vec := Vector{0.1, 0.2, 0.3, 0.4}
	str := vec.String()
	if str != "[0.100000,0.200000,0.300000,0.400000]" {
		t.Errorf("Unexpected vector string: %s", str)
	}

	// Test Vector JSON marshaling
	jsonData, err := vec.MarshalJSON()
	if err != nil {
		t.Fatalf("Failed to marshal vector: %v", err)
	}
	if string(jsonData) != "[0.1,0.2,0.3,0.4]" {
		t.Errorf("Unexpected JSON: %s", string(jsonData))
	}

	// Test Vector JSON unmarshaling
	var parsed Vector
	err = parsed.UnmarshalJSON([]byte("[0.5,0.6,0.7,0.8]"))
	if err != nil {
		t.Fatalf("Failed to unmarshal vector: %v", err)
	}
	if len(parsed) != 4 {
		t.Errorf("Expected 4 dimensions, got %d", len(parsed))
	}
	if parsed[0] != 0.5 {
		t.Errorf("Expected 0.5, got %f", parsed[0])
	}

	// Test ParseVector
	parsed2, err := ParseVector("[0.1,0.2,0.3]")
	if err != nil {
		t.Fatalf("Failed to parse vector: %v", err)
	}
	if len(parsed2) != 3 {
		t.Errorf("Expected 3 dimensions, got %d", len(parsed2))
	}
}

func TestVectorSQLExpressionFormats(t *testing.T) {
	// Test SQL expression generation (without a real VectorHelper)
	vec := Vector{0.1, 0.2, 0.3, 0.4}

	// Test vector string format for SQL
	expected := "[0.100000,0.200000,0.300000,0.400000]"
	if vec.String() != expected {
		t.Errorf("Expected %q, got %q", expected, vec.String())
	}
}

func TestConfigFromPlatformSettings(t *testing.T) {
	loader := NewConfigLoader("/tmp/agentctl-test")

	// Test SQLite default
	cfg := loader.ConfigFromPlatformSettings(PlatformDatabaseSettings{
		Driver: "",
	}, "test")
	if cfg.Driver != DriverSQLite {
		t.Errorf("Expected SQLite driver for empty driver, got %s", cfg.Driver)
	}

	// Test explicit SQLite
	cfg = loader.ConfigFromPlatformSettings(PlatformDatabaseSettings{
		Driver: "sqlite",
	}, "test")
	if cfg.Driver != DriverSQLite {
		t.Errorf("Expected SQLite driver, got %s", cfg.Driver)
	}
	if cfg.SQLite.Path != "/tmp/agentctl-test/test.db" {
		t.Errorf("Unexpected SQLite path: %s", cfg.SQLite.Path)
	}

	// Test Turso
	cfg = loader.ConfigFromPlatformSettings(PlatformDatabaseSettings{
		Driver:           "turso",
		TursoURL:         "libsql://test.turso.io",
		TursoAuthToken:   "test-token",
		VectorEnabled:    true,
		VectorDimensions: 1024,
	}, "memory")
	if cfg.Driver != DriverTurso {
		t.Errorf("Expected Turso driver, got %s", cfg.Driver)
	}
	if cfg.Turso.URL != "libsql://test.turso.io" {
		t.Errorf("Unexpected Turso URL: %s", cfg.Turso.URL)
	}
	if cfg.Turso.AuthToken != "test-token" {
		t.Errorf("Unexpected Turso token: %s", cfg.Turso.AuthToken)
	}
	if !cfg.Turso.EnableVectorSearch {
		t.Error("Expected vector search to be enabled")
	}
	if cfg.Turso.VectorDimensions != 1024 {
		t.Errorf("Expected 1024 dimensions, got %d", cfg.Turso.VectorDimensions)
	}

	// Test Turso with default dimensions
	cfg = loader.ConfigFromPlatformSettings(PlatformDatabaseSettings{
		Driver:         "turso",
		TursoURL:       "libsql://test.turso.io",
		TursoAuthToken: "test-token",
		VectorEnabled:  true,
		// VectorDimensions: 0 (unset)
	}, "memory")
	if cfg.Turso.VectorDimensions != DefaultVectorDimensions {
		t.Errorf("Expected default %d dimensions, got %d", DefaultVectorDimensions, cfg.Turso.VectorDimensions)
	}

	// Test LibSQL
	cfg = loader.ConfigFromPlatformSettings(PlatformDatabaseSettings{
		Driver:           "libsql",
		VectorEnabled:    true,
		VectorDimensions: 768,
	}, "memory")
	if cfg.Driver != DriverLibSQL {
		t.Errorf("Expected LibSQL driver, got %s", cfg.Driver)
	}
	if cfg.LibSQL.Path != "/tmp/agentctl-test/memory.db" {
		t.Errorf("Unexpected LibSQL path: %s", cfg.LibSQL.Path)
	}
	if !cfg.LibSQL.EnableVectorSearch {
		t.Error("Expected vector search to be enabled")
	}
	if cfg.LibSQL.VectorDimensions != 768 {
		t.Errorf("Expected 768 dimensions, got %d", cfg.LibSQL.VectorDimensions)
	}
}
