package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	agentdomain "github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	v2jido "github.com/jkatigb/agentctl/internal/v2/adapters/jido"
	libsqlworkers "github.com/jkatigb/agentctl/internal/v2/adapters/libsql/workers"
	coreworker "github.com/jkatigb/agentctl/internal/v2/core/worker"
)

type testAgentEventPublisher struct {
	mu     sync.Mutex
	types  []string
	events []agentChatEvent
}

func (p *testAgentEventPublisher) Publish(eventType string, data any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.types = append(p.types, eventType)
	if evt, ok := data.(agentChatEvent); ok {
		p.events = append(p.events, evt)
	}
}

func resetAgentStreamRegistry() {
	activeAgentStreams = newAgentStreamRegistry()
}

func TestAgentRuntimeGetHandler_ReturnsRuntimeTree(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")

	server, socketPath := startOrchestrationJSONRPCServer(t, func(method string, params json.RawMessage) (any, *jsonrpcTestError) {
		var parsed map[string]any
		_ = json.Unmarshal(params, &parsed)
		agentID := strings.TrimSpace(fmt.Sprint(parsed["agent_id"]))
		switch method {
		case v2jido.MethodRuntimeState:
			return map[string]any{
				"agent_id": agentID,
				"status":   "ok",
				"state": map[string]any{
					"agentctl": map[string]any{
						"status": "running",
						"agent":  agentID,
					},
				},
			}, nil
		case v2jido.MethodRuntimeGetChildren:
			switch agentID {
			case "agent-root":
				return map[string]any{
					"agent_id": agentID,
					"children": map[string]any{
						"agent-child-1": map[string]any{
							"tag":      "agent-child-1",
							"agent_id": "agent-child-1",
						},
					},
				}, nil
			case "agent-child-1":
				return map[string]any{
					"agent_id": agentID,
					"children": map[string]any{
						"agent-grandchild-1": map[string]any{
							"tag":      "agent-grandchild-1",
							"agent_id": "agent-grandchild-1",
						},
					},
				}, nil
			default:
				return map[string]any{
					"agent_id": agentID,
					"children": map[string]any{},
				}, nil
			}
		default:
			return nil, &jsonrpcTestError{Code: -32601, Message: "unknown method"}
		}
	})
	t.Cleanup(func() { _ = server.Close() })
	t.Setenv(v2jido.EnvJidoSocketPath, socketPath)

	cfg := orchestrationTestConfig(t.TempDir())
	store, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("close agents store: %v", closeErr)
		}
	}()

	err = store.Create(context.Background(), agentdomain.Agent{
		ID:          "agent-root",
		Namespace:   "ws-1",
		Name:        "Runtime Root",
		Role:        "overseer",
		SkillsAllow: []string{},
		Policy:      agentdomain.Policy{},
		ShareBB:     "scoped",
		State:       agentdomain.StateRunning,
		CreatedAt:   time.Date(2026, time.March, 6, 12, 0, 0, 0, time.UTC),
		ExecMode:    agentdomain.ModeReactive,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	h := AgentDetailHandler(cfg, zerolog.Nop(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/agent-root/runtime?depth=2", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	body := decodeResponseBody(t, rr)
	runtimeWrap, _ := body["runtime"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(runtimeWrap["agent_id"])) != "agent-root" {
		t.Fatalf("runtime.agent_id=%v want agent-root", runtimeWrap["agent_id"])
	}
	if depth := strings.TrimSpace(fmt.Sprint(runtimeWrap["depth"])); depth != "2" {
		t.Fatalf("runtime.depth=%v want 2", runtimeWrap["depth"])
	}

	root, _ := runtimeWrap["root"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(root["agent_id"])) != "agent-root" {
		t.Fatalf("root.agent_id=%v want agent-root", root["agent_id"])
	}

	children, _ := root["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("root children=%d want 1", len(children))
	}
	child, _ := children[0].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(child["agent_id"])) != "agent-child-1" {
		t.Fatalf("child.agent_id=%v want agent-child-1", child["agent_id"])
	}

	grandchildren, _ := child["children"].([]any)
	if len(grandchildren) != 1 {
		t.Fatalf("grandchildren=%d want 1", len(grandchildren))
	}
	grandchild, _ := grandchildren[0].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(grandchild["agent_id"])) != "agent-grandchild-1" {
		t.Fatalf("grandchild.agent_id=%v want agent-grandchild-1", grandchild["agent_id"])
	}
}

func TestAgentRuntimeGetHandler_ReturnsGoRuntimeTree(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv(EnvOrchestrationRuntimeBackend, orchestrationRuntimeBackendGoruntimeAPI)

	cfg := orchestrationTestConfig(t.TempDir())
	store, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("close agents store: %v", closeErr)
		}
	}()

	err = store.Create(context.Background(), agentdomain.Agent{
		ID:          "agent-go-root",
		Namespace:   "ws-1",
		Name:        "Go Runtime Root",
		Role:        "overseer",
		SkillsAllow: []string{},
		Policy:      agentdomain.Policy{},
		ShareBB:     "scoped",
		State:       agentdomain.StateRunning,
		CreatedAt:   time.Date(2026, time.April, 6, 12, 0, 0, 0, time.UTC),
		ExecMode:    agentdomain.ModeReactive,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	workerStore, closeWorkers, err := libsqlworkers.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open worker store: %v", err)
	}
	defer func() { _ = closeWorkers() }()
	if err := workerStore.Upsert(context.Background(), coreworker.Record{
		WorkerID:    "subprocess:agent-go-root",
		BackendKind: coreworker.BackendSubprocess,
		AgentID:     "agent-go-root",
		Status:      coreworker.StatusRunning,
		RawState:    json.RawMessage(`{"agentctl":{"status":"running","agent":"agent-go-root"}}`),
	}); err != nil {
		t.Fatalf("upsert root worker: %v", err)
	}
	if err := workerStore.Upsert(context.Background(), coreworker.Record{
		WorkerID:       "subprocess:agent-go-child-1",
		BackendKind:    coreworker.BackendSubprocess,
		AgentID:        "agent-go-child-1",
		ParentAgentID:  "agent-go-root",
		ParentWorkerID: "subprocess:agent-go-root",
		Status:         coreworker.StatusRunning,
		RawState:       json.RawMessage(`{"agentctl":{"status":"running","agent":"agent-go-child-1"}}`),
	}); err != nil {
		t.Fatalf("upsert child worker: %v", err)
	}

	h := AgentDetailHandler(cfg, zerolog.Nop(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/agent-go-root/runtime?depth=2", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	body := decodeResponseBody(t, rr)
	runtimeWrap, _ := body["runtime"].(map[string]any)
	root, _ := runtimeWrap["root"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(root["agent_id"])) != "agent-go-root" {
		t.Fatalf("root.agent_id=%v want agent-go-root", root["agent_id"])
	}
	children, _ := root["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("root children=%d want 1", len(children))
	}
	child, _ := children[0].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(child["agent_id"])) != "agent-go-child-1" {
		t.Fatalf("child.agent_id=%v want agent-go-child-1", child["agent_id"])
	}
	if strings.TrimSpace(fmt.Sprint(child["status"])) != "running" {
		t.Fatalf("child.status=%v want running", child["status"])
	}
}

func TestAgentDaemonKillWithRuntime_UsesInjectedHostForGoRuntime(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv(EnvOrchestrationRuntimeBackend, orchestrationRuntimeBackendGoruntimeAPI)

	cfg := orchestrationTestConfig(t.TempDir())
	store, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("close agents store: %v", closeErr)
		}
	}()

	err = store.Create(context.Background(), agentdomain.Agent{
		ID:          "agent-go-kill-1",
		Namespace:   "ws-1",
		Name:        "Go Runtime Kill",
		Role:        "coder",
		SkillsAllow: []string{},
		Policy:      agentdomain.Policy{},
		ShareBB:     "scoped",
		State:       agentdomain.StateRunning,
		CreatedAt:   time.Date(2026, time.April, 6, 12, 30, 0, 0, time.UTC),
		ExecMode:    agentdomain.ModeReactive,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	host := &fakeOrchestrationRuntimeHost{
		signalResp: coreworker.SignalResponse{
			WorkerID: "subprocess:agent-go-kill-1",
			AgentID:  "agent-go-kill-1",
			Status:   coreworker.StatusStopping,
		},
	}
	h := AgentDetailHandlerWithRuntime(cfg, zerolog.Nop(), nil, host)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/agent-go-kill-1/daemon/kill", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if host.signalCalls != 1 {
		t.Fatalf("signal_calls=%d want 1", host.signalCalls)
	}
	if host.lastSignalReq.AgentID != "agent-go-kill-1" {
		t.Fatalf("signal agent_id=%q want agent-go-kill-1", host.lastSignalReq.AgentID)
	}
	body := decodeResponseBody(t, rr)
	if ok := body["ok"]; ok != true {
		t.Fatalf("ok=%v want true", ok)
	}
	if status := strings.TrimSpace(fmt.Sprint(body["status"])); status != "stopping" {
		t.Fatalf("status=%q want stopping", status)
	}

	updated, err := store.Get(context.Background(), "agent-go-kill-1")
	if err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	if updated.State != agentdomain.StateStopped {
		t.Fatalf("agent state=%q want %q", updated.State, agentdomain.StateStopped)
	}
}

func TestAgentRuntimeLogsGetHandler_ReturnsGoRuntimeRecentLogs(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv(EnvOrchestrationRuntimeBackend, orchestrationRuntimeBackendGoruntimeAPI)

	cfg := orchestrationTestConfig(t.TempDir())
	store, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("close agents store: %v", closeErr)
		}
	}()

	err = store.Create(context.Background(), agentdomain.Agent{
		ID:          "agent-go-logs-1",
		Namespace:   "ws-1",
		Name:        "Go Runtime Logs",
		Role:        "coder",
		SkillsAllow: []string{},
		Policy:      agentdomain.Policy{},
		ShareBB:     "scoped",
		State:       agentdomain.StateRunning,
		CreatedAt:   time.Date(2026, time.April, 6, 12, 40, 0, 0, time.UTC),
		ExecMode:    agentdomain.ModeReactive,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	workerStore, closeWorkers, err := libsqlworkers.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open worker store: %v", err)
	}
	defer func() { _ = closeWorkers() }()
	if err := workerStore.Upsert(context.Background(), coreworker.Record{
		WorkerID:    "subprocess:agent-go-logs-1",
		BackendKind: coreworker.BackendSubprocess,
		AgentID:     "agent-go-logs-1",
		Status:      coreworker.StatusRunning,
		RawState: json.RawMessage(`{"agentctl":{"status":"running","recent_logs":[
			{"stream":"stdout","text":"first line","ts":"2026-04-06T12:40:01Z"},
			{"stream":"stderr","text":"second line","ts":"2026-04-06T12:40:02Z"}
		]}}`),
	}); err != nil {
		t.Fatalf("upsert worker: %v", err)
	}

	h := AgentDetailHandler(cfg, zerolog.Nop(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/agent-go-logs-1/runtime/logs?limit=1", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	if strings.TrimSpace(fmt.Sprint(body["agent_id"])) != "agent-go-logs-1" {
		t.Fatalf("agent_id=%v want agent-go-logs-1", body["agent_id"])
	}
	if strings.TrimSpace(fmt.Sprint(body["count"])) != "1" {
		t.Fatalf("count=%v want 1", body["count"])
	}
	entries, _ := body["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries len=%d want 1", len(entries))
	}
	entry, _ := entries[0].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(entry["stream"])) != "stderr" {
		t.Fatalf("stream=%v want stderr", entry["stream"])
	}
	if strings.TrimSpace(fmt.Sprint(entry["text"])) != "second line" {
		t.Fatalf("text=%v want second line", entry["text"])
	}
}

func TestAgentRuntimeLogsStreamHandler_StreamsGoRuntimeLogUpdates(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv(EnvOrchestrationRuntimeBackend, orchestrationRuntimeBackendGoruntimeAPI)

	cfg := orchestrationTestConfig(t.TempDir())
	store, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("close agents store: %v", closeErr)
		}
	}()

	err = store.Create(context.Background(), agentdomain.Agent{
		ID:          "agent-go-stream-logs-1",
		Namespace:   "ws-1",
		Name:        "Go Runtime Log Stream",
		Role:        "coder",
		SkillsAllow: []string{},
		Policy:      agentdomain.Policy{},
		ShareBB:     "scoped",
		State:       agentdomain.StateRunning,
		CreatedAt:   time.Date(2026, time.April, 6, 12, 50, 0, 0, time.UTC),
		ExecMode:    agentdomain.ModeReactive,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	workerStore, closeWorkers, err := libsqlworkers.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open worker store: %v", err)
	}
	defer func() { _ = closeWorkers() }()
	if err := workerStore.Upsert(context.Background(), coreworker.Record{
		WorkerID:    "subprocess:agent-go-stream-logs-1",
		BackendKind: coreworker.BackendSubprocess,
		AgentID:     "agent-go-stream-logs-1",
		Status:      coreworker.StatusRunning,
		RawState: json.RawMessage(`{"agentctl":{"status":"running","recent_logs":[
			{"stream":"stdout","text":"first line","ts":"2026-04-06T12:50:01Z"}
		]}}`),
	}); err != nil {
		t.Fatalf("upsert worker: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/agents/agent-go-stream-logs-1/runtime/logs/stream?poll_ms=25", nil).WithContext(ctx)
	rr := newStreamingResponseRecorder()
	done := make(chan struct{})
	go func() {
		AgentDetailHandler(cfg, zerolog.Nop(), nil).ServeHTTP(rr, req)
		close(done)
	}()

	time.Sleep(80 * time.Millisecond)
	if err := workerStore.Upsert(context.Background(), coreworker.Record{
		WorkerID:    "subprocess:agent-go-stream-logs-1",
		BackendKind: coreworker.BackendSubprocess,
		AgentID:     "agent-go-stream-logs-1",
		Status:      coreworker.StatusRunning,
		RawState: json.RawMessage(`{"agentctl":{"status":"running","recent_logs":[
			{"stream":"stdout","text":"first line","ts":"2026-04-06T12:50:01Z"},
			{"stream":"stderr","text":"second line","ts":"2026-04-06T12:50:02Z"}
		]}}`),
	}); err != nil {
		t.Fatalf("upsert worker update: %v", err)
	}

	waitForBodyContains(t, rr, "second line", 2*time.Second)
	cancel()
	<-done

	body := rr.BodyString()
	if !strings.Contains(body, "event: connected") {
		t.Fatalf("expected connected event, body=%s", body)
	}
	if !strings.Contains(body, "event: runtime.logs") {
		t.Fatalf("expected runtime.logs event, body=%s", body)
	}
	if !strings.Contains(body, "first line") || !strings.Contains(body, "second line") {
		t.Fatalf("expected streamed log lines, body=%s", body)
	}
}

func waitForBodyContains(t *testing.T, rr *streamingResponseRecorder, needle string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body := rr.BodyString()
		if strings.Contains(body, needle) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("response body did not contain %q within %s; body=%s", needle, timeout, rr.BodyString())
}

type streamingResponseRecorder struct {
	mu     sync.Mutex
	header http.Header
	body   bytes.Buffer
	status int
}

func newStreamingResponseRecorder() *streamingResponseRecorder {
	return &streamingResponseRecorder{
		header: make(http.Header),
		status: http.StatusOK,
	}
}

func (r *streamingResponseRecorder) Header() http.Header {
	return r.header
}

func (r *streamingResponseRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.Write(p)
}

func (r *streamingResponseRecorder) WriteHeader(statusCode int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = statusCode
}

func (r *streamingResponseRecorder) Flush() {}

func (r *streamingResponseRecorder) BodyString() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.String()
}

func TestResolveAgentSpawnPrompt_UsesRoomRoleDefaultAndOnboarding(t *testing.T) {
	got := resolveAgentSpawnPrompt(AgentSpawnRequest{
		Role:        "assistant",
		RoomID:      "triad-123",
		RoomRole:    "frontend-eng",
		WorkspaceID: "ws1",
	}, "ws1")
	for _, want := range []string{
		"You are a frontend engineering agent.",
		"ROOM ONBOARDING:",
		"`agentctl-room-operator` and `agentctl-room`",
		"`agentctl room status triad-123`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q\n%s", want, got)
		}
	}
}

func TestAttachSpawnedAgentToRoom_SendsOnboardingMessage(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()
	if err := attachSpawnedAgentToRoom(ctx, cfg, "ws1", "alpha", "agent-1", "frontend-eng", "frontend-eng"); err != nil {
		t.Fatalf("attachSpawnedAgentToRoom: %v", err)
	}

	store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open board store: %v", err)
	}
	defer func() { _ = store.Close() }()

	room, err := store.GetRoom(ctx, "ws1", "alpha", "")
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	foundMember := false
	for _, member := range room.Members {
		if member.ActorID == "agent-1" && member.Role == "frontend-eng" {
			foundMember = true
			break
		}
	}
	if !foundMember {
		t.Fatalf("expected agent-1 frontend-eng member, got %+v", room.Members)
	}

	messages, err := store.ListRoomMessages(ctx, "ws1", "alpha", 10)
	if err != nil {
		t.Fatalf("list room messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages=%d want 1", len(messages))
	}
	msg := messages[0]
	if got, want := msg.Recipient, "agent-1"; got != want {
		t.Fatalf("recipient=%q want %q", got, want)
	}
	if got, want := msg.Subject, "Room onboarding: frontend-eng"; got != want {
		t.Fatalf("subject=%q want %q", got, want)
	}
	if !strings.Contains(msg.Body, "ROOM ONBOARDING:") {
		t.Fatalf("body missing onboarding block\n%s", msg.Body)
	}
}

func TestAgentRuntimeGetHandler_ReturnsSandboxRuntimeSummary(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	store, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("close agents store: %v", closeErr)
		}
	}()

	err = store.Create(context.Background(), agentdomain.Agent{
		ID:              "agent-sandbox-runtime-1",
		Namespace:       "ws-sandbox",
		Name:            "Sandbox Runtime",
		Role:            "coder",
		SkillsAllow:     []string{},
		Policy:          agentdomain.Policy{},
		ShareBB:         "scoped",
		State:           agentdomain.StateRunning,
		CreatedAt:       time.Date(2026, time.March, 6, 12, 0, 0, 0, time.UTC),
		ExecMode:        agentdomain.ModeReactive,
		WorkspaceRoot:   "/workspace/repo",
		WorkspaceSource: "sandbox",
		SandboxProvider: "opensandbox",
		SandboxID:       "sbx-runtime-1",
		RepoURL:         "https://github.com/example/repo.git",
		RepoRef:         "main",
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agents/agent-sandbox-runtime-1/runtime?depth=2", nil)
	rr := httptest.NewRecorder()
	AgentDetailHandler(cfg, zerolog.Nop(), nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	body := decodeResponseBody(t, rr)
	runtimeWrap, _ := body["runtime"].(map[string]any)
	root, _ := runtimeWrap["root"].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(root["status"])); got != "sandbox" {
		t.Fatalf("root.status=%q want sandbox", got)
	}
	metadata, _ := root["metadata"].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(metadata["sandbox_id"])); got != "sbx-runtime-1" {
		t.Fatalf("metadata.sandbox_id=%q want sbx-runtime-1", got)
	}
	state, _ := root["state"].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(state["profile"])); got != "sandbox" {
		t.Fatalf("state.profile=%q want sandbox", got)
	}
	agentctlState, _ := state["agentctl"].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(agentctlState["status"])); got != "running" {
		t.Fatalf("state.agentctl.status=%q want running", got)
	}
}

func TestAgentPatchHandler_UpdatesMemoryScope(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	resetAgentStreamRegistry()

	cfg := orchestrationTestConfig(t.TempDir())
	store, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("close agents store: %v", closeErr)
		}
	}()

	err = store.Create(context.Background(), agentdomain.Agent{
		ID:          "agent-memory-1",
		Namespace:   "ws-1",
		Name:        "Memory Scope Agent",
		Role:        "companion",
		SkillsAllow: []string{},
		Policy:      agentdomain.Policy{},
		ShareBB:     "scoped",
		State:       agentdomain.StateStopped,
		CreatedAt:   time.Date(2026, time.March, 6, 12, 0, 0, 0, time.UTC),
		ExecMode:    agentdomain.ModeReactive,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/agents/agent-memory-1", strings.NewReader(`{"memory_scope":"session"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	AgentDetailHandler(cfg, zerolog.Nop(), nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeResponseBody(t, rr)
	agentWrap, _ := payload["agent"].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(agentWrap["memory_scope"])); got != "session" {
		t.Fatalf("memory_scope=%q want session", got)
	}

	stored, err := store.Get(context.Background(), "agent-memory-1")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if stored.MemoryScope != agentdomain.MemoryScopeSession {
		t.Fatalf("stored memory_scope=%q want %q", stored.MemoryScope, agentdomain.MemoryScopeSession)
	}
}

func TestAgentPatchHandler_UpdatesMemoryRetention(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	resetAgentStreamRegistry()

	cfg := orchestrationTestConfig(t.TempDir())
	store, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("close agents store: %v", closeErr)
		}
	}()

	err = store.Create(context.Background(), agentdomain.Agent{
		ID:          "agent-retention-1",
		Namespace:   "ws-1",
		Name:        "Memory Retention Agent",
		Role:        "companion",
		SkillsAllow: []string{},
		Policy:      agentdomain.Policy{},
		ShareBB:     "scoped",
		State:       agentdomain.StateStopped,
		CreatedAt:   time.Date(2026, time.March, 6, 12, 0, 0, 0, time.UTC),
		ExecMode:    agentdomain.ModeReactive,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/agents/agent-retention-1", strings.NewReader(`{"memory_retention":"ephemeral"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	AgentDetailHandler(cfg, zerolog.Nop(), nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeResponseBody(t, rr)
	agentWrap, _ := payload["agent"].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(agentWrap["memory_retention"])); got != "ephemeral" {
		t.Fatalf("memory_retention=%q want ephemeral", got)
	}

	stored, err := store.Get(context.Background(), "agent-retention-1")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if stored.MemoryRetention != agentdomain.MemoryRetentionEphemeral {
		t.Fatalf("stored memory_retention=%q want %q", stored.MemoryRetention, agentdomain.MemoryRetentionEphemeral)
	}
}

func TestPrepareSandboxBackedSpawn_OpenSandbox(t *testing.T) {
	var deleteCalls int

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sandboxes", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("OPEN-SANDBOX-API-KEY"); got != "test-key" {
			t.Fatalf("api key header=%q", got)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("create method=%s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "sbx-prepare-1",
			"status": map[string]any{
				"state": "Pending",
			},
		})
	})
	mux.HandleFunc("/v1/sandboxes/sbx-prepare-1/endpoints/44772", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"endpoint": r.Host + "/sandboxes/sbx-prepare-1/proxy/44772",
			"headers": map[string]string{
				"X-EXECD-ACCESS-TOKEN": "execd-token",
			},
		})
	})
	mux.HandleFunc("/sandboxes/sbx-prepare-1/proxy/44772/command", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"stdout\",\"text\":\"clone ok\"}\n\n"))
	})
	mux.HandleFunc("/v1/sandboxes/sbx-prepare-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteCalls++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Fatalf("unexpected method for sandbox detail: %s", r.Method)
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-key")

	prepared, err := prepareSandboxBackedSpawn(context.Background(), AgentSpawnRequest{
		SandboxProvider: "opensandbox",
		RepoURL:         "https://github.com/example/repo.git",
		RepoRef:         "main",
		AllowEgress:     []string{"api.github.com"},
	})
	if err != nil {
		t.Fatalf("prepareSandboxBackedSpawn() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("prepareSandboxBackedSpawn() returned nil")
		return
	}
	if prepared.workspaceSource != "sandbox" {
		t.Fatalf("workspaceSource=%q", prepared.workspaceSource)
	}
	if prepared.workspaceRoot != "/workspace/repo" {
		t.Fatalf("workspaceRoot=%q", prepared.workspaceRoot)
	}
	if prepared.workspaceID != "sandbox-sbx-prepare-1" {
		t.Fatalf("workspaceID=%q", prepared.workspaceID)
	}
	if prepared.sandboxID != "sbx-prepare-1" {
		t.Fatalf("sandboxID=%q", prepared.sandboxID)
	}
	prepared.cleanup(context.Background())
	if deleteCalls != 1 {
		t.Fatalf("deleteCalls=%d want 1", deleteCalls)
	}

	prepared2, err := prepareSandboxBackedSpawn(context.Background(), AgentSpawnRequest{
		SandboxProvider: "opensandbox",
		RepoURL:         "https://github.com/example/repo.git",
		RepoRef:         "main",
	})
	if err != nil {
		t.Fatalf("prepareSandboxBackedSpawn() second call error = %v", err)
	}
	prepared2.release()
	prepared2.cleanup(context.Background())
	if deleteCalls != 1 {
		t.Fatalf("deleteCalls after release=%d want 1", deleteCalls)
	}
}

func TestPrepareSandboxBackedSpawn_RequiresRepoURL(t *testing.T) {
	prepared, err := prepareSandboxBackedSpawn(context.Background(), AgentSpawnRequest{
		SandboxProvider: "opensandbox",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if prepared != nil {
		t.Fatalf("prepared=%v want nil", prepared)
	}
}

func TestAgentAskStreamHandler_AcceptsAndPublishesStartedEvent(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	resetAgentStreamRegistry()

	cfg := orchestrationTestConfig(t.TempDir())
	store, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("close agents store: %v", closeErr)
		}
	}()

	err = store.Create(context.Background(), agentdomain.Agent{
		ID:          "agent-stream-1",
		Namespace:   "ws-stream",
		Name:        "Streaming Agent",
		Role:        "companion",
		SkillsAllow: []string{},
		Policy:      agentdomain.Policy{},
		ShareBB:     "scoped",
		State:       agentdomain.StateRunning,
		CreatedAt:   time.Date(2026, time.March, 6, 12, 0, 0, 0, time.UTC),
		ExecMode:    agentdomain.ModeReactive,
		MemoryScope: agentdomain.MemoryScopeSession,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	pub := &testAgentEventPublisher{}
	req := httptest.NewRequest(http.MethodPost, "/api/agents/agent-stream-1/ask-stream", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	AgentDetailHandler(cfg, zerolog.Nop(), pub).ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeResponseBody(t, rr)
	if accepted, _ := payload["accepted"].(bool); !accepted {
		t.Fatalf("accepted=%v want true", payload["accepted"])
	}
	if got := strings.TrimSpace(fmt.Sprint(payload["agent_id"])); got != "agent-stream-1" {
		t.Fatalf("agent_id=%q want agent-stream-1", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(payload["conversation_id"])); got != "agent-stream-1" {
		t.Fatalf("conversation_id=%q want agent-stream-1", got)
	}

	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.types) == 0 || pub.types[0] != "agent.chat" {
		t.Fatalf("published types=%v want first agent.chat", pub.types)
	}
	if len(pub.events) == 0 {
		t.Fatalf("expected started event to be published")
	}
	if pub.events[0].Phase != "started" {
		t.Fatalf("first phase=%q want started", pub.events[0].Phase)
	}
	if got := fmt.Sprint(pub.events[0].Metadata["memory_scope"]); got != "session" {
		t.Fatalf("memory_scope metadata=%q want session", got)
	}
	if got := fmt.Sprint(pub.events[0].Metadata["memory_retention"]); got != "task" {
		t.Fatalf("memory_retention metadata=%q want task", got)
	}
}

func TestAgentAskStreamHandler_SandboxBackedCancelPublishesCancelledEvent(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	resetAgentStreamRegistry()

	commandStarted := make(chan struct{}, 1)
	cancelObserved := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sandboxes/sbx-stream-1/endpoints/44772", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"endpoint": r.Host + "/sandboxes/sbx-stream-1/proxy/44772",
			"headers": map[string]string{
				"X-EXECD-ACCESS-TOKEN": "execd-token",
			},
		})
	})
	mux.HandleFunc("/sandboxes/sbx-stream-1/proxy/44772/command", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case commandStarted <- struct{}{}:
		default:
		}
		<-r.Context().Done()
		select {
		case cancelObserved <- struct{}{}:
		default:
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-key")

	cfg := orchestrationTestConfig(t.TempDir())
	store, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agents store: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("close agents store: %v", closeErr)
		}
	}()

	err = store.Create(context.Background(), agentdomain.Agent{
		ID:              "agent-sandbox-stream-1",
		Namespace:       "ws-stream",
		Name:            "Sandbox Streaming Agent",
		Role:            "companion",
		SkillsAllow:     []string{},
		Policy:          agentdomain.Policy{Timeout: "1m"},
		ShareBB:         "scoped",
		State:           agentdomain.StateRunning,
		CreatedAt:       time.Date(2026, time.March, 6, 12, 0, 0, 0, time.UTC),
		ExecMode:        agentdomain.ModeReactive,
		MemoryScope:     agentdomain.MemoryScopeSession,
		MemoryRetention: agentdomain.MemoryRetentionTask,
		WorkspaceSource: "sandbox",
		SandboxProvider: "opensandbox",
		SandboxID:       "sbx-stream-1",
		RepoURL:         "https://github.com/example/repo.git",
		RepoRef:         "main",
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	pub := &testAgentEventPublisher{}
	req := httptest.NewRequest(http.MethodPost, "/api/agents/agent-sandbox-stream-1/ask-stream", strings.NewReader(`{"message":"hello from sandbox"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	AgentDetailHandler(cfg, zerolog.Nop(), pub).ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeResponseBody(t, rr)
	correlationID := strings.TrimSpace(fmt.Sprint(payload["correlation_id"]))
	if correlationID == "" {
		t.Fatal("expected correlation_id")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		activeAgentStreams.mu.Lock()
		children := activeAgentStreams.inflight["agent-sandbox-stream-1"]
		_, ok := children[correlationID]
		activeAgentStreams.mu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sandbox-backed stream was not registered for cancellation")
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case <-commandStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected sandbox exec request to start")
	}

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/agents/agent-sandbox-stream-1/ask-stream/cancel", strings.NewReader(fmt.Sprintf(`{"correlation_id":%q}`, correlationID)))
	cancelReq.Header.Set("Content-Type", "application/json")
	cancelRR := httptest.NewRecorder()
	AgentDetailHandler(cfg, zerolog.Nop(), pub).ServeHTTP(cancelRR, cancelReq)
	if cancelRR.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", cancelRR.Code, cancelRR.Body.String())
	}

	select {
	case <-cancelObserved:
	case <-time.After(2 * time.Second):
		t.Fatal("expected sandbox exec request to observe cancellation")
	}

	deadline = time.Now().Add(2 * time.Second)
	for {
		pub.mu.Lock()
		events := append([]agentChatEvent(nil), pub.events...)
		pub.mu.Unlock()
		if len(events) >= 2 {
			if events[0].Phase != "started" {
				t.Fatalf("first phase=%q want started", events[0].Phase)
			}
			if got := fmt.Sprint(events[0].Metadata["workspace_source"]); got != "sandbox" {
				t.Fatalf("workspace_source metadata=%q want sandbox", got)
			}
			if got := fmt.Sprint(events[0].Metadata["sandbox_provider"]); got != "opensandbox" {
				t.Fatalf("sandbox_provider metadata=%q want opensandbox", got)
			}
			if got := fmt.Sprint(events[0].Metadata["sandbox_id"]); got != "sbx-stream-1" {
				t.Fatalf("sandbox_id metadata=%q want sbx-stream-1", got)
			}
			if got := fmt.Sprint(events[0].Metadata["memory_scope"]); got != "session" {
				t.Fatalf("memory_scope metadata=%q want session", got)
			}
			if got := fmt.Sprint(events[1].Phase); got != "cancelled" {
				t.Fatalf("second phase=%q want cancelled", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected cancelled event, got %d events", len(events))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAgentAskStreamCancelHandler_CancelsRegisteredCorrelation(t *testing.T) {
	resetAgentStreamRegistry()

	cancelled := make(chan struct{}, 1)
	activeAgentStreams.Put("agent-cancel-1", "corr-cancel-1", func() {
		select {
		case cancelled <- struct{}{}:
		default:
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/agents/agent-cancel-1/ask-stream/cancel", strings.NewReader(`{"correlation_id":"corr-cancel-1"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	AgentDetailHandler(orchestrationTestConfig(t.TempDir()), zerolog.Nop(), nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	select {
	case <-cancelled:
	default:
		t.Fatal("expected cancel func to be called")
	}

	payload := decodeResponseBody(t, rr)
	if got := strings.TrimSpace(fmt.Sprint(payload["cancelled"])); got != "1" {
		t.Fatalf("cancelled=%q want 1", got)
	}
}

func TestAgentMemoryPolicyHelpers_VaryByRetention(t *testing.T) {
	companionCfg := agentMemoryConfig(agentdomain.Agent{
		MemoryRetention: agentdomain.MemoryRetentionCompanion,
	})
	taskCfg := agentMemoryConfig(agentdomain.Agent{
		MemoryRetention: agentdomain.MemoryRetentionTask,
	})
	ephemeralCfg := agentMemoryConfig(agentdomain.Agent{
		MemoryRetention: agentdomain.MemoryRetentionEphemeral,
	})

	if companionCfg.TotalTokenBudget <= taskCfg.TotalTokenBudget {
		t.Fatalf("companion total budget=%d want greater than task=%d", companionCfg.TotalTokenBudget, taskCfg.TotalTokenBudget)
	}
	if taskCfg.TotalTokenBudget <= ephemeralCfg.TotalTokenBudget {
		t.Fatalf("task total budget=%d want greater than ephemeral=%d", taskCfg.TotalTokenBudget, ephemeralCfg.TotalTokenBudget)
	}
	if !defaultDistillForAgent(agentdomain.Agent{MemoryRetention: agentdomain.MemoryRetentionCompanion}) {
		t.Fatal("companion retention should distill by default")
	}
	if defaultDistillForAgent(agentdomain.Agent{MemoryRetention: agentdomain.MemoryRetentionTask}) {
		t.Fatal("task retention should not distill by default")
	}
	if defaultDistillForAgent(agentdomain.Agent{MemoryRetention: agentdomain.MemoryRetentionEphemeral}) {
		t.Fatal("ephemeral retention should not distill by default")
	}
}
