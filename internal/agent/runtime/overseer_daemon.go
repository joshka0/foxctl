package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

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

// SpawnRequestPayload matches the JSON payload in the envelope data for a spawn request.
type SpawnRequestPayload struct {
	CmdID       string           `json:"cmd_id"`
	Action      string           `json:"action"` // "spawn"
	ChildConfig ChildAgentConfig `json:"child_config"`
}

// ChildAgentConfig defines the configuration for a child agent.
type ChildAgentConfig struct {
	Role        string   `json:"role"`         // e.g., "coder", "reviewer"
	Prompt      string   `json:"prompt"`       // system prompt
	SkillsAllow []string `json:"skills_allow"` // tool allowlist
	ParentNS    string   `json:"parent_ns"`    // for reply routing
}

// SpawnResponsePayload matches the JSON payload in the envelope data for a spawn response.
type SpawnResponsePayload struct {
	ChildID string `json:"child_id"`
	ChildNS string `json:"child_ns"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
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
					handleOverseerCmd(ctx, msg, agentManager, mailboxStore)
				}
				// Always ack message after processing attempts
				errspkg.Ignore(mailboxStore.Ack(ctx, msg.ID), "ack overseer message")
			}
		}
	}
}

func handleOverseerCmd(ctx context.Context, msg agent.Message, mgr *agentmanager.Manager, store mailbox.Store) {
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
	if reqEnv.Command != "agent.cmd" {
		return
	}

	var cmd SpawnRequestPayload
	cmdBytes, err := json.Marshal(reqEnv.Data)
	if err != nil {
		return
	}
	if err := json.Unmarshal(cmdBytes, &cmd); err != nil {
		return
	}
	if strings.TrimSpace(msg.FromNS) == "" {
		return
	}
	if strings.TrimSpace(cmd.CmdID) == "" {
		return
	}
	if cmd.Action != "spawn" {
		return // ignore unknown actions
	}
	if strings.TrimSpace(cmd.ChildConfig.Role) == "" {
		return
	}
	if strings.TrimSpace(cmd.ChildConfig.Prompt) == "" {
		return
	}

	// Spawn child agent
	spawnResp, err := mgr.Spawn(ctx, agentmanager.SpawnRequest{
		Role:        cmd.ChildConfig.Role,
		Prompt:      cmd.ChildConfig.Prompt,
		SkillsAllow: cmd.ChildConfig.SkillsAllow,
		ParentNS:    msg.FromNS,
		ShareBB:     "scoped", // Default to scoped for now
	})

	// Send response to parent
	response := SpawnResponsePayload{
		ChildID: spawnResp.AgentID,
		ChildNS: spawnResp.NS,
		Success: err == nil,
	}
	if err != nil {
		response.Error = err.Error()
	}

	replyEnv := envelope.OK("agent.reply", response)
	replyPayload, err := json.Marshal(replyEnv)
	if err != nil {
		return
	}

	if err := store.Send(ctx, agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    "actor:system:overseer",
		ToNS:      msg.FromNS,
		Type:      agent.MessageTypeReply,
		Headers:   map[string]string{"correlation": cmd.CmdID},
		Payload:   replyPayload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
	}); err != nil {
		log.Error().Err(err).Str("cmd_id", cmd.CmdID).Str("to_ns", msg.FromNS).Msg("overseer failed to send spawn response")
	}
}
