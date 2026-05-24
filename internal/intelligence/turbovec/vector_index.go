package turbovec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/joshka0/foxctl/internal/platform/workspace"
)

const (
	// DefaultBitWidth is the default quantization bit width (4 = 16 buckets per coordinate).
	DefaultBitWidth = 4
)

// VectorIndex manages a turbovec sidecar connection for a single workspace's
// vector search. It handles index lifecycle (create/load/save), adding vectors
// on upsert, and searching.
//
// It does NOT store document metadata — that stays in the SQL searchindex.
// This type only manages the compressed vector index and translates between
// document IDs (strings) and the sidecar's uint64 IDs.
type VectorIndex struct {
	mu         sync.Mutex
	client     *Client
	workspace  string
	dim        uint32
	bitWidth   uint8
	socketPath string
	connected  bool

	// idMap translates between foxctl document IDs (string) and
	// turbovec external IDs (uint64).
	idMap   map[string]uint64 // docID → externalID
	reverse map[uint64]string // externalID → docID
	nextID  uint64
	dirty   bool
	dataDir string
}

// IndexConfig holds configuration for creating a VectorIndex.
type IndexConfig struct {
	// SocketPath is the path to the turbovecd Unix socket.
	// Empty uses DefaultSocketPath().
	SocketPath string

	// DataDir is the directory for persisting index files.
	// Empty uses ~/.foxctl/storage/.
	DataDir string

	// BitWidth is the quantization bit width (2, 3, or 4).
	// Default: 4 (best recall, 16x compression).
	BitWidth int
}

// NewVectorIndex creates a new VectorIndex for the given workspace.
// The sidecar connection is established lazily on first use.
func NewVectorIndex(workspaceID string, dim int, cfg IndexConfig) *VectorIndex {
	workspaceID = workspace.CanonicalID(workspaceID)
	socketPath := cfg.SocketPath
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}

	dataDir := cfg.DataDir
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".foxctl", "storage")
	}

	bitWidth := uint8(cfg.BitWidth)
	if bitWidth == 0 {
		bitWidth = DefaultBitWidth
	}

	return &VectorIndex{
		workspace:  workspaceID,
		dim:        uint32(dim),
		bitWidth:   bitWidth,
		socketPath: socketPath,
		dataDir:    dataDir,
		idMap:      make(map[string]uint64),
		reverse:    make(map[uint64]string),
	}
}

// EnsureConnected establishes a connection to the sidecar if not already connected.
func (vi *VectorIndex) EnsureConnected() error {
	vi.mu.Lock()
	defer vi.mu.Unlock()

	if vi.connected {
		return nil
	}

	client, err := Dial(vi.socketPath)
	if err != nil {
		return fmt.Errorf("turbovec: connect: %w", err)
	}

	vi.client = client
	vi.connected = true

	// Try to load an existing index for this workspace.
	indexName := vi.indexName()
	indexPath := vi.indexFilePath()

	if _, err := os.Stat(indexPath); err == nil {
		// Index file exists — load it.
		if _, err := vi.client.Load(indexName, indexPath); err != nil {
			// Load failed — create fresh.
			_ = vi.client.Drop(indexName)
			if err := vi.client.Create(indexName, vi.dim, vi.bitWidth); err != nil {
				return fmt.Errorf("turbovec: create index: %w", err)
			}
		}
	} else {
		// No saved index — create fresh.
		if err := vi.client.Create(indexName, vi.dim, vi.bitWidth); err != nil {
			return fmt.Errorf("turbovec: create index: %w", err)
		}
	}

	// Load the ID map if it exists.
	vi.loadIDMap()

	return nil
}

// AddVector adds a single vector for a document ID.
func (vi *VectorIndex) AddVector(docID string, embedding []float32) error {
	if err := vi.EnsureConnected(); err != nil {
		return err
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()

	// Check if we already have this doc — if so, remove old entry first.
	if oldID, exists := vi.idMap[docID]; exists {
		_, _ = vi.client.Remove(vi.indexName(), oldID)
		delete(vi.reverse, oldID)
	}

	// Assign new external ID.
	extID := vi.nextID
	vi.nextID++

	vi.idMap[docID] = extID
	vi.reverse[extID] = docID

	// Add to turbovec.
	_, err := vi.client.Add(vi.indexName(), extID, embedding)
	if err != nil {
		// Rollback ID assignment on failure.
		delete(vi.idMap, docID)
		delete(vi.reverse, extID)
		vi.nextID--
		return fmt.Errorf("turbovec: add vector: %w", err)
	}

	vi.dirty = true
	return nil
}

// SearchResult is a scored document ID from a vector search.
type SearchResult struct {
	DocID string
	Score float64
}

// Search queries the vector index and returns scored document IDs.
func (vi *VectorIndex) Search(query []float32, k int) ([]SearchResult, error) {
	if err := vi.EnsureConnected(); err != nil {
		return nil, err
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()

	hits, err := vi.client.Search(vi.indexName(), query, uint32(k))
	if err != nil {
		return nil, err
	}

	// Translate external IDs back to document IDs.
	results := make([]SearchResult, 0, len(hits))
	for _, h := range hits {
		docID, ok := vi.reverse[h.ID]
		if !ok {
			continue // stale ID, skip
		}
		results = append(results, SearchResult{
			DocID: docID,
			Score: float64(h.Score),
		})
	}

	return results, nil
}

// SearchFiltered queries restricted to the given document IDs.
func (vi *VectorIndex) SearchFiltered(query []float32, k int, docIDs []string) ([]SearchResult, error) {
	if err := vi.EnsureConnected(); err != nil {
		return nil, err
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()

	// Translate document IDs to external IDs for the allowlist.
	allowlist := make([]uint64, 0, len(docIDs))
	for _, docID := range docIDs {
		if extID, ok := vi.idMap[docID]; ok {
			allowlist = append(allowlist, extID)
		}
	}

	if len(allowlist) == 0 {
		return nil, nil
	}

	hits, err := vi.client.SearchFiltered(vi.indexName(), query, uint32(k), allowlist)
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(hits))
	for _, h := range hits {
		docID, ok := vi.reverse[h.ID]
		if !ok {
			continue
		}
		results = append(results, SearchResult{
			DocID: docID,
			Score: float64(h.Score),
		})
	}

	return results, nil
}

// RemoveVector removes a vector by document ID.
func (vi *VectorIndex) RemoveVector(docID string) error {
	if err := vi.EnsureConnected(); err != nil {
		return err
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()

	extID, ok := vi.idMap[docID]
	if !ok {
		return nil // not in index
	}

	_, err := vi.client.Remove(vi.indexName(), extID)
	if err != nil {
		return err
	}

	delete(vi.idMap, docID)
	delete(vi.reverse, extID)
	vi.dirty = true
	return nil
}

// Save persists the index to disk.
func (vi *VectorIndex) Save() error {
	if vi.client == nil || !vi.dirty {
		return nil
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()

	indexPath := vi.indexFilePath()
	dir := filepath.Dir(indexPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("turbovec: create index dir: %w", err)
	}

	if err := vi.client.Save(vi.indexName(), indexPath); err != nil {
		return fmt.Errorf("turbovec: save index: %w", err)
	}

	if err := vi.saveIDMap(); err != nil {
		return fmt.Errorf("turbovec: save id map: %w", err)
	}

	vi.dirty = false
	return nil
}

// Close saves and disconnects.
func (vi *VectorIndex) Close() error {
	if vi.client == nil {
		return nil
	}
	_ = vi.Save()
	return vi.client.Close()
}

// Healthy checks if the sidecar is responsive.
func (vi *VectorIndex) Healthy(ctx context.Context) error {
	if vi.client == nil {
		return fmt.Errorf("turbovec: not connected")
	}
	return vi.client.Ping()
}

// indexName returns the sidecar index name for this workspace.
func (vi *VectorIndex) indexName() string {
	return vi.workspace
}

// indexFilePath returns the path where the compressed index is persisted.
func (vi *VectorIndex) indexFilePath() string {
	return filepath.Join(vi.dataDir, vi.workspace+".tvim")
}

// idMapPath returns the path where the ID map JSON is persisted.
func (vi *VectorIndex) idMapPath() string {
	return filepath.Join(vi.dataDir, vi.workspace+".idmap.json")
}
