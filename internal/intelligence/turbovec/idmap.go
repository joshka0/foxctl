package turbovec

import (
	"encoding/json"
	"fmt"
	"os"
)

// idMapFile is the JSON structure persisted alongside the .tvim index.
type idMapFile struct {
	NextID uint64            `json:"next_id"`
	Map    map[string]uint64 `json:"map"`
}

// saveIDMap persists the document ID ↔ external ID mapping to disk.
func (vi *VectorIndex) saveIDMap() error {
	data := idMapFile{
		NextID: vi.nextID,
		Map:    vi.idMap,
	}
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal id map: %w", err)
	}
	return os.WriteFile(vi.idMapPath(), b, 0o644)
}

// loadIDMap restores the document ID ↔ external ID mapping from disk.
// Must be called with vi.mu held.
func (vi *VectorIndex) loadIDMap() {
	b, err := os.ReadFile(vi.idMapPath())
	if err != nil {
		// No map file — start fresh.
		return
	}

	var data idMapFile
	if err := json.Unmarshal(b, &data); err != nil {
		return
	}

	vi.nextID = data.NextID
	vi.idMap = data.Map

	// Rebuild reverse map.
	vi.reverse = make(map[uint64]string, len(vi.idMap))
	for docID, extID := range vi.idMap {
		vi.reverse[extID] = docID
	}
}
