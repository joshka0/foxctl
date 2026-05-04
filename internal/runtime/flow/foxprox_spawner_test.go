package flow

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joshka/foxprox/foxprox/client"
	"github.com/joshka/foxprox/foxprox/transport/httpjson"
)

// ---------------------------------------------------------------------------
// Mock FoxproxClient — room-based
// ---------------------------------------------------------------------------

// mockFoxproxClient implements the FoxproxClient interface for testing.
type mockFoxproxClient struct {
	mu sync.Mutex

	// CreateSession behavior
	createSessionResp *httpjson.SessionResponse
	createSessionErr  error
	createSessionReqs []httpjson.CreateSessionRequest

	// DeleteSession behavior
	deleteSessionErr error
	deleteSessionIDs []string

	// SessionReadiness behavior
	readinessResults     map[string][]*httpjson.ReadinessResponse // sessionID -> sequence of responses
	readinessErr         error
	readinessDefaultIdle bool // default true; set false for timeout tests

	// CreateRoom behavior
	createRoomResp *httpjson.RoomResponse
	createRoomErr  error
	createRoomReqs []httpjson.CreateRoomRequest

	// JoinRoom behavior
	joinRoomResp *httpjson.MemberResponse
	joinRoomErr  error
	joinRoomReqs []joinRoomCall

	// LeaveRoom behavior
	leaveRoomResp *httpjson.MemberResponse
	leaveRoomErr  error
	leaveRoomReqs []leaveRoomCall

	// SendMessage behavior
	sendMessageResp *httpjson.SendMessageResponse
	sendMessageErr  error
	sendMessageReqs []httpjson.SendMessageRequest
}

type joinRoomCall struct {
	roomID string
	req    httpjson.JoinRoomRequest
}

type leaveRoomCall struct {
	roomID string
	req    httpjson.LeaveRoomRequest
}

func (m *mockFoxproxClient) CreateSession(ctx context.Context, req httpjson.CreateSessionRequest) (httpjson.SessionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createSessionReqs = append(m.createSessionReqs, req)
	if m.createSessionErr != nil {
		return httpjson.SessionResponse{}, m.createSessionErr
	}
	if m.createSessionResp != nil {
		return *m.createSessionResp, nil
	}
	return httpjson.SessionResponse{ID: "test-session-123", Status: "running"}, nil
}

func (m *mockFoxproxClient) DeleteSession(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteSessionIDs = append(m.deleteSessionIDs, id)
	return m.deleteSessionErr
}

func (m *mockFoxproxClient) SessionReadiness(ctx context.Context, id string, opts client.SessionReadinessOptions) (httpjson.ReadinessResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readinessErr != nil {
		return httpjson.ReadinessResponse{}, m.readinessErr
	}
	if results, ok := m.readinessResults[id]; ok && len(results) > 0 {
		result := results[0]
		m.readinessResults[id] = results[1:]
		return *result, nil
	}
	idle := m.readinessDefaultIdle
	return httpjson.ReadinessResponse{SessionID: id, Idle: idle}, nil
}

func (m *mockFoxproxClient) CreateRoom(ctx context.Context, req httpjson.CreateRoomRequest) (httpjson.RoomResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createRoomReqs = append(m.createRoomReqs, req)
	if m.createRoomErr != nil {
		return httpjson.RoomResponse{}, m.createRoomErr
	}
	if m.createRoomResp != nil {
		return *m.createRoomResp, nil
	}
	return httpjson.RoomResponse{ID: "test-room-123", Workspace: req.Workspace, Title: req.Title}, nil
}

func (m *mockFoxproxClient) JoinRoom(ctx context.Context, roomID string, req httpjson.JoinRoomRequest) (httpjson.MemberResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.joinRoomReqs = append(m.joinRoomReqs, joinRoomCall{roomID: roomID, req: req})
	if m.joinRoomErr != nil {
		return httpjson.MemberResponse{}, m.joinRoomErr
	}
	if m.joinRoomResp != nil {
		return *m.joinRoomResp, nil
	}
	return httpjson.MemberResponse{
		RoomID:    roomID,
		AgentID:   req.AgentID,
		SessionID: req.SessionID,
		Role:      req.Role,
	}, nil
}

func (m *mockFoxproxClient) LeaveRoom(ctx context.Context, roomID string, req httpjson.LeaveRoomRequest) (httpjson.MemberResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leaveRoomReqs = append(m.leaveRoomReqs, leaveRoomCall{roomID: roomID, req: req})
	if m.leaveRoomErr != nil {
		return httpjson.MemberResponse{}, m.leaveRoomErr
	}
	if m.leaveRoomResp != nil {
		return *m.leaveRoomResp, nil
	}
	return httpjson.MemberResponse{RoomID: roomID, AgentID: req.AgentID}, nil
}

func (m *mockFoxproxClient) SendMessage(ctx context.Context, req httpjson.SendMessageRequest) (httpjson.SendMessageResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendMessageReqs = append(m.sendMessageReqs, req)
	if m.sendMessageErr != nil {
		return httpjson.SendMessageResponse{}, m.sendMessageErr
	}
	if m.sendMessageResp != nil {
		return *m.sendMessageResp, nil
	}
	return httpjson.SendMessageResponse{MessageID: "msg-123", Delivered: 1}, nil
}

// ---------------------------------------------------------------------------
// Tests — Spawn
// ---------------------------------------------------------------------------

func TestFoxproxSpawner_Spawn_CreatesSessionRoomAndJoin(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-1",
			Status: "running",
			Cmd:    []string{"droid"},
		},
		createRoomResp: &httpjson.RoomResponse{
			ID:        "room-abc",
			Workspace: "/tmp/workspace",
			Title:     "flow-run-run-123",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
		// No FlowRunID — non-push mode, uses room-based coordination.
	})

	result, err := spawner.Spawn(context.Background(), "researcher", "Research the auth module", AgentSpawnOptions{
		Workspace: "/tmp/workspace",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.AgentID != "fp-session-1" {
		t.Errorf("expected agent_id fp-session-1, got %q", result.AgentID)
	}
	if result.SessionID != "fp-session-1" {
		t.Errorf("expected session_id fp-session-1, got %q", result.SessionID)
	}

	// Verify CreateSession was called with correct parameters.
	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	if len(req.Cmd) != 1 || req.Cmd[0] != "droid" {
		t.Errorf("expected cmd [droid], got %v", req.Cmd)
	}
	if req.Cwd != "/tmp/workspace" {
		t.Errorf("expected cwd /tmp/workspace, got %q", req.Cwd)
	}

	// Verify CreateRoom was called.
	if len(fpClient.createRoomReqs) != 1 {
		t.Fatalf("expected 1 CreateRoom call, got %d", len(fpClient.createRoomReqs))
	}
	roomReq := fpClient.createRoomReqs[0]
	if roomReq.Workspace != "/tmp/workspace" {
		t.Errorf("expected room workspace /tmp/workspace, got %q", roomReq.Workspace)
	}
	// Without FlowRunID, room title is based on agent session ID.
	if !strings.Contains(roomReq.Title, "agent-fp-session-1") {
		t.Errorf("expected room title containing 'agent-fp-session-1', got %q", roomReq.Title)
	}

	// Verify JoinRoom was called with the session.
	if len(fpClient.joinRoomReqs) != 1 {
		t.Fatalf("expected 1 JoinRoom call, got %d", len(fpClient.joinRoomReqs))
	}
	joinReq := fpClient.joinRoomReqs[0]
	if joinReq.roomID != "room-abc" {
		t.Errorf("expected join room_id room-abc, got %q", joinReq.roomID)
	}
	if joinReq.req.AgentID != "fp-session-1" {
		t.Errorf("expected join agent_id fp-session-1, got %q", joinReq.req.AgentID)
	}
	if joinReq.req.SessionID != "fp-session-1" {
		t.Errorf("expected join session_id fp-session-1, got %q", joinReq.req.SessionID)
	}
	if joinReq.req.Role != "researcher" {
		t.Errorf("expected join role researcher, got %q", joinReq.req.Role)
	}

	// Verify room_id is stored on spawner.
	if spawner.roomID != "room-abc" {
		t.Errorf("expected spawner roomID room-abc, got %q", spawner.roomID)
	}
}

func TestFoxproxSpawner_Spawn_ReusesExistingRoom(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-2",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
		RoomID: "existing-room-456",
	})

	result, err := spawner.Spawn(context.Background(), "coder", "Write code", AgentSpawnOptions{
		Workspace: "/tmp/ws",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.AgentID != "fp-session-2" {
		t.Errorf("expected agent_id fp-session-2, got %q", result.AgentID)
	}

	// No CreateRoom call — room was reused.
	if len(fpClient.createRoomReqs) != 0 {
		t.Errorf("expected 0 CreateRoom calls (reused room), got %d", len(fpClient.createRoomReqs))
	}

	// JoinRoom should use the existing room.
	if len(fpClient.joinRoomReqs) != 1 {
		t.Fatalf("expected 1 JoinRoom call, got %d", len(fpClient.joinRoomReqs))
	}
	if fpClient.joinRoomReqs[0].roomID != "existing-room-456" {
		t.Errorf("expected join room_id existing-room-456, got %q", fpClient.joinRoomReqs[0].roomID)
	}
	if spawner.roomID != "existing-room-456" {
		t.Errorf("expected spawner roomID existing-room-456, got %q", spawner.roomID)
	}
}

func TestFoxproxSpawner_Spawn_CustomCLICmd(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-3",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "claude",
	})

	_, err := spawner.Spawn(context.Background(), "coder", "Write code", AgentSpawnOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	if len(req.Cmd) != 1 || req.Cmd[0] != "claude" {
		t.Errorf("expected cmd [claude], got %v", req.Cmd)
	}
}

func TestFoxproxSpawner_Spawn_CLICmdOverrideFromOpts(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-override",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "coder", "Write code", AgentSpawnOptions{
		CLICmd: "claude",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	if len(req.Cmd) != 1 || req.Cmd[0] != "claude" {
		t.Errorf("expected cmd [claude] from opts override, got %v", req.Cmd)
	}
}

func TestFoxproxSpawner_Spawn_CLICmdDefaultWhenNoOverride(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-default",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "Research", AgentSpawnOptions{
		CLICmd: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	if len(req.Cmd) != 1 || req.Cmd[0] != "droid" {
		t.Errorf("expected cmd [droid] from default, got %v", req.Cmd)
	}
}

func TestFoxproxSpawner_Spawn_CreateSessionError(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionErr: fmt.Errorf("foxprox daemon not running"),
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "test", AgentSpawnOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "foxprox daemon not running") {
		t.Errorf("expected error containing 'foxprox daemon not running', got: %v", err)
	}
}

func TestFoxproxSpawner_Spawn_CreateRoomError(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-roomfail",
			Status: "running",
		},
		createRoomErr: fmt.Errorf("room creation failed"),
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "test", AgentSpawnOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "create room") {
		t.Errorf("expected error containing 'create room', got: %v", err)
	}

	// Session should have been cleaned up.
	if len(fpClient.deleteSessionIDs) != 1 {
		t.Errorf("expected 1 delete session call (cleanup), got %d", len(fpClient.deleteSessionIDs))
	}
}

func TestFoxproxSpawner_Spawn_JoinRoomError(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-joinfail",
			Status: "running",
		},
		createRoomResp: &httpjson.RoomResponse{
			ID: "room-joinfail",
		},
		joinRoomErr: fmt.Errorf("join room failed"),
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "test", AgentSpawnOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "join room") {
		t.Errorf("expected error containing 'join room', got: %v", err)
	}

	// Session should have been cleaned up.
	if len(fpClient.deleteSessionIDs) != 1 {
		t.Errorf("expected 1 delete session call (cleanup), got %d", len(fpClient.deleteSessionIDs))
	}
	// Should have attempted to leave the room.
	if len(fpClient.leaveRoomReqs) != 1 {
		t.Errorf("expected 1 leave room call (cleanup), got %d", len(fpClient.leaveRoomReqs))
	}
}

func TestFoxproxSpawner_Spawn_CancelledContext(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionErr: context.Canceled,
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := spawner.Spawn(ctx, "researcher", "test", AgentSpawnOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFoxproxSpawner_Spawn_ReadinessProfile(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-3",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "test", AgentSpawnOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	if req.Readiness == nil {
		t.Fatal("expected readiness profile to be set")
	}
	if req.Readiness.DebounceMS == 0 {
		t.Error("expected debounce_ms > 0 in readiness profile")
	}
}

func TestFoxproxSpawner_Spawn_RoomTitleWithFlowRunID(t *testing.T) {
	// When FlowRunID is set AND OutputMode is "push", exec mode is used
	// and no room is created. When OutputMode is not push, room-based
	// mode is used even if FlowRunID is set on the config.
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-title",
			Status: "running",
		},
	}

	// Config has FlowRunID but Spawn is called without push OutputMode
	// → room-based mode (FlowRunID is only for prompt injection in non-push).
	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd:     "droid",
		FlowRunID:  "my-run-42",
		FlowNodeID: "node-42",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "test", AgentSpawnOptions{
		Workspace: "/tmp/ws",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Room created in non-push mode even though FlowRunID is set.
	if len(fpClient.createRoomReqs) != 1 {
		t.Fatalf("expected 1 CreateRoom call in non-push mode, got %d", len(fpClient.createRoomReqs))
	}
	title := fpClient.createRoomReqs[0].Title
	if !strings.Contains(title, "my-run-42") {
		t.Errorf("expected room title containing 'my-run-42', got %q", title)
	}
}

func TestFoxproxSpawner_Spawn_RoomTitleWithoutFlowRunID(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-notitle",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "test", AgentSpawnOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fpClient.createRoomReqs) != 1 {
		t.Fatalf("expected 1 CreateRoom call, got %d", len(fpClient.createRoomReqs))
	}
	title := fpClient.createRoomReqs[0].Title
	if !strings.Contains(title, "agent-fp-session-notitle") {
		t.Errorf("expected room title containing 'agent-fp-session-notitle', got %q", title)
	}
}

// ---------------------------------------------------------------------------
// Tests — Ask (room message)
// ---------------------------------------------------------------------------

func TestFoxproxSpawner_Ask_SendsRoomMessage(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-1",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	// Spawn first to set up the room.
	_, err := spawner.Spawn(context.Background(), "researcher", "initial prompt", AgentSpawnOptions{})
	if err != nil {
		t.Fatalf("spawn: unexpected error: %v", err)
	}

	// Ask should send a room message.
	result, err := spawner.Ask(context.Background(), "fp-session-1", "What files match auth?", 5000)
	if err != nil {
		t.Fatalf("ask: unexpected error: %v", err)
	}

	// Fire-and-forget: status is "sent", not "replied".
	if result.Status != "sent" {
		t.Errorf("expected status sent, got %q", result.Status)
	}

	// Verify SendMessage was called with the room message.
	if len(fpClient.sendMessageReqs) != 1 {
		t.Fatalf("expected 1 SendMessage call, got %d", len(fpClient.sendMessageReqs))
	}
	msg := fpClient.sendMessageReqs[0]
	if msg.Text != "What files match auth?" {
		t.Errorf("expected message text 'What files match auth?', got %q", msg.Text)
	}
	if msg.RoomID != "test-room-123" {
		t.Errorf("expected room_id test-room-123, got %q", msg.RoomID)
	}
}

func TestFoxproxSpawner_Ask_ReturnsImmediately(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-fast",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, _ = spawner.Spawn(context.Background(), "researcher", "test", AgentSpawnOptions{})

	start := time.Now()
	result, err := spawner.Ask(context.Background(), "fp-session-fast", "Do something", 30000)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "sent" {
		t.Errorf("expected status sent, got %q", result.Status)
	}

	// Should return almost instantly (fire-and-forget), not wait for timeout.
	if elapsed > 500*time.Millisecond {
		t.Errorf("Ask took %v, expected immediate return", elapsed)
	}
}

func TestFoxproxSpawner_Ask_NoRoom(t *testing.T) {
	fpClient := &mockFoxproxClient{}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	// Ask without Spawn — no room set.
	_, err := spawner.Ask(context.Background(), "fp-session-1", "test", 5000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no room") {
		t.Errorf("expected error containing 'no room', got: %v", err)
	}
}

func TestFoxproxSpawner_Ask_SendMessageError(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-1",
			Status: "running",
		},
		sendMessageErr: fmt.Errorf("message delivery failed"),
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, _ = spawner.Spawn(context.Background(), "researcher", "test", AgentSpawnOptions{})

	_, err := spawner.Ask(context.Background(), "fp-session-1", "test", 5000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "send message") {
		t.Errorf("expected error containing 'send message', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests — Info
// ---------------------------------------------------------------------------

func TestFoxproxSpawner_Info_Running(t *testing.T) {
	fpClient := &mockFoxproxClient{
		readinessResults: map[string][]*httpjson.ReadinessResponse{
			"fp-session-1": {
				{SessionID: "fp-session-1", Idle: false, OutputRateBPS: 1500.0},
			},
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	info, err := spawner.Info(context.Background(), "fp-session-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Status != "running" {
		t.Errorf("expected status running, got %q", info.Status)
	}
}

func TestFoxproxSpawner_Info_Exited(t *testing.T) {
	fpClient := &mockFoxproxClient{
		readinessErr: fmt.Errorf("session not found"),
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	info, err := spawner.Info(context.Background(), "fp-session-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Status != "exited" {
		t.Errorf("expected status exited, got %q", info.Status)
	}
}

func TestFoxproxSpawner_Info_IdleSession(t *testing.T) {
	fpClient := &mockFoxproxClient{
		readinessResults: map[string][]*httpjson.ReadinessResponse{
			"fp-session-1": {
				{SessionID: "fp-session-1", Idle: true, IdleForMS: 5000},
			},
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	info, err := spawner.Info(context.Background(), "fp-session-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Status != "completed" {
		t.Errorf("expected status completed, got %q", info.Status)
	}
	if !strings.Contains(info.Summary, "idle for 5000ms") {
		t.Errorf("expected summary containing 'idle for 5000ms', got %q", info.Summary)
	}
}

func TestFoxproxSpawner_Info_NoScreenCapture(t *testing.T) {
	// Info should NOT call SessionScreen — only readiness check.
	// This test verifies no screen-related code runs.
	fpClient := &mockFoxproxClient{
		readinessResults: map[string][]*httpjson.ReadinessResponse{
			"fp-session-1": {
				{SessionID: "fp-session-1", Idle: true, IdleForMS: 3000},
			},
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	info, err := spawner.Info(context.Background(), "fp-session-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Info returns completed with idle duration summary — no screen content.
	if info.Status != "completed" {
		t.Errorf("expected status completed, got %q", info.Status)
	}
	// Summary should NOT contain screen-scraped content.
	if strings.Contains(info.Summary, "agent output") {
		t.Errorf("summary should not contain screen-scraped content, got %q", info.Summary)
	}
}

// ---------------------------------------------------------------------------
// Tests — Kill (leave room + delete session)
// ---------------------------------------------------------------------------

func TestFoxproxSpawner_Kill_LeavesRoomThenDeletesSession(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-kill",
			Status: "running",
		},
		createRoomResp: &httpjson.RoomResponse{
			ID: "room-kill",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	// Spawn first to set up the room.
	_, _ = spawner.Spawn(context.Background(), "researcher", "test", AgentSpawnOptions{})

	err := spawner.Kill(context.Background(), "fp-session-kill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify LeaveRoom was called.
	if len(fpClient.leaveRoomReqs) != 1 {
		t.Fatalf("expected 1 LeaveRoom call, got %d", len(fpClient.leaveRoomReqs))
	}
	leaveCall := fpClient.leaveRoomReqs[0]
	if leaveCall.roomID != "room-kill" {
		t.Errorf("expected leave room_id room-kill, got %q", leaveCall.roomID)
	}
	if leaveCall.req.AgentID != "fp-session-kill" {
		t.Errorf("expected leave agent_id fp-session-kill, got %q", leaveCall.req.AgentID)
	}

	// Verify DeleteSession was called.
	if len(fpClient.deleteSessionIDs) != 1 {
		t.Fatalf("expected 1 DeleteSession call, got %d", len(fpClient.deleteSessionIDs))
	}
	if fpClient.deleteSessionIDs[0] != "fp-session-kill" {
		t.Errorf("expected delete session fp-session-kill, got %q", fpClient.deleteSessionIDs[0])
	}
}

func TestFoxproxSpawner_Kill_NoRoom(t *testing.T) {
	// Kill without a room should still delete the session.
	fpClient := &mockFoxproxClient{}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	err := spawner.Kill(context.Background(), "fp-session-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No LeaveRoom call.
	if len(fpClient.leaveRoomReqs) != 0 {
		t.Errorf("expected 0 LeaveRoom calls, got %d", len(fpClient.leaveRoomReqs))
	}

	// Session deleted.
	if len(fpClient.deleteSessionIDs) != 1 {
		t.Fatalf("expected 1 DeleteSession call, got %d", len(fpClient.deleteSessionIDs))
	}
}

func TestFoxproxSpawner_Kill_DeleteSessionError(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-err",
			Status: "running",
		},
		createRoomResp: &httpjson.RoomResponse{
			ID: "room-err",
		},
		deleteSessionErr: fmt.Errorf("session already gone"),
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, _ = spawner.Spawn(context.Background(), "researcher", "test", AgentSpawnOptions{})

	err := spawner.Kill(context.Background(), "fp-session-err")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "session already gone") {
		t.Errorf("expected error containing 'session already gone', got: %v", err)
	}

	// LeaveRoom should have been attempted despite the session delete error.
	if len(fpClient.leaveRoomReqs) != 1 {
		t.Errorf("expected 1 LeaveRoom call, got %d", len(fpClient.leaveRoomReqs))
	}
}

// ---------------------------------------------------------------------------
// Tests — Droid exec push mode (output_mode=push uses droid exec --auto medium)
// ---------------------------------------------------------------------------

func TestFoxproxSpawner_ExecPushMode_SpawnCreatesSessionWithDroidExec(t *testing.T) {
	// When OutputMode is "push", the spawner should use
	// `droid exec --auto medium "<prompt>"` instead of just `droid`.
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-exec-session-1",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	result, err := spawner.Spawn(context.Background(), "researcher", "Research the auth module", AgentSpawnOptions{
		Workspace:  "/tmp/workspace",
		OutputMode: "push",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.AgentID != "fp-exec-session-1" {
		t.Errorf("expected agent_id fp-exec-session-1, got %q", result.AgentID)
	}
	if result.SessionID != "fp-exec-session-1" {
		t.Errorf("expected session_id fp-exec-session-1, got %q", result.SessionID)
	}

	// Verify CreateSession was called with droid exec --auto medium command.
	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	expectedCmd := []string{"droid", "exec", "--auto", "medium"}
	if len(req.Cmd) < 4 {
		t.Fatalf("expected cmd starting with [droid exec --auto medium <prompt>], got %v", req.Cmd)
	}
	for i, v := range expectedCmd {
		if req.Cmd[i] != v {
			t.Errorf("expected cmd[%d] = %q, got %q", i, v, req.Cmd[i])
		}
	}
	// The last argument should be the prompt.
	promptArg := strings.Join(req.Cmd[4:], " ")
	if !strings.Contains(promptArg, "Research the auth module") {
		t.Errorf("expected prompt arg containing 'Research the auth module', got %q", promptArg)
	}
}

func TestFoxproxSpawner_ExecPushMode_SpawnSkipsRoomCreationAndJoin(t *testing.T) {
	// In push mode (OutputMode "push"), room creation and join should be skipped.
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-exec-session-2",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "Do research", AgentSpawnOptions{
		Workspace:  "/tmp/workspace",
		OutputMode: "push",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No CreateRoom call in push mode.
	if len(fpClient.createRoomReqs) != 0 {
		t.Errorf("expected 0 CreateRoom calls in push mode, got %d", len(fpClient.createRoomReqs))
	}

	// No JoinRoom call in push mode.
	if len(fpClient.joinRoomReqs) != 0 {
		t.Errorf("expected 0 JoinRoom calls in push mode, got %d", len(fpClient.joinRoomReqs))
	}

	// Room ID should be empty.
	if spawner.roomID != "" {
		t.Errorf("expected empty roomID in push mode, got %q", spawner.roomID)
	}
}

func TestFoxproxSpawner_ExecPushMode_SpawnDoesNotInjectFlowContext(t *testing.T) {
	// In push mode, the AgentExecutor already injects push instructions.
	// The spawner should NOT add its own flow context to avoid duplication.
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-exec-session-3",
			Status: "running",
		},
	}

	// Spawner config has FlowRunID/FlowNodeID but these are only used in
	// non-push mode for prompt injection. Push mode ignores them.
	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd:     "droid",
		FlowRunID:  "run-push-789",
		FlowNodeID: "node-agent-3",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "Original prompt", AgentSpawnOptions{
		Workspace:  "/tmp/workspace",
		OutputMode: "push",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	promptArg := strings.Join(req.Cmd[4:], " ")

	// The prompt should NOT contain the spawner's own flow output push
	// configuration (the AgentExecutor handles that). It should be the
	// original prompt only.
	if strings.Contains(promptArg, "Flow Output Push Configuration") {
		t.Errorf("push mode prompt should NOT contain spawner-injected flow context, got: %q", promptArg)
	}
	if !strings.Contains(promptArg, "Original prompt") {
		t.Errorf("push mode prompt should contain the original prompt text, got: %q", promptArg)
	}
}

func TestFoxproxSpawner_ExecPushMode_SpawnIncludesPushInstructions(t *testing.T) {
	// The prompt passed to droid exec should contain the push instructions
	// already injected by the AgentExecutor (simulated here).
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-exec-session-4",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	// Simulate the prompt as AgentExecutor would construct it (with push instructions).
	promptWithPush := "Research the auth module\n\n--- Flow Output Push Configuration ---\nYou are running as a node..."
	_, err := spawner.Spawn(context.Background(), "researcher", promptWithPush, AgentSpawnOptions{
		Workspace:  "/tmp/workspace",
		OutputMode: "push",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	promptArg := strings.Join(req.Cmd[4:], " ")

	if !strings.Contains(promptArg, "Flow Output Push Configuration") {
		t.Errorf("expected prompt to contain the push instructions from AgentExecutor, got: %q", promptArg)
	}
}

func TestFoxproxSpawner_ExecPushMode_AskReturnsImmediately(t *testing.T) {
	// In exec mode, Ask should return immediately since the prompt was
	// already sent as the exec argument.
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-exec-session-5",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "Do work", AgentSpawnOptions{
		Workspace:  "/tmp/workspace",
		OutputMode: "push",
	})
	if err != nil {
		t.Fatalf("spawn: unexpected error: %v", err)
	}

	// Ask should return immediately in exec mode.
	start := time.Now()
	result, err := spawner.Ask(context.Background(), "fp-exec-session-5", "Extra instructions", 30000)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("ask: unexpected error: %v", err)
	}
	if result.Status != "exec" {
		t.Errorf("expected status exec, got %q", result.Status)
	}

	// Should return nearly instantly.
	if elapsed > 100*time.Millisecond {
		t.Errorf("Ask took %v in exec mode, expected immediate return", elapsed)
	}

	// No SendMessage call in exec mode.
	if len(fpClient.sendMessageReqs) != 0 {
		t.Errorf("expected 0 SendMessage calls in exec mode, got %d", len(fpClient.sendMessageReqs))
	}
}

func TestFoxproxSpawner_ExecPushMode_KillSkipsLeaveRoom(t *testing.T) {
	// In exec mode, Kill should skip LeaveRoom since no room was created.
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-exec-session-6",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "Do work", AgentSpawnOptions{
		Workspace:  "/tmp/workspace",
		OutputMode: "push",
	})
	if err != nil {
		t.Fatalf("spawn: unexpected error: %v", err)
	}

	err = spawner.Kill(context.Background(), "fp-exec-session-6")
	if err != nil {
		t.Fatalf("kill: unexpected error: %v", err)
	}

	// No LeaveRoom call in exec mode.
	if len(fpClient.leaveRoomReqs) != 0 {
		t.Errorf("expected 0 LeaveRoom calls in exec mode, got %d", len(fpClient.leaveRoomReqs))
	}

	// Session should be deleted.
	if len(fpClient.deleteSessionIDs) != 1 {
		t.Fatalf("expected 1 DeleteSession call, got %d", len(fpClient.deleteSessionIDs))
	}
	if fpClient.deleteSessionIDs[0] != "fp-exec-session-6" {
		t.Errorf("expected delete session fp-exec-session-6, got %q", fpClient.deleteSessionIDs[0])
	}
}

func TestFoxproxSpawner_NonPushMode_UsesRoomBasedFlow(t *testing.T) {
	// When FlowRunID is empty, the spawner should use the existing room-based flow.
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-room",
			Status: "running",
		},
		createRoomResp: &httpjson.RoomResponse{
			ID:        "room-nonpush",
			Workspace: "/tmp/workspace",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
		// FlowRunID is empty — non-push mode.
	})

	result, err := spawner.Spawn(context.Background(), "researcher", "Research", AgentSpawnOptions{
		Workspace: "/tmp/workspace",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.AgentID != "fp-session-room" {
		t.Errorf("expected agent_id fp-session-room, got %q", result.AgentID)
	}

	// Verify room-based flow: CreateSession with just [droid].
	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	if len(req.Cmd) != 1 || req.Cmd[0] != "droid" {
		t.Errorf("expected cmd [droid] in non-push mode, got %v", req.Cmd)
	}

	// Room created.
	if len(fpClient.createRoomReqs) != 1 {
		t.Errorf("expected 1 CreateRoom call in non-push mode, got %d", len(fpClient.createRoomReqs))
	}

	// Joined room.
	if len(fpClient.joinRoomReqs) != 1 {
		t.Errorf("expected 1 JoinRoom call in non-push mode, got %d", len(fpClient.joinRoomReqs))
	}
}

// ---------------------------------------------------------------------------
// Tests — Interface compliance
// ---------------------------------------------------------------------------

func TestFoxproxSpawner_ImplementsAgentSpawnerInterface(t *testing.T) {
	// Compile-time interface compliance check.
	var _ AgentSpawner = &foxproxAgentSpawner{}
}
