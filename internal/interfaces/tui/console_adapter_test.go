package tui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleAdapterCreateAskCancel(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/console/sessions":
			var req CreateConsoleSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			if req.Workspace != "/tmp/workspace" {
				t.Fatalf("create workspace = %q, want %q", req.Workspace, "/tmp/workspace")
			}
			if req.Profile != "explorer" {
				t.Fatalf("create profile = %q, want %q", req.Profile, "explorer")
			}

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(CreateConsoleSessionResponse{
				Session: ConsoleSession{
					ID:        "sess-123",
					Workspace: req.Workspace,
					Profile:   req.Profile,
				},
			})
			return

		case r.Method == http.MethodPost && r.URL.Path == "/api/console/sessions/sess-123/ask":
			var req AskConsoleSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode ask request: %v", err)
			}
			if req.Content != "hello" {
				t.Fatalf("ask content = %q, want %q", req.Content, "hello")
			}
			_ = json.NewEncoder(w).Encode(AskConsoleSessionResponse{
				OK:            true,
				CorrelationID: "corr-1",
				Message:       "request queued",
			})
			return

		case r.Method == http.MethodPost && r.URL.Path == "/api/console/sessions/sess-123/cancel":
			var req CancelConsoleSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode cancel request: %v", err)
			}
			if req.CorrelationID != "corr-1" {
				t.Fatalf("cancel correlation_id = %q, want %q", req.CorrelationID, "corr-1")
			}
			_ = json.NewEncoder(w).Encode(CancelConsoleSessionResponse{
				OK:      true,
				Message: "cancel requested",
			})
			return
		}

		t.Fatalf("unexpected route: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}
	adapter, err := NewConsoleAdapter(client)
	if err != nil {
		t.Fatalf("NewConsoleAdapter error: %v", err)
	}

	createResp, err := adapter.CreateSession(context.Background(), CreateConsoleSessionRequest{
		Workspace: "/tmp/workspace",
		Profile:   "explorer",
	})
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	if createResp.Session.ID != "sess-123" {
		t.Fatalf("session id = %q, want %q", createResp.Session.ID, "sess-123")
	}

	askResp, err := adapter.AskSession(context.Background(), createResp.Session.ID, AskConsoleSessionRequest{
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("AskSession error: %v", err)
	}
	if !askResp.OK {
		t.Fatalf("ask response OK = %v, want true", askResp.OK)
	}
	if askResp.CorrelationID != "corr-1" {
		t.Fatalf("ask correlation_id = %q, want %q", askResp.CorrelationID, "corr-1")
	}

	cancelResp, err := adapter.CancelSession(context.Background(), createResp.Session.ID, CancelConsoleSessionRequest{
		CorrelationID: askResp.CorrelationID,
	})
	if err != nil {
		t.Fatalf("CancelSession error: %v", err)
	}
	if !cancelResp.OK {
		t.Fatalf("cancel response OK = %v, want true", cancelResp.OK)
	}
}

func TestConsoleAdapterReturnsNon2xxError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "session not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}
	adapter, err := NewConsoleAdapter(client)
	if err != nil {
		t.Fatalf("NewConsoleAdapter error: %v", err)
	}

	_, err = adapter.AskSession(context.Background(), "missing", AskConsoleSessionRequest{Content: "hello"})
	if err == nil {
		t.Fatal("AskSession error = nil, want non-2xx error")
	}

	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error type = %T, want wrapped *HTTPStatusError", err)
	}
	if statusErr.StatusCode != http.StatusNotFound {
		t.Fatalf("StatusCode = %d, want %d", statusErr.StatusCode, http.StatusNotFound)
	}
	if !strings.Contains(statusErr.Body, "session not found") {
		t.Fatalf("Body = %q, want not-found detail", statusErr.Body)
	}
}

func TestConsoleAdapterRejectsEmptySessionID(t *testing.T) {
	t.Parallel()

	adapter := &ConsoleAdapter{client: &APIClient{}}
	_, err := adapter.CancelSession(context.Background(), "   ", CancelConsoleSessionRequest{})
	if err == nil {
		t.Fatal("CancelSession error = nil, want validation error")
	}
}
