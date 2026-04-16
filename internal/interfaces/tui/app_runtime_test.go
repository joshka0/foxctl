package tui

import (
	"context"
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
		wantPath := fmt.Sprintf("/api/console/sessions/%s/events", sessionID)
		if r.URL.Path != wantPath {
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: connected\n"))
		_, _ = w.Write([]byte(`data: {"type":"connected","data":{"session_id":"sess-runtime"}}` + "\n\n"))
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
	if got := len(runtime.shell.Watchers()); got != 1 {
		t.Fatalf("len(runtime.shell.Watchers()) = %d, want 1", got)
	}
}
