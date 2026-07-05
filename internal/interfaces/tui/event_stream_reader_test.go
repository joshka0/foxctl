package tui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadConsoleEventStreamSuccess(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/console/sessions/sess-1/events" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/api/console/sessions/sess-1/events")
		}
		if got := r.URL.Query().Get("format"); got != "" {
			t.Fatalf("format query = %q, want empty", got)
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Fatalf("accept header = %q, want %q", got, "text/event-stream")
		}

		_, _ = w.Write([]byte(
			"event: connected\n" +
				`data: {"session_id":"sess-1"}` + "\n\n" +
				"event: reply\n" +
				`data: {"type":"reply","data":{"type":"reply","correlation_id":"corr-1","content":"done"}}` + "\n\n",
		))
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}

	var got []ConsoleStreamEvent
	err = ReadConsoleEventStream(context.Background(), client, "sess-1", ConsoleEventStreamOptions{}, func(event ConsoleStreamEvent) error {
		got = append(got, event)
		return nil
	})
	if err != nil {
		t.Fatalf("ReadConsoleEventStream error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Type != "connected" {
		t.Fatalf("got[0].Type = %q, want %q", got[0].Type, "connected")
	}
	if got[1].Payload == nil || got[1].Payload.Content != "done" {
		t.Fatalf("got[1].Payload = %#v, want reply payload content %q", got[1].Payload, "done")
	}
}

func TestReadConsoleEventStreamPayloadFormat(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/console/sessions/sess-2/events" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/api/console/sessions/sess-2/events")
		}
		if got := r.URL.Query().Get("format"); got != "payload" {
			t.Fatalf("format query = %q, want %q", got, "payload")
		}
		_, _ = w.Write([]byte(`data: {"type":"event","content":"chunk"}` + "\n\n"))
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}

	var got []ConsoleStreamEvent
	err = ReadConsoleEventStream(
		context.Background(),
		client,
		"sess-2",
		ConsoleEventStreamOptions{PayloadFormat: true},
		func(event ConsoleStreamEvent) error {
			got = append(got, event)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("ReadConsoleEventStream error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Type != "event" {
		t.Fatalf("got[0].Type = %q, want %q", got[0].Type, "event")
	}
}

func TestReadConsoleEventStreamReturnsHTTPStatusError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "stream unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}

	err = ReadConsoleEventStream(context.Background(), client, "sess-status", ConsoleEventStreamOptions{}, nil)
	if err == nil {
		t.Fatal("ReadConsoleEventStream error = nil, want HTTPStatusError")
	}

	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error type = %T, want *HTTPStatusError", err)
	}
	if statusErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode = %d, want %d", statusErr.StatusCode, http.StatusServiceUnavailable)
	}
	if !strings.Contains(statusErr.Body, "stream unavailable") {
		t.Fatalf("Body = %q, want stream error detail", statusErr.Body)
	}
}

func TestReadConsoleEventStreamStopsOnCallbackError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(
			"event: connected\n" +
				`data: {"session_id":"sess-1"}` + "\n\n" +
				"event: reply\n" +
				`data: {"type":"reply","data":{"type":"reply","content":"should-not-arrive"}}` + "\n\n",
		))
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}

	wantErr := errors.New("stop")
	count := 0
	err = ReadConsoleEventStream(context.Background(), client, "sess-callback", ConsoleEventStreamOptions{}, func(ConsoleStreamEvent) error {
		count++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want callback error %v", err, wantErr)
	}
	if count != 1 {
		t.Fatalf("callback count = %d, want 1", count)
	}
}

func TestReadConsoleEventStreamPropagatesContextCancellation(t *testing.T) {
	t.Parallel()

	canceled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not implement http.Flusher")
		}

		_, _ = w.Write([]byte("event: connected\ndata: {\"session_id\":\"sess-cancel\"}\n\n"))
		flusher.Flush()

		<-r.Context().Done()
		canceled <- struct{}{}
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err = ReadConsoleEventStream(ctx, client, "sess-cancel", ConsoleEventStreamOptions{}, nil)
	if err == nil {
		t.Fatal("ReadConsoleEventStream error = nil, want context cancellation")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation error", err)
	}

	select {
	case <-canceled:
	// Generous failsafe: the happy path fires in ~20ms. A tight bound flakes
	// only when the scheduler is starved under a full parallel test run.
	case <-time.After(10 * time.Second):
		t.Fatal("handler context was not canceled")
	}
}

func TestReadConsoleEventStreamRejectsEmptySessionID(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be called for invalid session id")
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}

	err = ReadConsoleEventStream(context.Background(), client, "   ", ConsoleEventStreamOptions{}, nil)
	if err == nil {
		t.Fatal("ReadConsoleEventStream error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "session id is required") {
		t.Fatalf("error = %v, want session id validation detail", err)
	}
}
