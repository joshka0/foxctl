package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestRunOverseer_Spawn(t *testing.T) {
	// Setup stores
	tmpDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create parent agent
	agentStore, err := agents.Open(ctx, tmpDir)
	require.NoError(t, err)
	parentAgent := agent.Agent{
		ID:          "parent",
		Namespace:   "actor:agent:parent",
		Role:        "manager",
		State:       agent.StateRunning,
		SkillsAllow: []string{"fs.read_file"},
		ShareBB:     "scoped",
	}
	require.NoError(t, agentStore.Create(ctx, parentAgent))
	require.NoError(t, agentStore.Close())

	// Initialize mailbox store before starting the daemon so migrations complete
	// on a single connection and race tests do not contend on first open.
	mailboxStore, err := mailbox.Open(ctx, tmpDir)
	require.NoError(t, err)
	defer func() {
		if err := mailboxStore.Close(); err != nil {
			t.Errorf("close mailbox store: %v", err)
		}
	}()

	// Create overseer options
	opts := OverseerDaemonOptions{
		StorageRoot:  tmpDir,
		PollInterval: 10 * time.Millisecond,
	}

	// Start overseer in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunOverseer(ctx, opts)
	}()

	// Give the overseer loop time to start polling before sending the command.
	time.Sleep(100 * time.Millisecond)

	// Send spawn request
	cmdID := ulid.Make().String()
	spawnReq := types.SpawnRequest{
		EpicID:      "epic-1",
		SpawnReason: "Need parallel coding",
		RequestedSubagents: []types.SubagentRequest{
			{
				Role: types.RoleCoder,
				Task: "Implement helper logic",
			},
		},
		CallerActorID:       "actor:agent:parent",
		CallerDepth:         1,
		CallerMaxDepth:      3,
		CallerLocalMaxDepth: 3,
	}

	// Create proper envelope payload
	env := envelope.OK("agent.spawn", spawnReq)
	payload, err := json.Marshal(env)
	require.NoError(t, err)

	msg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    "actor:agent:parent",
		ToNS:      "actor:system:overseer",
		Type:      agent.MessageTypeCmd,
		Headers:   map[string]string{"correlation": cmdID},
		Payload:   payload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
	}
	require.NoError(t, mailboxStore.Send(ctx, msg))

	// Poll for reply
	deadline := time.Now().Add(5 * time.Second)
	var reply *agent.Message
	for time.Now().Before(deadline) {
		msgs, err := mailboxStore.List(ctx, "actor:agent:parent", 10)
		require.NoError(t, err)
		for _, m := range msgs {
			if m.Type == agent.MessageTypeReply && m.Headers["correlation"] == cmdID {
				reply = &m
				break
			}
		}
		if reply != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	require.NotNil(t, reply, "timed out waiting for reply")

	// Verify reply content
	var replyEnv envelope.Envelope
	err = json.Unmarshal(reply.Payload, &replyEnv)
	require.NoError(t, err)
	require.NoError(t, envelope.Validate(replyEnv))

	// Unmarshal data into SpawnResponse
	respBytes, err := json.Marshal(replyEnv.Data)
	require.NoError(t, err)

	var spawnResp types.SpawnResponse
	err = json.Unmarshal(respBytes, &spawnResp)
	require.NoError(t, err)

	if !spawnResp.Accepted {
		t.Logf("Spawn error: %s", spawnResp.Reason)
		for i, denied := range spawnResp.DeniedAgents {
			t.Logf("Denied[%d]: role=%s task=%s reason=%s", i, denied.Role, denied.Task, denied.Reason)
		}
	}
	require.True(t, spawnResp.Accepted)
	require.NotEmpty(t, spawnResp.SpawnedAgents)
	require.NotEmpty(t, spawnResp.SpawnedAgents[0].ActorID)
	require.NotEmpty(t, spawnResp.SpawnedAgents[0].SessionID)

	cancel()
	err = <-errCh
	// RunOverseer returns context.Canceled on clean shutdown
	require.ErrorIs(t, err, context.Canceled)
}
