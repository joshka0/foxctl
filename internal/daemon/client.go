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
