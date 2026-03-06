package jido

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultSocketPath = "/tmp/agentctl-jido.sock"
	defaultRPCPath    = "/rpc"
	defaultTimeout    = 10 * time.Second
)

// JSONRPCClientConfig configures the Unix-socket JSON-RPC bridge client.
type JSONRPCClientConfig struct {
	SocketPath string
	RPCPath    string
	Timeout    time.Duration
}

// JSONRPCClient is a JSON-RPC 2.0 client over HTTP+Unix socket.
type JSONRPCClient struct {
	socketPath string
	rpcPath    string
	timeout    time.Duration
	httpClient *http.Client
	seq        atomic.Uint64
}

// NewJSONRPCClient builds a bridge client that talks to a local Unix socket.
func NewJSONRPCClient(cfg JSONRPCClientConfig) (*JSONRPCClient, error) {
	socket := strings.TrimSpace(cfg.SocketPath)
	if socket == "" {
		socket = defaultSocketPath
	}
	rpcPath := strings.TrimSpace(cfg.RPCPath)
	if rpcPath == "" {
		rpcPath = defaultRPCPath
	}
	if !strings.HasPrefix(rpcPath, "/") {
		rpcPath = "/" + rpcPath
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}

	return &JSONRPCClient{
		socketPath: socket,
		rpcPath:    rpcPath,
		timeout:    timeout,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
		},
	}, nil
}

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *jsonrpcError) Error() string {
	if e == nil {
		return ""
	}
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = "json-rpc error"
	}
	return fmt.Sprintf("%s (code=%d)", msg, e.Code)
}

func (c *JSONRPCClient) nextID() string {
	n := c.seq.Add(1)
	return strconv.FormatUint(n, 10)
}

func (c *JSONRPCClient) call(ctx context.Context, method string, params any, out any) error {
	if c == nil || c.httpClient == nil {
		return fmt.Errorf("json-rpc client is not configured")
	}
	method = strings.TrimSpace(method)
	if method == "" {
		return fmt.Errorf("json-rpc method is required")
	}

	reqBody, err := json.Marshal(jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      c.nextID(),
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return fmt.Errorf("marshal json-rpc request: %w", err)
	}

	ctxWithTimeout := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && c.timeout > 0 {
		ctxWithTimeout, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	url := "http://unix" + c.rpcPath
	httpReq, err := http.NewRequestWithContext(ctxWithTimeout, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("build json-rpc request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("perform json-rpc request to %s: %w", c.socketPath, err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	rawResp, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("read json-rpc response: %w", err)
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return fmt.Errorf("json-rpc http status=%d body=%s", httpResp.StatusCode, strings.TrimSpace(string(rawResp)))
	}

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(rawResp, &rpcResp); err != nil {
		return fmt.Errorf("decode json-rpc response: %w", err)
	}
	if rpcResp.Error != nil {
		return rpcResp.Error
	}
	if out == nil {
		return nil
	}
	if len(rpcResp.Result) == 0 || string(rpcResp.Result) == "null" {
		return nil
	}
	if err := json.Unmarshal(rpcResp.Result, out); err != nil {
		return fmt.Errorf("decode json-rpc result: %w", err)
	}
	return nil
}

// Health returns runtime health details.
func (c *JSONRPCClient) Health(ctx context.Context) (HealthResponse, error) {
	var out HealthResponse
	if err := c.call(ctx, MethodRuntimeHealth, map[string]any{}, &out); err != nil {
		return HealthResponse{}, err
	}
	return out, nil
}

// StartAgent starts one runtime agent.
func (c *JSONRPCClient) StartAgent(ctx context.Context, req StartAgentRequest) (StartAgentResponse, error) {
	var out StartAgentResponse
	if err := c.call(ctx, MethodRuntimeStartAgent, req, &out); err != nil {
		return StartAgentResponse{}, err
	}
	return out, nil
}

// StopAgent stops one runtime agent.
func (c *JSONRPCClient) StopAgent(ctx context.Context, req StopAgentRequest) (StopAgentResponse, error) {
	var out StopAgentResponse
	if err := c.call(ctx, MethodRuntimeStopAgent, req, &out); err != nil {
		return StopAgentResponse{}, err
	}
	return out, nil
}

// Signal dispatches one runtime signal.
func (c *JSONRPCClient) Signal(ctx context.Context, req SignalRequest) (SignalResponse, error) {
	var out SignalResponse
	if err := c.call(ctx, MethodRuntimeSignal, req, &out); err != nil {
		return SignalResponse{}, err
	}
	return out, nil
}

// SpawnChild requests runtime child spawn.
func (c *JSONRPCClient) SpawnChild(ctx context.Context, req SignalRequest) (SignalResponse, error) {
	var out SignalResponse
	if err := c.call(ctx, MethodRuntimeSpawnChild, req, &out); err != nil {
		return SignalResponse{}, err
	}
	return out, nil
}

// Await waits for runtime completion.
func (c *JSONRPCClient) Await(ctx context.Context, req AwaitRequest) (AwaitResponse, error) {
	var out AwaitResponse
	if err := c.call(ctx, MethodRuntimeAwait, req, &out); err != nil {
		return AwaitResponse{}, err
	}
	return out, nil
}

// GetChildren queries runtime children.
func (c *JSONRPCClient) GetChildren(ctx context.Context, req GetChildrenRequest) (GetChildrenResponse, error) {
	var out GetChildrenResponse
	if err := c.call(ctx, MethodRuntimeGetChildren, req, &out); err != nil {
		return GetChildrenResponse{}, err
	}
	return out, nil
}

// State queries runtime state.
func (c *JSONRPCClient) State(ctx context.Context, req StateRequest) (StateResponse, error) {
	var out StateResponse
	if err := c.call(ctx, MethodRuntimeState, req, &out); err != nil {
		return StateResponse{}, err
	}
	return out, nil
}

var _ Client = (*JSONRPCClient)(nil)
