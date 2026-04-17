package tui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConsoleAskRuntimeEnqueueOrder(t *testing.T) {
	t.Parallel()

	runtime, err := NewConsoleAskRuntime(
		context.Background(),
		ConsoleAskSubmitterFunc(func(_ context.Context, req AskConsoleSessionRequest) (AskConsoleSessionResponse, error) {
			return AskConsoleSessionResponse{
				OK:            true,
				CorrelationID: "ack-" + req.Content,
				Message:       "queued",
			}, nil
		}),
		4,
		4,
	)
	if err != nil {
		t.Fatalf("NewConsoleAskRuntime error: %v", err)
	}
	defer runtime.Close()

	if err := runtime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "first"}); err != nil {
		t.Fatalf("Enqueue first error: %v", err)
	}
	if err := runtime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "second"}); err != nil {
		t.Fatalf("Enqueue second error: %v", err)
	}

	first := readAskUpdate(t, runtime.Updates())
	second := readAskUpdate(t, runtime.Updates())

	if first.Type != ConsoleAskUpdateAccepted || first.Accepted == nil {
		t.Fatalf("first update = %#v, want accepted payload", first)
	}
	if first.Accepted.Content != "first" {
		t.Fatalf("first.Accepted.Content = %q, want %q", first.Accepted.Content, "first")
	}
	if first.Accepted.CorrelationID != "ack-first" {
		t.Fatalf("first.Accepted.CorrelationID = %q, want %q", first.Accepted.CorrelationID, "ack-first")
	}

	if second.Type != ConsoleAskUpdateAccepted || second.Accepted == nil {
		t.Fatalf("second update = %#v, want accepted payload", second)
	}
	if second.Accepted.Content != "second" {
		t.Fatalf("second.Accepted.Content = %q, want %q", second.Accepted.Content, "second")
	}
	if second.Accepted.CorrelationID != "ack-second" {
		t.Fatalf("second.Accepted.CorrelationID = %q, want %q", second.Accepted.CorrelationID, "ack-second")
	}
}

func TestConsoleAskRuntimeRejectsEmptyContent(t *testing.T) {
	t.Parallel()

	var called atomic.Int32
	runtime, err := NewConsoleAskRuntime(
		context.Background(),
		ConsoleAskSubmitterFunc(func(_ context.Context, _ AskConsoleSessionRequest) (AskConsoleSessionResponse, error) {
			called.Add(1)
			return AskConsoleSessionResponse{OK: true}, nil
		}),
		1,
		1,
	)
	if err != nil {
		t.Fatalf("NewConsoleAskRuntime error: %v", err)
	}
	defer runtime.Close()

	err = runtime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "   \n\t  "})
	if err == nil {
		t.Fatal("Enqueue empty content error = nil, want error")
	}
	if !strings.Contains(err.Error(), "content is required") {
		t.Fatalf("Enqueue empty content error = %q, want contains %q", err.Error(), "content is required")
	}
	if called.Load() != 0 {
		t.Fatalf("submitter called %d times, want 0", called.Load())
	}
}

func TestConsoleAskRuntimeBackpressureUnblocksOnStop(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	runtime, err := NewConsoleAskRuntime(
		context.Background(),
		ConsoleAskSubmitterFunc(func(ctx context.Context, _ AskConsoleSessionRequest) (AskConsoleSessionResponse, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return AskConsoleSessionResponse{}, ctx.Err()
		}),
		1,
		1,
	)
	if err != nil {
		t.Fatalf("NewConsoleAskRuntime error: %v", err)
	}

	if err := runtime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "one"}); err != nil {
		t.Fatalf("Enqueue one error: %v", err)
	}
	select {
	case <-started:
	case <-time.After(1 * time.Second):
		t.Fatal("submitter did not start first request")
	}

	if err := runtime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "two"}); err != nil {
		t.Fatalf("Enqueue two error: %v", err)
	}

	thirdResult := make(chan error, 1)
	go func() {
		thirdResult <- runtime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "three"})
	}()

	select {
	case <-time.After(100 * time.Millisecond):
	case err := <-thirdResult:
		t.Fatalf("third enqueue returned early with %v; expected to block on full queue", err)
	}

	stopDone := make(chan struct{})
	go func() {
		runtime.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
	case <-time.After(1 * time.Second):
		t.Fatal("runtime.Stop() did not return")
	}

	select {
	case err := <-thirdResult:
		if err == nil {
			t.Fatal("third enqueue error = nil, want context cancellation")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("third enqueue error = %v, want context.Canceled", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("third enqueue did not unblock after Stop")
	}
}

func TestConsoleAskRuntimeBackpressureUnblocksOnParentContextCancel(t *testing.T) {
	t.Parallel()

	parent, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{}, 1)
	runtime, err := NewConsoleAskRuntime(
		parent,
		ConsoleAskSubmitterFunc(func(ctx context.Context, _ AskConsoleSessionRequest) (AskConsoleSessionResponse, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return AskConsoleSessionResponse{}, ctx.Err()
		}),
		1,
		1,
	)
	if err != nil {
		t.Fatalf("NewConsoleAskRuntime error: %v", err)
	}
	defer runtime.Close()

	if err := runtime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "one"}); err != nil {
		t.Fatalf("Enqueue one error: %v", err)
	}
	select {
	case <-started:
	case <-time.After(1 * time.Second):
		t.Fatal("submitter did not start first request")
	}

	if err := runtime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "two"}); err != nil {
		t.Fatalf("Enqueue two error: %v", err)
	}

	thirdResult := make(chan error, 1)
	go func() {
		thirdResult <- runtime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "three"})
	}()

	select {
	case <-time.After(100 * time.Millisecond):
	case err := <-thirdResult:
		t.Fatalf("third enqueue returned early with %v; expected to block on full queue", err)
	}

	cancel()

	select {
	case err := <-thirdResult:
		if err == nil {
			t.Fatal("third enqueue error = nil, want context cancellation")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("third enqueue error = %v, want context.Canceled", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("third enqueue did not unblock after parent context cancel")
	}
}

func TestConsoleAskRuntimeUpdateBackpressureUnblocksOnStop(t *testing.T) {
	t.Parallel()

	runtime, err := NewConsoleAskRuntime(
		context.Background(),
		ConsoleAskSubmitterFunc(func(_ context.Context, req AskConsoleSessionRequest) (AskConsoleSessionResponse, error) {
			return AskConsoleSessionResponse{
				OK:            true,
				CorrelationID: "ack-" + req.Content,
			}, nil
		}),
		1,
		1,
	)
	if err != nil {
		t.Fatalf("NewConsoleAskRuntime error: %v", err)
	}

	if err := runtime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "one"}); err != nil {
		t.Fatalf("Enqueue one error: %v", err)
	}
	if err := runtime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "two"}); err != nil {
		t.Fatalf("Enqueue two error: %v", err)
	}

	stopDone := make(chan struct{})
	go func() {
		runtime.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
	case <-time.After(1 * time.Second):
		t.Fatal("runtime.Stop() did not return while update channel was saturated")
	}
}

func TestConsoleAskRuntimePropagatesSubmitterError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("submit failed")
	runtime, err := NewConsoleAskRuntime(
		context.Background(),
		ConsoleAskSubmitterFunc(func(_ context.Context, _ AskConsoleSessionRequest) (AskConsoleSessionResponse, error) {
			return AskConsoleSessionResponse{}, wantErr
		}),
		1,
		1,
	)
	if err != nil {
		t.Fatalf("NewConsoleAskRuntime error: %v", err)
	}
	defer runtime.Close()

	if err := runtime.Enqueue(context.Background(), AskConsoleSessionRequest{
		Content:       "ship it",
		CorrelationID: "corr-local",
	}); err != nil {
		t.Fatalf("Enqueue error: %v", err)
	}

	update := readAskUpdate(t, runtime.Updates())
	if update.Type != ConsoleAskUpdateError || update.Failed == nil {
		t.Fatalf("update = %#v, want error payload", update)
	}
	if update.Failed.Content != "ship it" {
		t.Fatalf("update.Failed.Content = %q, want %q", update.Failed.Content, "ship it")
	}
	if update.Failed.CorrelationID != "corr-local" {
		t.Fatalf("update.Failed.CorrelationID = %q, want %q", update.Failed.CorrelationID, "corr-local")
	}
	if !errors.Is(update.Failed.Err, wantErr) {
		t.Fatalf("update.Failed.Err = %v, want %v", update.Failed.Err, wantErr)
	}
}

func TestConsoleAskRuntimeStopClosesUpdatesChannel(t *testing.T) {
	t.Parallel()

	runtime, err := NewConsoleAskRuntime(
		context.Background(),
		ConsoleAskSubmitterFunc(func(_ context.Context, req AskConsoleSessionRequest) (AskConsoleSessionResponse, error) {
			return AskConsoleSessionResponse{
				OK:            true,
				CorrelationID: "ack-" + req.Content,
			}, nil
		}),
		1,
		1,
	)
	if err != nil {
		t.Fatalf("NewConsoleAskRuntime error: %v", err)
	}

	if err := runtime.Enqueue(context.Background(), AskConsoleSessionRequest{Content: "one"}); err != nil {
		t.Fatalf("Enqueue error: %v", err)
	}
	_ = readAskUpdate(t, runtime.Updates())

	runtime.Stop()

	select {
	case _, ok := <-runtime.Updates():
		if ok {
			t.Fatal("updates channel remained open after Stop")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("updates channel did not close after Stop")
	}
}

func TestHTTPConsoleAskSubmitterPostsExpectedAskRequest(t *testing.T) {
	t.Parallel()

	var body AskConsoleSessionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/console/sessions/session-http/ask" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/api/console/sessions/session-http/ask")
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content-type = %q, want %q", ct, "application/json")
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(AskConsoleSessionResponse{
			OK:            true,
			CorrelationID: "corr-server",
			Message:       "request queued",
		})
	}))
	defer server.Close()

	client, err := NewAPIClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}
	adapter, err := NewConsoleAdapter(client)
	if err != nil {
		t.Fatalf("NewConsoleAdapter error: %v", err)
	}
	submitter, err := NewHTTPConsoleAskSubmitter(adapter, "session-http")
	if err != nil {
		t.Fatalf("NewHTTPConsoleAskSubmitter error: %v", err)
	}

	response, err := submitter.SubmitAsk(context.Background(), AskConsoleSessionRequest{
		Content:       "hello from composer",
		CorrelationID: "corr-local",
	})
	if err != nil {
		t.Fatalf("SubmitAsk error: %v", err)
	}
	if body.Content != "hello from composer" {
		t.Fatalf("body.Content = %q, want %q", body.Content, "hello from composer")
	}
	if body.CorrelationID != "corr-local" {
		t.Fatalf("body.CorrelationID = %q, want %q", body.CorrelationID, "corr-local")
	}
	if !response.OK {
		t.Fatalf("response.OK = false, want true")
	}
	if response.CorrelationID != "corr-server" {
		t.Fatalf("response.CorrelationID = %q, want %q", response.CorrelationID, "corr-server")
	}
}

func TestHTTPAgentAskSubmitterPostsExpectedAskRequest(t *testing.T) {
	t.Parallel()

	var body AskAgentRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/agents/agent-http/ask" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/api/agents/agent-http/ask")
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content-type = %q, want %q", ct, "application/json")
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(AskAgentResponse{
			Reply:          "agent reply",
			ConversationID: "agent-http",
		})
	}))
	defer server.Close()

	client, err := NewAPIClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}
	adapter, err := NewAgentAdapter(client)
	if err != nil {
		t.Fatalf("NewAgentAdapter error: %v", err)
	}
	submitter, err := NewHTTPAgentAskSubmitter(adapter, "agent-http")
	if err != nil {
		t.Fatalf("NewHTTPAgentAskSubmitter error: %v", err)
	}

	response, err := submitter.SubmitAsk(context.Background(), AskConsoleSessionRequest{
		Content: "hello from composer",
	})
	if err != nil {
		t.Fatalf("SubmitAsk error: %v", err)
	}
	if body.Message != "hello from composer" {
		t.Fatalf("body.Message = %q, want %q", body.Message, "hello from composer")
	}
	if !response.OK {
		t.Fatalf("response.OK = false, want true")
	}
	if response.Message != "agent reply" {
		t.Fatalf("response.Message = %q, want %q", response.Message, "agent reply")
	}
}

func readAskUpdate(t *testing.T, updates <-chan ConsoleAskUpdate) ConsoleAskUpdate {
	t.Helper()

	select {
	case update, ok := <-updates:
		if !ok {
			t.Fatal("updates channel closed before receiving update")
		}
		return update
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ask update")
		return ConsoleAskUpdate{}
	}
}
