package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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

	// Wait for startup and open mailbox with retry (SQLite WAL can cause brief locks)
	var mailboxStore mailbox.Store
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		mailboxStore, err = mailbox.Open(ctx, tmpDir)
		if err == nil {
			break
		}
		t.Logf("retry %d: mailbox open: %v", i+1, err)
	}
	require.NoError(t, err, "failed to open mailbox after retries")
	defer func() {
		if err := mailboxStore.Close(); err != nil {
			t.Errorf("close mailbox store: %v", err)
		}
	}()

	// Send spawn request
	cmdID := ulid.Make().String()
	spawnReq := SpawnRequestPayload{
		CmdID:  cmdID,
		Action: "spawn",
		ChildConfig: ChildAgentConfig{
			Role:        "coder",
			Prompt:      "You are a coder.",
			SkillsAllow: []string{"fs.read_file"},
			ParentNS:    "actor:agent:parent",
		},
	}

	// Create proper envelope payload
	env := envelope.OK("agent.cmd", spawnReq)
	payload, err := json.Marshal(env)
	require.NoError(t, err)

	msg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    "actor:agent:parent",
		ToNS:      "actor:system:overseer",
		Type:      agent.MessageTypeCmd,
		Headers:   map[string]string{},
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

	// Unmarshal data into SpawnResponsePayload
	respBytes, err := json.Marshal(replyEnv.Data)
	require.NoError(t, err)

	var spawnResp SpawnResponsePayload
	err = json.Unmarshal(respBytes, &spawnResp)
	require.NoError(t, err)

	if !spawnResp.Success {
		t.Logf("Spawn error: %s", spawnResp.Error)
	}
	require.True(t, spawnResp.Success)
	require.NotEmpty(t, spawnResp.ChildID)
	require.NotEmpty(t, spawnResp.ChildNS)

	cancel()
	err = <-errCh
	// RunOverseer returns context.Canceled on clean shutdown
	require.ErrorIs(t, err, context.Canceled)
}
