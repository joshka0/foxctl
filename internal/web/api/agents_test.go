package api

import (
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
	v2jido "github.com/jkatigb/agentctl/internal/v2/adapters/jido"
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
