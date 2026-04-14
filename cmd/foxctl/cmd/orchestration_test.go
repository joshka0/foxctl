package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	v2jido "github.com/joshka0/foxctl/internal/v2/adapters/jido"
	libsqlworkers "github.com/joshka0/foxctl/internal/v2/adapters/libsql/workers"
	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	coreorchestration "github.com/joshka0/foxctl/internal/v2/core/orchestration"
	coreworker "github.com/joshka0/foxctl/internal/v2/core/worker"
)

func TestOrchestrationCardActionCommand_ReleaseMovesCardBackToTodo(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	cfg := setupOrchestrationTestEnv(t)
	ctx := context.Background()
	store, closeFn, err := openOverseerOrchestrationStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-orch-cli-action-001",
		StreamID:      "run-orch-cli-action-001",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunCompleted,
		OccurredAt:    time.Date(2026, time.March, 6, 14, 0, 0, 0, time.UTC),
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-orch-cli-action-seed-001",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-cli-action-001",
			"issue_identifier": "ABC-CLI-ACTION-1",
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

	body := runOrchestrationCommand(t, cfg, newOrchestrationCardActionCommand(),
		"--workspace", "ws-1",
		"--request-id", "req-orch-cli-action-001",
		"--action", "release",
		"issue-cli-action-001",
	)

	data, _ := body["data"].(map[string]any)
	card, _ := data["card"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(card["lane"])) != "Todo" {
		t.Fatalf("lane=%v want Todo", card["lane"])
	}
	if raw, ok := card["tracker_state"]; ok && strings.TrimSpace(fmt.Sprint(raw)) != "" {
		t.Fatalf("tracker_state=%v want empty/omitted", raw)
	}
}

func TestOrchestrationCardRuntimeCommand_ReturnsRuntimeTree(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	server, socketPath := startOrchestrationCLIJSONRPCServer(t, func(method string, params json.RawMessage) (any, *jsonrpcCLITestError) {
		var req map[string]any
		_ = json.Unmarshal(params, &req)
		agentID := strings.TrimSpace(fmt.Sprint(req["agent_id"]))

		switch method {
		case v2jido.MethodRuntimeState:
			switch agentID {
			case "agent:root-runtime":
				return map[string]any{
					"agent_id": agentID,
					"status":   "running",
					"state": map[string]any{
						"status":  "running",
						"profile": "overseer",
					},
				}, nil
			case "agent:child-runtime":
				return map[string]any{
					"agent_id": agentID,
					"status":   "completed",
					"state": map[string]any{
						"status":  "completed",
						"profile": "worker",
					},
				}, nil
			default:
				return nil, &jsonrpcCLITestError{Code: -32602, Message: "unknown agent"}
			}
		case v2jido.MethodRuntimeGetChildren:
			if agentID == "agent:root-runtime" {
				return map[string]any{
					"agent_id": agentID,
					"children": map[string]any{
						"agent:child-runtime": map[string]any{
							"tag":      "agent:child-runtime",
							"agent_id": "agent:child-runtime",
						},
					},
				}, nil
			}
			return map[string]any{"agent_id": agentID, "children": map[string]any{}}, nil
		default:
			return nil, &jsonrpcCLITestError{Code: -32601, Message: "unknown method"}
		}
	})
	t.Cleanup(func() { _ = server.Close() })
	t.Setenv(v2jido.EnvJidoSocketPath, socketPath)

	cfg := setupOrchestrationTestEnv(t)
	ctx := context.Background()
	store, closeFn, err := openOverseerOrchestrationStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-orch-cli-runtime-001",
		StreamID:      "run-orch-cli-runtime-001",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.March, 6, 14, 15, 0, 0, time.UTC),
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-orch-cli-runtime-seed-001",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-cli-runtime-001",
			"issue_identifier": "ABC-CLI-RUNTIME-1",
			"title":            "Inspect runtime tree",
			"state":            "Running",
			"eligibility":      "eligible",
			"agent_id":         "agent:root-runtime",
		}),
	}); err != nil {
		t.Fatalf("seed apply: %v", err)
	}

	body := runOrchestrationCommand(t, cfg, newOrchestrationCardRuntimeCommand(),
		"--workspace", "ws-1",
		"--depth", "2",
		"issue-cli-runtime-001",
	)

	data, _ := body["data"].(map[string]any)
	runtimeWrap, _ := data["runtime"].(map[string]any)
	root, _ := runtimeWrap["root"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(root["agent_id"])) != "agent:root-runtime" {
		t.Fatalf("root.agent_id=%v want agent:root-runtime", root["agent_id"])
	}
	children, _ := root["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("root children=%d want 1", len(children))
	}
	child, _ := children[0].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(child["agent_id"])) != "agent:child-runtime" {
		t.Fatalf("child.agent_id=%v want agent:child-runtime", child["agent_id"])
	}
}

func TestOrchestrationDispatchIssueCommand_ProjectsRunningCard(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")

	server, socketPath := startOrchestrationCLIJSONRPCServer(t, func(method string, params json.RawMessage) (any, *jsonrpcCLITestError) {
		switch method {
		case v2jido.MethodRuntimeSpawnChild:
			return map[string]any{
				"agent_id":   "agent:dispatch-root",
				"message_id": "msg-dispatch-cli-001",
				"signal_id":  "sig-dispatch-cli-001",
				"status":     "spawned",
				"data": map[string]any{
					"child": map[string]any{
						"tag":      "agent:worker-cli-1",
						"agent_id": "agent:worker-cli-1",
					},
				},
			}, nil
		default:
			return nil, &jsonrpcCLITestError{Code: -32601, Message: "unknown method"}
		}
	})
	t.Cleanup(func() { _ = server.Close() })
	t.Setenv(v2jido.EnvJidoSocketPath, socketPath)
	t.Setenv(v2jido.EnvJidoOrchestrationParentAgentIDs, "agent:dispatch-root")
	t.Setenv(v2jido.EnvJidoOrchestrationDispatchParentAgentID, "agent:dispatch-root")

	cfg := setupOrchestrationTestEnv(t)
	body := runOrchestrationCommand(t, cfg, newOrchestrationDispatchIssueCommand(),
		"--workspace", "ws-1",
		"--request-id", "req-orch-cli-dispatch-001",
		"--issue-identifier", "ABC-CLI-DISPATCH-1",
		"--title", "Dispatch from CLI",
		"issue-cli-dispatch-001",
	)

	data, _ := body["data"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(data["status"])) != "dispatched" {
		t.Fatalf("status=%v want dispatched", data["status"])
	}
	if strings.TrimSpace(fmt.Sprint(data["agent_id"])) != "agent:worker-cli-1" {
		t.Fatalf("agent_id=%v want agent:worker-cli-1", data["agent_id"])
	}

	store, closeFn, err := openOverseerOrchestrationStore(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	card, err := store.Card(context.Background(), coreorchestration.CardRequest{
		WorkspaceID: "ws-1",
		IssueID:     "issue-cli-dispatch-001",
	})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.Lane != "Running" {
		t.Fatalf("lane=%q want Running", card.Card.Lane)
	}
	if card.Card.AgentID != "agent:worker-cli-1" {
		t.Fatalf("agent_id=%q want agent:worker-cli-1", card.Card.AgentID)
	}
}

func setupOrchestrationTestEnv(t *testing.T) config.Config {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg, err := config.Load(context.Background())
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	dirs := []string{cfg.Home, cfg.Paths.CAS, cfg.Paths.Jobs, cfg.Paths.Cache, cfg.Paths.Skills, cfg.Storage.Root}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	return cfg
}

func runOrchestrationCommand(t *testing.T, cfg config.Config, cmd *cobra.Command, args ...string) map[string]any {
	t.Helper()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("orchestration command failed: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}

	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("remarshal envelope: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode envelope as map: %v", err)
	}
	return body
}

type jsonrpcCLITestError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type jsonrpcCLITestHandler func(method string, params json.RawMessage) (any, *jsonrpcCLITestError)

func startOrchestrationCLIJSONRPCServer(t *testing.T, handle jsonrpcCLITestHandler) (*http.Server, string) {
	t.Helper()

	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("foxctl-jido-cli-%d.sock", time.Now().UnixNano()))
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

func TestOrchestrationCardRuntimeCommand_ReturnsGoRuntimeTree(t *testing.T) {
	t.Setenv("AGENTCTL_DB_DRIVER", "")
	t.Setenv("AGENTCTL_V2_EVENTS_DB_DRIVER", "")
	t.Setenv(envCLIRuntimeBackend, cliRuntimeBackendGoruntime)

	cfg := setupOrchestrationTestEnv(t)
	ctx := context.Background()
	store, closeFn, err := openOverseerOrchestrationStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open orchestration store: %v", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			t.Fatalf("close orchestration store: %v", closeErr)
		}
	}()

	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-orch-cli-go-runtime-001",
		StreamID:      "run-orch-cli-go-runtime-001",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.April, 7, 14, 15, 0, 0, time.UTC),
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-orch-cli-go-runtime-seed-001",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-cli-go-runtime-001",
			"issue_identifier": "ABC-CLI-GO-RUNTIME-1",
			"title":            "Inspect go runtime tree via CLI",
			"state":            "Running",
			"eligibility":      "eligible",
			"agent_id":         "agent:root-go-runtime",
		}),
	}); err != nil {
		t.Fatalf("seed apply: %v", err)
	}

	workerStore, closeWorkers, err := libsqlworkers.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open worker store: %v", err)
	}
	defer func() { _ = closeWorkers() }()

	if err := workerStore.Upsert(ctx, coreworker.Record{
		WorkerID:    "subprocess:agent:root-go-runtime",
		BackendKind: coreworker.BackendSubprocess,
		AgentID:     "agent:root-go-runtime",
		RunID:       "run-orch-cli-go-runtime-001",
		Status:      coreworker.StatusRunning,
		RawState:    json.RawMessage(`{"foxctl":{"status":"running","agent":"agent:root-go-runtime"}}`),
	}); err != nil {
		t.Fatalf("upsert root worker: %v", err)
	}
	if err := workerStore.Upsert(ctx, coreworker.Record{
		WorkerID:       "subprocess:agent:child-go-runtime",
		BackendKind:    coreworker.BackendSubprocess,
		AgentID:        "agent:child-go-runtime",
		ParentAgentID:  "agent:root-go-runtime",
		ParentWorkerID: "subprocess:agent:root-go-runtime",
		RunID:          "run-orch-cli-go-runtime-001",
		Status:         coreworker.StatusCompleted,
		RawState:       json.RawMessage(`{"foxctl":{"status":"completed","agent":"agent:child-go-runtime"}}`),
	}); err != nil {
		t.Fatalf("upsert child worker: %v", err)
	}

	body := runOrchestrationCommand(t, cfg, newOrchestrationCardRuntimeCommand(),
		"--workspace", "ws-1",
		"--depth", "2",
		"issue-cli-go-runtime-001",
	)

	data, _ := body["data"].(map[string]any)
	runtimeWrap, _ := data["runtime"].(map[string]any)
	root, _ := runtimeWrap["root"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(root["agent_id"])) != "agent:root-go-runtime" {
		t.Fatalf("root.agent_id=%v want agent:root-go-runtime", root["agent_id"])
	}
	if strings.TrimSpace(fmt.Sprint(root["status"])) != "running" {
		t.Fatalf("root.status=%v want running", root["status"])
	}
	children, _ := root["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("root children=%d want 1", len(children))
	}
	child, _ := children[0].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(child["agent_id"])) != "agent:child-go-runtime" {
		t.Fatalf("child.agent_id=%v want agent:child-go-runtime", child["agent_id"])
	}
	if strings.TrimSpace(fmt.Sprint(child["status"])) != "completed" {
		t.Fatalf("child.status=%v want completed", child["status"])
	}
}
