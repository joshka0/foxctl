package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// mockWriteCloser wraps a buffer with a Close method.
type mockWriteCloser struct {
	buf    *bytes.Buffer
	closed bool
}

func (m *mockWriteCloser) Write(p []byte) (n int, err error) {
	if m.closed {
		return 0, io.ErrClosedPipe
	}
	return m.buf.Write(p)
}

func (m *mockWriteCloser) Close() error {
	m.closed = true
	return nil
}

func TestNewClient(t *testing.T) {
	stdin := &mockWriteCloser{buf: &bytes.Buffer{}}
	stdout := strings.NewReader("")

	client := NewClient(stdin, stdout)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestClient_Notify(t *testing.T) {
	stdin := &mockWriteCloser{buf: &bytes.Buffer{}}
	stdout := strings.NewReader("")

	client := NewClient(stdin, stdout)

	err := client.Notify("initialized", map[string]any{})
	if err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	// Check output has Content-Length header
	output := stdin.buf.String()
	if !strings.HasPrefix(output, "Content-Length:") {
		t.Errorf("Output missing Content-Length header: %q", output)
	}

	// Parse the message
	parts := strings.SplitN(output, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("Invalid output format: %q", output)
	}

	var notif Notification
	if err := json.Unmarshal([]byte(parts[1]), &notif); err != nil {
		t.Fatalf("Failed to unmarshal notification: %v", err)
	}

	if notif.JSONRPC != "2.0" {
		t.Errorf("JSONRPC = %q, want %q", notif.JSONRPC, "2.0")
	}
	if notif.Method != "initialized" {
		t.Errorf("Method = %q, want %q", notif.Method, "initialized")
	}
}

func TestClient_Call(t *testing.T) {
	stdin := &mockWriteCloser{buf: &bytes.Buffer{}}

	// Prepare mock response
	result := map[string]any{
		"capabilities": map[string]any{},
	}
	resultBytes, _ := json.Marshal(result)
	resp := Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  resultBytes,
	}
	respBytes, _ := json.Marshal(resp)
	mockResponse := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(respBytes), respBytes)
	stdout := strings.NewReader(mockResponse)

	client := NewClient(stdin, stdout)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	raw, err := client.Call(ctx, "initialize", map[string]any{"processId": 123})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	// Check result
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}
	if got["capabilities"] == nil {
		t.Error("Result missing capabilities")
	}
}

func TestClient_CallInto(t *testing.T) {
	stdin := &mockWriteCloser{buf: &bytes.Buffer{}}

	// Prepare mock response
	type InitResult struct {
		Capabilities map[string]any `json:"capabilities"`
	}
	result := InitResult{
		Capabilities: map[string]any{"textDocument": map[string]any{}},
	}
	resultBytes, _ := json.Marshal(result)
	resp := Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  resultBytes,
	}
	respBytes, _ := json.Marshal(resp)
	mockResponse := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(respBytes), respBytes)
	stdout := strings.NewReader(mockResponse)

	client := NewClient(stdin, stdout)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var got InitResult
	err := client.CallInto(ctx, "initialize", nil, &got)
	if err != nil {
		t.Fatalf("CallInto failed: %v", err)
	}

	if got.Capabilities == nil {
		t.Error("Result missing capabilities")
	}
}

func TestClient_Call_Error(t *testing.T) {
	stdin := &mockWriteCloser{buf: &bytes.Buffer{}}

	// Prepare error response
	resp := Response{
		JSONRPC: "2.0",
		ID:      1,
		Error: &Error{
			Code:    -32601,
			Message: "Method not found",
		},
	}
	respBytes, _ := json.Marshal(resp)
	mockResponse := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(respBytes), respBytes)
	stdout := strings.NewReader(mockResponse)

	client := NewClient(stdin, stdout)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Call(ctx, "unknownMethod", nil)
	if err == nil {
		t.Fatal("Call should have failed")
	}

	// Check error message
	if !strings.Contains(err.Error(), "Method not found") {
		t.Errorf("Error message = %q, should contain 'Method not found'", err.Error())
	}
}

func TestClient_Call_ContextCanceled(t *testing.T) {
	stdin := &mockWriteCloser{buf: &bytes.Buffer{}}

	// Create a pipe that will return EOF (simulating closed connection)
	// when context is already canceled
	stdout := strings.NewReader("") // Empty reader returns EOF immediately

	client := NewClient(stdin, stdout)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.Call(ctx, "initialize", nil)
	if err == nil {
		t.Fatal("Call should have failed")
	}
	// Error can be context.Canceled or EOF depending on timing
}

func TestClient_Call_EmptyResponse(t *testing.T) {
	stdin := &mockWriteCloser{buf: &bytes.Buffer{}}

	// Empty response (no Content-Length) - should fail
	stdout := strings.NewReader("")

	client := NewClient(stdin, stdout)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.Call(ctx, "initialize", nil)
	if err == nil {
		t.Fatal("Call should have failed with empty response")
	}
}

func TestClient_Call_SkipsNotifications(t *testing.T) {
	stdin := &mockWriteCloser{buf: &bytes.Buffer{}}

	// Send a notification first, then the response
	notification := map[string]any{
		"jsonrpc": "2.0",
		"method":  "window/logMessage",
		"params":  map[string]any{"message": "test"},
	}
	notifBytes, _ := json.Marshal(notification)

	resp := Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  []byte(`"ok"`),
	}
	respBytes, _ := json.Marshal(resp)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n%s", len(notifBytes), notifBytes)
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n%s", len(respBytes), respBytes)

	client := NewClient(stdin, &buf)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	raw, err := client.Call(ctx, "test", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if string(raw) != `"ok"` {
		t.Errorf("Result = %q, want %q", string(raw), `"ok"`)
	}
}

func TestClient_Call_SkipsDifferentID(t *testing.T) {
	stdin := &mockWriteCloser{buf: &bytes.Buffer{}}

	// Send response with wrong ID first, then correct one
	wrongResp := Response{
		JSONRPC: "2.0",
		ID:      999,
		Result:  []byte(`"wrong"`),
	}
	wrongBytes, _ := json.Marshal(wrongResp)

	correctResp := Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  []byte(`"correct"`),
	}
	correctBytes, _ := json.Marshal(correctResp)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n%s", len(wrongBytes), wrongBytes)
	buf.WriteString(fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(correctBytes), correctBytes))

	client := NewClient(stdin, &buf)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	raw, err := client.Call(ctx, "test", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if string(raw) != `"correct"` {
		t.Errorf("Result = %q, want %q", string(raw), `"correct"`)
	}
}

func TestClient_Close(t *testing.T) {
	stdin := &mockWriteCloser{buf: &bytes.Buffer{}}
	stdout := strings.NewReader("")

	client := NewClient(stdin, stdout)
	err := client.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if !stdin.closed {
		t.Error("stdin should be closed")
	}
}

func TestError_Error(t *testing.T) {
	err := &Error{
		Code:    -32601,
		Message: "Method not found",
	}

	got := err.Error()
	if !strings.Contains(got, "-32601") {
		t.Errorf("Error() = %q, should contain code", got)
	}
	if !strings.Contains(got, "Method not found") {
		t.Errorf("Error() = %q, should contain message", got)
	}
}

func TestClient_ReadMessage(t *testing.T) {
	stdin := &mockWriteCloser{buf: &bytes.Buffer{}}

	// Prepare a server-initiated notification
	msg := Response{
		JSONRPC: "2.0",
		ID:      0,
		Result:  []byte(`{"message": "hello"}`),
	}
	msgBytes, _ := json.Marshal(msg)
	mockMessage := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(msgBytes), msgBytes)
	stdout := strings.NewReader(mockMessage)

	client := NewClient(stdin, stdout)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	if resp.JSONRPC != "2.0" {
		t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, "2.0")
	}
}

func TestWriteMessage_Format(t *testing.T) {
	stdin := &mockWriteCloser{buf: &bytes.Buffer{}}
	stdout := strings.NewReader("")

	client := NewClient(stdin, stdout)

	req := Request{
		JSONRPC: "2.0",
		ID:      42,
		Method:  "test/method",
		Params:  map[string]any{"key": "value"},
	}

	// Access writeMessage via Call
	err := client.Notify("test", nil)
	if err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	output := stdin.buf.String()

	// Check Content-Length header format
	if !strings.Contains(output, "Content-Length:") {
		t.Error("Missing Content-Length header")
	}
	if !strings.Contains(output, "\r\n\r\n") {
		t.Error("Missing CRLF header terminator")
	}

	_ = req // unused in this test, just checking format
}
