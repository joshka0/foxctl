package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/runtime/observability"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	v2jido "github.com/jkatigb/agentctl/internal/v2/adapters/jido"
	libsqlworkers "github.com/jkatigb/agentctl/internal/v2/adapters/libsql/workers"
	coreevents "github.com/jkatigb/agentctl/internal/v2/core/events"
	coreorchestration "github.com/jkatigb/agentctl/internal/v2/core/orchestration"
	corespawn "github.com/jkatigb/agentctl/internal/v2/core/spawn"
	coreworker "github.com/jkatigb/agentctl/internal/v2/core/worker"
)

func TestOrchestrationBoardGetHandler_ReturnsEnvelope(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	seedOrchestrationCards(t, cfg, 2, 12)

	h := OrchestrationBoardGetHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&limit=10", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	if body["status"] != "ok" {
		t.Fatalf("status field=%v want ok", body["status"])
	}
	if body["command"] != commandOrchestrationBoardGet {
		t.Fatalf("command=%v want %q", body["command"], commandOrchestrationBoardGet)
	}
	errObj, _ := body["error"].(map[string]any)
	if _, ok := errObj["code"]; !ok || errObj["code"] != nil {
		t.Fatalf("error.code=%v want nil", errObj["code"])
	}
	if _, ok := errObj["message"]; !ok || errObj["message"] != nil {
		t.Fatalf("error.message=%v want nil", errObj["message"])
	}

	data, _ := body["data"].(map[string]any)
	lanes, _ := data["lanes"].([]any)
	if len(lanes) == 0 {
		t.Fatalf("lanes empty in board response: %v", data)
	}
}

func TestOrchestrationBoardGetHandler_EmitsSSEOrchestrationMetadata(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	seedOrchestrationCards(t, cfg, 2, 16)

	pub := &captureSSEPublisher{}
	observability.SetSSEPublisher(pub)
	t.Cleanup(func() {
		observability.SetSSEPublisher(nil)
	})

	h := OrchestrationBoardGetHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&request_id=req-board-sse-001&limit=10&lane=Running", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(pub.calls) == 0 {
		t.Fatal("expected at least one SSE publish call")
	}
	call := pub.calls[len(pub.calls)-1]
	activity, ok := call.data.(observability.ActivityEvent)
	if !ok {
		t.Fatalf("activity payload type=%T want observability.ActivityEvent", call.data)
	}
	if activity.Operation != opWebOrchestrationBoardGet {
		t.Fatalf("operation=%q want %q", activity.Operation, opWebOrchestrationBoardGet)
	}
	if strings.TrimSpace(activity.TraceID) == "" {
		t.Fatal("trace_id should be present on emitted SSE activity event")
	}
	if activity.Data == nil {
		t.Fatal("activity data should be present")
	}
	if got := strings.TrimSpace(fmt.Sprint(activity.Data["request_id"])); got != "req-board-sse-001" {
		t.Fatalf("request_id=%q want req-board-sse-001", got)
	}
	if _, ok := activity.Data["card_count"]; !ok {
		t.Fatalf("card_count missing from activity data: %+v", activity.Data)
	}
}

func TestOrchestrationBoardCardGetHandler_ReturnsCard(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	seedOrchestrationCards(t, cfg, 1, 16)

	h := OrchestrationBoardCardGetHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-card-get?workspace_id=ws-1&issue_id=issue-001", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	if body["command"] != commandOrchestrationBoardCardGet {
		t.Fatalf("command=%v want %q", body["command"], commandOrchestrationBoardCardGet)
	}
	data, _ := body["data"].(map[string]any)
	cardWrap, _ := data["card"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(cardWrap["issue_id"])) != "issue-001" {
		t.Fatalf("issue_id=%v want issue-001", cardWrap["issue_id"])
	}
}

func TestOrchestrationBoardCardGetHandler_IncludeRuntimeReturnsLiveState(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	server, socketPath := startOrchestrationJSONRPCServer(t, func(method string, params json.RawMessage) (any, *jsonrpcTestError) {
		switch method {
		case v2jido.MethodRuntimeState:
			return map[string]any{
				"agent_id": "agent:worker-runtime-1",
				"status":   "ok",
				"state": map[string]any{
					"agentctl": map[string]any{
						"status": "running",
					},
				},
			}, nil
		case v2jido.MethodRuntimeGetChildren:
			return map[string]any{
				"agent_id": "agent:worker-runtime-1",
				"children": map[string]any{
					"agent:child-runtime-1": map[string]any{
						"tag":      "agent:child-runtime-1",
						"agent_id": "agent:child-runtime-1",
					},
				},
			}, nil
		default:
			return nil, &jsonrpcTestError{Code: -32601, Message: "unknown method"}
		}
	})
	t.Cleanup(func() { _ = server.Close() })
	t.Setenv(v2jido.EnvJidoSocketPath, socketPath)

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()
	store, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	evt := coreevents.Event{
		ID:            "evt-runtime-card-001",
		StreamID:      "run-runtime-001",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.March, 6, 10, 0, 0, 0, time.UTC),
		Command:       commandOrchestrationDispatchIssue,
		RequestID:     "req-runtime-card-001",
		ActorID:       "actor:system:overseer",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-runtime-001",
			"issue_identifier": "ABC-RUNTIME-1",
			"title":            "Inspect runtime",
			"state":            "Running",
			"eligibility":      "eligible",
			"agent_id":         "agent:worker-runtime-1",
		}),
	}
	if err := store.Apply(ctx, evt); err != nil {
		t.Fatalf("apply runtime card event: %v", err)
	}

	h := OrchestrationBoardCardGetHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-card-get?workspace_id=ws-1&issue_id=issue-runtime-001&include_runtime=true", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	data, _ := body["data"].(map[string]any)
	runtimeWrap, _ := data["runtime"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(runtimeWrap["agent_id"])) != "agent:worker-runtime-1" {
		t.Fatalf("runtime.agent_id=%v want agent:worker-runtime-1", runtimeWrap["agent_id"])
	}
	if strings.TrimSpace(fmt.Sprint(runtimeWrap["status"])) != "ok" {
		t.Fatalf("runtime.status=%v want ok", runtimeWrap["status"])
	}
	children, _ := runtimeWrap["children"].(map[string]any)
	if _, ok := children["agent:child-runtime-1"]; !ok {
		t.Fatalf("runtime children missing child entry: %+v", children)
	}
}

func TestOrchestrationBoardCardRuntimeGetHandler_ReturnsRuntimeTree(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

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
			case "agent:root-tree":
				return map[string]any{
					"agent_id": agentID,
					"children": map[string]any{
						"agent:child-tree-1": map[string]any{
							"tag":      "agent:child-tree-1",
							"agent_id": "agent:child-tree-1",
						},
					},
				}, nil
			case "agent:child-tree-1":
				return map[string]any{
					"agent_id": agentID,
					"children": map[string]any{
						"agent:grandchild-tree-1": map[string]any{
							"tag":      "agent:grandchild-tree-1",
							"agent_id": "agent:grandchild-tree-1",
						},
					},
				}, nil
			default:
				return map[string]any{"agent_id": agentID, "children": map[string]any{}}, nil
			}
		default:
			return nil, &jsonrpcTestError{Code: -32601, Message: "unknown method"}
		}
	})
	t.Cleanup(func() { _ = server.Close() })
	t.Setenv(v2jido.EnvJidoSocketPath, socketPath)

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()
	store, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	evt := coreevents.Event{
		ID:            "evt-runtime-tree-001",
		StreamID:      "run-runtime-tree-001",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.March, 6, 10, 30, 0, 0, time.UTC),
		Command:       commandOrchestrationDispatchIssue,
		RequestID:     "req-runtime-tree-001",
		ActorID:       "actor:system:overseer",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-runtime-tree-001",
			"issue_identifier": "ABC-RUNTIME-TREE-1",
			"title":            "Inspect runtime tree",
			"state":            "Running",
			"eligibility":      "eligible",
			"agent_id":         "agent:root-tree",
		}),
	}
	if err := store.Apply(ctx, evt); err != nil {
		t.Fatalf("apply runtime tree card event: %v", err)
	}

	h := OrchestrationBoardCardRuntimeGetHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-card-runtime-get?workspace_id=ws-1&issue_id=issue-runtime-tree-001&depth=2", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	data, _ := body["data"].(map[string]any)
	runtimeWrap, _ := data["runtime"].(map[string]any)
	root, _ := runtimeWrap["root"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(root["agent_id"])) != "agent:root-tree" {
		t.Fatalf("root.agent_id=%v want agent:root-tree", root["agent_id"])
	}
	children, _ := root["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("root children=%d want 1", len(children))
	}
	child, _ := children[0].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(child["agent_id"])) != "agent:child-tree-1" {
		t.Fatalf("child.agent_id=%v want agent:child-tree-1", child["agent_id"])
	}
	grandchildren, _ := child["children"].([]any)
	if len(grandchildren) != 1 {
		t.Fatalf("grandchildren=%d want 1", len(grandchildren))
	}
	grandchild, _ := grandchildren[0].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(grandchild["agent_id"])) != "agent:grandchild-tree-1" {
		t.Fatalf("grandchild.agent_id=%v want agent:grandchild-tree-1", grandchild["agent_id"])
	}
}

func TestOrchestrationBoardCardGetHandler_ReturnsGoRuntimeSummary(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")
	t.Setenv(EnvOrchestrationRuntimeBackend, orchestrationRuntimeBackendGoruntimeAPI)

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()
	store, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-runtime-go-001",
		StreamID:      "run-runtime-go-001",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.April, 6, 12, 0, 0, 0, time.UTC),
		Command:       commandOrchestrationDispatchIssue,
		RequestID:     "req-runtime-go-001",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-runtime-go-001",
			"issue_identifier": "ABC-RUNTIME-GO-1",
			"title":            "Inspect go runtime",
			"state":            "Running",
			"eligibility":      "eligible",
			"agent_id":         "agent:worker-runtime-go-1",
		}),
	}); err != nil {
		t.Fatalf("apply runtime card event: %v", err)
	}

	workerStore, closeWorkers, err := libsqlworkers.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open worker store: %v", err)
	}
	defer func() { _ = closeWorkers() }()
	if err := workerStore.Upsert(ctx, coreworker.Record{
		WorkerID:      "subprocess:agent:worker-runtime-go-1",
		BackendKind:   coreworker.BackendSubprocess,
		AgentID:       "agent:worker-runtime-go-1",
		ParentAgentID: "agent:dispatch-root",
		RunID:         "run-runtime-go-001",
		Status:        coreworker.StatusRunning,
		RawState:      json.RawMessage(`{"agentctl":{"status":"running","agent":"agent:worker-runtime-go-1"}}`),
	}); err != nil {
		t.Fatalf("upsert root worker: %v", err)
	}
	if err := workerStore.Upsert(ctx, coreworker.Record{
		WorkerID:       "subprocess:agent:child-runtime-go-1",
		BackendKind:    coreworker.BackendSubprocess,
		AgentID:        "agent:child-runtime-go-1",
		ParentAgentID:  "agent:worker-runtime-go-1",
		ParentWorkerID: "subprocess:agent:worker-runtime-go-1",
		RunID:          "run-runtime-go-001",
		Status:         coreworker.StatusRunning,
		Metadata:       map[string]any{"issue_id": "issue-runtime-go-001", "workspace_id": "ws-1"},
	}); err != nil {
		t.Fatalf("upsert child worker: %v", err)
	}

	h := OrchestrationBoardCardGetHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-card-get?workspace_id=ws-1&issue_id=issue-runtime-go-001&include_runtime=true", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	data, _ := body["data"].(map[string]any)
	runtimeWrap, _ := data["runtime"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(runtimeWrap["agent_id"])) != "agent:worker-runtime-go-1" {
		t.Fatalf("runtime.agent_id=%v want agent:worker-runtime-go-1", runtimeWrap["agent_id"])
	}
	if strings.TrimSpace(fmt.Sprint(runtimeWrap["status"])) != "running" {
		t.Fatalf("runtime.status=%v want running", runtimeWrap["status"])
	}
	children, _ := runtimeWrap["children"].(map[string]any)
	if _, ok := children["agent:child-runtime-go-1"]; !ok {
		t.Fatalf("runtime children missing child entry: %+v", children)
	}
}

func TestOrchestrationBoardCardRuntimeGetHandler_ReturnsGoRuntimeTree(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")
	t.Setenv(EnvOrchestrationRuntimeBackend, orchestrationRuntimeBackendGoruntimeAPI)

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()
	store, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-runtime-go-tree-001",
		StreamID:      "run-runtime-go-tree-001",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.April, 6, 12, 5, 0, 0, time.UTC),
		Command:       commandOrchestrationDispatchIssue,
		RequestID:     "req-runtime-go-tree-001",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-runtime-go-tree-001",
			"issue_identifier": "ABC-RUNTIME-GO-TREE-1",
			"title":            "Inspect go runtime tree",
			"state":            "Running",
			"eligibility":      "eligible",
			"agent_id":         "agent:worker-runtime-go-tree-1",
		}),
	}); err != nil {
		t.Fatalf("apply runtime card event: %v", err)
	}

	workerStore, closeWorkers, err := libsqlworkers.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open worker store: %v", err)
	}
	defer func() { _ = closeWorkers() }()
	if err := workerStore.Upsert(ctx, coreworker.Record{
		WorkerID:    "subprocess:agent:worker-runtime-go-tree-1",
		BackendKind: coreworker.BackendSubprocess,
		AgentID:     "agent:worker-runtime-go-tree-1",
		RunID:       "run-runtime-go-tree-001",
		Status:      coreworker.StatusRunning,
		RawState:    json.RawMessage(`{"agentctl":{"status":"running","agent":"agent:worker-runtime-go-tree-1"}}`),
	}); err != nil {
		t.Fatalf("upsert root worker: %v", err)
	}
	if err := workerStore.Upsert(ctx, coreworker.Record{
		WorkerID:       "subprocess:agent:child-runtime-go-tree-1",
		BackendKind:    coreworker.BackendSubprocess,
		AgentID:        "agent:child-runtime-go-tree-1",
		ParentAgentID:  "agent:worker-runtime-go-tree-1",
		ParentWorkerID: "subprocess:agent:worker-runtime-go-tree-1",
		RunID:          "run-runtime-go-tree-001",
		Status:         coreworker.StatusCompleted,
		RawState:       json.RawMessage(`{"agentctl":{"status":"completed","agent":"agent:child-runtime-go-tree-1"}}`),
	}); err != nil {
		t.Fatalf("upsert child worker: %v", err)
	}

	h := OrchestrationBoardCardRuntimeGetHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-card-runtime-get?workspace_id=ws-1&issue_id=issue-runtime-go-tree-001&depth=2", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	data, _ := body["data"].(map[string]any)
	runtimeWrap, _ := data["runtime"].(map[string]any)
	root, _ := runtimeWrap["root"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(root["agent_id"])) != "agent:worker-runtime-go-tree-1" {
		t.Fatalf("root.agent_id=%v want agent:worker-runtime-go-tree-1", root["agent_id"])
	}
	children, _ := root["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("root children=%d want 1", len(children))
	}
	child, _ := children[0].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(child["agent_id"])) != "agent:child-runtime-go-tree-1" {
		t.Fatalf("child.agent_id=%v want agent:child-runtime-go-tree-1", child["agent_id"])
	}
	if strings.TrimSpace(fmt.Sprint(child["status"])) != "completed" {
		t.Fatalf("child.status=%v want completed", child["status"])
	}
}

func TestOrchestrationBoardCardGetHandler_EmitsSSEOrchestrationMetadata(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()
	store, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	evt := coreevents.Event{
		ID:            "evt-sse-card-001",
		StreamID:      "run-sse-card-001",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.March, 5, 14, 0, 0, 0, time.UTC),
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-seed-sse-001",
		ActorID:       "actor:system:overseer",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-sse-001",
			"issue_identifier": "ABC-SSE-1",
			"title":            "SSE card",
			"state":            "Running",
			"eligibility":      "ineligible",
			"policy_status":    "denied",
			"last_outcome":     "policy_denied",
		}),
	}
	if err := store.Apply(ctx, evt); err != nil {
		t.Fatalf("apply orchestration card seed event: %v", err)
	}

	pub := &captureSSEPublisher{}
	observability.SetSSEPublisher(pub)
	t.Cleanup(func() {
		observability.SetSSEPublisher(nil)
	})

	h := OrchestrationBoardCardGetHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-card-get?workspace_id=ws-1&issue_id=issue-sse-001&request_id=req-card-sse-001", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(pub.calls) == 0 {
		t.Fatal("expected at least one SSE publish call")
	}
	call := pub.calls[len(pub.calls)-1]
	if call.eventType != "activity" {
		t.Fatalf("eventType=%q want activity", call.eventType)
	}
	activity, ok := call.data.(observability.ActivityEvent)
	if !ok {
		t.Fatalf("activity payload type=%T want observability.ActivityEvent", call.data)
	}
	if strings.TrimSpace(activity.TraceID) == "" {
		t.Fatal("trace_id should be present on emitted SSE activity event")
	}
	if activity.Data == nil {
		t.Fatal("activity data should be present")
	}
	if got := strings.TrimSpace(fmt.Sprint(activity.Data["request_id"])); got != "req-card-sse-001" {
		t.Fatalf("request_id=%q want req-card-sse-001", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(activity.Data["lane"])); got == "" {
		t.Fatalf("lane should be present in activity data: %+v", activity.Data)
	}
	if got := strings.TrimSpace(fmt.Sprint(activity.Data["last_outcome"])); got != "policy_denied" {
		t.Fatalf("last_outcome=%q want policy_denied", got)
	}
}

func TestOrchestrationDispatchIssueHandler_SpawnsJidoChildAndProjectsRunningCard(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	server, socketPath := startOrchestrationJSONRPCServer(t, func(method string, params json.RawMessage) (any, *jsonrpcTestError) {
		switch method {
		case v2jido.MethodRuntimeSpawnChild:
			return map[string]any{
				"agent_id":   "agent:dispatch-root",
				"message_id": "msg-dispatch-001",
				"signal_id":  "sig-dispatch-001",
				"status":     "spawned",
				"data": map[string]any{
					"child": map[string]any{
						"tag":      "agent:worker-dispatch-1",
						"agent_id": "agent:worker-dispatch-1",
					},
				},
			}, nil
		default:
			return nil, &jsonrpcTestError{Code: -32601, Message: "unknown method"}
		}
	})
	t.Cleanup(func() { _ = server.Close() })
	t.Setenv(v2jido.EnvJidoSocketPath, socketPath)
	t.Setenv(v2jido.EnvJidoOrchestrationParentAgentIDs, "agent:dispatch-root")
	t.Setenv(v2jido.EnvJidoOrchestrationDispatchParentAgentID, "agent:dispatch-root")

	cfg := orchestrationTestConfig(t.TempDir())
	h := OrchestrationDispatchIssueHandler(cfg, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/orchestration/dispatch-issue", bytes.NewBufferString(`{
		"request_id":"req-dispatch-001",
		"workspace_id":"ws-1",
		"issue_id":"issue-dispatch-001",
		"issue_identifier":"ABC-DISPATCH-1",
		"title":"Dispatch issue"
	}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	data, _ := body["data"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(data["status"])) != "dispatched" {
		t.Fatalf("dispatch status=%v want dispatched", data["status"])
	}
	if strings.TrimSpace(fmt.Sprint(data["agent_id"])) != "agent:worker-dispatch-1" {
		t.Fatalf("dispatch agent_id=%v want agent:worker-dispatch-1", data["agent_id"])
	}

	ctx := context.Background()
	store, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	card, err := store.Card(ctx, coreorchestration.CardRequest{
		WorkspaceID: "ws-1",
		IssueID:     "issue-dispatch-001",
	})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.Lane != coreorchestration.LaneRunning {
		t.Fatalf("lane=%q want %q", card.Card.Lane, coreorchestration.LaneRunning)
	}
	if card.Card.AgentID != "agent:worker-dispatch-1" {
		t.Fatalf("agent_id=%q want agent:worker-dispatch-1", card.Card.AgentID)
	}
}

func TestOrchestrationCardActionHandler_ReleaseMovesDoneCardBackToTodo(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()
	store, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-card-action-001",
		StreamID:      "run-card-action-001",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunCompleted,
		OccurredAt:    time.Date(2026, time.March, 6, 11, 0, 0, 0, time.UTC),
		Command:       commandOrchestrationDispatchIssue,
		RequestID:     "req-card-action-seed-001",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-card-action-001",
			"issue_identifier": "ABC-ACTION-1",
			"title":            "Release done card",
			"state":            "Released",
			"eligibility":      "eligible",
			"policy_status":    "ok",
			"tracker_state":    "Done",
			"denial_reason":    "old error",
			"suggestion":       "old suggestion",
		}),
	}); err != nil {
		t.Fatalf("seed apply: %v", err)
	}

	h := OrchestrationCardActionHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodPost, "/api/orchestration/card-action", bytes.NewBufferString(`{
		"request_id":"req-card-action-001",
		"workspace_id":"ws-1",
		"issue_id":"issue-card-action-001",
		"action":"release"
	}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	data, _ := body["data"].(map[string]any)
	cardWrap, _ := data["card"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(cardWrap["lane"])) != "Todo" {
		t.Fatalf("lane=%v want Todo", cardWrap["lane"])
	}
	if raw, ok := cardWrap["tracker_state"]; ok && strings.TrimSpace(fmt.Sprint(raw)) != "" {
		t.Fatalf("tracker_state=%v want empty/omitted", raw)
	}
	if raw, ok := cardWrap["denial_reason"]; ok && strings.TrimSpace(fmt.Sprint(raw)) != "" {
		t.Fatalf("denial_reason=%v want empty/omitted", raw)
	}
}

func TestOrchestrationCardActionHandler_RetryNowMovesBlockedCardToRetryQueue(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()
	store, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-card-action-002",
		StreamID:      "run-card-action-002",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunFailed,
		OccurredAt:    time.Date(2026, time.March, 6, 11, 10, 0, 0, time.UTC),
		Command:       commandOrchestrationDispatchIssue,
		RequestID:     "req-card-action-seed-002",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-card-action-002",
			"issue_identifier": "ABC-ACTION-2",
			"title":            "Retry blocked card",
			"state":            "Released",
			"eligibility":      "ineligible",
			"policy_status":    "blocked",
			"last_outcome":     "execution_failed",
			"denial_reason":    "connection refused",
			"suggestion":       "retry later",
		}),
	}); err != nil {
		t.Fatalf("seed apply: %v", err)
	}

	h := OrchestrationCardActionHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodPost, "/api/orchestration/card-action", bytes.NewBufferString(`{
		"request_id":"req-card-action-002",
		"workspace_id":"ws-1",
		"issue_id":"issue-card-action-002",
		"action":"retry-now"
	}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	data, _ := body["data"].(map[string]any)
	cardWrap, _ := data["card"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(cardWrap["lane"])) != "RetryQueued" {
		t.Fatalf("lane=%v want RetryQueued", cardWrap["lane"])
	}
	if strings.TrimSpace(fmt.Sprint(cardWrap["policy_status"])) != "ok" {
		t.Fatalf("policy_status=%v want ok", cardWrap["policy_status"])
	}
	if strings.TrimSpace(fmt.Sprint(cardWrap["retry_due_at"])) == "" {
		t.Fatal("retry_due_at should be populated")
	}
}

func TestOrchestrationRefreshHandler_RequiresRequestID(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	h := OrchestrationRefreshHandler(cfg, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/orchestration/refresh", bytes.NewBufferString(`{"workspace_id":"ws-1"}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	if body["status"] != "error" {
		t.Fatalf("status field=%v want error", body["status"])
	}
	errObj, _ := body["error"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(errObj["code"])) != "EARG" {
		t.Fatalf("error.code=%v want EARG", errObj["code"])
	}
}

func TestOrchestrationRefreshHandler_IdempotentDuplicate(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	h := OrchestrationRefreshHandler(cfg, zerolog.Nop())

	body := `{"request_id":"req-refresh-001","workspace_id":"ws-1"}`
	req1 := httptest.NewRequest(http.MethodPost, "/api/orchestration/refresh", bytes.NewBufferString(body))
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", rr1.Code, rr1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/orchestration/refresh", bytes.NewBufferString(body))
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", rr2.Code, rr2.Body.String())
	}

	first := decodeResponseBody(t, rr1)
	second := decodeResponseBody(t, rr2)
	firstData, _ := first["data"].(map[string]any)
	secondData, _ := second["data"].(map[string]any)

	if queued, _ := firstData["queued"].(bool); !queued {
		t.Fatalf("first queued=%v want true", firstData["queued"])
	}
	if queued, _ := secondData["queued"].(bool); !queued {
		t.Fatalf("second queued=%v want true", secondData["queued"])
	}
	if idempotent, _ := secondData["idempotent"].(bool); !idempotent {
		t.Fatalf("second idempotent=%v want true", secondData["idempotent"])
	}
}

func TestOrchestrationRefreshHandler_EmitsSSEOrchestrationMetadata(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	pub := &captureSSEPublisher{}
	observability.SetSSEPublisher(pub)
	t.Cleanup(func() {
		observability.SetSSEPublisher(nil)
	})

	h := OrchestrationRefreshHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodPost, "/api/orchestration/refresh", bytes.NewBufferString(`{"request_id":"req-refresh-sse-001","workspace_id":"ws-1"}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(pub.calls) == 0 {
		t.Fatal("expected at least one SSE publish call")
	}
	call := pub.calls[len(pub.calls)-1]
	activity, ok := call.data.(observability.ActivityEvent)
	if !ok {
		t.Fatalf("activity payload type=%T want observability.ActivityEvent", call.data)
	}
	if activity.Operation != opWebOrchestrationRefresh {
		t.Fatalf("operation=%q want %q", activity.Operation, opWebOrchestrationRefresh)
	}
	if strings.TrimSpace(activity.TraceID) == "" {
		t.Fatal("trace_id should be present on emitted SSE activity event")
	}
	if activity.Data == nil {
		t.Fatal("activity data should be present")
	}
	if got := strings.TrimSpace(fmt.Sprint(activity.Data["request_id"])); got != "req-refresh-sse-001" {
		t.Fatalf("request_id=%q want req-refresh-sse-001", got)
	}
	if _, ok := activity.Data["queued"]; !ok {
		t.Fatalf("queued missing from activity data: %+v", activity.Data)
	}
}

func TestOrchestrationDispatchIssueHandlerWithRuntime_UsesInjectedHost(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	host := &fakeOrchestrationRuntimeHost{
		spawnResp: corespawn.Response{
			RunID:     "run-host-1",
			AgentID:   "agent:host-1",
			ActorID:   "actor:host-1",
			RequestID: "req-host-1",
			Status:    "spawned",
			CreatedAt: time.Date(2026, time.April, 6, 22, 0, 0, 0, time.UTC),
		},
	}
	h := OrchestrationDispatchIssueHandlerWithRuntime(cfg, zerolog.Nop(), host)

	req := httptest.NewRequest(http.MethodPost, "/api/orchestration/dispatch-issue", bytes.NewBufferString(`{
		"request_id":"req-host-1",
		"workspace_id":"ws-1",
		"issue_id":"issue-host-1",
		"issue_identifier":"ABC-HOST-1",
		"title":"Dispatch via injected host",
		"parent_agent_id":"agent:parent-host"
	}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if host.spawnCalls != 1 {
		t.Fatalf("spawn_calls=%d want 1", host.spawnCalls)
	}
	if host.lastSpawnReq.ParentAgentID != "agent:parent-host" {
		t.Fatalf("parent_agent_id=%q want agent:parent-host", host.lastSpawnReq.ParentAgentID)
	}
	body := decodeResponseBody(t, rr)
	data, _ := body["data"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(data["run_id"])) != "run-host-1" {
		t.Fatalf("run_id=%v want run-host-1", data["run_id"])
	}
	if strings.TrimSpace(fmt.Sprint(data["agent_id"])) != "agent:host-1" {
		t.Fatalf("agent_id=%v want agent:host-1", data["agent_id"])
	}
}

func TestOrchestrationRefreshHandlerWithRuntime_UsesInjectedHost(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	host := &fakeOrchestrationRuntimeHost{}
	h := OrchestrationRefreshHandlerWithRuntime(cfg, zerolog.Nop(), host)

	req := httptest.NewRequest(http.MethodPost, "/api/orchestration/refresh", bytes.NewBufferString(`{"request_id":"req-refresh-host-1","workspace_id":"ws-1"}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if host.refreshCalls != 1 {
		t.Fatalf("refresh_calls=%d want 1", host.refreshCalls)
	}
	if host.lastRefreshWorkspaceID != "ws-1" {
		t.Fatalf("workspace_id=%q want ws-1", host.lastRefreshWorkspaceID)
	}
	if host.lastRefreshRequestID != "req-refresh-host-1" {
		t.Fatalf("request_id=%q want req-refresh-host-1", host.lastRefreshRequestID)
	}
}

func TestOrchestrationSeedCardsHandler_RequiresRequestID(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	h := OrchestrationSeedCardsHandler(cfg, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/orchestration/seed-cards", bytes.NewBufferString(`{"workspace_id":"ws-1","cards":[{"title":"Plan migration"}]}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	if body["status"] != "error" {
		t.Fatalf("status field=%v want error", body["status"])
	}
}

func TestOrchestrationSeedCardsHandler_CreatesProjectedCards(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	seedHandler := OrchestrationSeedCardsHandler(cfg, zerolog.Nop())

	seedReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/seed-cards", bytes.NewBufferString(`{
		"request_id":"req-seed-001",
		"workspace_id":"ws-1",
		"cards":[
			{"issue_identifier":"V2-1","title":"Define migration milestones"},
			{"title":"Implement orchestration board generation"}
		]
	}`))
	seedRR := httptest.NewRecorder()
	seedHandler.ServeHTTP(seedRR, seedReq)

	if seedRR.Code != http.StatusOK {
		t.Fatalf("seed status=%d body=%s", seedRR.Code, seedRR.Body.String())
	}
	seedBody := decodeResponseBody(t, seedRR)
	if seedBody["status"] != "ok" {
		t.Fatalf("seed status field=%v want ok", seedBody["status"])
	}
	seedData, _ := seedBody["data"].(map[string]any)
	if got := int(seedData["created"].(float64)); got != 2 {
		t.Fatalf("created=%d want 2", got)
	}

	boardHandler := OrchestrationBoardGetHandler(cfg, zerolog.Nop())
	boardReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&limit=10", nil)
	boardRR := httptest.NewRecorder()
	boardHandler.ServeHTTP(boardRR, boardReq)

	if boardRR.Code != http.StatusOK {
		t.Fatalf("board status=%d body=%s", boardRR.Code, boardRR.Body.String())
	}
	if got := boardCardCountFromEnvelope(decodeResponseBody(t, boardRR)); got < 2 {
		t.Fatalf("expected at least 2 cards after seed, got %d", got)
	}
}

func TestOrchestrationCleanupCardsHandler_RemovesCardsAndReplayHistory(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	seedHandler := OrchestrationSeedCardsHandler(cfg, zerolog.Nop())
	cleanupHandler := OrchestrationCleanupCardsHandler(cfg, zerolog.Nop())
	boardHandler := OrchestrationBoardGetHandler(cfg, zerolog.Nop())
	refreshHandler := OrchestrationRefreshHandler(cfg, zerolog.Nop())

	seedReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/seed-cards", bytes.NewBufferString(`{
		"request_id":"req-cleanup-seed-001",
		"workspace_id":"ws-1",
		"cards":[
			{"issue_id":"cleanup-1","issue_identifier":"CLEAN-1","title":"Cleanup one"},
			{"issue_id":"cleanup-2","issue_identifier":"CLEAN-2","title":"Cleanup two"}
		]
	}`))
	seedRR := httptest.NewRecorder()
	seedHandler.ServeHTTP(seedRR, seedReq)
	if seedRR.Code != http.StatusOK {
		t.Fatalf("seed status=%d body=%s", seedRR.Code, seedRR.Body.String())
	}

	cleanupReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/cleanup-cards", bytes.NewBufferString(`{
		"request_id":"req-cleanup-001",
		"workspace_id":"ws-1",
		"issue_ids":["cleanup-1","cleanup-2"]
	}`))
	cleanupRR := httptest.NewRecorder()
	cleanupHandler.ServeHTTP(cleanupRR, cleanupReq)
	if cleanupRR.Code != http.StatusOK {
		t.Fatalf("cleanup status=%d body=%s", cleanupRR.Code, cleanupRR.Body.String())
	}
	cleanupBody := decodeResponseBody(t, cleanupRR)
	cleanupData, _ := cleanupBody["data"].(map[string]any)
	if got := int(cleanupData["deleted_cards"].(float64)); got != 2 {
		t.Fatalf("deleted_cards=%d want 2", got)
	}

	boardReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&limit=10", nil)
	boardRR := httptest.NewRecorder()
	boardHandler.ServeHTTP(boardRR, boardReq)
	if boardRR.Code != http.StatusOK {
		t.Fatalf("board after cleanup status=%d body=%s", boardRR.Code, boardRR.Body.String())
	}
	if got := boardCardCountFromEnvelope(decodeResponseBody(t, boardRR)); got != 0 {
		t.Fatalf("expected 0 cards after cleanup, got %d", got)
	}

	refreshReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/refresh", bytes.NewBufferString(`{
		"request_id":"req-cleanup-refresh-001",
		"workspace_id":"ws-1"
	}`))
	refreshRR := httptest.NewRecorder()
	refreshHandler.ServeHTTP(refreshRR, refreshReq)
	if refreshRR.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshRR.Code, refreshRR.Body.String())
	}

	boardReplayReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&limit=10", nil)
	boardReplayRR := httptest.NewRecorder()
	boardHandler.ServeHTTP(boardReplayRR, boardReplayReq)
	if boardReplayRR.Code != http.StatusOK {
		t.Fatalf("board after refresh status=%d body=%s", boardReplayRR.Code, boardReplayRR.Body.String())
	}
	if got := boardCardCountFromEnvelope(decodeResponseBody(t, boardReplayRR)); got != 0 {
		t.Fatalf("expected 0 cards after refresh replay, got %d", got)
	}
}

func TestOrchestrationArchiveCardsHandler_ArchivesAndRestoresCards(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	seedHandler := OrchestrationSeedCardsHandler(cfg, zerolog.Nop())
	archiveHandler := OrchestrationArchiveCardsHandler(cfg, zerolog.Nop())
	restoreHandler := OrchestrationRestoreCardsHandler(cfg, zerolog.Nop())
	boardHandler := OrchestrationBoardGetHandler(cfg, zerolog.Nop())
	cardHandler := OrchestrationBoardCardGetHandler(cfg, zerolog.Nop())

	seedReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/seed-cards", bytes.NewBufferString(`{
		"request_id":"req-archive-seed-001",
		"workspace_id":"ws-1",
		"cards":[{"issue_id":"archive-1","issue_identifier":"ARCH-1","title":"Archive me"}]
	}`))
	seedRR := httptest.NewRecorder()
	seedHandler.ServeHTTP(seedRR, seedReq)
	if seedRR.Code != http.StatusOK {
		t.Fatalf("seed status=%d body=%s", seedRR.Code, seedRR.Body.String())
	}

	archiveReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/archive-cards", bytes.NewBufferString(`{
		"request_id":"req-archive-001",
		"workspace_id":"ws-1",
		"issue_ids":["archive-1"]
	}`))
	archiveRR := httptest.NewRecorder()
	archiveHandler.ServeHTTP(archiveRR, archiveReq)
	if archiveRR.Code != http.StatusOK {
		t.Fatalf("archive status=%d body=%s", archiveRR.Code, archiveRR.Body.String())
	}
	archiveBody := decodeResponseBody(t, archiveRR)
	archiveData, _ := archiveBody["data"].(map[string]any)
	if got := int(archiveData["updated"].(float64)); got != 1 {
		t.Fatalf("updated=%d want 1", got)
	}

	activeBoardReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&limit=10", nil)
	activeBoardRR := httptest.NewRecorder()
	boardHandler.ServeHTTP(activeBoardRR, activeBoardReq)
	if activeBoardRR.Code != http.StatusOK {
		t.Fatalf("active board status=%d body=%s", activeBoardRR.Code, activeBoardRR.Body.String())
	}
	if got := boardCardCountFromEnvelope(decodeResponseBody(t, activeBoardRR)); got != 0 {
		t.Fatalf("active board count=%d want 0", got)
	}

	archivedBoardReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&limit=10&archived_only=true", nil)
	archivedBoardRR := httptest.NewRecorder()
	boardHandler.ServeHTTP(archivedBoardRR, archivedBoardReq)
	if archivedBoardRR.Code != http.StatusOK {
		t.Fatalf("archived board status=%d body=%s", archivedBoardRR.Code, archivedBoardRR.Body.String())
	}
	if got := boardCardCountFromEnvelope(decodeResponseBody(t, archivedBoardRR)); got != 1 {
		t.Fatalf("archived board count=%d want 1", got)
	}

	cardReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-card-get?workspace_id=ws-1&issue_id=archive-1", nil)
	cardRR := httptest.NewRecorder()
	cardHandler.ServeHTTP(cardRR, cardReq)
	if cardRR.Code != http.StatusOK {
		t.Fatalf("card status=%d body=%s", cardRR.Code, cardRR.Body.String())
	}
	cardBody := decodeResponseBody(t, cardRR)
	cardData, _ := cardBody["data"].(map[string]any)
	cardWrap, _ := cardData["card"].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(cardWrap["archived_at"])); got == "" {
		t.Fatal("archived_at should be populated on archived card detail")
	}

	restoreReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/restore-cards", bytes.NewBufferString(`{
		"request_id":"req-restore-001",
		"workspace_id":"ws-1",
		"issue_ids":["archive-1"]
	}`))
	restoreRR := httptest.NewRecorder()
	restoreHandler.ServeHTTP(restoreRR, restoreReq)
	if restoreRR.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", restoreRR.Code, restoreRR.Body.String())
	}

	activeAfterRestoreReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&limit=10", nil)
	activeAfterRestoreRR := httptest.NewRecorder()
	boardHandler.ServeHTTP(activeAfterRestoreRR, activeAfterRestoreReq)
	if activeAfterRestoreRR.Code != http.StatusOK {
		t.Fatalf("active after restore status=%d body=%s", activeAfterRestoreRR.Code, activeAfterRestoreRR.Body.String())
	}
	if got := boardCardCountFromEnvelope(decodeResponseBody(t, activeAfterRestoreRR)); got != 1 {
		t.Fatalf("active board count after restore=%d want 1", got)
	}
}

func TestOrchestrationBoardGetHandler_ArtifactizesLargePayload(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	seedOrchestrationCards(t, cfg, 200, 1200)

	h := OrchestrationBoardGetHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&limit=200", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	data, _ := body["data"].(map[string]any)

	artifact := strings.TrimSpace(fmt.Sprint(data["artifact"]))
	if !strings.HasPrefix(artifact, "sha256:") {
		t.Fatalf("artifact=%q want sha256:*", artifact)
	}
	if _, hasLanes := data["lanes"]; hasLanes {
		t.Fatalf("expected artifactized payload without inline lanes, got lanes=%v", data["lanes"])
	}
}

func TestOrchestrationRefreshHandler_ReplaysEventsIntoBoardProjection(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()

	eventStore, err := openOrchestrationEventStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open event store: %v", err)
	}
	defer func() {
		if closeErr := eventStore.Close(); closeErr != nil {
			t.Fatalf("close event store: %v", closeErr)
		}
	}()

	event := coreevents.Event{
		ID:            "evt-refresh-001",
		StreamID:      "run-refresh-001",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.March, 5, 13, 0, 0, 0, time.UTC),
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-refresh-seed-001",
		ActorID:       "actor:system:overseer",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-refresh-001",
			"issue_identifier": "ABC-R1",
			"title":            "Replay me",
			"state":            "Running",
			"eligibility":      "eligible",
		}),
	}
	if err := eventStore.Append(ctx, event); err != nil {
		t.Fatalf("append event: %v", err)
	}

	boardHandler := OrchestrationBoardGetHandler(cfg, zerolog.Nop())
	beforeReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&limit=10", nil)
	beforeRR := httptest.NewRecorder()
	boardHandler.ServeHTTP(beforeRR, beforeReq)
	if beforeRR.Code != http.StatusOK {
		t.Fatalf("board before refresh status=%d body=%s", beforeRR.Code, beforeRR.Body.String())
	}
	beforeBody := decodeResponseBody(t, beforeRR)
	beforeCards := boardCardCountFromEnvelope(beforeBody)
	if beforeCards != 0 {
		t.Fatalf("expected no cards before refresh replay, got %d", beforeCards)
	}

	refreshHandler := OrchestrationRefreshHandler(cfg, zerolog.Nop())
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/refresh", bytes.NewBufferString(`{"request_id":"req-refresh-apply-001","workspace_id":"ws-1"}`))
	refreshRR := httptest.NewRecorder()
	refreshHandler.ServeHTTP(refreshRR, refreshReq)
	if refreshRR.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshRR.Code, refreshRR.Body.String())
	}

	afterReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&limit=10", nil)
	afterRR := httptest.NewRecorder()
	boardHandler.ServeHTTP(afterRR, afterReq)
	if afterRR.Code != http.StatusOK {
		t.Fatalf("board after refresh status=%d body=%s", afterRR.Code, afterRR.Body.String())
	}
	afterBody := decodeResponseBody(t, afterRR)
	afterCards := boardCardCountFromEnvelope(afterBody)
	if afterCards == 0 {
		t.Fatalf("expected cards after refresh replay, got 0")
	}
}

func TestOrchestrationRefreshHandler_EndToEndReplayProjectionLaneTransitions(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()

	eventStore, err := openOrchestrationEventStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open event store: %v", err)
	}
	defer func() {
		if closeErr := eventStore.Close(); closeErr != nil {
			t.Fatalf("close event store: %v", closeErr)
		}
	}()

	base := time.Date(2026, time.March, 5, 15, 0, 0, 0, time.UTC)
	appendEvent := func(id, issueID, state, eligibility, policyStatus, lastOutcome string) {
		t.Helper()
		evt := coreevents.Event{
			ID:            id,
			StreamID:      "run-" + issueID,
			StreamType:    coreevents.StreamTypeRun,
			StreamVersion: 1,
			EventType:     coreevents.EventRunStarted,
			OccurredAt:    base,
			Command:       "orchestration/dispatch-issue",
			RequestID:     "req-" + issueID,
			ActorID:       "actor:system:overseer",
			Payload: coreevents.MustMarshalPayload(map[string]any{
				"workspace_id":     "ws-1",
				"issue_id":         issueID,
				"issue_identifier": strings.ToUpper(issueID),
				"title":            "transition " + issueID,
				"state":            state,
				"eligibility":      eligibility,
				"policy_status":    policyStatus,
				"last_outcome":     lastOutcome,
				"tracker_state":    "In Progress",
			}),
		}
		if err := eventStore.Append(ctx, evt); err != nil {
			t.Fatalf("append event %s: %v", issueID, err)
		}
		base = base.Add(time.Second)
	}

	appendEvent("evt-e2e-001", "issue-running", "Running", "eligible", "ok", "")
	appendEvent("evt-e2e-002", "issue-retry", "RetryQueued", "eligible", "ok", "")
	appendEvent("evt-e2e-003", "issue-claimed", "Claimed", "eligible", "ok", "")
	appendEvent("evt-e2e-004", "issue-todo", "Released", "eligible", "ok", "")
	appendEvent("evt-e2e-005", "issue-blocked", "Released", "ineligible", "denied", "policy_denied")

	refreshHandler := OrchestrationRefreshHandler(cfg, zerolog.Nop())
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/refresh", bytes.NewBufferString(`{"request_id":"req-refresh-e2e-001","workspace_id":"ws-1"}`))
	refreshRR := httptest.NewRecorder()
	refreshHandler.ServeHTTP(refreshRR, refreshReq)
	if refreshRR.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshRR.Code, refreshRR.Body.String())
	}

	boardHandler := OrchestrationBoardGetHandler(cfg, zerolog.Nop())
	boardReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&limit=20", nil)
	boardRR := httptest.NewRecorder()
	boardHandler.ServeHTTP(boardRR, boardReq)
	if boardRR.Code != http.StatusOK {
		t.Fatalf("board status=%d body=%s", boardRR.Code, boardRR.Body.String())
	}

	body := decodeResponseBody(t, boardRR)
	counts := laneCountsFromEnvelope(body)
	if counts["Running"] != 1 {
		t.Fatalf("running count=%d want 1", counts["Running"])
	}
	if counts["RetryQueued"] != 1 {
		t.Fatalf("retry count=%d want 1", counts["RetryQueued"])
	}
	if counts["Claimed"] != 1 {
		t.Fatalf("claimed count=%d want 1", counts["Claimed"])
	}
	if counts["Todo"] != 1 {
		t.Fatalf("todo count=%d want 1", counts["Todo"])
	}
	if counts["Blocked"] != 1 {
		t.Fatalf("blocked count=%d want 1", counts["Blocked"])
	}
}

func TestOrchestrationRefreshHandler_ReconcilesJidoCompletedChildIntoReviewLane(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()

	eventStore, err := openOrchestrationEventStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open event store: %v", err)
	}
	defer func() {
		if closeErr := eventStore.Close(); closeErr != nil {
			t.Fatalf("close event store: %v", closeErr)
		}
	}()

	evt := coreevents.Event{
		ID:            "evt-refresh-jido-001",
		StreamID:      "run-jido-001",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.March, 6, 9, 0, 0, 0, time.UTC),
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-refresh-jido-seed-001",
		ActorID:       "actor:system:overseer",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-jido-001",
			"issue_identifier": "JIDO-1",
			"title":            "Jido child completes",
			"state":            "Running",
			"eligibility":      "eligible",
			"run_id":           "run-jido-001",
			"actor_id":         "actor:system:overseer",
		}),
	}
	if err := eventStore.Append(ctx, evt); err != nil {
		t.Fatalf("append event: %v", err)
	}

	server, socketPath := startOrchestrationJSONRPCServer(t, func(method string, params json.RawMessage) (any, *jsonrpcTestError) {
		switch method {
		case v2jido.MethodRuntimeGetChildren:
			return map[string]any{
				"agent_id": "agent:overseer",
				"children": map[string]any{
					"issue-jido-001": map[string]any{
						"tag":      "issue-jido-001",
						"agent_id": "agent:worker-1",
						"metadata": map[string]any{
							"workspace_id":     "ws-1",
							"issue_id":         "issue-jido-001",
							"issue_identifier": "JIDO-1",
							"title":            "Jido child completes",
							"run_id":           "run-jido-001",
							"actor_id":         "actor:system:overseer",
							"request_id":       "req-refresh-jido-seed-001",
						},
					},
				},
			}, nil
		case v2jido.MethodRuntimeState:
			return map[string]any{
				"agent_id": "agent:worker-1",
				"status":   "ok",
				"state": map[string]any{
					"agentctl": map[string]any{
						"status": "completed",
						"last_result": map[string]any{
							"envelope": map[string]any{
								"status": "ok",
							},
						},
					},
				},
			}, nil
		default:
			return nil, &jsonrpcTestError{Code: -32601, Message: "method not found"}
		}
	})
	defer server.Close()

	t.Setenv(v2jido.EnvJidoSocketPath, socketPath)
	t.Setenv(v2jido.EnvJidoOrchestrationParentAgentIDs, "agent:overseer")

	refreshHandler := OrchestrationRefreshHandler(cfg, zerolog.Nop())
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/refresh", bytes.NewBufferString(`{"request_id":"req-refresh-jido-apply-001","workspace_id":"ws-1"}`))
	refreshRR := httptest.NewRecorder()
	refreshHandler.ServeHTTP(refreshRR, refreshReq)
	if refreshRR.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshRR.Code, refreshRR.Body.String())
	}

	store, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	card, err := store.Card(ctx, coreorchestration.CardRequest{
		WorkspaceID: "ws-1",
		IssueID:     "issue-jido-001",
	})
	if err != nil {
		t.Fatalf("read card: %v", err)
	}
	if card.Card.State != coreorchestration.StateReleased {
		t.Fatalf("state=%q want %q", card.Card.State, coreorchestration.StateReleased)
	}
	if card.Card.Lane != coreorchestration.LaneReview {
		t.Fatalf("lane=%q want %q", card.Card.Lane, coreorchestration.LaneReview)
	}
	if card.Card.TrackerState != "Human Review" {
		t.Fatalf("tracker_state=%q want Human Review", card.Card.TrackerState)
	}
	if card.Card.AgentID != "agent:worker-1" {
		t.Fatalf("agent_id=%q want agent:worker-1", card.Card.AgentID)
	}
	if card.Card.LastEvent != string(coreevents.EventRunCompleted) {
		t.Fatalf("last_event=%q want %q", card.Card.LastEvent, coreevents.EventRunCompleted)
	}
}

func TestOrchestrationDBConfig_OverrideRejectsPostgres(t *testing.T) {
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "postgres")
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	cfg := orchestrationTestConfig(t.TempDir())

	_, err := orchestrationDBConfig(cfg)
	if err == nil {
		t.Fatal("expected postgres override to be rejected")
	}
	if !strings.Contains(err.Error(), "postgres is not supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOrchestrationDBConfig_GlobalPostgresFallsBackToSQLite(t *testing.T) {
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")
	t.Setenv("AGENTCTL_DB_DRIVER", "postgres")

	root := t.TempDir()
	cfg := orchestrationTestConfig(root)
	cfg.Database.Driver = "postgres"

	dbCfg, err := orchestrationDBConfig(cfg)
	if err != nil {
		t.Fatalf("orchestrationDBConfig() error = %v", err)
	}
	if dbCfg.Driver != dbdriver.DriverSQLite {
		t.Fatalf("driver=%q want %q", dbCfg.Driver, dbdriver.DriverSQLite)
	}
	if got, want := dbCfg.SQLite.Path, filepath.Join(root, "v2_events.db"); got != want {
		t.Fatalf("sqlite path=%q want %q", got, want)
	}
}

func TestOrchestrationDBConfig_GlobalPostgresHonorsV2EventsPathOverride(t *testing.T) {
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")
	t.Setenv("AGENTCTL_DB_DRIVER", "postgres")

	root := t.TempDir()
	overridePath := filepath.Join(root, "custom", "v2_events.libsql")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_PATH", overridePath)

	cfg := orchestrationTestConfig(root)
	cfg.Database.Driver = "postgres"

	dbCfg, err := orchestrationDBConfig(cfg)
	if err != nil {
		t.Fatalf("orchestrationDBConfig() error = %v", err)
	}
	if dbCfg.Driver != dbdriver.DriverSQLite {
		t.Fatalf("driver=%q want %q", dbCfg.Driver, dbdriver.DriverSQLite)
	}
	if got := dbCfg.SQLite.Path; got != overridePath {
		t.Fatalf("sqlite path=%q want %q", got, overridePath)
	}
}

func TestOrchestrationBoardGetHandler_GlobalPostgresStillWorks(t *testing.T) {
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")
	t.Setenv("AGENTCTL_DB_DRIVER", "postgres")

	cfg := orchestrationTestConfig(t.TempDir())
	cfg.Database.Driver = "postgres"
	seedOrchestrationCards(t, cfg, 2, 24)

	h := OrchestrationBoardGetHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&limit=10", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	if body["status"] != "ok" {
		t.Fatalf("status field=%v want ok", body["status"])
	}
}

func TestOrchestrationRefreshHandler_GlobalPostgresStillWorks(t *testing.T) {
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")
	t.Setenv("AGENTCTL_DB_DRIVER", "postgres")

	cfg := orchestrationTestConfig(t.TempDir())
	cfg.Database.Driver = "postgres"

	refreshHandler := OrchestrationRefreshHandler(cfg, zerolog.Nop())
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/refresh", bytes.NewBufferString(`{"request_id":"req-postgres-refresh-001","workspace_id":"ws-1"}`))
	refreshRR := httptest.NewRecorder()
	refreshHandler.ServeHTTP(refreshRR, refreshReq)
	if refreshRR.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshRR.Code, refreshRR.Body.String())
	}
	body := decodeResponseBody(t, refreshRR)
	if body["status"] != "ok" {
		t.Fatalf("status field=%v want ok", body["status"])
	}
}

func TestOrchestrationBoardGetHandler_DefaultLibSQLWithoutCGOFallsBackToSQLite(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := orchestrationTestConfig(t.TempDir())
	cfg.Database.Driver = ""
	seedOrchestrationCards(t, cfg, 2, 20)

	h := OrchestrationBoardGetHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/orchestration/board-get?workspace_id=ws-1&limit=10", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	if body["status"] != "ok" {
		t.Fatalf("status field=%v want ok", body["status"])
	}
}

func TestInMemoryRefreshQueue_ConcurrentCallerReceivesInflightFailure(t *testing.T) {
	firstStarted := make(chan struct{})
	release := make(chan struct{})
	callbackErr := errors.New("refresh replay failed")
	var callbackCalls atomic.Int32

	queue := newInMemoryRefreshQueue(time.Second, func(_ context.Context, _, _ string) error {
		call := callbackCalls.Add(1)
		if call == 1 {
			close(firstStarted)
			<-release
			return callbackErr
		}
		return callbackErr
	})

	var wg sync.WaitGroup
	type result struct {
		queued    bool
		coalesced bool
		err       error
	}
	results := make([]result, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		queued, coalesced, err := queue.Enqueue(context.Background(), "ws-1", "req-1")
		results[0] = result{queued: queued, coalesced: coalesced, err: err}
	}()

	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first enqueue callback to start")
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		queued, coalesced, err := queue.Enqueue(context.Background(), "ws-1", "req-2")
		results[1] = result{queued: queued, coalesced: coalesced, err: err}
	}()

	close(release)
	wg.Wait()

	if !errors.Is(results[0].err, callbackErr) {
		t.Fatalf("first enqueue err=%v want %v", results[0].err, callbackErr)
	}
	if !errors.Is(results[1].err, callbackErr) {
		t.Fatalf("second enqueue err=%v want %v", results[1].err, callbackErr)
	}
	if results[1].coalesced {
		t.Fatalf("second enqueue coalesced=%v want false on inflight failure", results[1].coalesced)
	}
	if calls := callbackCalls.Load(); calls > 1 {
		t.Fatalf("callback calls=%d want 1 (second caller should wait on inflight result)", calls)
	}
}

func orchestrationTestConfig(root string) config.Config {
	return config.Config{
		Storage: config.StorageSettings{Root: root},
		Database: config.DatabaseSettings{
			Driver: "sqlite",
		},
	}
}

func seedOrchestrationCards(t *testing.T, cfg config.Config, count int, titleLen int) {
	t.Helper()

	ctx := context.Background()
	store, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	title := strings.Repeat("x", titleLen)
	baseTime := time.Date(2026, time.March, 5, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= count; i++ {
		idx := strconv.Itoa(i)
		issueID := fmt.Sprintf("issue-%03d", i)
		evt := coreevents.Event{
			ID:            "evt-" + idx,
			StreamID:      "run-" + idx,
			StreamType:    coreevents.StreamTypeRun,
			StreamVersion: 1,
			EventType:     coreevents.EventRunStarted,
			OccurredAt:    baseTime.Add(time.Duration(i) * time.Second),
			Command:       "orchestration/dispatch-issue",
			RequestID:     "req-" + idx,
			ActorID:       "actor:system:overseer",
			Payload: coreevents.MustMarshalPayload(map[string]any{
				"workspace_id":     "ws-1",
				"issue_id":         issueID,
				"issue_identifier": "ABC-" + idx,
				"title":            title,
				"state":            "Running",
				"eligibility":      "eligible",
				"tracker_state":    "In Progress",
			}),
		}
		if err := store.Apply(ctx, evt); err != nil {
			t.Fatalf("apply %s: %v", issueID, err)
		}
	}
}

func decodeResponseBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	return body
}

type jsonrpcTestError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type jsonrpcTestHandler func(method string, params json.RawMessage) (any, *jsonrpcTestError)

func startOrchestrationJSONRPCServer(t *testing.T, handle jsonrpcTestHandler) (*http.Server, string) {
	t.Helper()

	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("agentctl-jido-refresh-%d.sock", time.Now().UnixNano()))
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()

		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      nil,
				"error": map[string]any{
					"code":    -32700,
					"message": err.Error(),
				},
			})
			return
		}

		result, rpcErr := handle(req.Method, req.Params)
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      json.RawMessage(req.ID),
		}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := &http.Server{Handler: mux}
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
	})
	go func() {
		_ = server.Serve(listener)
	}()
	return server, socketPath
}

func boardCardCountFromEnvelope(body map[string]any) int {
	data, _ := body["data"].(map[string]any)
	lanes, _ := data["lanes"].([]any)
	total := 0
	for _, rawLane := range lanes {
		lane, _ := rawLane.(map[string]any)
		cards, _ := lane["cards"].([]any)
		total += len(cards)
	}
	return total
}

func laneCountsFromEnvelope(body map[string]any) map[string]int {
	out := map[string]int{}
	data, _ := body["data"].(map[string]any)
	lanes, _ := data["lanes"].([]any)
	for _, rawLane := range lanes {
		lane, _ := rawLane.(map[string]any)
		id := strings.TrimSpace(fmt.Sprint(lane["id"]))
		cards, _ := lane["cards"].([]any)
		out[id] = len(cards)
	}
	return out
}

type captureSSEPublishCall struct {
	eventType string
	data      any
}

type captureSSEPublisher struct {
	calls []captureSSEPublishCall
}

func (c *captureSSEPublisher) Publish(eventType string, data any) {
	c.calls = append(c.calls, captureSSEPublishCall{
		eventType: eventType,
		data:      data,
	})
}

type fakeOrchestrationRuntimeHost struct {
	spawnResp              corespawn.Response
	spawnErr               error
	refreshErr             error
	signalResp             coreworker.SignalResponse
	signalErr              error
	spawnCalls             int
	refreshCalls           int
	signalCalls            int
	lastSpawnReq           corespawn.Request
	lastRefreshWorkspaceID string
	lastRefreshRequestID   string
	lastSignalReq          coreworker.SignalRequest
}

func (f *fakeOrchestrationRuntimeHost) Run(context.Context) error { return nil }

func (f *fakeOrchestrationRuntimeHost) Close() error { return nil }

func (f *fakeOrchestrationRuntimeHost) Spawn(_ context.Context, req corespawn.Request) (corespawn.Response, error) {
	f.spawnCalls++
	f.lastSpawnReq = req
	if f.spawnErr != nil {
		return corespawn.Response{}, f.spawnErr
	}
	return f.spawnResp, nil
}

func (f *fakeOrchestrationRuntimeHost) Refresh(_ context.Context, workspaceID, requestID string) error {
	f.refreshCalls++
	f.lastRefreshWorkspaceID = workspaceID
	f.lastRefreshRequestID = requestID
	return f.refreshErr
}

func (f *fakeOrchestrationRuntimeHost) Signal(_ context.Context, req coreworker.SignalRequest) (coreworker.SignalResponse, error) {
	f.signalCalls++
	f.lastSignalReq = req
	if f.signalErr != nil {
		return coreworker.SignalResponse{}, f.signalErr
	}
	return f.signalResp, nil
}
