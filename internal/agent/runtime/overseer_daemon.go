package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/execution/agentmanager"
	errspkg "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/oklog/ulid/v2"
)

// OverseerDaemonOptions configures the overseer daemon.
type OverseerDaemonOptions struct {
	StorageRoot  string
	PollInterval time.Duration
	DryRun       bool // If true, validate decisions without executing them
}

// RunOverseer runs the overseer daemon loop.
func RunOverseer(ctx context.Context, opts OverseerDaemonOptions) error {
	mailboxStore, err := mailbox.Open(ctx, opts.StorageRoot)
	if err != nil {
		return fmt.Errorf("open mailbox store: %w", err)
	}
	defer func() { errspkg.Ignore(mailboxStore.Close(), "close mailbox store") }()

	agentStore, err := agents.Open(ctx, opts.StorageRoot)
	if err != nil {
		return fmt.Errorf("open agent store: %w", err)
	}
	defer func() { errspkg.Ignore(agentStore.Close(), "close agent store") }()

	agentManager := agentmanager.New(agentStore, mailboxStore)

	// Poll loop
	ticker := time.NewTicker(opts.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Poll for spawn requests to overseer namespace
			messages, err := mailboxStore.Poll(ctx, "actor:system:overseer", 0, 10)
			if err != nil {
				log.Error().Err(err).Msg("overseer poll failed")
				continue
			}
			for _, msg := range messages {
				if msg.Type == agent.MessageTypeCmd {
					handleOverseerCmd(ctx, msg, agentManager, mailboxStore, agentStore)
				}
				// Always ack message after processing attempts
				errspkg.Ignore(mailboxStore.Ack(ctx, msg.ID), "ack overseer message")
			}
		}
	}
}

func handleOverseerCmd(ctx context.Context, msg agent.Message, mgr *agentmanager.Manager, store mailbox.Store, agentStore agents.Store) {
	var reqEnv envelope.Envelope
	if err := json.Unmarshal(msg.Payload, &reqEnv); err != nil {
		// Malformed payload
		return
	}
	if err := envelope.Validate(reqEnv); err != nil {
		return
	}
	if reqEnv.Status != envelope.StatusOK {
		return
	}
	if reqEnv.Command != "agent.spawn" {
		return
	}

	var req types.SpawnRequest
	reqBytes, err := json.Marshal(reqEnv.Data)
	if err != nil {
		return
	}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		return
	}
	if strings.TrimSpace(msg.FromNS) == "" {
		return
	}
	if strings.TrimSpace(req.SpawnReason) == "" {
		resp := types.SpawnResponse{
			Accepted:   false,
			Reason:     "invalid_request: spawn_reason is required",
			Suggestion: "Provide spawn_reason and requested_subagents before retrying.",
		}
		sendSpawnResponse(ctx, store, msg, resp)
		return
	}
	if len(req.RequestedSubagents) == 0 {
		resp := types.SpawnResponse{
			Accepted:   false,
			Reason:     "invalid_request: requested_subagents is required",
			Suggestion: "Provide at least one requested_subagents entry.",
		}
		sendSpawnResponse(ctx, store, msg, resp)
		return
	}

	var parentAgent agent.Agent
	parent, err := agentStore.GetByNamespace(ctx, msg.FromNS)
	if err == nil {
		parentAgent = parent
	}

	resp := types.SpawnResponse{
		SpawnedAgents: []types.SpawnedAgent{},
		DeniedAgents:  []types.DeniedAgent{},
	}

	for _, sub := range req.RequestedSubagents {
		if strings.TrimSpace(string(sub.Role)) == "" {
			resp.DeniedAgents = append(resp.DeniedAgents, types.DeniedAgent{
				Role:   sub.Role,
				Task:   sub.Task,
				Reason: "invalid role",
			})
			continue
		}
		prompt := buildSpawnPrompt(req, sub)
		spawnResp, err := mgr.Spawn(ctx, agentmanager.SpawnRequest{
			ParentNS:    msg.FromNS,
			Role:        string(sub.Role),
			Prompt:      prompt,
			SkillsAllow: parentAgent.SkillsAllow,
			Policy:      parentAgent.Policy,
			ShareBB:     "scoped",
			LLMProvider: parentAgent.LLMProvider,
			LLMModel:    parentAgent.LLMModel,
			LLMAPIKey:   parentAgent.LLMAPIKey,
		})
		if err != nil {
			resp.DeniedAgents = append(resp.DeniedAgents, types.DeniedAgent{
				Role:   sub.Role,
				Task:   sub.Task,
				Reason: err.Error(),
			})
			continue
		}

		resp.SpawnedAgents = append(resp.SpawnedAgents, types.SpawnedAgent{
			ActorID:   spawnResp.NS,
			SessionID: spawnResp.AgentID,
			Depth:     strings.Count(spawnResp.NS, "/child-"),
		})
	}

	resp.Accepted = len(resp.SpawnedAgents) > 0
	if len(resp.DeniedAgents) > 0 && len(resp.SpawnedAgents) == 0 {
		resp.Reason = "all requested agents were denied"
	} else if len(resp.DeniedAgents) > 0 {
		resp.Reason = "some agents were denied"
	}

	sendSpawnResponse(ctx, store, msg, resp)
}

func buildSpawnPrompt(req types.SpawnRequest, sub types.SubagentRequest) string {
	prompt := strings.TrimSpace(sub.Task)
	if prompt == "" {
		prompt = "Task not specified."
	}
	if strings.TrimSpace(req.SpawnReason) == "" {
		return prompt
	}
	return fmt.Sprintf("%s\n\nSpawn reason: %s", prompt, strings.TrimSpace(req.SpawnReason))
}

func sendSpawnResponse(ctx context.Context, store mailbox.Store, msg agent.Message, resp types.SpawnResponse) {
	replyEnv := envelope.OK("agent.spawn", resp)
	replyPayload, err := json.Marshal(replyEnv)
	if err != nil {
		return
	}

	headers := map[string]string{}
	if corr := strings.TrimSpace(msg.Headers["correlation"]); corr != "" {
		headers["correlation"] = corr
	}

	if err := store.Send(ctx, agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    "actor:system:overseer",
		ToNS:      msg.FromNS,
		Type:      agent.MessageTypeReply,
		Headers:   headers,
		Payload:   replyPayload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
	}); err != nil {
		log.Error().Err(err).Str("to_ns", msg.FromNS).Msg("overseer failed to send spawn response")
	}
}
