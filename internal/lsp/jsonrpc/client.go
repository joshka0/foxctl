// Package jsonrpc provides a JSON-RPC 2.0 client for LSP communication.
// It handles Content-Length framing used by LSP servers over stdio.
package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Client is a JSON-RPC 2.0 client that communicates via Content-Length framed messages.
// It is safe for concurrent use.
type Client struct {
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID atomic.Int64

	mu sync.Mutex // protects writes and reads
}

// NewClient creates a new JSON-RPC client from stdin/stdout pipes.
// The caller is responsible for managing the underlying process.
func NewClient(stdin io.WriteCloser, stdout io.Reader) *Client {
	return &Client{
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}
}

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Notification represents a JSON-RPC 2.0 notification (no ID, no response expected).
type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error represents a JSON-RPC 2.0 error.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// Call sends a request and waits for a response.
// Returns the result as raw JSON, or an error if the call fails.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID.Add(1)

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := c.writeMessage(req); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	resp, err := c.readResponse(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	return resp.Result, nil
}

// CallInto sends a request and unmarshals the result into the provided value.
func (c *Client) CallInto(ctx context.Context, method string, params any, result any) error {
	raw, err := c.Call(ctx, method, params)
	if err != nil {
		return err
	}

	if result != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, result); err != nil {
			return fmt.Errorf("unmarshal result: %w", err)
		}
	}

	return nil
}

// Notify sends a notification (no ID, no response expected).
func (c *Client) Notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	notif := Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	return c.writeMessage(notif)
}

// writeMessage writes a JSON-RPC message with Content-Length framing.
func (c *Client) writeMessage(msg any) error {
	content, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(content))
	if _, err := c.stdin.Write([]byte(header)); err != nil {
		return err
	}
	if _, err := c.stdin.Write(content); err != nil {
		return err
	}

	return nil
}

// readResponse reads a response with the expected ID.
// It skips notifications and responses with different IDs.
func (c *Client) readResponse(ctx context.Context, expectedID int64) (*Response, error) {
	// Set deadline
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(30 * time.Second)
	}

	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if time.Now().After(deadline) {
			return nil, context.DeadlineExceeded
		}

		// Read Content-Length header
		contentLength, err := c.readHeaders()
		if err != nil {
			return nil, err
		}

		if contentLength == 0 {
			continue
		}

		// Read content
		content := make([]byte, contentLength)
		if _, err := io.ReadFull(c.stdout, content); err != nil {
			return nil, fmt.Errorf("read content: %w", err)
		}

		// Parse as response
		var resp Response
		if err := json.Unmarshal(content, &resp); err != nil {
			// Might be a notification or malformed, skip
			continue
		}

		// Check if this is our response
		if resp.ID == expectedID {
			return &resp, nil
		}

		// Otherwise it's a notification or different response, skip
	}
}

// readHeaders reads LSP headers and returns the Content-Length value.
func (c *Client) readHeaders() (int, error) {
	var contentLength int

	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return 0, fmt.Errorf("read header: %w", err)
		}

		// Trim CRLF
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")

		if line == "" {
			break // End of headers
		}

		if n, _ := fmt.Sscanf(line, "Content-Length: %d", &contentLength); n == 1 {
			continue
		}
		// Ignore other headers (Content-Type, etc.)
	}

	return contentLength, nil
}

// ReadMessage reads the next message from the server.
// This can be a response or a server-initiated notification.
// Useful for handling server push notifications.
func (c *Client) ReadMessage(ctx context.Context) (*Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	contentLength, err := c.readHeaders()
	if err != nil {
		return nil, err
	}

	if contentLength == 0 {
		return nil, fmt.Errorf("empty message")
	}

	content := make([]byte, contentLength)
	if _, err := io.ReadFull(c.stdout, content); err != nil {
		return nil, fmt.Errorf("read content: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(content, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal message: %w", err)
	}

	return &resp, nil
}

// Close closes the stdin writer.
// The caller should handle closing the underlying process.
func (c *Client) Close() error {
	return c.stdin.Close()
}
