package tui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestConsoleStreamPumpDeliversEventOrder(t *testing.T) {
	t.Parallel()

	source := ConsoleStreamSourceFunc(func(_ context.Context, onEvent func(ConsoleStreamEvent) error) error {
		if err := onEvent(ConsoleStreamEvent{Type: "ask", Payload: &ConsoleEventPayload{Type: "ask", Content: "one"}}); err != nil {
			return err
		}
		if err := onEvent(ConsoleStreamEvent{Type: "reply", Payload: &ConsoleEventPayload{Type: "reply", Content: "two"}}); err != nil {
			return err
		}
		return nil
	})

	pump, err := NewConsoleStreamPump(context.Background(), source, 4)
	if err != nil {
		t.Fatalf("NewConsoleStreamPump error: %v", err)
	}
	defer pump.Close()

	updates := collectPumpUpdates(t, pump.Updates())
	if len(updates) != 3 {
		t.Fatalf("len(updates) = %d, want 3", len(updates))
	}
	if updates[0].Type != ConsoleStreamUpdateEvent {
		t.Fatalf("updates[0].Type = %q, want %q", updates[0].Type, ConsoleStreamUpdateEvent)
	}
	if updates[0].Event.Payload == nil || updates[0].Event.Payload.Content != "one" {
		t.Fatalf("updates[0].Event = %#v, want first payload", updates[0].Event)
	}
	if updates[1].Type != ConsoleStreamUpdateEvent {
		t.Fatalf("updates[1].Type = %q, want %q", updates[1].Type, ConsoleStreamUpdateEvent)
	}
	if updates[1].Event.Payload == nil || updates[1].Event.Payload.Content != "two" {
		t.Fatalf("updates[1].Event = %#v, want second payload", updates[1].Event)
	}
	if updates[2].Type != ConsoleStreamUpdateDone {
		t.Fatalf("updates[2].Type = %q, want %q", updates[2].Type, ConsoleStreamUpdateDone)
	}
}

func TestConsoleStreamPumpBackpressureUnblocksOnStop(t *testing.T) {
	t.Parallel()

	secondEventStarted := make(chan struct{}, 1)
	source := ConsoleStreamSourceFunc(func(_ context.Context, onEvent func(ConsoleStreamEvent) error) error {
		if err := onEvent(ConsoleStreamEvent{Type: "event", Payload: &ConsoleEventPayload{Type: "event", Content: "first"}}); err != nil {
			return err
		}
		secondEventStarted <- struct{}{}
		if err := onEvent(ConsoleStreamEvent{Type: "event", Payload: &ConsoleEventPayload{Type: "event", Content: "second"}}); err != nil {
			return err
		}
		return nil
	})

	pump, err := NewConsoleStreamPump(context.Background(), source, 1)
	if err != nil {
		t.Fatalf("NewConsoleStreamPump error: %v", err)
	}

	select {
	case <-secondEventStarted:
	case <-time.After(1 * time.Second):
		t.Fatal("second event callback was not reached")
	}

	stopDone := make(chan struct{})
	go func() {
		pump.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
	case <-time.After(1 * time.Second):
		t.Fatal("pump.Stop did not return; likely blocked on full channel")
	}

	updates := collectPumpUpdates(t, pump.Updates())
	if len(updates) != 1 {
		t.Fatalf("len(updates) = %d, want 1 buffered event after stop", len(updates))
	}
	if updates[0].Type != ConsoleStreamUpdateEvent {
		t.Fatalf("updates[0].Type = %q, want %q", updates[0].Type, ConsoleStreamUpdateEvent)
	}
	if updates[0].Event.Payload == nil || updates[0].Event.Payload.Content != "first" {
		t.Fatalf("updates[0].Event = %#v, want first event only", updates[0].Event)
	}
}

func TestConsoleStreamPumpBackpressureUnblocksOnParentContextCancel(t *testing.T) {
	t.Parallel()

	parent, cancel := context.WithCancel(context.Background())
	defer cancel()

	secondEventStarted := make(chan struct{}, 1)
	source := ConsoleStreamSourceFunc(func(_ context.Context, onEvent func(ConsoleStreamEvent) error) error {
		if err := onEvent(ConsoleStreamEvent{Type: "event", Payload: &ConsoleEventPayload{Type: "event", Content: "first"}}); err != nil {
			return err
		}
		secondEventStarted <- struct{}{}
		if err := onEvent(ConsoleStreamEvent{Type: "event", Payload: &ConsoleEventPayload{Type: "event", Content: "second"}}); err != nil {
			return err
		}
		return nil
	})

	pump, err := NewConsoleStreamPump(parent, source, 1)
	if err != nil {
		t.Fatalf("NewConsoleStreamPump error: %v", err)
	}
	defer pump.Close()

	select {
	case <-secondEventStarted:
	case <-time.After(1 * time.Second):
		t.Fatal("second event callback was not reached")
	}

	cancel()

	updates := collectPumpUpdates(t, pump.Updates())
	if len(updates) < 1 {
		t.Fatalf("len(updates) = %d, want at least 1 update", len(updates))
	}
	if updates[0].Type != ConsoleStreamUpdateEvent {
		t.Fatalf("updates[0].Type = %q, want %q", updates[0].Type, ConsoleStreamUpdateEvent)
	}
	for i, update := range updates {
		if update.Type == ConsoleStreamUpdateError {
			t.Fatalf("updates[%d] = error %v, want cancellation-driven shutdown without source error", i, update.Err)
		}
	}
}

func TestConsoleStreamPumpPropagatesSourceError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("source failed")
	source := ConsoleStreamSourceFunc(func(_ context.Context, _ func(ConsoleStreamEvent) error) error {
		return wantErr
	})

	pump, err := NewConsoleStreamPump(context.Background(), source, 2)
	if err != nil {
		t.Fatalf("NewConsoleStreamPump error: %v", err)
	}
	defer pump.Close()

	updates := collectPumpUpdates(t, pump.Updates())
	if len(updates) != 1 {
		t.Fatalf("len(updates) = %d, want 1", len(updates))
	}
	if updates[0].Type != ConsoleStreamUpdateError {
		t.Fatalf("updates[0].Type = %q, want %q", updates[0].Type, ConsoleStreamUpdateError)
	}
	if !errors.Is(updates[0].Err, wantErr) {
		t.Fatalf("updates[0].Err = %v, want %v", updates[0].Err, wantErr)
	}
}

func TestConsoleStreamPumpDeliversTerminalUpdateWhenEventBufferIsFull(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("source failed after event")
	source := ConsoleStreamSourceFunc(func(_ context.Context, onEvent func(ConsoleStreamEvent) error) error {
		if err := onEvent(ConsoleStreamEvent{Type: "event", Payload: &ConsoleEventPayload{Type: "event", Content: "first"}}); err != nil {
			return err
		}
		return wantErr
	})

	pump, err := NewConsoleStreamPump(context.Background(), source, 1)
	if err != nil {
		t.Fatalf("NewConsoleStreamPump error: %v", err)
	}
	defer pump.Close()

	updates := collectPumpUpdates(t, pump.Updates())
	if len(updates) != 2 {
		t.Fatalf("len(updates) = %d, want 2", len(updates))
	}
	if updates[0].Type != ConsoleStreamUpdateEvent {
		t.Fatalf("updates[0].Type = %q, want %q", updates[0].Type, ConsoleStreamUpdateEvent)
	}
	if updates[1].Type != ConsoleStreamUpdateError {
		t.Fatalf("updates[1].Type = %q, want %q", updates[1].Type, ConsoleStreamUpdateError)
	}
	if !errors.Is(updates[1].Err, wantErr) {
		t.Fatalf("updates[1].Err = %v, want %v", updates[1].Err, wantErr)
	}
}

func TestConsoleStreamPumpEmitsDoneOnNormalCompletion(t *testing.T) {
	t.Parallel()

	source := ConsoleStreamSourceFunc(func(_ context.Context, _ func(ConsoleStreamEvent) error) error {
		return nil
	})

	pump, err := NewConsoleStreamPump(context.Background(), source, 1)
	if err != nil {
		t.Fatalf("NewConsoleStreamPump error: %v", err)
	}
	defer pump.Close()

	updates := collectPumpUpdates(t, pump.Updates())
	if len(updates) != 1 {
		t.Fatalf("len(updates) = %d, want 1", len(updates))
	}
	if updates[0].Type != ConsoleStreamUpdateDone {
		t.Fatalf("updates[0].Type = %q, want %q", updates[0].Type, ConsoleStreamUpdateDone)
	}
}

func TestConsoleStreamPumpDeliversDoneWhenEventBufferIsFull(t *testing.T) {
	t.Parallel()

	source := ConsoleStreamSourceFunc(func(_ context.Context, onEvent func(ConsoleStreamEvent) error) error {
		return onEvent(ConsoleStreamEvent{Type: "event", Payload: &ConsoleEventPayload{Type: "event", Content: "first"}})
	})

	pump, err := NewConsoleStreamPump(context.Background(), source, 1)
	if err != nil {
		t.Fatalf("NewConsoleStreamPump error: %v", err)
	}
	defer pump.Close()

	updates := collectPumpUpdates(t, pump.Updates())
	if len(updates) != 2 {
		t.Fatalf("len(updates) = %d, want 2", len(updates))
	}
	if updates[0].Type != ConsoleStreamUpdateEvent {
		t.Fatalf("updates[0].Type = %q, want %q", updates[0].Type, ConsoleStreamUpdateEvent)
	}
	if updates[1].Type != ConsoleStreamUpdateDone {
		t.Fatalf("updates[1].Type = %q, want %q", updates[1].Type, ConsoleStreamUpdateDone)
	}
}

func TestHTTPConsoleStreamSourceUsesExistingEventEndpoint(t *testing.T) {
	t.Parallel()

	var called atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/console/sessions/sess-http/events" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/api/console/sessions/sess-http/events")
		}
		if got := r.URL.Query().Get("format"); got != "payload" {
			t.Fatalf("format query = %q, want %q", got, "payload")
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Fatalf("accept = %q, want %q", got, "text/event-stream")
		}
		_, _ = w.Write([]byte(`data: {"type":"event","content":"chunk"}` + "\n\n"))
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}

	source := NewHTTPConsoleStreamSource(client, "sess-http", ConsoleEventStreamOptions{PayloadFormat: true})
	pump, err := NewConsoleStreamPump(context.Background(), source, 2)
	if err != nil {
		t.Fatalf("NewConsoleStreamPump error: %v", err)
	}
	defer pump.Close()

	updates := collectPumpUpdates(t, pump.Updates())
	if called.Load() != 1 {
		t.Fatalf("endpoint called %d times, want 1", called.Load())
	}
	if len(updates) != 2 {
		t.Fatalf("len(updates) = %d, want 2", len(updates))
	}
	if updates[0].Type != ConsoleStreamUpdateEvent {
		t.Fatalf("updates[0].Type = %q, want %q", updates[0].Type, ConsoleStreamUpdateEvent)
	}
	if updates[0].Event.Payload == nil || updates[0].Event.Payload.Content != "chunk" {
		t.Fatalf("updates[0].Event = %#v, want payload chunk", updates[0].Event)
	}
	if updates[1].Type != ConsoleStreamUpdateDone {
		t.Fatalf("updates[1].Type = %q, want %q", updates[1].Type, ConsoleStreamUpdateDone)
	}
}

func collectPumpUpdates(t *testing.T, updates <-chan ConsoleStreamUpdate) []ConsoleStreamUpdate {
	t.Helper()

	collected := make([]ConsoleStreamUpdate, 0, 8)
	for {
		select {
		case update, ok := <-updates:
			if !ok {
				return collected
			}
			collected = append(collected, update)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for pump updates")
		}
	}
}
