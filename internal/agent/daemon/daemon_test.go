package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/maputil"
	"github.com/joshka0/foxctl/internal/runtime/execution/agentmanager"
	storagents "github.com/joshka0/foxctl/internal/storage/agents"
	"github.com/joshka0/foxctl/internal/storage/mailbox"
	"github.com/joshka0/foxctl/internal/storage/sqliteutil"
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

type fakeCompanionService struct {
	mu              sync.Mutex
	responses       []string
	chatResponses   map[int]*companion.ChatResponse
	errors          map[int]error
	callIndex       int
	capturedPrompts []string
}

func newFakeCompanionService(responses ...string) *fakeCompanionService {
	return &fakeCompanionService{
		responses:     responses,
		chatResponses: make(map[int]*companion.ChatResponse),
		errors:        make(map[int]error),
	}
}

func (f *fakeCompanionService) Chat(ctx context.Context, req companion.ChatRequest) (*companion.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.capturedPrompts = append(f.capturedPrompts, req.Message)
	idx := f.callIndex
	f.callIndex++

	if err, ok := f.errors[idx]; ok {
		return nil, err
	}

	if resp, ok := f.chatResponses[idx]; ok {
		return resp, nil
	}

	response := "No more scripted responses"
	if idx < len(f.responses) {
		response = f.responses[idx]
	}
	return &companion.ChatResponse{Response: response}, nil
}

func (f *fakeCompanionService) GetCallIndex() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callIndex
}

func (f *fakeCompanionService) SetError(index int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errors[index] = err
}

func (f *fakeCompanionService) SetChatResponse(index int, resp *companion.ChatResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatResponses[index] = resp
}

func setupDaemon(t *testing.T) (context.Context, string, Options, *fakeCompanionService) {
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

	// Create agent - execution mode does not affect companion path selection
	agentID := ulid.Make().String()
	a := agent.Agent{
		ID:        agentID,
		Namespace: "actor:agent:" + agentID,
		Role:      "coder",
		State:     agent.StateStarting, // Run will transition to Running
		Prompt:    "You are a coder.",
		ShareBB:   "scoped",
		ExecMode:  agent.ModeReactive,
	}
	err = as.Create(ctx, a)
	require.NoError(t, err)

	fakeSvc := newFakeCompanionService()

	opts := Options{
		AgentID:           agentID,
		StorageRoot:       root,
		PollInterval:      10 * time.Millisecond,
		HeartbeatInterval: 100 * time.Millisecond,
		MaxPollMessages:   10,
		UseMemoryDedupe:   true,
		CompanionService:  fakeSvc,
	}

	return ctx, agentID, opts, fakeSvc
}

func TestDaemon_TTLExpiry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon test in short mode (race detector uses too much memory)")
	}
	ctx, agentID, opts, fakeSvc := setupDaemon(t)
	fakeSvc.responses = []string{"Response"}

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
	assert.Equal(t, 0, fakeSvc.GetCallIndex())

	// Verify message acked (removed from mailbox)
	msgs, err := ms.List(context.Background(), "actor:agent:"+agentID, 10)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestDaemon_Dedupe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon test in short mode (race detector uses too much memory)")
	}
	ctx, agentID, opts, fakeSvc := setupDaemon(t)
	fakeSvc.responses = []string{"Response"}

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
	assert.Equal(t, 0, fakeSvc.GetCallIndex())
}

func TestDaemon_AskReplyCorrelation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon test in short mode (race detector uses too much memory)")
	}
	ctx, agentID, opts, fakeSvc := setupDaemon(t)
	fakeSvc.responses = []string{"I am fine."}

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

func TestDaemon_AskReplyIncludesPresenceAndTone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon test in short mode (race detector uses too much memory)")
	}
	ctx, agentID, opts, fakeSvc := setupDaemon(t)
	fakeSvc.SetChatResponse(0, &companion.ChatResponse{
		Response: "I can help with that.",
		Presence: &companion.PresenceBundle{Emotion: "joy", Intensity: 0.7},
		Tone:     &companion.ChatTone{Voice: "friendly"},
	})

	ms, err := mailbox.Open(ctx, opts.StorageRoot)
	require.NoError(t, err)
	defer func() { require.NoError(t, ms.Close()) }()

	askID := ulid.Make().String()
	askData := agent.AskData{AskID: askID, Question: "status?"}
	payload, err := json.Marshal(envelope.Envelope{Version: 1, Status: "ok", Command: "agent.ask", Data: askData})
	require.NoError(t, err)

	msg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    "caller:presence",
		ToNS:      "actor:agent:" + agentID,
		Type:      agent.MessageTypeAsk,
		Timestamp: time.Now().Unix(),
		Headers:   map[string]string{"correlation": askID},
		Payload:   payload,
	}
	require.NoError(t, ms.Send(ctx, msg))

	daemonCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- Run(daemonCtx, opts) }()

	var reply agent.Message
	deadline := time.Now().Add(5 * time.Second)
	for {
		replies, listErr := ms.List(context.Background(), "caller:presence", 10)
		require.NoError(t, listErr)
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

	var env envelope.Envelope
	require.NoError(t, json.Unmarshal(reply.Payload, &env))
	dataMap, ok := maputil.AsStringMap(env.Data)
	require.True(t, ok)
	answer, ok := maputil.AsStringMap(dataMap["answer"])
	require.True(t, ok)
	assert.Equal(t, "I can help with that.", answer["response"])
	presence, ok := maputil.AsStringMap(answer["presence"])
	require.True(t, ok)
	assert.Equal(t, "joy", presence["emotion"])
	tone, ok := maputil.AsStringMap(answer["tone"])
	require.True(t, ok)
	assert.Equal(t, "friendly", tone["voice"])
}

func TestHandleConsoleAsk_ReplyIncludesPresenceAndTone(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	ms, err := mailbox.Open(ctx, root)
	require.NoError(t, err)
	defer func() { require.NoError(t, ms.Close()) }()

	fakeSvc := newFakeCompanionService()
	fakeSvc.SetChatResponse(0, &companion.ChatResponse{
		Response: "Done.",
		Presence: &companion.PresenceBundle{Emotion: "surprise", Intensity: 0.4},
		Tone:     &companion.ChatTone{Voice: "direct"},
	})

	askID := ulid.Make().String()
	consoleID := ulid.Make().String()
	payload, err := json.Marshal(envelope.Envelope{
		Version: 1,
		Status:  "ok",
		Command: "console.ask",
		Data: agent.ConsoleAskData{
			AskID:     askID,
			Prompt:    "check",
			ConsoleID: consoleID,
		},
	})
	require.NoError(t, err)

	msg := agent.Message{
		ID:      ulid.Make().String(),
		FromNS:  "caller:console",
		ToNS:    "actor:agent:test",
		Type:    agent.MessageTypeConsoleAsk,
		Headers: map[string]string{"correlation": askID},
		Payload: payload,
	}

	logger := zerolog.Nop()
	err = handleConsoleAsk(ctx, logger, msg, fakeSvc, ms, agent.Policy{}, nil, nil, "agent-1")
	require.NoError(t, err)

	replies, err := ms.List(ctx, "caller:console", 10)
	require.NoError(t, err)
	require.Len(t, replies, 1)
	reply := replies[0]
	require.Equal(t, agent.MessageTypeConsoleReply, reply.Type)
	require.Equal(t, askID, reply.Headers["correlation"])

	var env envelope.Envelope
	require.NoError(t, json.Unmarshal(reply.Payload, &env))
	dataMap, ok := maputil.AsStringMap(env.Data)
	require.True(t, ok)
	assert.Equal(t, "Done.", dataMap["response"])
	assert.Equal(t, "ok", dataMap["status"])
	presence, ok := maputil.AsStringMap(dataMap["presence"])
	require.True(t, ok)
	assert.Equal(t, "surprise", presence["emotion"])
	tone, ok := maputil.AsStringMap(dataMap["tone"])
	require.True(t, ok)
	assert.Equal(t, "direct", tone["voice"])
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
		ExecMode: agent.ModeReactive,
	})
	require.NoError(t, err)

	require.NoError(t, agentStore.Close())
	require.NoError(t, mailStore.Close())

	fakeSvc := newFakeCompanionService("It works.")

	opts := Options{
		AgentID:           spawnResp.AgentID,
		StorageRoot:       root,
		PollInterval:      10 * time.Millisecond,
		HeartbeatInterval: 50 * time.Millisecond,
		MaxPollMessages:   10,
		UseMemoryDedupe:   true,
		CompanionService:  fakeSvc,
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
	ctx, agentID, opts, fakeSvc := setupDaemon(t)
	// Inject error
	fakeSvc.SetError(0, errors.New("processing failed"))

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
	for fakeSvc.GetCallIndex() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	require.Greater(t, fakeSvc.GetCallIndex(), 0, "LLM should have been called")

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
	if os.Getenv("AGENTCTL_LLM_API_KEY") == "" &&
		os.Getenv("CEREBRAS_API_KEY") == "" &&
		os.Getenv("GROQ_API_KEY") == "" &&
		os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("skipping companion memory test: requires LLM API key (set AGENTCTL_LLM_API_KEY or provider-specific *_API_KEY)")
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

	provider := os.Getenv("AGENTCTL_LLM_PROVIDER")
	apiKey := os.Getenv("AGENTCTL_LLM_API_KEY")
	if provider == "" || apiKey == "" {
		switch {
		case os.Getenv("CEREBRAS_API_KEY") != "":
			provider = "cerebras"
			apiKey = os.Getenv("CEREBRAS_API_KEY")
		case os.Getenv("GROQ_API_KEY") != "":
			provider = "groq"
			apiKey = os.Getenv("GROQ_API_KEY")
		case os.Getenv("OPENROUTER_API_KEY") != "":
			provider = "openrouter"
			apiKey = os.Getenv("OPENROUTER_API_KEY")
		}
	}

	opts := Options{
		AgentID:               agentID,
		StorageRoot:           root,
		PollInterval:          10 * time.Millisecond,
		HeartbeatInterval:     100 * time.Millisecond,
		MaxPollMessages:       10,
		UseMemoryDedupe:       true,
		EnableCompanionMemory: true, // Enable companion memory
		LLMProvider:           provider,
		LLMAPIKey:             apiKey,
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
	db, closeFn, err := sqliteutil.OpenDBShared(ctx, dbPath, nil)
	require.NoError(t, err)
	defer func() { _ = closeFn() }()

	mem, err := companion.NewConversationMemory(db)
	require.NoError(t, err)

	// Composite conversation ID: agentID:callerNS
	conversationID := agentID + ":" + callerNS
	stats, err := mem.GetStats(ctx, conversationID)
	require.NoError(t, err)

	// Should have at least 2 turns (user + assistant)
	assert.GreaterOrEqual(t, stats.TotalTurns, 2, "expected at least 2 turns (user + assistant)")
}
