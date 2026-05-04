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
	providerTelemetry := extractCodeSearchProviderTelemetry(hits)
	for _, hit := range hits {
		ref := EvidenceRef{
			Type:        RefTypePath,
			Ref:         hit.Path,
			WorkspaceID: cfg.WorkspaceID,
		}
		if hit.Path == "" && hit.Symbol != "" {
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
			if key == "code_search_provider_telemetry" {
				continue
			}
			node.Metadata[key] = value
		}
		if len(hit.Sources) > 0 {
			node.Metadata["sources"] = append([]string(nil), hit.Sources...)
		}
		if hit.Path != "" {
			node.Metadata["path"] = hit.Path
		}
		if hit.Symbol != "" {
			node.Metadata["symbol"] = hit.Symbol
			node.Metadata["symbol_ref"] = FormatEvidenceRef(EvidenceRef{
				Type:        RefTypeSymbol,
				Ref:         hit.Symbol,
				WorkspaceID: cfg.WorkspaceID,
			})
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
	if providerTelemetry != nil {
		pack.Metadata = map[string]any{
			"code_search_provider_telemetry": providerTelemetry,
		}
	}

	_ = recordPack(ctx, cfg, pack)
	episodeID, _ := recordEpisode(ctx, cfg, query, LaneCode, packID, elapsed.Milliseconds(), len(nodes), nil)
	if pack.Metadata == nil {
		pack.Metadata = map[string]any{}
	}
	pack.Metadata["episode_id"] = episodeID
	return pack, nil
}

func extractCodeSearchProviderTelemetry(hits []CodeSearchHit) any {
	for _, hit := range hits {
		if hit.Metadata == nil {
			continue
		}
		if telemetry, ok := hit.Metadata["code_search_provider_telemetry"]; ok {
			return telemetry
		}
	}
	return nil
}
