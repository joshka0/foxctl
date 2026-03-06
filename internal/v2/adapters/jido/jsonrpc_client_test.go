package jido

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJSONRPCClient_Signal(t *testing.T) {
	t.Parallel()

	server, socketPath := startJSONRPCServer(t, func(method string, params json.RawMessage) (any, *jsonrpcError) {
		if method != MethodRuntimeSignal {
			return nil, &jsonrpcError{Code: -32601, Message: "method not found"}
		}
		var req SignalRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &jsonrpcError{Code: -32602, Message: err.Error()}
		}
		if req.AgentID != "agent-1" {
			return nil, &jsonrpcError{Code: -32000, Message: "unexpected agent_id"}
		}
		return SignalResponse{
			AgentID:   req.AgentID,
			MessageID: "msg-1",
			Status:    "sent",
		}, nil
	})
	defer server.Close()

	client, err := NewJSONRPCClient(JSONRPCClientConfig{
		SocketPath: socketPath,
		Timeout:    3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewJSONRPCClient() error = %v", err)
	}

	resp, err := client.Signal(context.Background(), SignalRequest{
		AgentID: "agent-1",
		Signal: Signal{
			ID:     "sig-1",
			Type:   "agent.ask",
			Source: "/tests",
			Data:   json.RawMessage(`{"question":"hi"}`),
		},
		Mode: SignalModeCall,
	})
	if err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	if resp.MessageID != "msg-1" {
		t.Fatalf("message_id=%q want msg-1", resp.MessageID)
	}
}

func TestJSONRPCClient_SpawnChild(t *testing.T) {
	t.Parallel()

	server, socketPath := startJSONRPCServer(t, func(method string, params json.RawMessage) (any, *jsonrpcError) {
		if method != MethodRuntimeSpawnChild {
			return nil, &jsonrpcError{Code: -32601, Message: "method not found"}
		}
		var req SignalRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &jsonrpcError{Code: -32602, Message: err.Error()}
		}
		if req.AgentID != "agent:parent" {
			return nil, &jsonrpcError{Code: -32000, Message: "unexpected agent_id"}
		}
		return SignalResponse{
			AgentID:   req.AgentID,
			MessageID: "spawn-1",
			Status:    "spawned",
		}, nil
	})
	defer server.Close()

	client, err := NewJSONRPCClient(JSONRPCClientConfig{
		SocketPath: socketPath,
		Timeout:    3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewJSONRPCClient() error = %v", err)
	}

	resp, err := client.SpawnChild(context.Background(), SignalRequest{
		AgentID: "agent:parent",
		Signal: Signal{
			ID:     "req-1",
			Type:   DefaultSpawnChildSignal,
			Source: "/tests",
			Data:   json.RawMessage(`{"tag":"worker-1","child_id":"agent:worker-1","profile":"worker"}`),
		},
		Mode: SignalModeCall,
	})
	if err != nil {
		t.Fatalf("SpawnChild() error = %v", err)
	}
	if resp.MessageID != "spawn-1" {
		t.Fatalf("message_id=%q want spawn-1", resp.MessageID)
	}
}

func TestJSONRPCClient_JSONRPCError(t *testing.T) {
	t.Parallel()

	server, socketPath := startJSONRPCServer(t, func(string, json.RawMessage) (any, *jsonrpcError) {
		return nil, &jsonrpcError{Code: -32001, Message: "boom"}
	})
	defer server.Close()

	client, err := NewJSONRPCClient(JSONRPCClientConfig{
		SocketPath: socketPath,
		Timeout:    3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewJSONRPCClient() error = %v", err)
	}

	_, err = client.Health(context.Background())
	if err == nil {
		t.Fatal("expected json-rpc error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error=%q want contains boom", err.Error())
	}
}

type rpcHandler func(method string, params json.RawMessage) (any, *jsonrpcError)

func startJSONRPCServer(t *testing.T, handle rpcHandler) (*http.Server, string) {
	t.Helper()

	socketPath := filepath.Join(t.TempDir(), "jido.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}

	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc(defaultRPCPath, func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      nil,
				"error":   map[string]any{"code": -32700, "message": err.Error()},
			})
			return
		}

		mu.Lock()
		result, rpcErr := handle(req.Method, req.Params)
		mu.Unlock()

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

	server := &http.Server{
		Handler: mux,
	}
	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
		_ = os.Remove(socketPath)
	})

	return server, socketPath
}
