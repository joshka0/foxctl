package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errspkg "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/oklog/ulid/v2"
)

// registerAgentTools registers agent lifecycle tools.
func (r *Registry) registerAgentTools() error {
	agentSpawnTool := dstools.NewFuncTool(
		"agent.spawn",
		"Spawn a child agent to perform a specific role.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"role": {
					Type:        "string",
					Description: "The role of the child agent (e.g., 'coder', 'reviewer')",
					Required:    true,
				},
				"prompt": {
					Type:        "string",
					Description: "The system prompt/instructions for the child agent",
					Required:    true,
				},
				"skills_allow": {
					Type:        "array",
					Description: "List of allowed skills/tools for the child agent",
				},
			},
		},
		r.wrapWithTelemetry("agent.spawn", r.agentSpawn),
	)
	if err := r.tools.Register(agentSpawnTool); err != nil {
		return fmt.Errorf("register agent.spawn: %w", err)
	}
	return nil
}

// agentSpawn spawns a child agent via the overseer.
func (r *Registry) agentSpawn(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	role, _ := args["role"].(string)
	prompt, _ := args["prompt"].(string)

	var skillsAllow []string
	if v, ok := args["skills_allow"].([]any); ok {
		for _, s := range v {
			if sStr, ok := s.(string); ok {
				skillsAllow = append(skillsAllow, sStr)
			}
		}
	} else if v, ok := args["skills_allow"].([]string); ok {
		skillsAllow = v
	}

	if role == "" {
		return nil, fmt.Errorf("role is required")
	}
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	if r.openMailboxStore == nil {
		return nil, fmt.Errorf("mailbox store not configured")
	}
	mailboxStore, err := r.openMailboxStore(ctx)
	if err != nil {
		return nil, fmt.Errorf("open mailbox store: %w", err)
	}
	defer func() { errspkg.Ignore(mailboxStore.Close(), "close mailbox store") }()

	cmdID := ulid.Make().String()

	type ChildAgentConfig struct {
		Role        string   `json:"role"`
		Prompt      string   `json:"prompt"`
		SkillsAllow []string `json:"skills_allow"`
		ParentNS    string   `json:"parent_ns"`
	}
	type SpawnRequestPayload struct {
		CmdID       string           `json:"cmd_id"`
		Action      string           `json:"action"`
		ChildConfig ChildAgentConfig `json:"child_config"`
	}

	spawnReq := SpawnRequestPayload{
		CmdID:  cmdID,
		Action: "spawn",
		ChildConfig: ChildAgentConfig{
			Role:        role,
			Prompt:      prompt,
			SkillsAllow: skillsAllow,
			ParentNS:    r.config.ActorID,
		},
	}

	payload, err := json.Marshal(envelope.OK("agent.cmd", spawnReq))
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	msg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    r.config.ActorID,
		ToNS:      "actor:system:overseer",
		Type:      agent.MessageTypeCmd,
		Headers:   map[string]string{"correlation": cmdID},
		Payload:   payload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
	}

	if err := mailboxStore.Send(ctx, msg); err != nil {
		return nil, fmt.Errorf("send spawn request: %w", err)
	}

	return successResult(map[string]any{
		"spawn_request_id": cmdID,
		"sent_to":          "overseer",
		"note":             "Spawn request sent. Reply will arrive in mailbox.",
	}), nil
}
