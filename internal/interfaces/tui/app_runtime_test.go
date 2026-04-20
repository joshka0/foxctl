package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewShellRuntimeWithoutStreamConfig(t *testing.T) {
	initial := DefaultShellState(Options{})
	runtime, err := newShellRuntime(context.Background(), Options{}, initial)
	if err != nil {
		t.Fatalf("newShellRuntime error: %v", err)
	}
	defer runtime.close()

	if runtime.shell == nil {
		t.Fatal("runtime.shell is nil")
	}
	if got := len(runtime.shell.Watchers()); got != 0 {
		t.Fatalf("len(runtime.shell.Watchers()) = %d, want 0", got)
	}
}

func TestNewShellRuntimeWithConsoleStreamConfig(t *testing.T) {
	sessionID := "sess-runtime"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case fmt.Sprintf("/api/console/sessions/%s/events", sessionID):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: connected\n"))
			_, _ = w.Write([]byte(`data: {"type":"connected","data":{"session_id":"sess-runtime"}}` + "\n\n"))
		default:
			http.Error(w, "bad path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := DefaultShellState(Options{})
	opts := Options{
		APIBaseURL:          server.URL,
		ConsoleSessionID:    sessionID,
		ConsoleStreamBuffer: 4,
		TranscriptLimit:     50,
	}
	runtime, err := newShellRuntime(ctx, opts, initial)
	if err != nil {
		t.Fatalf("newShellRuntime error: %v", err)
	}
	defer runtime.close()

	if runtime.shell == nil {
		t.Fatal("runtime.shell is nil")
	}
	if got := len(runtime.shell.Watchers()); got != 3 {
		t.Fatalf("len(runtime.shell.Watchers()) = %d, want 3", got)
	}
	if runtime.consoleAskRuntime == nil {
		t.Fatal("runtime.consoleAskRuntime is nil")
	}
	if runtime.consoleCancelRuntime == nil {
		t.Fatal("runtime.consoleCancelRuntime is nil")
	}
	if runtime.consoleStreamPump == nil {
		t.Fatal("runtime.consoleStreamPump is nil")
	}

	runtime.close()
	err = runtime.consoleAskRuntime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "hello"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("consoleAskRuntime.Enqueue after runtime.close() error = %v, want context.Canceled", err)
	}
	err = runtime.consoleCancelRuntime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "corr-1"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("consoleCancelRuntime.Enqueue after runtime.close() error = %v, want context.Canceled", err)
	}
}

func TestNewShellRuntimeWithAgentCompanionConfig(t *testing.T) {
	agentID := "agent-runtime"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case fmt.Sprintf("/api/agents/%s/ask", agentID):
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			_, _ = w.Write([]byte(`{"reply":"agent ok","conversation_id":"agent-runtime"}`))
		default:
			http.Error(w, "bad path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := DefaultShellState(Options{})
	opts := Options{
		APIBaseURL:      server.URL,
		AgentID:         agentID,
		TranscriptLimit: 50,
	}
	runtime, err := newShellRuntime(ctx, opts, initial)
	if err != nil {
		t.Fatalf("newShellRuntime error: %v", err)
	}
	defer runtime.close()

	if runtime.shell == nil {
		t.Fatal("runtime.shell is nil")
	}
	if got := len(runtime.shell.Watchers()); got != 1 {
		t.Fatalf("len(runtime.shell.Watchers()) = %d, want 1", got)
	}
	if runtime.consoleAskRuntime == nil {
		t.Fatal("runtime.consoleAskRuntime is nil")
	}
	if runtime.consoleCancelRuntime != nil {
		t.Fatal("runtime.consoleCancelRuntime is not nil")
	}
	if runtime.consoleStreamPump != nil {
		t.Fatal("runtime.consoleStreamPump is not nil")
	}

	err = runtime.consoleAskRuntime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "hello"})
	if err != nil {
		t.Fatalf("consoleAskRuntime.Enqueue error: %v", err)
	}
	update := <-runtime.consoleAskRuntime.Updates()
	if update.Type != ConsoleAskUpdateAccepted || update.Accepted == nil || update.Accepted.Message != "agent ok" {
		t.Fatalf("update = %#v, want accepted agent reply", update)
	}
}
