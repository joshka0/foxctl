package adapters

import (
	"fmt"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

// retrievalv2 adapter converts search response types to canonical EvidencePack.
// The source package (internal/context/retrievalv2) does not exist yet,
// so this adapter defines the expected source types locally for forward compatibility.
// Once the source package is created, replace these local types with imports.

// SearchResponse represents a retrieval search result from the v2 search system.
type SearchResponse struct {
	Query  string      `json:"query"`
	Plan   string      `json:"plan,omitempty"`
	Hits   []FusedHit  `json:"hits,omitempty"`
	Groups []string    `json:"groups,omitempty"`
	Stats  SearchStats `json:"stats,omitempty"`
}

// SearchStats holds search result statistics.
type SearchStats struct {
	TotalHits  int   `json:"total_hits"`
	DurationMS int64 `json:"duration_ms"`
	TokensUsed int   `json:"tokens_used,omitempty"`
}

// FusedHit is a ranked search result with multiple source contributions.
type FusedHit struct {
	Document      string             `json:"document"`
	Score         float64            `json:"score"`
	Sources       []string           `json:"sources,omitempty"`
	SourceScores  map[string]float64 `json:"source_scores,omitempty"`
	Contributions []HitContribution  `json:"contributions,omitempty"`
	Title         string             `json:"title,omitempty"`
	Snippet       string             `json:"snippet,omitempty"`
}

// HitContribution describes one source's contribution to a fused hit.
type HitContribution struct {
	Source string  `json:"source"`
	Score  float64 `json:"score"`
}

// ConvertSearchResponse converts a retrievalv2 SearchResponse to a contextengine.EvidencePack.
func ConvertSearchResponse(workspaceID string, src SearchResponse) contextengine.EvidencePack {
	nodes := make([]contextengine.EvidenceNode, 0, len(src.Hits))
	for i, hit := range src.Hits {
		nodes = append(nodes, ConvertFusedHit(workspaceID, i, hit))
	}
	return contextengine.EvidencePack{
		ID:          fmt.Sprintf("search_%s_%s", workspaceID, src.Query),
		WorkspaceID: workspaceID,
		Query:       src.Query,
		Lane:        contextengine.LaneMixed,
		Nodes:       nodes,
		Telemetry: contextengine.EvidenceTelemetry{
			DurationMs: src.Stats.DurationMS,
			TokensUsed: src.Stats.TokensUsed,
		},
		Metadata: map[string]any{
			"plan":   src.Plan,
			"groups": src.Groups,
			"stats":  src.Stats,
		},
	}
}

// ConvertFusedHit converts a FusedHit to a contextengine.EvidenceNode.
func ConvertFusedHit(workspaceID string, index int, src FusedHit) contextengine.EvidenceNode {
	ref := contextengine.EvidenceRef{
		Type: contextengine.RefTypePath,
		Ref:  src.Document,
	}
	return contextengine.EvidenceNode{
		ID:          fmt.Sprintf("hit_%s_%d", workspaceID, index),
		WorkspaceID: workspaceID,
		NodeType:    contextengine.EvidenceNodeTypeRetrieval,
		Ref:         ref,
		Statement:   src.Snippet,
		Confidence:  src.Score,
		Grounding:   contextengine.GroundingSemantic,
		Metadata: map[string]any{
			"document":      src.Document,
			"title":         src.Title,
			"score":         src.Score,
			"sources":       src.Sources,
			"source_scores": src.SourceScores,
		},
	}
}
