package run

import (
	"context"
	"encoding/json"
)

// ArtifactSearchPath identifies the retrieval execution path for semantic search.
type ArtifactSearchPath string

const (
	ArtifactSearchPathVector   ArtifactSearchPath = "vector"
	ArtifactSearchPathFallback ArtifactSearchPath = "fallback"
	ArtifactSearchPathDisabled ArtifactSearchPath = "disabled"
	ArtifactSearchPathError    ArtifactSearchPath = "error"
)

// ArtifactVectorCapability indicates whether native vector search is currently
// available for the retriever at runtime.
type ArtifactVectorCapability string

const (
	ArtifactVectorCapabilityEnabled  ArtifactVectorCapability = "enabled"
	ArtifactVectorCapabilityDisabled ArtifactVectorCapability = "disabled"
	ArtifactVectorCapabilityUnknown  ArtifactVectorCapability = "unknown"
)

// ArtifactSearchOptions controls semantic artifact retrieval.
type ArtifactSearchOptions struct {
	SessionID     string
	ArtifactTypes []string
	Limit         int
	MinSimilarity float64
}

// ScoredArtifact is one semantic artifact search hit.
type ScoredArtifact struct {
	Ref             string          `json:"ref"`
	TurnID          string          `json:"turn_id"`
	ArtifactType    string          `json:"artifact_type"`
	ArtifactVersion string          `json:"artifact_version"`
	Similarity      float64         `json:"similarity"`
	Distance        float64         `json:"distance,omitempty"`
	Summary         string          `json:"summary,omitempty"`
	MetadataJSON    json.RawMessage `json:"metadata_json,omitempty"`
}

// Clone returns a deep copy safe for cross-goroutine reads.
func (a ScoredArtifact) Clone() ScoredArtifact {
	out := a
	if len(a.MetadataJSON) > 0 {
		out.MetadataJSON = append(json.RawMessage(nil), a.MetadataJSON...)
	}
	return out
}

// ArtifactSearchResult is the semantic retrieval result set plus path metadata.
type ArtifactSearchResult struct {
	Hits             []ScoredArtifact         `json:"hits,omitempty"`
	SearchPath       ArtifactSearchPath       `json:"search_path,omitempty"`
	VectorCapability ArtifactVectorCapability `json:"vector_capability,omitempty"`
}

// ArtifactSemanticRetriever searches persisted turn artifacts by semantic embedding.
type ArtifactSemanticRetriever interface {
	SearchArtifactsByEmbedding(ctx context.Context, queryEmbedding []float32, opts ArtifactSearchOptions) (ArtifactSearchResult, error)
}
