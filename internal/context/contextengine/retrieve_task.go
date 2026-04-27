package contextengine

import (
	"context"
)

// RetrieveTask retrieves evidence from task store and TaskContext.
// VAL-RETR-004: Returns EvidencePack with Lane="task".
// VAL-RETR-014: Includes TaskContext as EvidenceNode in output pack.
func RetrieveTask(ctx context.Context, cfg LaneConfig, taskQueryFn TaskQueryFunc, taskListFn TaskListFunc, taskID, query string) (EvidencePack, error) {
	if err := validateQuery(query, LaneTask); err != nil {
		return EvidencePack{}, err
	}

	start := cfg.Clock()

	var nodes []EvidenceNode

	// If a specific taskID is given, query that task context.
	if taskID != "" {
		tc, err := taskQueryFn(ctx, cfg.WorkspaceID, taskID)
		elapsed := cfg.Clock().Sub(start)
		packID := cfg.IDGen()

		if err != nil {
			pack := EvidencePack{
				ID:          packID,
				WorkspaceID: cfg.WorkspaceID,
				Query:       query,
				Lane:        LaneTask,
				Nodes:       nil,
				Telemetry: EvidenceTelemetry{
					DurationMs: elapsed.Milliseconds(),
				},
				Metadata: map[string]any{
					"error": err.Error(),
				},
			}
			_ = recordEpisode(ctx, cfg, query, LaneTask, packID, elapsed.Milliseconds(), 0, nil)
			return pack, LaneError{Lane: LaneTask, Err: err}
		}

		if tc != nil {
			nodes = taskContextToNodes(tc, cfg)
		}

		pack := EvidencePack{
			ID:          packID,
			WorkspaceID: cfg.WorkspaceID,
			Query:       query,
			Lane:        LaneTask,
			Nodes:       nodes,
			Telemetry: EvidenceTelemetry{
				DurationMs: elapsed.Milliseconds(),
			},
		}
		_ = recordPack(ctx, cfg, pack)
		_ = recordEpisode(ctx, cfg, query, LaneTask, packID, elapsed.Milliseconds(), len(nodes), nil)
		return pack, nil
	}

	// No specific task: query all tasks for the workspace.
	taskIDs, err := taskListFn(ctx, cfg.WorkspaceID)
	elapsed := cfg.Clock().Sub(start)
	packID := cfg.IDGen()

	if err != nil {
		pack := EvidencePack{
			ID:          packID,
			WorkspaceID: cfg.WorkspaceID,
			Query:       query,
			Lane:        LaneTask,
			Nodes:       nil,
			Telemetry: EvidenceTelemetry{
				DurationMs: elapsed.Milliseconds(),
			},
			Metadata: map[string]any{
				"error": err.Error(),
			},
		}
		_ = recordEpisode(ctx, cfg, query, LaneTask, packID, elapsed.Milliseconds(), 0, nil)
		return pack, LaneError{Lane: LaneTask, Err: err}
	}

	for _, tid := range taskIDs {
		tc, tcErr := taskQueryFn(ctx, cfg.WorkspaceID, tid)
		if tcErr != nil || tc == nil {
			continue
		}
		nodes = append(nodes, taskContextToNodes(tc, cfg)...)
	}

	pack := EvidencePack{
		ID:          packID,
		WorkspaceID: cfg.WorkspaceID,
		Query:       query,
		Lane:        LaneTask,
		Nodes:       nodes,
		Telemetry: EvidenceTelemetry{
			DurationMs: elapsed.Milliseconds(),
		},
	}

	_ = recordPack(ctx, cfg, pack)
	_ = recordEpisode(ctx, cfg, query, LaneTask, packID, elapsed.Milliseconds(), len(nodes), nil)
	return pack, nil
}

// taskContextToNodes converts a TaskContext into EvidenceNodes.
func taskContextToNodes(tc *TaskContext, cfg LaneConfig) []EvidenceNode {
	var nodes []EvidenceNode

	// Primary task context node.
	taskRef := EvidenceRef{
		Type:        RefTypeTask,
		Ref:         tc.TaskID,
		WorkspaceID: cfg.WorkspaceID,
	}

	meta := map[string]any{
		"status":    tc.Status,
		"objective": tc.Objective,
	}
	if len(tc.OpenGaps) > 0 {
		meta["open_gaps"] = tc.OpenGaps
	}
	if len(tc.StaleWarnings) > 0 {
		meta["stale_warnings"] = tc.StaleWarnings
	}
	if len(tc.NextActions) > 0 {
		meta["next_actions"] = tc.NextActions
	}

	statement := tc.Objective
	if statement == "" {
		statement = "task context for " + tc.TaskID
	}

	nodes = append(nodes, EvidenceNode{
		ID:          cfg.IDGen(),
		WorkspaceID: cfg.WorkspaceID,
		NodeType:    EvidenceNodeTypeTask,
		Ref:         taskRef,
		Statement:   statement,
		Confidence:  0.85,
		Grounding:   GroundingLoaded,
		Metadata:    meta,
	})

	// Add related code refs as secondary nodes.
	for _, ref := range tc.RelatedCodeRefs {
		if err := ValidateEvidenceRef(ref); err != nil {
			continue
		}
		nodes = append(nodes, EvidenceNode{
			ID:          cfg.IDGen(),
			WorkspaceID: cfg.WorkspaceID,
			NodeType:    EvidenceNodeTypeTask,
			Ref:         ref,
			Statement:   refTitle(ref),
			Confidence:  0.7,
			Grounding:   GroundingLoaded,
			Metadata: map[string]any{
				"task_id": tc.TaskID,
				"role":    "related_code",
			},
		})
	}

	return nodes
}
