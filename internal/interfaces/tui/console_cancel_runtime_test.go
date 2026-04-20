package tui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestConsoleCancelRuntimeEnqueueOrder(t *testing.T) {
	t.Parallel()

	runtime, err := NewConsoleCancelRuntime(
		context.Background(),
		ConsoleCancelerFunc(func(_ context.Context, req CancelConsoleSessionRequest) (CancelConsoleSessionResponse, error) {
			return CancelConsoleSessionResponse{
				OK:      true,
				Message: "accepted " + req.CorrelationID,
			}, nil
		}),
		4,
		4,
	)
	if err != nil {
		t.Fatalf("NewConsoleCancelRuntime error: %v", err)
	}
	defer runtime.Close()

	if err := runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "corr-1"}); err != nil {
		t.Fatalf("Enqueue corr-1 error: %v", err)
	}
	if err := runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "corr-2"}); err != nil {
		t.Fatalf("Enqueue corr-2 error: %v", err)
	}

	first := readCancelUpdate(t, runtime.Updates())
	second := readCancelUpdate(t, runtime.Updates())

	if first.Type != ConsoleCancelUpdateAccepted || first.Accepted == nil {
		t.Fatalf("first update = %#v, want accepted payload", first)
	}
	if first.Accepted.CorrelationID != "corr-1" {
		t.Fatalf("first.Accepted.CorrelationID = %q, want %q", first.Accepted.CorrelationID, "corr-1")
	}

	if second.Type != ConsoleCancelUpdateAccepted || second.Accepted == nil {
		t.Fatalf("second update = %#v, want accepted payload", second)
	}
	if second.Accepted.CorrelationID != "corr-2" {
		t.Fatalf("second.Accepted.CorrelationID = %q, want %q", second.Accepted.CorrelationID, "corr-2")
	}
}

func TestConsoleCancelRuntimeTrimsOptionalCorrelationID(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		captured []CancelConsoleSessionRequest
	)

	runtime, err := NewConsoleCancelRuntime(
		context.Background(),
		ConsoleCancelerFunc(func(_ context.Context, req CancelConsoleSessionRequest) (CancelConsoleSessionResponse, error) {
			mu.Lock()
			captured = append(captured, req)
			mu.Unlock()
			return CancelConsoleSessionResponse{OK: true}, nil
		}),
		2,
		2,
	)
	if err != nil {
		t.Fatalf("NewConsoleCancelRuntime error: %v", err)
	}
	defer runtime.Close()

	if err := runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "  corr-trim  "}); err != nil {
		t.Fatalf("Enqueue corr-trim error: %v", err)
	}
	if err := runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "   \n\t   "}); err != nil {
		t.Fatalf("Enqueue empty-correlation error: %v", err)
	}

	_ = readCancelUpdate(t, runtime.Updates())
	_ = readCancelUpdate(t, runtime.Updates())

	mu.Lock()
	defer mu.Unlock()
	if got := len(captured); got != 2 {
		t.Fatalf("len(captured) = %d, want 2", got)
	}
	if captured[0].CorrelationID != "corr-trim" {
		t.Fatalf("captured[0].CorrelationID = %q, want %q", captured[0].CorrelationID, "corr-trim")
	}
	if captured[1].CorrelationID != "" {
		t.Fatalf("captured[1].CorrelationID = %q, want empty", captured[1].CorrelationID)
	}
}

func TestConsoleCancelRuntimeBackpressureUnblocksOnStop(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	runtime, err := NewConsoleCancelRuntime(
		context.Background(),
		ConsoleCancelerFunc(func(ctx context.Context, _ CancelConsoleSessionRequest) (CancelConsoleSessionResponse, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return CancelConsoleSessionResponse{}, ctx.Err()
		}),
		1,
		1,
	)
	if err != nil {
		t.Fatalf("NewConsoleCancelRuntime error: %v", err)
	}

	if err := runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "one"}); err != nil {
		t.Fatalf("Enqueue one error: %v", err)
	}
	select {
	case <-started:
	case <-time.After(1 * time.Second):
		t.Fatal("canceler did not start first request")
	}

	if err := runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "two"}); err != nil {
		t.Fatalf("Enqueue two error: %v", err)
	}

	thirdResult := make(chan error, 1)
	go func() {
		thirdResult <- runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "three"})
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

func TestConsoleCancelRuntimeBackpressureUnblocksOnParentContextCancel(t *testing.T) {
	t.Parallel()

	parent, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{}, 1)
	runtime, err := NewConsoleCancelRuntime(
		parent,
		ConsoleCancelerFunc(func(ctx context.Context, _ CancelConsoleSessionRequest) (CancelConsoleSessionResponse, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return CancelConsoleSessionResponse{}, ctx.Err()
		}),
		1,
		1,
	)
	if err != nil {
		t.Fatalf("NewConsoleCancelRuntime error: %v", err)
	}
	defer runtime.Close()

	if err := runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "one"}); err != nil {
		t.Fatalf("Enqueue one error: %v", err)
	}
	select {
	case <-started:
	case <-time.After(1 * time.Second):
		t.Fatal("canceler did not start first request")
	}

	if err := runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "two"}); err != nil {
		t.Fatalf("Enqueue two error: %v", err)
	}

	thirdResult := make(chan error, 1)
	go func() {
		thirdResult <- runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "three"})
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

func TestConsoleCancelRuntimeUpdateBackpressureUnblocksOnStop(t *testing.T) {
	t.Parallel()

	runtime, err := NewConsoleCancelRuntime(
		context.Background(),
		ConsoleCancelerFunc(func(_ context.Context, _ CancelConsoleSessionRequest) (CancelConsoleSessionResponse, error) {
			return CancelConsoleSessionResponse{
				OK:      true,
				Message: "accepted",
			}, nil
		}),
		1,
		1,
	)
	if err != nil {
		t.Fatalf("NewConsoleCancelRuntime error: %v", err)
	}

	if err := runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "one"}); err != nil {
		t.Fatalf("Enqueue one error: %v", err)
	}
	if err := runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "two"}); err != nil {
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

func TestConsoleCancelRuntimePropagatesCancelerError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("cancel failed")
	runtime, err := NewConsoleCancelRuntime(
		context.Background(),
		ConsoleCancelerFunc(func(_ context.Context, _ CancelConsoleSessionRequest) (CancelConsoleSessionResponse, error) {
			return CancelConsoleSessionResponse{}, wantErr
		}),
		1,
		1,
	)
	if err != nil {
		t.Fatalf("NewConsoleCancelRuntime error: %v", err)
	}
	defer runtime.Close()

	if err := runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{
		CorrelationID: "corr-local",
	}); err != nil {
		t.Fatalf("Enqueue error: %v", err)
	}

	update := readCancelUpdate(t, runtime.Updates())
	if update.Type != ConsoleCancelUpdateError || update.Failed == nil {
		t.Fatalf("update = %#v, want error payload", update)
	}
	if update.Failed.CorrelationID != "corr-local" {
		t.Fatalf("update.Failed.CorrelationID = %q, want %q", update.Failed.CorrelationID, "corr-local")
	}
	if !errors.Is(update.Failed.Err, wantErr) {
		t.Fatalf("update.Failed.Err = %v, want %v", update.Failed.Err, wantErr)
	}
}

func TestConsoleCancelRuntimeHandlesNonOKResponse(t *testing.T) {
	t.Parallel()

	runtime, err := NewConsoleCancelRuntime(
		context.Background(),
		ConsoleCancelerFunc(func(_ context.Context, _ CancelConsoleSessionRequest) (CancelConsoleSessionResponse, error) {
			return CancelConsoleSessionResponse{
				OK:      false,
				Message: "not cancelable",
			}, nil
		}),
		1,
		1,
	)
	if err != nil {
		t.Fatalf("NewConsoleCancelRuntime error: %v", err)
	}
	defer runtime.Close()

	if err := runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{
		CorrelationID: "corr-local",
	}); err != nil {
		t.Fatalf("Enqueue error: %v", err)
	}

	update := readCancelUpdate(t, runtime.Updates())
	if update.Type != ConsoleCancelUpdateError || update.Failed == nil {
		t.Fatalf("update = %#v, want error payload", update)
	}
	if update.Failed.CorrelationID != "corr-local" {
		t.Fatalf("update.Failed.CorrelationID = %q, want %q", update.Failed.CorrelationID, "corr-local")
	}
	if update.Failed.Err == nil {
		t.Fatal("update.Failed.Err = nil, want error")
	}
	if got := update.Failed.Err.Error(); got != "not cancelable" {
		t.Fatalf("update.Failed.Err = %q, want %q", got, "not cancelable")
	}
}

func TestConsoleCancelRuntimeStopClosesUpdatesChannel(t *testing.T) {
	t.Parallel()

	runtime, err := NewConsoleCancelRuntime(
		context.Background(),
		ConsoleCancelerFunc(func(_ context.Context, _ CancelConsoleSessionRequest) (CancelConsoleSessionResponse, error) {
			return CancelConsoleSessionResponse{OK: true}, nil
		}),
		1,
		1,
	)
	if err != nil {
		t.Fatalf("NewConsoleCancelRuntime error: %v", err)
	}

	if err := runtime.Enqueue(context.Background(), CancelConsoleSessionRequest{CorrelationID: "corr-one"}); err != nil {
		t.Fatalf("Enqueue error: %v", err)
	}
	_ = readCancelUpdate(t, runtime.Updates())

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

func TestHTTPConsoleCancelerPostsExpectedCancelRequest(t *testing.T) {
	t.Parallel()

	var body CancelConsoleSessionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/console/sessions/session-http/cancel" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/api/console/sessions/session-http/cancel")
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content-type = %q, want %q", ct, "application/json")
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(CancelConsoleSessionResponse{
			OK:      true,
			Message: "cancel requested",
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
	canceler, err := NewHTTPConsoleCanceler(adapter, "session-http")
	if err != nil {
		t.Fatalf("NewHTTPConsoleCanceler error: %v", err)
	}

	response, err := canceler.SubmitCancel(context.Background(), CancelConsoleSessionRequest{
		CorrelationID: "corr-local",
	})
	if err != nil {
		t.Fatalf("SubmitCancel error: %v", err)
	}
	if body.CorrelationID != "corr-local" {
		t.Fatalf("body.CorrelationID = %q, want %q", body.CorrelationID, "corr-local")
	}
	if !response.OK {
		t.Fatalf("response.OK = false, want true")
	}
	if response.Message != "cancel requested" {
		t.Fatalf("response.Message = %q, want %q", response.Message, "cancel requested")
	}
}

func readCancelUpdate(t *testing.T, updates <-chan ConsoleCancelUpdate) ConsoleCancelUpdate {
	t.Helper()

	select {
	case update, ok := <-updates:
		if !ok {
			t.Fatal("updates channel closed before receiving update")
		}
		return update
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cancel update")
		return ConsoleCancelUpdate{}
	}
}
