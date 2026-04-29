package contextengine

import (
	"context"
	"fmt"
)

// RetrieveMixed fans out to all four lanes concurrently and fuses results
// by typed ref identity.
//
// VAL-RETR-005: Concurrently calls all four lanes.
// VAL-RETR-006: Fuses by EvidenceRef.Type+Ref identity key.
// VAL-RETR-007: Fused nodes preserve all source lanes in metadata.
// VAL-RETR-009: Records parent episode with sub-episodes.
// VAL-RETR-018: Continues with partial results when one lane fails.
func RetrieveMixed(
	ctx context.Context,
	cfg LaneConfig,
	codeFn CodeSearchFunc,
	memoryFn MemoryQueryFunc,
	contextFn ContextQueryFunc,
	taskQueryFn TaskQueryFunc,
	taskListFn TaskListFunc,
	taskID string,
	query string,
) (EvidencePack, error) {
	if err := validateQuery(query, LaneMixed); err != nil {
		return EvidencePack{}, err
	}

	start := cfg.Clock()

	// Fan out to all four lanes concurrently.
	type laneResult struct {
		pack EvidencePack
		lane EvidenceLane
		err  error
	}

	ch := make(chan laneResult, 4)

	// Code lane
	go func() {
		pack, err := RetrieveCode(ctx, cfg, codeFn, query)
		ch <- laneResult{pack: pack, lane: LaneCode, err: err}
	}()

	// Memory lane
	go func() {
		pack, err := RetrieveMemory(ctx, cfg, memoryFn, query)
		ch <- laneResult{pack: pack, lane: LaneMemory, err: err}
	}()

	// Context lane
	go func() {
		pack, err := RetrieveContext(ctx, cfg, contextFn, query)
		ch <- laneResult{pack: pack, lane: LaneContext, err: err}
	}()

	// Task lane
	go func() {
		pack, err := RetrieveTask(ctx, cfg, taskQueryFn, taskListFn, taskID, query)
		ch <- laneResult{pack: pack, lane: LaneTask, err: err}
	}()

	// Collect results.
	var subEpisodeIDs []string
	var allNodes []EvidenceNode
	var laneErrors []LaneError
	lanesSucceeded := 0

	for i := 0; i < 4; i++ {
		result := <-ch
		// Extract the sub-lane episode ID from the pack metadata.
		epID := extractEpisodeID(result.pack.Metadata)

		if result.err != nil {
			// Check if it's a LaneError (partial failure with results).
			if le, ok := result.err.(LaneError); ok {
				laneErrors = append(laneErrors, le)
				// Partial failure: still collect nodes from the pack.
				if len(result.pack.Nodes) > 0 {
					allNodes = append(allNodes, result.pack.Nodes...)
				}
				// Even on partial failure, the lane recorded an episode.
				if epID != "" {
					subEpisodeIDs = append(subEpisodeIDs, epID)
				}
				lanesSucceeded++
			}
			// Non-LaneError (e.g. EmptyQueryError): skip.
			continue
		}
		allNodes = append(allNodes, result.pack.Nodes...)
		if epID != "" {
			subEpisodeIDs = append(subEpisodeIDs, epID)
		}
		lanesSucceeded++
	}

	elapsed := cfg.Clock().Sub(start)
	packID := cfg.IDGen()

	// Fuse nodes by typed ref identity.
	fusedNodes := fuseNodes(allNodes)

	// Build error metadata for partial failures.
	packMeta := map[string]any{}
	if len(laneErrors) > 0 {
		errStrs := make([]string, 0, len(laneErrors))
		for _, le := range laneErrors {
			errStrs = append(errStrs, le.Error())
		}
		packMeta["lane_errors"] = errStrs
	}

	pack := EvidencePack{
		ID:          packID,
		WorkspaceID: cfg.WorkspaceID,
		Query:       query,
		Lane:        LaneMixed,
		Nodes:       fusedNodes,
		Telemetry: EvidenceTelemetry{
			DurationMs: elapsed.Milliseconds(),
			LanesFused: lanesSucceeded,
		},
		Metadata: packMeta,
	}

	_ = recordPack(ctx, cfg, pack)
	_, _ = recordEpisode(ctx, cfg, query, LaneMixed, packID, elapsed.Milliseconds(), len(fusedNodes), subEpisodeIDs)

	// If ALL lanes failed, return an error.
	if lanesSucceeded == 0 && len(laneErrors) > 0 {
		return pack, fmt.Errorf("contextengine: all lanes failed: %v", laneErrors)
	}

	return pack, nil
}

// fuseNodes merges EvidenceNodes by typed ref identity.
// VAL-RETR-006: Uses EvidenceRef.Type+Ref as identity key.
// VAL-RETR-007: Fused nodes preserve all source lanes in metadata.
func fuseNodes(nodes []EvidenceNode) []EvidenceNode {
	type refKey struct {
		Type RefType
		Ref  string
	}

	seen := make(map[refKey]int) // index into fused slice
	var fused []EvidenceNode

	for _, node := range nodes {
		key := refKey{Type: node.Ref.Type, Ref: node.Ref.Ref}
		// Always derive the incoming lane from the node type.
		incomingLane := string(nodeTypeToLane(node.NodeType))

		if idx, ok := seen[key]; ok {
			// Merge: accumulate source lanes.
			existing := &fused[idx]
			lanes := extractLanes(existing.Metadata)
			if !containsLane(lanes, incomingLane) {
				lanes = append(lanes, incomingLane)
			}
			if existing.Metadata == nil {
				existing.Metadata = map[string]any{}
			}
			existing.Metadata["source_lanes"] = lanes

			// Take the higher confidence.
			if node.Confidence > existing.Confidence {
				existing.Confidence = node.Confidence
			}

			// Merge statements if different.
			if node.Statement != "" && existing.Statement != node.Statement {
				if existing.Statement != "" {
					existing.Statement = existing.Statement + "; " + node.Statement
				} else {
					existing.Statement = node.Statement
				}
			}
		} else {
			// First occurrence: set source_lanes from node's own lane type.
			meta := make(map[string]any, len(node.Metadata)+1)
			for k, v := range node.Metadata {
				meta[k] = v
			}
			meta["source_lanes"] = []string{incomingLane}

			fusedNode := node
			fusedNode.Metadata = meta
			seen[key] = len(fused)
			fused = append(fused, fusedNode)
		}
	}

	return fused
}

// extractLanes gets the source_lanes from metadata.
func extractLanes(meta map[string]any) []string {
	if meta == nil {
		return nil
	}
	v, ok := meta["source_lanes"]
	if !ok {
		return nil
	}
	switch lanes := v.(type) {
	case []string:
		return lanes
	case []any:
		result := make([]string, 0, len(lanes))
		for _, l := range lanes {
			if s, ok := l.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// containsLane checks if a lane string is in the slice.
func containsLane(lanes []string, lane string) bool {
	for _, l := range lanes {
		if l == lane {
			return true
		}
	}
	return false
}

// nodeTypeToLane maps EvidenceNodeType to EvidenceLane.
func nodeTypeToLane(nt EvidenceNodeType) EvidenceLane {
	switch nt {
	case EvidenceNodeTypeCode:
		return LaneCode
	case EvidenceNodeTypeMemory:
		return LaneMemory
	case EvidenceNodeTypeContext:
		return LaneContext
	case EvidenceNodeTypeTask:
		return LaneTask
	default:
		return LaneMixed
	}
}

// extractEpisodeID retrieves the episode_id from pack metadata.
func extractEpisodeID(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	v, ok := meta["episode_id"]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
