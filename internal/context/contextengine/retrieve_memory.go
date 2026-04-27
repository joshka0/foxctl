package contextengine

import (
	"context"
	"fmt"
)

// RetrieveMemory queries memory store and session evidence directly.
// VAL-RETR-002: Returns EvidencePack with Lane="memory".
// VAL-RETR-012: Uses direct store queries, not recursive scouts.
func RetrieveMemory(ctx context.Context, cfg LaneConfig, queryFn MemoryQueryFunc, query string) (EvidencePack, error) {
	if err := validateQuery(query, LaneMemory); err != nil {
		return EvidencePack{}, err
	}

	start := cfg.Clock()

	claims, err := queryFn(ctx, cfg.WorkspaceID)
	elapsed := cfg.Clock().Sub(start)

	packID := cfg.IDGen()

	if err != nil {
		pack := EvidencePack{
			ID:          packID,
			WorkspaceID: cfg.WorkspaceID,
			Query:       query,
			Lane:        LaneMemory,
			Nodes:       nil,
			Telemetry: EvidenceTelemetry{
				DurationMs: elapsed.Milliseconds(),
			},
			Metadata: map[string]any{
				"error": err.Error(),
			},
		}
		_ = recordEpisode(ctx, cfg, query, LaneMemory, packID, elapsed.Milliseconds(), 0, nil)
		return pack, LaneError{Lane: LaneMemory, Err: err}
	}

	nodes := make([]EvidenceNode, 0, len(claims))
	for _, claim := range claims {
		ref := EvidenceRef{
			Type:        RefTypeMemoryClaim,
			Ref:         claim.ID,
			WorkspaceID: cfg.WorkspaceID,
		}

		node := EvidenceNode{
			ID:          cfg.IDGen(),
			WorkspaceID: cfg.WorkspaceID,
			NodeType:    EvidenceNodeTypeMemory,
			Ref:         ref,
			Statement:   claim.Summary,
			Confidence:  claim.Confidence,
			Grounding:   GroundingValidated,
			Metadata: map[string]any{
				"claim_type": claim.ClaimType,
				"status":     string(claim.Status),
			},
		}
		nodes = append(nodes, node)
	}

	pack := EvidencePack{
		ID:          packID,
		WorkspaceID: cfg.WorkspaceID,
		Query:       query,
		Lane:        LaneMemory,
		Nodes:       nodes,
		Telemetry: EvidenceTelemetry{
			DurationMs: elapsed.Milliseconds(),
		},
	}

	_ = recordEpisode(ctx, cfg, query, LaneMemory, packID, elapsed.Milliseconds(), len(nodes), nil)
	return pack, nil
}

// Ensure RetrieveMemory compiles with expected signature.
var _ = fmt.Sprintf
