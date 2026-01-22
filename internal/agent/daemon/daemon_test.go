package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/agents"
	"github.com/XiaoConstantine/dspy-go/pkg/agents/react"
	"github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jkatigb/agentctl/internal/agent/tools"
	"github.com/jkatigb/agentctl/internal/companion"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/execution/agentmanager"
	"github.com/jkatigb/agentctl/internal/platform/maputil"
	storagents "github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
)

func TestBackoffDuration(t *testing.T) {
	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 5 * time.Second},
		{1, 10 * time.Second},
		{2, 20 * time.Second},
		{3, 40 * time.Second},
		{4, 80 * time.Second},
		{5, 160 * time.Second},
		{6, 160 * time.Second}, // Capped
	}

	for _, tt := range tests {
		got := backoffDuration(tt.attempt)
		assert.Equal(t, tt.expected, got, "attempt %d", tt.attempt)
	}
}

func TestMemoryDedupeStore(t *testing.T) {
	s := NewMemoryDedupeStore()
	ctx := context.Background()
	agentID := "agent-1"
	msgID := "msg-1"

	processed, err := s.IsProcessed(ctx, agentID, msgID)
	require.NoError(t, err)
	assert.False(t, processed)

	err = s.MarkProcessed(ctx, agentID, msgID)
	require.NoError(t, err)

	processed, err = s.IsProcessed(ctx, agentID, msgID)
	require.NoError(t, err)
	assert.True(t, processed)

	// Different agent
	processed, err = s.IsProcessed(ctx, "agent-2", msgID)
	require.NoError(t, err)
	assert.False(t, processed)
}

func setupDaemon(t *testing.T) (context.Context, string, Options, *FakeLLM) {
	root := t.TempDir()
	ctx := context.Background()

	// Init stores
	as, err := storagents.Open(ctx, root)
	require.NoError(t, err)
	defer func() {
		if err := as.Close(); err != nil {
			t.Errorf("failed to close agent store: %v", err)
		}
	}()

	// Create agent - use autonomous mode to ensure DSPy path is used (where AgentFactory applies)
	agentID := ulid.Make().String()
	a := agent.Agent{
		ID:        agentID,
		Namespace: "actor:agent:" + agentID,
		Role:      "coder",
		State:     agent.StateStarting, // Run will transition to Running
		Prompt:    "You are a coder.",
		ShareBB:   "scoped",
		ExecMode:  agent.ModeAutonomous, // Use DSPy path so FakeLLM is used
	}
	err = as.Create(ctx, a)
	require.NoError(t, err)

	fakeLLM := NewFakeLLM()

	opts := Options{
		AgentID:           agentID,
		StorageRoot:       root,
		PollInterval:      10 * time.Millisecond,
		HeartbeatInterval: 100 * time.Millisecond,
		MaxPollMessages:   10,
		UseMemoryDedupe:   true,
		AgentFactory: func(ctx context.Context, a agent.Agent, r *tools.Registry) (agents.Agent, error) {
			dspyAgent := react.NewReActAgent(a.ID, a.Role)
			sig := core.NewSignature(
				[]core.InputField{
					{Field: core.NewField("task", core.WithDescription("task"))},
				},
				[]core.OutputField{
					{Field: core.NewField("result", core.WithDescription("result"))},
				},
			)
			if err := dspyAgent.Initialize(fakeLLM, sig); err != nil {
				return nil, err
			}
			return dspyAgent, nil
		},
	}

	return ctx, agentID, opts, fakeLLM
}

func TestDaemon_TTLExpiry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon test in short mode (race detector uses too much memory)")
	}
	ctx, agentID, opts, fakeLLM := setupDaemon(t)
	fakeLLM.Responses = []string{"Response"}

	// Send expired message
	ms, err := mailbox.Open(ctx, opts.StorageRoot)
	require.NoError(t, err)
	defer func() { require.NoError(t, ms.Close()) }()

	msg := agent.Message{
		ID:        ulid.Make().String(),
		ToNS:      "actor:agent:" + agentID,
		Type:      agent.MessageTypeAsk,
		TTLMS:     1,                                       // 1ms TTL
		Timestamp: time.Now().Add(-1 * time.Second).Unix(), // already expired
		Payload:   []byte(`{"data":{"question":"hi"}}`),
	}
	err = ms.Send(ctx, msg)
	require.NoError(t, err)

	// Run daemon
	daemonCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(daemonCtx, opts)
	}()

	// Wait for daemon to start processing - allow extra time in CI environments
	time.Sleep(100 * time.Millisecond)

	deadline := time.Now().Add(5 * time.Second)
	for {
		msgs, err := ms.List(context.Background(), "actor:agent:"+agentID, 10)
		require.NoError(t, err)
		if len(msgs) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for expired message to be acked; still have %d message(s)", len(msgs))
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	err = <-errCh
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("daemon run error: %v", err)
	}

	// Verify LLM not called
	assert.Equal(t, 0, fakeLLM.GetCallIndex())

	// Verify message acked (removed from mailbox)
	msgs, err := ms.List(context.Background(), "actor:agent:"+agentID, 10)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestDaemon_Dedupe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon test in short mode (race detector uses too much memory)")
	}
	ctx, agentID, opts, fakeLLM := setupDaemon(t)
	fakeLLM.Responses = []string{"Response"}

	ms, err := mailbox.Open(ctx, opts.StorageRoot)
	require.NoError(t, err)
	defer func() { require.NoError(t, ms.Close()) }()

	msgID := ulid.Make().String()
	payload := []byte(`{"data":{"question":"hi"}}`)

	msg1 := agent.Message{
		ID:        msgID,
		ToNS:      "actor:agent:" + agentID,
		FromNS:    "caller",
		Type:      agent.MessageTypeAsk,
		Timestamp: time.Now().Unix(),
		Payload:   payload,
	}

	opts.UseMemoryDedupe = false // Use SQLite

	// Open SQLite dedupe and mark processed
	sqliteDS, err := OpenSQLiteDedupeStore(ctx, opts.StorageRoot)
	require.NoError(t, err)
	require.NoError(t, sqliteDS.MarkProcessed(ctx, agentID, msgID))
	require.NoError(t, sqliteDS.Close())

	// Send message
	err = ms.Send(ctx, msg1)
	require.NoError(t, err)

	// Run daemon
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, opts) }()

	// Wait for message to be acked (removed from mailbox) - dedupe should ack without calling LLM
	deadline := time.Now().Add(5 * time.Second)
	for {
		msgs, err := ms.List(context.Background(), "actor:agent:"+agentID, 10)
		require.NoError(t, err)
		if len(msgs) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for duplicate message to be acked")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	err = <-errCh
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("daemon run error: %v", err)
	}

	// Verify LLM not called (dedupe skipped processing)
	assert.Equal(t, 0, fakeLLM.GetCallIndex())
}

func TestDaemon_AskReplyCorrelation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon test in short mode (race detector uses too much memory)")
	}
	ctx, agentID, opts, fakeLLM := setupDaemon(t)
	fakeLLM.Responses = []string{
		"thought: I will answer.\naction: <action><tool_name>Finish</tool_name><arguments><arg key=\"result\">I am fine.</arg></arguments></action>\nanswer: I am fine.",
	}

	ms, err := mailbox.Open(ctx, opts.StorageRoot)
	require.NoError(t, err)
	defer func() { require.NoError(t, ms.Close()) }()

	askID := ulid.Make().String()
	askData := agent.AskData{
		AskID:    askID,
		Question: "How are you?",
	}
	payload, err := json.Marshal(envelope.Envelope{
		Version: 1,
		Status:  "ok",
		Command: "agent.ask",
		Data:    askData,
	})
	require.NoError(t, err)

	msg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    "caller:1",
		ToNS:      "actor:agent:" + agentID,
		Type:      agent.MessageTypeAsk,
		Timestamp: time.Now().Unix(),
		Headers:   map[string]string{"correlation": askID},
		Payload:   payload,
	}
	err = ms.Send(ctx, msg)
	require.NoError(t, err)

	// Run daemon
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, opts) }()

	// Wait for reply to appear
	var reply agent.Message
	deadline := time.Now().Add(5 * time.Second)
	for {
		replies, err := ms.List(context.Background(), "caller:1", 10)
		require.NoError(t, err)
		if len(replies) > 0 {
			reply = replies[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for reply")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	err = <-errCh
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("daemon run error: %v", err)
	}
	assert.Equal(t, agent.MessageTypeReply, reply.Type)
	assert.Equal(t, askID, reply.Headers["correlation"])
	assert.Equal(t, "actor:agent:"+agentID, reply.FromNS)

	// Verify payload
	var env envelope.Envelope
	require.NoError(t, json.Unmarshal(reply.Payload, &env))
	dataMap, ok := maputil.AsStringMap(env.Data)
	require.True(t, ok)
	assert.Equal(t, askID, dataMap["ask_id"])
	// "answer" -> map -> "response" -> "I am fine."
	answer, ok := maputil.AsStringMap(dataMap["answer"])
	require.True(t, ok)
	assert.Equal(t, "I am fine.", answer["response"])
}

func TestDaemon_EndToEnd_SpawnAskReplyStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy end-to-end test in short mode (race detector uses too much memory)")
	}
	ctx := context.Background()
	root := t.TempDir()

	agentStore, err := storagents.Open(ctx, root)
	require.NoError(t, err)
	mailStore, err := mailbox.Open(ctx, root)
	require.NoError(t, err)

	mgr := agentmanager.New(agentStore, mailStore)
	spawnResp, err := mgr.Spawn(ctx, agentmanager.SpawnRequest{
		Role:     "coder",
		Prompt:   "You are a coder.",
		ShareBB:  "scoped",
		ExecMode: agent.ModeAutonomous, // Use DSPy path so FakeLLM is used
	})
	require.NoError(t, err)

	require.NoError(t, agentStore.Close())
	require.NoError(t, mailStore.Close())

	fakeLLM := NewFakeLLM()
	fakeLLM.Responses = []string{
		"thought: I will answer.\naction: <action><tool_name>Finish</tool_name><arguments><arg key=\"result\">It works.</arg></arguments></action>\nanswer: It works.",
	}

	opts := Options{
		AgentID:           spawnResp.AgentID,
		StorageRoot:       root,
		PollInterval:      10 * time.Millisecond,
		HeartbeatInterval: 50 * time.Millisecond,
		MaxPollMessages:   10,
		UseMemoryDedupe:   true,
		AgentFactory: func(ctx context.Context, a agent.Agent, r *tools.Registry) (agents.Agent, error) {
			dspyAgent := react.NewReActAgent(a.ID, a.Role)
			sig := buildSignature(a)
			if err := dspyAgent.Initialize(fakeLLM, *sig); err != nil {
				return nil, err
			}
			for _, tool := range r.List() {
				if err := dspyAgent.RegisterTool(tool); err != nil {
					return nil, err
				}
			}
			return dspyAgent, nil
		},
	}

	daemonCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(daemonCtx, opts)
	}()

	mb, err := mailbox.Open(ctx, root)
	require.NoError(t, err)
	defer func() { require.NoError(t, mb.Close()) }()

	callerNS := "caller:" + ulid.Make().String()
	askID := ulid.Make().String()
	askData := agent.AskData{
		AskID:    askID,
		Question: "How are you?",
	}
	payload, err := json.Marshal(envelope.Envelope{
		Version: 1,
		Status:  "ok",
		Command: "agent.ask",
		Data:    askData,
	})
	require.NoError(t, err)

	msg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    callerNS,
		ToNS:      spawnResp.NS,
		Type:      agent.MessageTypeAsk,
		TTLMS:     300000,
		Headers:   map[string]string{"correlation": askID},
		Payload:   payload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
	}
	require.NoError(t, mb.Send(ctx, msg))

	deadline := time.Now().Add(2 * time.Second)
	var reply agent.Message
	for {
		msgs, err := mb.List(context.Background(), callerNS, 10)
		require.NoError(t, err)
		if len(msgs) > 0 {
			reply = msgs[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for reply")
		}
		time.Sleep(10 * time.Millisecond)
	}

	require.Equal(t, agent.MessageTypeReply, reply.Type)
	require.Equal(t, askID, reply.Headers["correlation"])
	require.Equal(t, spawnResp.NS, reply.FromNS)

	var replyEnv envelope.Envelope
	require.NoError(t, json.Unmarshal(reply.Payload, &replyEnv))
	dataMap, ok := replyEnv.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, askID, dataMap["ask_id"])
	answer, ok := dataMap["answer"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "It works.", answer["response"])

	agentStore, err = storagents.Open(ctx, root)
	require.NoError(t, err)
	mailStore, err = mailbox.Open(ctx, root)
	require.NoError(t, err)
	mgr = agentmanager.New(agentStore, mailStore)
	_, err = mgr.Kill(ctx, agentmanager.KillRequest{AgentID: spawnResp.AgentID})
	require.NoError(t, err)
	require.NoError(t, agentStore.Close())
	require.NoError(t, mailStore.Close())

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("daemon did not exit after kill")
	}

	cancel()
}

func TestDaemon_NackBackoff(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon test in short mode (race detector uses too much memory)")
	}
	ctx, agentID, opts, fakeLLM := setupDaemon(t)
	// Inject error
	fakeLLM.SetError(0, errors.New("processing failed"))

	ms, err := mailbox.Open(ctx, opts.StorageRoot)
	require.NoError(t, err)
	defer func() { require.NoError(t, ms.Close()) }()

	msg := agent.Message{
		ID:        ulid.Make().String(),
		ToNS:      "actor:agent:" + agentID,
		Type:      agent.MessageTypeAsk,
		Timestamp: time.Now().Unix(),
		Payload:   []byte(`{"data":{"question":"fail"}}`),
	}
	err = ms.Send(ctx, msg)
	require.NoError(t, err)

	// Run daemon
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, opts) }()

	// Wait for LLM to be called (message processed) or timeout
	deadline := time.Now().Add(5 * time.Second)
	for fakeLLM.GetCallIndex() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	require.Greater(t, fakeLLM.GetCallIndex(), 0, "LLM should have been called")

	cancel()
	err = <-errCh
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("daemon run error: %v", err)
	}

	// Verify message is still in mailbox with BACKGROUND context
	msgs, err := ms.List(context.Background(), "actor:agent:"+agentID, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, msg.ID, msgs[0].ID)
}

func TestDaemon_StopOnKill(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon test in short mode (race detector uses too much memory)")
	}
	ctx, agentID, opts, _ := setupDaemon(t)

	as, err := storagents.Open(ctx, opts.StorageRoot)
	require.NoError(t, err)
	defer func() { require.NoError(t, as.Close()) }()

	// Run daemon
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, opts) }()

	// Wait for daemon to start (agent state transitions to Running)
	deadline := time.Now().Add(5 * time.Second)
	for {
		a, err := as.Get(context.Background(), agentID)
		require.NoError(t, err)
		if a.State == agent.StateRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for daemon to start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Update agent state to Stopped
	err = as.UpdateState(context.Background(), agentID, agent.StateStopped)
	require.NoError(t, err)

	// Daemon should exit
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not exit after agent stop")
	}
}

func TestDaemon_Heartbeat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon test in short mode (race detector uses too much memory)")
	}
	ctx, agentID, opts, _ := setupDaemon(t)
	opts.HeartbeatInterval = 50 * time.Millisecond

	as, err := storagents.Open(ctx, opts.StorageRoot)
	require.NoError(t, err)
	defer func() { require.NoError(t, as.Close()) }()

	// Get initial heartbeat
	a, err := as.Get(ctx, agentID)
	require.NoError(t, err)
	initialHeartbeat := a.HeartbeatAt

	// Run daemon
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, opts) }()

	// Wait for heartbeat to be updated (with timeout)
	deadline := time.Now().Add(5 * time.Second)
	for {
		a, err = as.Get(context.Background(), agentID)
		require.NoError(t, err)
		if a.HeartbeatAt.After(initialHeartbeat) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for heartbeat update: initial %v, current %v", initialHeartbeat, a.HeartbeatAt)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	err = <-errCh
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("daemon run error: %v", err)
	}

	// Verify heartbeat updated (already confirmed above, but assert for clarity)
	assert.True(t, a.HeartbeatAt.After(initialHeartbeat), "heartbeat should update: initial %v, current %v", initialHeartbeat, a.HeartbeatAt)
}

func TestDaemon_CompanionMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon test in short mode")
	}
	// This test requires companion.Service which needs a real LLM API key
	if os.Getenv("AGENTCTL_LLM_API_KEY") == "" && os.Getenv("CEREBRAS_API_KEY") == "" {
		t.Skip("skipping companion memory test: requires LLM API key (set AGENTCTL_LLM_API_KEY or CEREBRAS_API_KEY)")
	}
	root := t.TempDir()
	ctx := context.Background()

	// Init agent store and create companion agent
	as, err := storagents.Open(ctx, root)
	require.NoError(t, err)

	agentID := ulid.Make().String()
	a := agent.Agent{
		ID:        agentID,
		Namespace: "actor:agent:" + agentID,
		Role:      "companion", // Companion role triggers memory
		State:     agent.StateStarting,
		Prompt:    "You are a friendly companion.",
		ShareBB:   "scoped",
		// Uses reactive mode (default) to go through companion.Service path
	}
	err = as.Create(ctx, a)
	require.NoError(t, err)
	require.NoError(t, as.Close())

	// Setup fake LLM with responses
	fakeLLM := NewFakeLLM()
	fakeLLM.Responses = []string{
		"thought: I will greet the user.\naction: <action><tool_name>Finish</tool_name><arguments><arg key=\"result\">Hello! Nice to meet you, John!</arg></arguments></action>\nanswer: Hello! Nice to meet you, John!",
	}

	opts := Options{
		AgentID:               agentID,
		StorageRoot:           root,
		PollInterval:          10 * time.Millisecond,
		HeartbeatInterval:     100 * time.Millisecond,
		MaxPollMessages:       10,
		UseMemoryDedupe:       true,
		EnableCompanionMemory: true, // Enable companion memory
		AgentFactory: func(ctx context.Context, a agent.Agent, r *tools.Registry) (agents.Agent, error) {
			dspyAgent := react.NewReActAgent(a.ID, a.Role)
			sig := core.NewSignature(
				[]core.InputField{
					{Field: core.NewField("task", core.WithDescription("task"))},
				},
				[]core.OutputField{
					{Field: core.NewField("result", core.WithDescription("result"))},
				},
			)
			if err := dspyAgent.Initialize(fakeLLM, sig); err != nil {
				return nil, err
			}
			return dspyAgent, nil
		},
	}

	// Open mailbox and send ask message
	ms, err := mailbox.Open(ctx, root)
	require.NoError(t, err)
	defer func() { require.NoError(t, ms.Close()) }()

	callerNS := "caller:test"
	askID := ulid.Make().String()
	askData := agent.AskData{
		AskID:    askID,
		Question: "Hi, my name is John!",
	}
	payload, err := json.Marshal(envelope.Envelope{
		Version: 1,
		Status:  "ok",
		Command: "agent.ask",
		Data:    askData,
	})
	require.NoError(t, err)

	msg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    callerNS,
		ToNS:      "actor:agent:" + agentID,
		Type:      agent.MessageTypeAsk,
		Timestamp: time.Now().Unix(),
		Headers:   map[string]string{"correlation": askID},
		Payload:   payload,
	}
	err = ms.Send(ctx, msg)
	require.NoError(t, err)

	// Run daemon
	daemonCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- Run(daemonCtx, opts) }()

	// Wait for reply
	deadline := time.Now().Add(5 * time.Second)
	for {
		replies, err := ms.List(context.Background(), callerNS, 10)
		require.NoError(t, err)
		if len(replies) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for reply")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	err = <-errCh
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("daemon run error: %v", err)
	}

	// Verify conversation turns were stored in companion memory
	dbPath := filepath.Join(root, "companion.db")
	db, err := sqliteutil.OpenDB(ctx, dbPath, nil)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mem, err := companion.NewConversationMemory(db)
	require.NoError(t, err)

	// Composite conversation ID: agentID:callerNS
	conversationID := agentID + ":" + callerNS
	stats, err := mem.GetStats(ctx, conversationID)
	require.NoError(t, err)

	// Should have at least 2 turns (user + assistant)
	assert.GreaterOrEqual(t, stats.TotalTurns, 2, "expected at least 2 turns (user + assistant)")
}
