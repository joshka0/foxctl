package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

const EnvDaemonSocketPath = "FOXCTL_DAEMON_SOCKET"

// Client connects to the daemon over Unix socket.
type Client struct {
	socketPath string
}

// NewClient creates a new daemon client.
//
// Index:
//   Purpose: Initialize a daemon client with resolved socket path
//   Keywords: daemon_client, socket_path, unix_socket
//   Related: SocketPath, Client.call
//   Flow: resolve socket path → return client
//   Resources: FOXCTL_DAEMON_SOCKET env
//   Events: none
//   OutputFields: *Client
func NewClient() *Client {
	return &Client{
		socketPath: SocketPath(),
	}
}

// IsRunning checks if the daemon is running by attempting a connection.
func (c *Client) IsRunning() bool {
	return c.isRunningWithTimeout(2 * time.Second)
}

// isRunningWithTimeout checks if the daemon is running with a custom dial timeout.
func (c *Client) isRunningWithTimeout(timeout time.Duration) bool {
	conn, err := net.DialTimeout("unix", c.socketPath, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Status returns the daemon status.
//
// Index:
//   Purpose: Fetch daemon status over RPC
//   Keywords: daemon_status, status, rpc, socket
//   Related: Client.call, marshalResult
//   Flow: call status -> decode result -> return
//   Resources: unix socket
//   Events: none
//   OutputFields: *StatusResult
func (c *Client) Status() (*StatusResult, error) {
	resp, err := c.call("status", nil)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result StatusResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// Run executes a skill via the daemon.
//
// Index:
//   Purpose: Execute a skill through the daemon RPC
//   Keywords: run, skill, workspace, ephemeral, rpc, run_result
//   Related: Client.call, marshalResult
//   Flow: build params -> call run -> decode result -> return
//   Resources: unix socket
//   Events: none
//   OutputFields: *RunResult
func (c *Client) Run(skill string, input []byte, workspace string, ephemeral bool) (*RunResult, error) {
	params := RunParams{
		Skill:     skill,
		Input:     input,
		Workspace: workspace,
		Ephemeral: ephemeral,
	}

	resp, err := c.call("run", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result RunResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// Warm requests the daemon to pre-warm a workspace.
func (c *Client) Warm(workspace string) error {
	params := WarmParams{
		Workspace: workspace,
	}

	resp, err := c.call("warm", params)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	return nil
}

// Shutdown requests the daemon to shut down.
func (c *Client) Shutdown() error {
	resp, err := c.call("shutdown", nil)
	if err != nil {
		// Connection closed is expected during shutdown
		return nil
	}

	if resp.Error != nil {
		return fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	return nil
}

// AgentSpawn spawns a new agent via the daemon.
//
// Index:
//   Purpose: Request a new agent session from daemon
//   Keywords: agent_spawn, agent.spawn, session_id, rpc
//   Related: Client.call, marshalResult
//   Flow: call agent.spawn -> decode result -> return
//   Resources: unix socket
//   Events: none
//   OutputFields: *AgentSpawnResult
func (c *Client) AgentSpawn(params AgentSpawnParams) (*AgentSpawnResult, error) {
	resp, err := c.call("agent.spawn", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result AgentSpawnResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// AgentList lists active agent sessions.
func (c *Client) AgentList() (*AgentListResult, error) {
	resp, err := c.call("agent.list", nil)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result AgentListResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// AgentStatus gets the status of an agent session.
//
// Index:
//   Purpose: Fetch agent session status from daemon
//   Keywords: agent_status, agent.status, session_id, rpc
//   Related: Client.call, marshalResult
//   Flow: call agent.status -> decode result -> return
//   Resources: unix socket
//   Events: none
//   OutputFields: *AgentStatusResult
func (c *Client) AgentStatus(sessionID string) (*AgentStatusResult, error) {
	params := AgentStatusParams{SessionID: sessionID}
	resp, err := c.call("agent.status", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result AgentStatusResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// AgentKill terminates an agent session.
//
// Index:
//   Purpose: Request termination of an agent session
//   Keywords: agent_kill, agent.kill, session_id, rpc
//   Related: Client.call, marshalResult
//   Flow: call agent.kill -> decode result -> return
//   Resources: unix socket
//   Events: none
//   OutputFields: *AgentKillResult
func (c *Client) AgentKill(sessionID string) (*AgentKillResult, error) {
	params := AgentKillParams{SessionID: sessionID}
	resp, err := c.call("agent.kill", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result AgentKillResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// AgentAsk sends a message to a persistent agent and waits for its reply.
func (c *Client) AgentAsk(params AgentAskParams) (*AgentAskResult, error) {
	resp, err := c.call("agent.ask", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result AgentAskResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// AgentResume continues a previous agent session.
func (c *Client) AgentResume(params AgentResumeParams) (*AgentResumeResult, error) {
	resp, err := c.call("agent.resume", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result AgentResumeResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// AgentHierarchyParams are the parameters for agent.hierarchy.
type AgentHierarchyParams struct {
	SessionID string `json:"session_id,omitempty"` // Optional, defaults to all roots
}

// AgentHierarchyResult is the result of an agent hierarchy query.
type AgentHierarchyResult struct {
	Nodes []HierarchyNode `json:"nodes"`
}

// HierarchyNode represents a node in the agent hierarchy tree.
type HierarchyNode struct {
	SessionID string          `json:"session_id"`
	ActorID   string          `json:"actor_id"`
	Role      string          `json:"role"`
	Depth     int             `json:"depth"`
	Status    string          `json:"status"`
	Children  []HierarchyNode `json:"children,omitempty"`
}

// AgentHierarchy gets the agent hierarchy tree.
func (c *Client) AgentHierarchy(sessionID string) (*AgentHierarchyResult, error) {
	params := AgentHierarchyParams{SessionID: sessionID}
	resp, err := c.call("agent.hierarchy", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result AgentHierarchyResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// connect establishes a connection to the daemon socket.
func (c *Client) connect() (net.Conn, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}
	return conn, nil
}

// call makes a request to the daemon and returns the response.
//
// Index:
//   Purpose: Send a JSON request over the daemon socket and decode response
//   Keywords: daemon_call, json_rpc, socket, request, response
//   Related: Client.connect, Client.EnsureRunningContext
//   Flow: connect → set deadline → encode request → decode response → return
//   Resources: unix socket
//   Events: none
//   OutputFields: *Response
//
// [[protocol:daemon-rpc]]
// [[invariant:request-response-correlation]]
func (c *Client) call(method string, params any) (*Response, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Set timeouts
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, fmt.Errorf("set deadline: %w", err)
	}

	// Build request
	req := Request{
		Method: method,
		ID:     fmt.Sprintf("%d", time.Now().UnixNano()),
	}

	if params != nil {
		paramsBytes, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		req.Params = paramsBytes
	}

	// Send request
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	// Read response
	decoder := json.NewDecoder(conn)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return &resp, nil
}

// SocketPath returns the daemon socket path.
func SocketPath() string {
	if socketPath := os.Getenv(EnvDaemonSocketPath); socketPath != "" {
		return socketPath
	}

	// Use XDG_RUNTIME_DIR if available (Linux)
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "foxctl.sock")
	}

	// Fall back to /tmp with UID for security
	return fmt.Sprintf("/tmp/foxctl-%d.sock", os.Getuid())
}

// PIDPath returns the daemon PID file path.
func PIDPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fall back to /tmp if home directory is unavailable
		return filepath.Join("/tmp", fmt.Sprintf("foxctl-%d.pid", os.Getuid()))
	}
	return filepath.Join(home, ".foxctl", "daemon.pid")
}

// ErrDaemonNotRunning is returned when the daemon is not running.
var ErrDaemonNotRunning = errors.New("daemon not running")

// EnsureRunning starts the daemon if it's not already running.
// It returns nil if the daemon is now running, or an error if it couldn't be started.
// This is a convenience wrapper that uses a 10-second timeout.
func (c *Client) EnsureRunning() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.EnsureRunningContext(ctx)
}

// EnsureRunningContext starts the daemon if it's not already running, with context support.
// It returns nil if the daemon is now running, or an error if it couldn't be started.
//
// Index:
//   Purpose: Ensure a daemon process is running before issuing requests
//   Keywords: ensure_running, daemonize, socket, poll_interval, timeout
//   Related: Client.isRunningWithTimeout, Daemonize
//   Flow: check context → probe socket → daemonize → poll until ready
//   Resources: unix socket, os.Executable
//   Events: none
//   OutputFields: error
//
// [[protocol:daemon-lifecycle]]
// [[risk:daemon-start-timeout]]
func (c *Client) EnsureRunningContext(ctx context.Context) error {
	// Check context before starting
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Use short dial timeout for quick checks
	const pollDialTimeout = 200 * time.Millisecond
	const pollInterval = 200 * time.Millisecond

	if c.isRunningWithTimeout(pollDialTimeout) {
		return nil
	}

	// Start daemon in background
	if err := Daemonize(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// Wait for daemon to be ready, respecting context
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("daemon started but not responding: %w", ctx.Err())
		case <-ticker.C:
			if c.isRunningWithTimeout(pollDialTimeout) {
				return nil
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Flow RPC Client Methods
// ---------------------------------------------------------------------------

// FlowStart starts a flow execution via the daemon.
func (c *Client) FlowStart(flowID, workspace string) (*FlowStartResult, error) {
	params := FlowStartParams{
		FlowID:    flowID,
		Workspace: workspace,
	}

	resp, err := c.call("flow.start", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result FlowStartResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// FlowStop stops a running or paused flow via the daemon.
func (c *Client) FlowStop(flowID, workspace string) (*FlowStopResult, error) {
	params := FlowStopParams{
		FlowID:    flowID,
		Workspace: workspace,
	}

	resp, err := c.call("flow.stop", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result FlowStopResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// FlowPause pauses a running flow via the daemon.
func (c *Client) FlowPause(flowID, workspace string) (*FlowPauseResult, error) {
	params := FlowPauseParams{
		FlowID:    flowID,
		Workspace: workspace,
	}

	resp, err := c.call("flow.pause", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result FlowPauseResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// FlowStatus returns the current status of a flow via the daemon.
func (c *Client) FlowStatus(flowID, workspace string) (*FlowStatusResult, error) {
	params := FlowStatusParams{
		FlowID:    flowID,
		Workspace: workspace,
	}

	resp, err := c.call("flow.status", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result FlowStatusResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// FlowOutput pushes structured output data into a running flow's OutputBus via the daemon.
// Either flowID or runID should be provided. If flowID is empty, the daemon resolves it from runID.
func (c *Client) FlowOutput(flowID, runID, nodeID string, data json.RawMessage, workspace string) (*FlowOutputResult, error) {
	params := FlowOutputParams{
		FlowID:    flowID,
		RunID:     runID,
		NodeID:    nodeID,
		Data:      data,
		Workspace: workspace,
	}

	resp, err := c.call("flow.output", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result FlowOutputResult
	payload, err := marshalResult(resp.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &result, nil
}

// marshalResult marshals the given value to JSON and returns the resulting bytes.
// If marshaling fails, the error is wrapped with context "marshal result".
func marshalResult(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return b, nil
}
