package contextengine

import (
	"context"
)

// RetrieveCode searches code using a code search function and returns an EvidencePack.
// VAL-RETR-001: Returns EvidencePack with Lane="code".
// VAL-RETR-011: Wraps code_search_ensemble output into EvidencePack.
func RetrieveCode(ctx context.Context, cfg LaneConfig, searchFn CodeSearchFunc, query string) (EvidencePack, error) {
	if err := validateQuery(query, LaneCode); err != nil {
		return EvidencePack{}, err
	}

	start := cfg.Clock()
	hits, err := searchFn(ctx, query)
	elapsed := cfg.Clock().Sub(start)

	packID := cfg.IDGen()

	// If the search function failed, return partial results with error metadata.
	if err != nil {
		pack := EvidencePack{
			ID:          packID,
			WorkspaceID: cfg.WorkspaceID,
			Query:       query,
			Lane:        LaneCode,
			Nodes:       nil,
			Telemetry: EvidenceTelemetry{
				DurationMs: elapsed.Milliseconds(),
			},
			Metadata: map[string]any{
				"error": err.Error(),
			},
		}
		_ = recordPack(ctx, cfg, pack)
		episodeID, _ := recordEpisode(ctx, cfg, query, LaneCode, packID, elapsed.Milliseconds(), 0, nil)
		pack.Metadata["episode_id"] = episodeID
		return pack, LaneError{Lane: LaneCode, Err: err}
	}

	nodes := make([]EvidenceNode, 0, len(hits))
	for _, hit := range hits {
		ref := EvidenceRef{
			Type:        RefTypePath,
			Ref:         hit.Path,
			WorkspaceID: cfg.WorkspaceID,
		}
		if hit.Symbol != "" {
			ref = EvidenceRef{
				Type:        RefTypeSymbol,
				Ref:         hit.Symbol,
				WorkspaceID: cfg.WorkspaceID,
			}
		}

		node := EvidenceNode{
			ID:          cfg.IDGen(),
			WorkspaceID: cfg.WorkspaceID,
			NodeType:    EvidenceNodeTypeCode,
			Ref:         ref,
			Statement:   hit.Snippet,
			Confidence:  hit.Score,
			Grounding:   GroundingIndexed,
			Metadata: map[string]any{
				"line":     hit.Line,
				"language": hit.Language,
			},
		}
		for key, value := range hit.Metadata {
			node.Metadata[key] = value
		}
		if len(hit.Sources) > 0 {
			node.Metadata["sources"] = append([]string(nil), hit.Sources...)
		}
		if hit.Path != "" {
			node.Metadata["path"] = hit.Path
		}
		nodes = append(nodes, node)
	}

	pack := EvidencePack{
		ID:          packID,
		WorkspaceID: cfg.WorkspaceID,
		Query:       query,
		Lane:        LaneCode,
		Nodes:       nodes,
		Telemetry: EvidenceTelemetry{
			DurationMs: elapsed.Milliseconds(),
		},
	}

	_ = recordPack(ctx, cfg, pack)
	episodeID, _ := recordEpisode(ctx, cfg, query, LaneCode, packID, elapsed.Milliseconds(), len(nodes), nil)
	pack.Metadata = map[string]any{"episode_id": episodeID}
	return pack, nil
}
