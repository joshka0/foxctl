package contextengine

import (
	"context"
)

// RetrieveContext retrieves context from TopOfMind projection and ACA.
// VAL-RETR-003: Returns EvidencePack with Lane="context".
// VAL-RETR-013: Includes TopOfMind as EvidenceNode in output pack.
func RetrieveContext(ctx context.Context, cfg LaneConfig, queryFn ContextQueryFunc, query string) (EvidencePack, error) {
	if err := validateQuery(query, LaneContext); err != nil {
		return EvidencePack{}, err
	}

	start := cfg.Clock()

	packet, err := queryFn(ctx, cfg.WorkspaceID)
	elapsed := cfg.Clock().Sub(start)

	packID := cfg.IDGen()

	if err != nil {
		pack := EvidencePack{
			ID:          packID,
			WorkspaceID: cfg.WorkspaceID,
			Query:       query,
			Lane:        LaneContext,
			Nodes:       nil,
			Telemetry: EvidenceTelemetry{
				DurationMs: elapsed.Milliseconds(),
			},
			Metadata: map[string]any{
				"error": err.Error(),
			},
		}
		episodeID, _ := recordEpisode(ctx, cfg, query, LaneContext, packID, elapsed.Milliseconds(), 0, nil)
		pack.Metadata["episode_id"] = episodeID
		return pack, LaneError{Lane: LaneContext, Err: err}
	}

	var nodes []EvidenceNode

	if packet != nil {
		// Create a node from the context packet (TopOfMind/handoff/ACA).
		objRef := EvidenceRef{
			Type:        RefTypeNote,
			Ref:         "top_of_mind:" + cfg.WorkspaceID,
			WorkspaceID: cfg.WorkspaceID,
		}

		statement := packet.Objective
		if statement == "" {
			statement = "context packet for workspace " + cfg.WorkspaceID
		}

		meta := map[string]any{
			"phase":     packet.Phase,
			"objective": packet.Objective,
		}
		if len(packet.HardConstraints) > 0 {
			meta["hard_constraints"] = packet.HardConstraints
		}
		if len(packet.Blockers) > 0 {
			meta["blockers"] = packet.Blockers
		}
		if len(packet.NextActions) > 0 {
			meta["next_actions"] = packet.NextActions
		}
		if len(packet.RecentDecisions) > 0 {
			decisions := make([]string, 0, len(packet.RecentDecisions))
			for _, d := range packet.RecentDecisions {
				decisions = append(decisions, d.Text)
			}
			meta["recent_decisions"] = decisions
		}

		node := EvidenceNode{
			ID:          cfg.IDGen(),
			WorkspaceID: cfg.WorkspaceID,
			NodeType:    EvidenceNodeTypeContext,
			Ref:         objRef,
			Statement:   statement,
			Confidence:  0.9,
			Grounding:   GroundingLoaded,
			Metadata:    meta,
		}
		nodes = append(nodes, node)

		// Add relevant refs as additional nodes.
		for _, ref := range packet.RelevantRefs {
			if err := ValidateEvidenceRef(ref); err != nil {
				continue
			}
			node := EvidenceNode{
				ID:          cfg.IDGen(),
				WorkspaceID: cfg.WorkspaceID,
				NodeType:    EvidenceNodeTypeContext,
				Ref:         ref,
				Statement:   refTitle(ref),
				Confidence:  0.7,
				Grounding:   GroundingLoaded,
			}
			nodes = append(nodes, node)
		}
	}

	pack := EvidencePack{
		ID:          packID,
		WorkspaceID: cfg.WorkspaceID,
		Query:       query,
		Lane:        LaneContext,
		Nodes:       nodes,
		Telemetry: EvidenceTelemetry{
			DurationMs: elapsed.Milliseconds(),
		},
	}

	_ = recordPack(ctx, cfg, pack)
	episodeID, _ := recordEpisode(ctx, cfg, query, LaneContext, packID, elapsed.Milliseconds(), len(nodes), nil)
	pack.Metadata = map[string]any{"episode_id": episodeID}
	return pack, nil
}

// refTitle returns a human-readable title for an EvidenceRef.
func refTitle(ref EvidenceRef) string {
	if ref.Title != "" {
		return ref.Title
	}
	return FormatEvidenceRef(ref)
}
