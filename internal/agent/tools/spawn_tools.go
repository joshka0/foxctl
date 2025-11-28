// Package tools provides dspy-go tool wrappers for agentctl skills.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"

	"github.com/jkatigb/agentctl/internal/agent/types"
)

// SpawnToolConfig holds configuration for the spawn tool.
type SpawnToolConfig struct {
	// CallerActorID is the actor ID of the agent calling spawn.
	CallerActorID string

	// CallerDepth is the caller's depth in the hierarchy.
	CallerDepth int

	// CallerMaxDepth is the global max depth.
	CallerMaxDepth int

	// CallerLocalMaxDepth is the caller's subtree limit.
	CallerLocalMaxDepth int

	// EpicID is the current epic scope.
	EpicID string

	// MailSender is a function to send mail to overseer.
	// Returns the response mail body or error.
	MailSender func(ctx context.Context, to, subject string, body any) (any, error)
}

// RegisterSpawnTool registers the agent.spawn tool with the registry.
func (r *Registry) RegisterSpawnTool(cfg SpawnToolConfig) error {
	tool := dstools.NewFuncTool(
		"agent.spawn",
		"Request overseer to spawn subagent(s) for parallel or specialized work. "+
			"Use this when work can be decomposed into cleanly separable subtasks and you have remaining depth budget.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"epic_id": {
					Type:        "string",
					Description: "Epic scope for the spawn request (defaults to caller's epic)",
				},
				"parent_plan_node_id": {
					Type:        "string",
					Description: "Plan node to attach new subtasks under",
				},
				"spawn_reason": {
					Type:        "string",
					Description: "Why splitting is beneficial (parallelism, specialization, etc.)",
					Required:    true,
				},
				"requested_subagents": {
					Type:        "string", // JSON array as string since InputSchema doesn't support array
					Description: "JSON array of subagents to spawn: [{role, task, suggested_actor_id?, local_max_depth?}]",
					Required:    true,
				},
				"wait_for_completion": {
					Type:        "boolean",
					Description: "If true, block until subagents finish; if false, continue async",
				},
			},
		},
		r.wrapWithTelemetry("agent.spawn", func(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
			return r.executeSpawn(ctx, args, cfg), nil
		}),
	)

	if err := r.tools.Register(tool); err != nil {
		return fmt.Errorf("register agent.spawn: %w", err)
	}
	return nil
}

// executeSpawn handles the agent.spawn tool logic.
func (r *Registry) executeSpawn(ctx context.Context, args map[string]any, cfg SpawnToolConfig) *models.CallToolResult {
	// Check if we have depth budget
	if cfg.CallerDepth >= cfg.CallerMaxDepth {
		return errorResult(fmt.Sprintf(
			"Cannot spawn: global depth limit reached (depth=%d, max=%d). Complete task yourself or escalate to overseer.",
			cfg.CallerDepth, cfg.CallerMaxDepth,
		))
	}
	if cfg.CallerDepth >= cfg.CallerLocalMaxDepth {
		return errorResult(fmt.Sprintf(
			"Cannot spawn: local depth limit reached (depth=%d, local_max=%d). Complete task yourself or escalate to overseer.",
			cfg.CallerDepth, cfg.CallerLocalMaxDepth,
		))
	}

	// Parse requested subagents (handles both []any and JSON string)
	var subagentsRaw []any
	switch v := args["requested_subagents"].(type) {
	case []any:
		subagentsRaw = v
	case string:
		if err := json.Unmarshal([]byte(v), &subagentsRaw); err != nil {
			return errorResult(fmt.Sprintf("requested_subagents JSON parse error: %v", err))
		}
	default:
		return errorResult("requested_subagents must be a JSON array or array string")
	}
	if len(subagentsRaw) == 0 {
		return errorResult("requested_subagents must be non-empty")
	}

	spawnReason, _ := args["spawn_reason"].(string)
	if spawnReason == "" {
		return errorResult("spawn_reason is required")
	}

	epicID, _ := args["epic_id"].(string)
	if epicID == "" {
		epicID = cfg.EpicID
	}
	if epicID == "" {
		return errorResult("epic_id is required (either in args or caller context)")
	}

	parentPlanNodeID, _ := args["parent_plan_node_id"].(string)
	waitForCompletion, _ := args["wait_for_completion"].(bool)

	// Parse subagent requests
	var requestedSubagents []types.SubagentRequest
	for i, raw := range subagentsRaw {
		subMap, ok := raw.(map[string]any)
		if !ok {
			return errorResult(fmt.Sprintf("requested_subagents[%d] must be an object", i))
		}

		role, _ := subMap["role"].(string)
		task, _ := subMap["task"].(string)
		if role == "" || task == "" {
			return errorResult(fmt.Sprintf("requested_subagents[%d] requires role and task", i))
		}

		suggestedActorID, _ := subMap["suggested_actor_id"].(string)
		localMaxDepth := 0
		if lmd, ok := subMap["local_max_depth"].(float64); ok {
			localMaxDepth = int(lmd)
		}

		// Clamp local_max_depth to parent's limit
		if localMaxDepth <= 0 || localMaxDepth > cfg.CallerLocalMaxDepth {
			localMaxDepth = cfg.CallerLocalMaxDepth
		}

		requestedSubagents = append(requestedSubagents, types.SubagentRequest{
			Role:             types.AgentRole(role),
			Task:             task,
			SuggestedActorID: suggestedActorID,
			LocalMaxDepth:    localMaxDepth,
		})
	}

	// Build spawn request
	req := types.SpawnRequest{
		EpicID:              epicID,
		ParentPlanNodeID:    parentPlanNodeID,
		SpawnReason:         spawnReason,
		RequestedSubagents:  requestedSubagents,
		WaitForCompletion:   waitForCompletion,
		CallerActorID:       cfg.CallerActorID,
		CallerDepth:         cfg.CallerDepth,
		CallerMaxDepth:      cfg.CallerMaxDepth,
		CallerLocalMaxDepth: cfg.CallerLocalMaxDepth,
	}

	// If no mail sender configured, return the request as pending
	if cfg.MailSender == nil {
		// Return structured response indicating mail would be sent
		result := map[string]any{
			"status":  "pending",
			"message": "spawn.request prepared but no mail sender configured",
			"request": req,
			"hint":    "In production, this sends mail to actor:system:overseer for approval",
		}
		return successResult(result)
	}

	// Send mail to overseer
	subject := fmt.Sprintf("spawn.request:%s:%s", epicID, parentPlanNodeID)
	response, err := cfg.MailSender(ctx, "actor:system:overseer", subject, req)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to send spawn request: %v", err))
	}

	// Parse response
	var spawnResp types.SpawnResponse
	respBytes, _ := json.Marshal(response)
	if err := json.Unmarshal(respBytes, &spawnResp); err != nil {
		// Return raw response if not parseable
		return successResult(map[string]any{
			"status":   "sent",
			"response": response,
		})
	}

	// Return structured result
	result := map[string]any{
		"accepted":       spawnResp.Accepted,
		"spawned_agents": spawnResp.SpawnedAgents,
		"denied_agents":  spawnResp.DeniedAgents,
	}
	if spawnResp.Reason != "" {
		result["reason"] = spawnResp.Reason
	}
	if spawnResp.Suggestion != "" {
		result["suggestion"] = spawnResp.Suggestion
	}

	return successResult(result)
}

// ValidateSpawnDepth checks if a spawn is allowed given hierarchy constraints.
// Returns nil if allowed, error describing why not if denied.
func ValidateSpawnDepth(parentDepth, parentMaxDepth, parentLocalMaxDepth int) error {
	if parentDepth >= parentMaxDepth {
		return fmt.Errorf("%s: parent depth %d >= max depth %d",
			types.DenialDepthLimitExceeded, parentDepth, parentMaxDepth)
	}
	if parentDepth >= parentLocalMaxDepth {
		return fmt.Errorf("%s: parent depth %d >= local max depth %d",
			types.DenialLocalLimitExceeded, parentDepth, parentLocalMaxDepth)
	}
	return nil
}

// ComputeChildDepthLimits computes the depth limits for a child agent.
func ComputeChildDepthLimits(parentDepth, parentMaxDepth, parentLocalMaxDepth, requestedLocalMaxDepth int) (childDepth, childMaxDepth, childLocalMaxDepth int) {
	childDepth = parentDepth + 1
	childMaxDepth = parentMaxDepth // Always inherited from root

	// Clamp requested local max to parent's limit
	childLocalMaxDepth = parentLocalMaxDepth
	if requestedLocalMaxDepth > 0 && requestedLocalMaxDepth < parentLocalMaxDepth {
		childLocalMaxDepth = requestedLocalMaxDepth
	}

	return childDepth, childMaxDepth, childLocalMaxDepth
}
