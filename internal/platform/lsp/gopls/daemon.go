package gopls

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
)

// debug enables verbose logging when AGENTCTL_GOPLS_DEBUG=1
var debug = os.Getenv("AGENTCTL_GOPLS_DEBUG") == "1"

func debugLog(format string, args ...any) {
	if debug {
		fmt.Fprintf(os.Stderr, "[gopls-daemon] "+format+"\n", args...)
	}
}

// Daemon manages a persistent gopls process.
type Daemon struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	requestID atomic.Int64
	workspace string
	ready     bool
}

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Global daemon instance (one per process)
var (
	globalDaemon *Daemon
	daemonMu     sync.Mutex
)

// GetDaemon returns the global daemon instance, starting it if needed.
func GetDaemon(ctx context.Context, workspace string) (*Daemon, error) {
	debugLog("GetDaemon called for workspace: %s", workspace)
	daemonMu.Lock()
	defer daemonMu.Unlock()

	// Check if existing daemon is for the same workspace and still alive
	if globalDaemon != nil {
		if globalDaemon.workspace == workspace {
			if globalDaemon.isAlive() {
				debugLog("Reusing existing daemon")
				return globalDaemon, nil
			}
			// Daemon died, clean up
			debugLog("Daemon died, cleaning up")
			globalDaemon.Close()
			globalDaemon = nil
		} else {
			// Different workspace requested, close existing daemon
			debugLog("Workspace changed from %s to %s, closing existing daemon", globalDaemon.workspace, workspace)
			if err := globalDaemon.Close(); err != nil {
				debugLog("Warning: failed to close existing daemon: %v", err)
			}
			globalDaemon = nil
		}
	}

	// Start new daemon
	debugLog("Starting new daemon")
	d, err := startDaemon(ctx, workspace)
	if err != nil {
		debugLog("Failed to start daemon: %v", err)
		return nil, err
	}
	debugLog("Daemon started successfully")
	globalDaemon = d
	return d, nil
}

// IsDaemonReady returns true if a gopls daemon is already running for the workspace.
// Unlike GetDaemon, this does NOT start the daemon if it's not running.
// Use this to avoid cold-start delays in performance-sensitive code paths like hooks.
func IsDaemonReady(workspace string) bool {
	daemonMu.Lock()
	defer daemonMu.Unlock()

	if globalDaemon == nil {
		return false
	}
	if globalDaemon.workspace != workspace {
		return false
	}
	return globalDaemon.isAlive()
}

// startDaemon spawns a new gopls process.
// Note: First request will be slow (~30-40s) as gopls loads packages.
// Subsequent requests reusing the same daemon are much faster (~100ms).
func startDaemon(ctx context.Context, workspace string) (*Daemon, error) {
	goplsPath, err := executil.RequireTool("gopls", "install gopls (go install golang.org/x/tools/gopls@latest)")
	if err != nil {
		return nil, fmt.Errorf("gopls not found: %w", err)
	}
	debugLog("Found gopls at: %s", goplsPath)

	// Use exec.Command (not CommandContext) so the daemon persists across
	// multiple skill invocations. The ctx is only used for initialization timeout.
	cmd := exec.Command(goplsPath, "serve")
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(),
		"GOFLAGS=-mod=readonly", // Faster startup
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	// Capture stderr for debugging
	var stderrWriter io.Writer = io.Discard
	if debug {
		stderrWriter = os.Stderr
	}
	cmd.Stderr = stderrWriter

	debugLog("Starting gopls process...")
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("start gopls: %w", err)
	}
	debugLog("gopls process started, PID: %d", cmd.Process.Pid)

	d := &Daemon{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    bufio.NewReader(stdout),
		workspace: workspace,
	}

	// Initialize LSP connection
	debugLog("Initializing LSP connection...")
	if err := d.initialize(ctx); err != nil {
		d.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}

	debugLog("LSP connection initialized")
	d.ready = true
	return d, nil
}

// initialize performs LSP handshake.
func (d *Daemon) initialize(ctx context.Context) error {
	debugLog("Preparing initialize params...")
	initParams := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   "file://" + d.workspace,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"definition":     map[string]any{},
				"references":     map[string]any{},
				"implementation": map[string]any{},
				"callHierarchy":  map[string]any{},
				"documentSymbol": map[string]any{},
			},
			"workspace": map[string]any{
				"symbol": map[string]any{},
			},
		},
	}

	debugLog("Sending initialize request...")
	var result json.RawMessage
	if err := d.call(ctx, "initialize", initParams, &result); err != nil {
		return fmt.Errorf("initialize request: %w", err)
	}
	debugLog("Initialize response received (%d bytes)", len(result))

	// Send initialized notification
	debugLog("Sending initialized notification...")
	if err := d.notify("initialized", map[string]any{}); err != nil {
		return fmt.Errorf("initialized notification: %w", err)
	}
	debugLog("Initialized notification sent")

	return nil
}

// call sends an LSP request and waits for response.
func (d *Daemon) call(ctx context.Context, method string, params any, result any) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	id := d.requestID.Add(1)
	debugLog("call(%s) id=%d", method, id)

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := d.writeMessage(req); err != nil {
		debugLog("call(%s) write error: %v", method, err)
		return fmt.Errorf("write request: %w", err)
	}
	debugLog("call(%s) request written, waiting for response...", method)

	resp, err := d.readResponse(ctx, id)
	if err != nil {
		debugLog("call(%s) read error: %v", method, err)
		return fmt.Errorf("read response: %w", err)
	}
	debugLog("call(%s) response received", method)

	if resp.Error != nil {
		debugLog("call(%s) LSP error: %d - %s", method, resp.Error.Code, resp.Error.Message)
		return fmt.Errorf("LSP error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("unmarshal result: %w", err)
		}
	}

	return nil
}

// notify sends an LSP notification (no response expected).
func (d *Daemon) notify(method string, params any) error {
	// Notifications have no ID (unlike requests)
	notif := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return d.writeMessage(notif)
}

// writeMessage writes a JSON-RPC message with LSP headers.
func (d *Daemon) writeMessage(msg any) error {
	content, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	debugLog("writeMessage: %s", string(content))
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(content))
	if _, err := d.stdin.Write([]byte(header)); err != nil {
		debugLog("writeMessage header error: %v", err)
		return err
	}
	if _, err := d.stdin.Write(content); err != nil {
		debugLog("writeMessage content error: %v", err)
		return err
	}
	debugLog("writeMessage: sent %d bytes", len(content))
	return nil
}

// readResponse reads a JSON-RPC response with the given ID.
func (d *Daemon) readResponse(ctx context.Context, expectedID int64) (*JSONRPCResponse, error) {
	// Set deadline if context has timeout
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(30 * time.Second)
	}
	debugLog("readResponse(id=%d) deadline in %v", expectedID, time.Until(deadline))

	for {
		// Check for context cancellation first
		if err := ctx.Err(); err != nil {
			debugLog("readResponse(id=%d) context canceled: %v", expectedID, err)
			return nil, err
		}

		if time.Now().After(deadline) {
			debugLog("readResponse(id=%d) deadline exceeded", expectedID)
			return nil, context.DeadlineExceeded
		}

		// Read Content-Length header
		var contentLength int
		debugLog("readResponse(id=%d) reading headers...", expectedID)
		for {
			line, err := d.stdout.ReadString('\n')
			if err != nil {
				debugLog("readResponse(id=%d) header read error: %v", expectedID, err)
				return nil, fmt.Errorf("read header: %w", err)
			}
			line = line[:len(line)-1] // Remove \n
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1] // Remove \r
			}

			if line == "" {
				break // End of headers
			}

			if n, _ := fmt.Sscanf(line, "Content-Length: %d", &contentLength); n == 1 {
				debugLog("readResponse(id=%d) Content-Length: %d", expectedID, contentLength)
				continue
			}
		}

		if contentLength == 0 {
			debugLog("readResponse(id=%d) empty message, skipping", expectedID)
			continue
		}

		// Read content
		debugLog("readResponse(id=%d) reading %d bytes of content...", expectedID, contentLength)
		content := make([]byte, contentLength)
		if _, err := io.ReadFull(d.stdout, content); err != nil {
			debugLog("readResponse(id=%d) content read error: %v", expectedID, err)
			return nil, fmt.Errorf("read content: %w", err)
		}
		debugLog("readResponse(id=%d) content: %.100s...", expectedID, string(content))

		// Try to parse as response
		var resp JSONRPCResponse
		if err := json.Unmarshal(content, &resp); err != nil {
			debugLog("readResponse(id=%d) parse error (notification?): %v", expectedID, err)
			continue // Might be a notification, skip
		}

		// Check if this is the response we're waiting for
		debugLog("readResponse(id=%d) got response with id=%d", expectedID, resp.ID)
		if resp.ID == expectedID {
			return &resp, nil
		}
		// Otherwise it's a notification or different response, skip
		debugLog("readResponse(id=%d) skipping message with different id", expectedID)
	}
}

// isAlive checks if the gopls process is still running.
func (d *Daemon) isAlive() bool {
	if d.cmd == nil || d.cmd.Process == nil {
		return false
	}
	// Check if process is still running
	return d.cmd.ProcessState == nil
}

// Close shuts down the daemon.
func (d *Daemon) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stdin != nil {
		d.stdin.Close()
	}
	if d.cmd != nil && d.cmd.Process != nil {
		d.cmd.Process.Kill()
		d.cmd.Wait()
	}
	return nil
}

// Definition returns the definition location for a symbol.
func (d *Daemon) Definition(ctx context.Context, file string, line, col int) ([]Location, error) {
	if !filepath.IsAbs(file) {
		file = filepath.Join(d.workspace, file)
	}

	params := map[string]any{
		"textDocument": map[string]any{
			"uri": "file://" + file,
		},
		"position": map[string]any{
			"line":      line - 1, // LSP is 0-indexed
			"character": col - 1,
		},
	}

	var result json.RawMessage
	if err := d.call(ctx, "textDocument/definition", params, &result); err != nil {
		return nil, err
	}

	return parseLocations(result, d.workspace)
}

// References returns all references to a symbol.
func (d *Daemon) References(ctx context.Context, file string, line, col int) ([]Location, error) {
	if !filepath.IsAbs(file) {
		file = filepath.Join(d.workspace, file)
	}

	params := map[string]any{
		"textDocument": map[string]any{
			"uri": "file://" + file,
		},
		"position": map[string]any{
			"line":      line - 1,
			"character": col - 1,
		},
		"context": map[string]any{
			"includeDeclaration": true,
		},
	}

	var result json.RawMessage
	if err := d.call(ctx, "textDocument/references", params, &result); err != nil {
		return nil, err
	}

	return parseLocations(result, d.workspace)
}

// Implementation returns implementations of an interface.
func (d *Daemon) Implementation(ctx context.Context, file string, line, col int) ([]Location, error) {
	if !filepath.IsAbs(file) {
		file = filepath.Join(d.workspace, file)
	}

	params := map[string]any{
		"textDocument": map[string]any{
			"uri": "file://" + file,
		},
		"position": map[string]any{
			"line":      line - 1,
			"character": col - 1,
		},
	}

	var result json.RawMessage
	if err := d.call(ctx, "textDocument/implementation", params, &result); err != nil {
		return nil, err
	}

	return parseLocations(result, d.workspace)
}

// Location represents an LSP location.
type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// parseLocations parses LSP location responses.
func parseLocations(data json.RawMessage, workspace string) ([]Location, error) {
	// LSP can return a single Location, []Location, or []LocationLink
	// Initialize to empty slice to avoid nil JSON serialization
	locs := []Location{}

	// Try array first
	var arr []struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
		} `json:"range"`
		// LocationLink fields
		TargetURI   string `json:"targetUri"`
		TargetRange *struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
		} `json:"targetRange"`
	}

	if err := json.Unmarshal(data, &arr); err == nil {
		for _, item := range arr {
			uri := item.URI
			line := item.Range.Start.Line
			col := item.Range.Start.Character

			// Handle LocationLink format
			if item.TargetURI != "" {
				uri = item.TargetURI
				if item.TargetRange != nil {
					line = item.TargetRange.Start.Line
					col = item.TargetRange.Start.Character
				}
			}

			file := uriToPath(uri)
			if rel, err := filepath.Rel(workspace, file); err == nil {
				file = rel
			}

			locs = append(locs, Location{
				File:   file,
				Line:   line + 1, // Convert to 1-indexed
				Column: col + 1,
			})
		}
		return locs, nil
	}

	// Try single location
	var single struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
		} `json:"range"`
	}

	if err := json.Unmarshal(data, &single); err == nil && single.URI != "" {
		file := uriToPath(single.URI)
		if rel, err := filepath.Rel(workspace, file); err == nil {
			file = rel
		}
		locs = append(locs, Location{
			File:   file,
			Line:   single.Range.Start.Line + 1,
			Column: single.Range.Start.Character + 1,
		})
	}

	return locs, nil
}

func uriToPath(uri string) string {
	if len(uri) > 7 && uri[:7] == "file://" {
		return uri[7:]
	}
	return uri
}
