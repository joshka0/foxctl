package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Client connects to the daemon over Unix socket.
type Client struct {
	socketPath string
}

// NewClient creates a new daemon client.
func NewClient() *Client {
	return &Client{
		socketPath: SocketPath(),
	}
}

// IsRunning checks if the daemon is running by attempting a connection.
func (c *Client) IsRunning() bool {
	conn, err := c.connect()
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Status returns the daemon status.
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
	// Use XDG_RUNTIME_DIR if available (Linux)
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "agentctl.sock")
	}

	// Fall back to /tmp with UID for security
	return fmt.Sprintf("/tmp/agentctl-%d.sock", os.Getuid())
}

// PIDPath returns the daemon PID file path.
func PIDPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fall back to /tmp if home directory is unavailable
		return filepath.Join("/tmp", fmt.Sprintf("agentctl-%d.pid", os.Getuid()))
	}
	return filepath.Join(home, ".agentctl", "daemon.pid")
}

// ErrDaemonNotRunning is returned when the daemon is not running.
var ErrDaemonNotRunning = errors.New("daemon not running")

func marshalResult(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return b, nil
}
