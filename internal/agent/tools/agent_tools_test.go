package tools

import (
	"context"
	"encoding/json"
	"testing"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
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
		OpenMailboxStore: func(ctx context.Context) (mailbox.Store, error) {
			return mailbox.Open(ctx, tmpDir)
		},
	}

	registry, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	// Call agent.spawn
	var tool dstools.Tool
	for _, t := range registry.List() {
		if t.Name() == "agent.spawn" {
			tool = t
			break
		}
	}
	require.NotNil(t, tool)

	args := map[string]any{
		"role":         "coder",
		"prompt":       "You are a coder",
		"skills_allow": []string{"fs.read_file"},
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
	require.Equal(t, "agent.cmd", env.Command)

	dataBytes, err := json.Marshal(env.Data)
	require.NoError(t, err)
	var payload struct {
		Action      string `json:"action"`
		ChildConfig struct {
			Role string `json:"role"`
		} `json:"child_config"`
	}
	require.NoError(t, json.Unmarshal(dataBytes, &payload))
	require.Equal(t, "spawn", payload.Action)
	require.Equal(t, "coder", payload.ChildConfig.Role)
}
