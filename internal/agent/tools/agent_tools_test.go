package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/stretchr/testify/require"
)

func TestAgentSpawnTool(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	cfg := Config{
		WorkspaceRoot: tmpDir,
		WorkspaceID:   "workspace-1",
		ActorID:       "actor:agent:tester",
		Depth:         1,
		MaxDepth:      3,
		LocalMaxDepth: 3,
		OpenMailboxStore: func(ctx context.Context) (mailbox.Store, error) {
			return mailbox.Open(ctx, tmpDir)
		},
	}

	registry, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	// Call agent.spawn
	var tool Tool
	for _, t := range registry.List() {
		if t.Name() == "agent.spawn" {
			tool = t
			break
		}
	}
	require.NotNil(t, tool)

	args := map[string]any{
		"epic_id":      "epic-1",
		"spawn_reason": "Need parallel coding",
		"requested_subagents": []any{
			map[string]any{
				"role": "coder",
				"task": "Implement helper logic",
			},
		},
	}
	result, err := tool.Call(ctx, args)
	require.NoError(t, err)
	require.False(t, result.IsError)

	// Verify message in mailbox
	store, err := mailbox.Open(ctx, tmpDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	msgs, err := store.List(ctx, "actor:system:overseer", 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	msg := msgs[0]
	require.Equal(t, agent.MessageTypeCmd, msg.Type)
	require.Equal(t, "actor:agent:tester", msg.FromNS)

	// Verify payload
	var env envelope.Envelope
	require.NoError(t, json.Unmarshal(msg.Payload, &env))
	require.NoError(t, envelope.Validate(env))
	require.Equal(t, "agent.spawn", env.Command)

	dataBytes, err := json.Marshal(env.Data)
	require.NoError(t, err)
	var payload struct {
		EpicID             string `json:"epic_id"`
		SpawnReason        string `json:"spawn_reason"`
		RequestedSubagents []struct {
			Role string `json:"role"`
			Task string `json:"task"`
		} `json:"requested_subagents"`
	}
	require.NoError(t, json.Unmarshal(dataBytes, &payload))
	require.Equal(t, "epic-1", payload.EpicID)
	require.Equal(t, "Need parallel coding", payload.SpawnReason)
	require.Len(t, payload.RequestedSubagents, 1)
	require.Equal(t, "coder", payload.RequestedSubagents[0].Role)
	require.Equal(t, "Implement helper logic", payload.RequestedSubagents[0].Task)
}
