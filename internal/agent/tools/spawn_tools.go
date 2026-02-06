package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tooling "github.com/jkatigb/agentctl/internal/tooling"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/oklog/ulid/v2"

	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errspkg "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/maputil"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
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
	tool := tooling.NewFuncTool(
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
		subMap, ok := maputil.AsStringMap(raw)
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
	// Marshal error is nil for map[string]any from valid response.
	respBytes, _ := json.Marshal(response) //nolint:errcheck
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

const (
	defaultSpawnResponseWait = 30 * time.Second
	spawnResponsePollEvery   = 200 * time.Millisecond
)

func (r *Registry) spawnMailSender(ctx context.Context, to, subject string, body any) (any, error) {
	req, ok := body.(types.SpawnRequest)
	if !ok {
		var decoded types.SpawnRequest
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal spawn request: %w", err)
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return nil, fmt.Errorf("decode spawn request: %w", err)
		}
		req = decoded
	}

	return r.sendSpawnRequest(ctx, to, subject, req)
}

func (r *Registry) sendSpawnRequest(ctx context.Context, to, subject string, req types.SpawnRequest) (any, error) {
	if r.openMailboxStore == nil {
		return nil, fmt.Errorf("mailbox store not configured")
	}

	store, err := r.openMailboxStore(ctx)
	if err != nil {
		return nil, fmt.Errorf("open mailbox store: %w", err)
	}
	defer func() { errspkg.Ignore(store.Close(), "close mailbox store") }()

	requestID := ulid.Make().String()
	env := envelope.OK("agent.spawn", req)
	payload, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal spawn envelope: %w", err)
	}

	msg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    r.config.ActorID,
		ToNS:      to,
		Type:      agent.MessageTypeCmd,
		Headers:   map[string]string{"correlation": requestID, "subject": subject},
		Payload:   payload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
		SessionID: r.config.SessionID,
		Workspace: r.config.WorkspaceID,
		AgentID:   r.config.ActorID,
	}

	if err := store.Send(ctx, msg); err != nil {
		return nil, fmt.Errorf("send spawn request: %w", err)
	}

	if !req.WaitForCompletion {
		return map[string]any{
			"status":     "sent",
			"request_id": requestID,
			"note":       "Spawn request sent. Reply will arrive in mailbox.",
		}, nil
	}

	response, err := r.waitForSpawnResponse(ctx, store, requestID)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (r *Registry) waitForSpawnResponse(ctx context.Context, store mailbox.Store, requestID string) (any, error) {
	deadline := time.Now().Add(defaultSpawnResponseWait)
	ticker := time.NewTicker(spawnResponsePollEvery)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("spawn response timeout")
		}

		messages, err := store.List(ctx, r.config.ActorID, 50)
		if err != nil {
			return nil, fmt.Errorf("list mailbox replies: %w", err)
		}
		for _, msg := range messages {
			if msg.Type != agent.MessageTypeReply {
				continue
			}
			if msg.Headers["correlation"] != requestID {
				continue
			}

			if err := store.Ack(ctx, msg.ID); err != nil {
				return nil, fmt.Errorf("ack spawn response: %w", err)
			}
			return parseSpawnResponse(msg.Payload)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func parseSpawnResponse(payload []byte) (any, error) {
	var env envelope.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, fmt.Errorf("decode spawn response: %w", err)
	}
	if err := envelope.Validate(env); err != nil {
		return nil, fmt.Errorf("invalid spawn response: %w", err)
	}
	if env.Status != envelope.StatusOK {
		return nil, fmt.Errorf("spawn response status: %s", env.Status)
	}
	return env.Data, nil
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
