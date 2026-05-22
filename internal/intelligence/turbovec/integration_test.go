package turbovec

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	testDim      = 128 // small dim for fast tests (must be multiple of 8)
	testBitWidth = 4
	testK        = 5
)

// TestIntegration requires turbovecd to be built. It starts the sidecar,
// runs commands through the Go client, and verifies results.
func TestIntegration(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	dataDir := t.TempDir()

	// Find turbovecd binary.
	bin := findTurbovecd(t)
	if bin == "" {
		t.Skip("turbovecd binary not found — build with: cargo build -p turbovec-server --release")
	}

	// Start the sidecar.
	cmd := exec.Command(bin, "--socket", socketPath, "--data-dir", dataDir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start turbovecd: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for socket to appear.
	if !waitForSocket(socketPath, 5*time.Second) {
		t.Fatal("socket did not appear")
	}

	// Connect.
	client, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	// PING
	if err := client.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// CREATE
	if err := client.Create("test-idx", testDim, testBitWidth); err != nil {
		t.Fatalf("create: %v", err)
	}

	// INFO
	info, err := client.Info("test-idx")
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.Dim != testDim {
		t.Errorf("info dim = %d, want %d", info.Dim, testDim)
	}
	if info.NVectors != 0 {
		t.Errorf("info n_vectors = %d, want 0", info.NVectors)
	}

	// Generate test vectors — 100 random unit vectors.
	nVectors := 100
	vectors := make([][]float32, nVectors)
	ids := make([]uint64, nVectors)
	for i := 0; i < nVectors; i++ {
		vectors[i] = randomUnitVector(testDim)
		ids[i] = uint64(i + 1) // IDs start at 1
	}

	// ADD — one at a time.
	for i := 0; i < nVectors; i++ {
		total, err := client.Add("test-idx", ids[i], vectors[i])
		if err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
		if total != uint32(i+1) {
			t.Errorf("add total = %d, want %d", total, i+1)
		}
	}

	// INFO after adds.
	info, err = client.Info("test-idx")
	if err != nil {
		t.Fatalf("info after adds: %v", err)
	}
	if info.NVectors != uint32(nVectors) {
		t.Errorf("info n_vectors = %d, want %d", info.NVectors, nVectors)
	}

	// PREPARE (eager cache population).
	if err := client.Prepare("test-idx"); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	// SEARCH — query with vector 0, should find vector 0 as top result.
	hits, err := client.Search("test-idx", vectors[0], testK)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("search returned 0 hits")
	}

	// The query IS vector 0 (id=1), so it should be the top result.
	if hits[0].ID != 1 {
		t.Errorf("top hit id = %d, want 1 (exact match)", hits[0].ID)
	}
	if hits[0].Score < 0.9 {
		t.Errorf("top hit score = %f, want > 0.9 (exact match)", hits[0].Score)
	}

	t.Logf("Search for vector 0: top-5 hits:")
	for i, h := range hits {
		t.Logf("  [%d] id=%d score=%.6f", i, h.ID, h.Score)
	}

	// SEARCH_FILTERED — only allow IDs [1, 50, 99].
	allowlist := []uint64{1, 50, 99}
	hits, err = client.SearchFiltered("test-idx", vectors[0], testK, allowlist)
	if err != nil {
		t.Fatalf("search filtered: %v", err)
	}
	for _, h := range hits {
		found := false
		for _, allowed := range allowlist {
			if h.ID == allowed {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("filtered search returned id=%d not in allowlist", h.ID)
		}
	}
	t.Logf("Filtered search (allowlist [1,50,99]): %d hits", len(hits))

	// REMOVE — remove vector with id=1.
	removed, err := client.Remove("test-idx", 1)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !removed {
		t.Error("remove returned false, want true")
	}

	// Verify removal.
	info, err = client.Info("test-idx")
	if err != nil {
		t.Fatalf("info after remove: %v", err)
	}
	if info.NVectors != uint32(nVectors-1) {
		t.Errorf("n_vectors after remove = %d, want %d", info.NVectors, nVectors-1)
	}

	// SAVE
	indexFile := filepath.Join(dataDir, "test-idx.tvim")
	if err := client.Save("test-idx", indexFile); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(indexFile); err != nil {
		t.Errorf("index file not created: %v", err)
	}

	// DROP
	if err := client.Drop("test-idx"); err != nil {
		t.Fatalf("drop: %v", err)
	}

	// LOAD from saved file.
	info, err = client.Load("test-idx", indexFile)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if info.NVectors != uint32(nVectors-1) {
		t.Errorf("loaded n_vectors = %d, want %d", info.NVectors, nVectors-1)
	}

	// Search after load.
	hits, err = client.Search("test-idx", vectors[50], testK)
	if err != nil {
		t.Fatalf("search after load: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("search after load returned 0 hits")
	}
	t.Logf("Search after load/reload: top hit id=%d score=%.6f", hits[0].ID, hits[0].Score)

	t.Log("All integration tests passed!")
}

// TestVectorIndexIntegration tests the higher-level VectorIndex API.
func TestVectorIndexIntegration(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	dataDir := t.TempDir()

	bin := findTurbovecd(t)
	if bin == "" {
		t.Skip("turbovecd binary not found")
	}

	cmd := exec.Command(bin, "--socket", socketPath, "--data-dir", dataDir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start turbovecd: %v", err)
	}
	defer cmd.Process.Kill()

	if !waitForSocket(socketPath, 5*time.Second) {
		t.Fatal("socket did not appear")
	}

	vi := NewVectorIndex("test-workspace", testDim, IndexConfig{
		SocketPath: socketPath,
		DataDir:    dataDir,
		BitWidth:   testBitWidth,
	})
	defer vi.Close()

	// Add documents.
	nDocs := 50
	for i := 0; i < nDocs; i++ {
		docID := fmt.Sprintf("doc-%03d", i)
		vec := randomUnitVector(testDim)
		if err := vi.AddVector(docID, vec); err != nil {
			t.Fatalf("AddVector %s: %v", docID, err)
		}
	}

	// Search.
	query := randomUnitVector(testDim)
	results, err := vi.Search(query, testK)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search returned 0 results")
	}
	t.Logf("VectorIndex search: %d results", len(results))
	for i, r := range results {
		t.Logf("  [%d] doc=%s score=%.6f", i, r.DocID, r.Score)
	}

	// Save and reload.
	if err := vi.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify files were created.
	tvFile := filepath.Join(dataDir, "test-workspace.tvim")
	idFile := filepath.Join(dataDir, "test-workspace.idmap.json")
	if _, err := os.Stat(tvFile); err != nil {
		t.Errorf("tvim file not found: %v", err)
	}
	if _, err := os.Stat(idFile); err != nil {
		t.Errorf("idmap file not found: %v", err)
	}

	// Verify idmap JSON is valid.
	idmapData, err := os.ReadFile(idFile)
	if err != nil {
		t.Fatalf("read idmap: %v", err)
	}
	var idmap map[string]any
	if err := json.Unmarshal(idmapData, &idmap); err != nil {
		t.Fatalf("parse idmap: %v", err)
	}
	nextID, ok := idmap["next_id"].(float64)
	if !ok || nextID != float64(nDocs) {
		t.Errorf("idmap next_id = %v, want %d", idmap["next_id"], nDocs)
	}

	// Filtered search.
	docIDs := []string{"doc-010", "doc-020", "doc-030"}
	results, err = vi.SearchFiltered(query, testK, docIDs)
	if err != nil {
		t.Fatalf("SearchFiltered: %v", err)
	}
	for _, r := range results {
		found := false
		for _, allowed := range docIDs {
			if r.DocID == allowed {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("filtered search returned doc=%s not in allowlist", r.DocID)
		}
	}

	// Remove.
	if err := vi.RemoveVector("doc-000"); err != nil {
		t.Fatalf("RemoveVector: %v", err)
	}

	t.Log("VectorIndex integration tests passed!")
}

func findTurbovecd(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join(os.Getenv("HOME"), "repos/turbovec/target/release/turbovecd"),
		"/usr/local/bin/turbovecd",
		"turbovecd", // hope it's on PATH
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func waitForSocket(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func randomUnitVector(dim int) []float32 {
	vec := make([]float32, dim)
	var norm float64
	for i := range vec {
		v := rand.NormFloat64()
		vec[i] = float32(v)
		norm += v * v
	}
	norm = math.Sqrt(norm)
	for i := range vec {
		vec[i] /= float32(norm)
	}
	return vec
}
