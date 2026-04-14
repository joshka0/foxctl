package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/agents"
)

func TestAgentMemoryStatsCommand_ReturnsRetentionMetadata(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")

	cfg := setupOrchestrationTestEnv(t)
	ctx := context.Background()

	store, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer requireClose(t, store, "agents store")

	if err := store.Create(ctx, agent.Agent{
		ID:              "agent-memory-cli-1",
		Namespace:       cfg.Home,
		Name:            "CLI Memory Agent",
		Role:            "companion",
		SkillsAllow:     []string{},
		Policy:          agent.Policy{},
		ShareBB:         "scoped",
		State:           agent.StateStopped,
		CreatedAt:       time.Date(2026, time.March, 6, 15, 0, 0, 0, time.UTC),
		ExecMode:        agent.ModeReactive,
		MemoryScope:     agent.MemoryScopeSession,
		MemoryRetention: agent.MemoryRetentionTask,
	}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	body := runAgentSubcommand(t, cfg, newAgentMemoryStatsCommand(), "agent-memory-cli-1")
	data, _ := body["data"].(map[string]any)
	if got := data["memory_scope"]; got != "session" {
		t.Fatalf("memory_scope=%v want session", got)
	}
	if got := data["memory_retention"]; got != "task" {
		t.Fatalf("memory_retention=%v want task", got)
	}
	if _, ok := data["stats"].(map[string]any); !ok {
		t.Fatalf("stats=%T want map", data["stats"])
	}
}

func TestAgentRoomPolicyAndInfoCommands_RoundTripDispatchDefaults(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")

	cfg := setupOrchestrationTestEnv(t)
	ctx := context.Background()

	store, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer requireClose(t, store, "agents store")

	if err := store.Create(ctx, agent.Agent{
		ID:          "agent-room-cli-1",
		Namespace:   cfg.Home,
		Name:        "CLI Room Agent",
		Role:        "overseer",
		SkillsAllow: []string{},
		Policy:      agent.Policy{},
		ShareBB:     "scoped",
		State:       agent.StateStopped,
		CreatedAt:   time.Date(2026, time.March, 6, 15, 5, 0, 0, time.UTC),
		ExecMode:    agent.ModeReactive,
	}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	policyBody := runAgentSubcommand(t, cfg, newAgentRoomPolicyCommand(),
		"--workspace", cfg.Home,
		"--dispatch-policy", "lead_only",
		"--dispatch-agent", "agent-room-cli-1",
		"agent-room-cli-1",
	)
	policyData, _ := policyBody["data"].(map[string]any)
	if got := policyData["dispatch_policy"]; got != "lead_only" {
		t.Fatalf("dispatch_policy=%v want lead_only", got)
	}

	infoBody := runAgentSubcommand(t, cfg, newAgentRoomInfoCommand(),
		"--workspace", cfg.Home,
		"agent-room-cli-1",
	)
	infoData, _ := infoBody["data"].(map[string]any)
	room, _ := infoData["room"].(map[string]any)
	if got := room["dispatch_policy"]; got != "lead_only" {
		t.Fatalf("room.dispatch_policy=%v want lead_only", got)
	}
	targets, _ := room["dispatch_agent_ids"].([]any)
	if len(targets) != 1 {
		t.Fatalf("dispatch_agent_ids=%v want 1 target", room["dispatch_agent_ids"])
	}
}

func runAgentSubcommand(t *testing.T, cfg config.Config, cmd *cobra.Command, args ...string) map[string]any {
	t.Helper()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("agent subcommand failed: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("remarshal envelope: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode envelope as map: %v", err)
	}
	return body
}
