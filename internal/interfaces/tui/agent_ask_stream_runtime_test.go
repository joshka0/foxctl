package tui

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// AgentAskStreamRuntime tests (TDD red → green)
// ---------------------------------------------------------------------------

func TestAgentAskStreamRuntime_TokensRenderProgressively(t *testing.T) {
	t.Parallel()

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		for _, tok := range []string{"Hello", " world", "!"} {
			if err := onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: tok}); err != nil {
				return err
			}
		}
		return nil
	})

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 4)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()

	if err := rt.Submit("agent-1", "hello"); err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	updates := collectAgentAskUpdates(t, rt.Updates())

	// Expect: started (implicit from first event), 3 tokens, done.
	var tokens []string
	for _, u := range updates {
		switch u.Type {
		case AgentAskUpdateToken:
			if u.Token != nil {
				tokens = append(tokens, u.Token.Delta)
			}
		}
	}

	if len(tokens) < 3 {
		t.Fatalf("expected >=3 token updates, got %d: %+v", len(tokens), updates)
	}
	if strings.Join(tokens, "") != "Hello world!" {
		t.Fatalf("tokens joined = %q, want %q", strings.Join(tokens, ""), "Hello world!")
	}
}

func TestAgentAskStreamRuntime_FinalUpdateHasTerminalStatus(t *testing.T) {
	t.Parallel()

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "done"})
		return nil
	})

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 4)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()

	if err := rt.Submit("agent-1", "hello"); err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	updates := collectAgentAskUpdates(t, rt.Updates())

	if len(updates) == 0 {
		t.Fatal("expected at least one update")
	}

	last := updates[len(updates)-1]
	if last.Type != AgentAskUpdateDone {
		t.Fatalf("last update.Type = %q, want %q", last.Type, AgentAskUpdateDone)
	}
	if last.Done == nil || !last.Done.OK {
		t.Fatalf("last update.Done = %+v, want OK=true", last.Done)
	}
}

func TestAgentAskStreamRuntime_NoGoroutineLeakOnDone(t *testing.T) {
	t.Parallel()

	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "x"})
		return nil
	})

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 4)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}

	if err := rt.Submit("agent-1", "hello"); err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	_ = collectAgentAskUpdates(t, rt.Updates())
	rt.Stop()

	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()

	delta := after - baseline
	if delta > 2 {
		t.Fatalf("potential goroutine leak: %d extra goroutines (baseline=%d, after=%d)", delta, baseline, after)
	}
}

func TestAgentAskStreamRuntime_DoubleSubmitRejectsWithError(t *testing.T) {
	t.Parallel()

	var called atomic.Int32
	source := AgentAskStreamSourceFunc(func(ctx context.Context, onEvent func(AgentAskStreamEvent) error) error {
		called.Add(1)
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "first"})
		// Block until context cancelled to simulate in-flight stream.
		<-ctx.Done()
		return ctx.Err()
	})

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 4)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()

	// First submit should succeed.
	if err := rt.Submit("agent-1", "hello"); err != nil {
		t.Fatalf("first Submit error: %v", err)
	}

	// Wait a bit for the source goroutine to start.
	time.Sleep(50 * time.Millisecond)

	// Second submit should be rejected.
	err = rt.Submit("agent-1", "hello again")
	if err == nil {
		t.Fatal("second Submit error = nil, want rejection")
	}
	if !strings.Contains(err.Error(), "in flight") {
		t.Fatalf("second Submit error = %q, want contains 'in flight'", err.Error())
	}

	if called.Load() != 1 {
		t.Fatalf("source called %d times, want 1", called.Load())
	}
}

func TestAgentAskStreamRuntime_PropagatesSourceError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("stream failed")
	source := AgentAskStreamSourceFunc(func(_ context.Context, _ func(AgentAskStreamEvent) error) error {
		return wantErr
	})

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 2)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()

	if err := rt.Submit("agent-1", "hello"); err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	updates := collectAgentAskUpdates(t, rt.Updates())
	if len(updates) != 1 {
		t.Fatalf("len(updates) = %d, want 1", len(updates))
	}
	if updates[0].Type != AgentAskUpdateError {
		t.Fatalf("updates[0].Type = %q, want %q", updates[0].Type, AgentAskUpdateError)
	}
	if !errors.Is(updates[0].Error.Err, wantErr) {
		t.Fatalf("updates[0].Error.Err = %v, want %v", updates[0].Error.Err, wantErr)
	}
}

func TestAgentAskStreamRuntime_EmitsStartedEvent(t *testing.T) {
	t.Parallel()

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		_ = onEvent(AgentAskStreamEvent{Phase: "started", CorrelationID: "corr-123"})
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "hi"})
		return nil
	})

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 4)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()

	if err := rt.Submit("agent-1", "hello"); err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	updates := collectAgentAskUpdates(t, rt.Updates())

	var foundStarted bool
	for _, u := range updates {
		if u.Type == AgentAskUpdateStarted && u.Started != nil && u.Started.CorrelationID == "corr-123" {
			foundStarted = true
		}
	}
	if !foundStarted {
		t.Fatalf("expected started event with correlationID corr-123, got %+v", updates)
	}
}

func TestAgentAskStreamRuntime_ToolCallAndToolResult(t *testing.T) {
	t.Parallel()

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		_ = onEvent(AgentAskStreamEvent{Phase: "tool_call", ToolName: "search", ToolCallID: "tc-1", ToolArguments: map[string]any{"q": "foo"}})
		_ = onEvent(AgentAskStreamEvent{Phase: "tool_result", ToolCallID: "tc-1", ToolName: "search", ToolOutput: "result"})
		return nil
	})

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 4)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()

	if err := rt.Submit("agent-1", "hello"); err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	updates := collectAgentAskUpdates(t, rt.Updates())

	var foundToolCall, foundToolResult bool
	for _, u := range updates {
		switch u.Type {
		case AgentAskUpdateToolCall:
			if u.ToolCall != nil && u.ToolCall.ToolName == "search" {
				foundToolCall = true
			}
		case AgentAskUpdateToolResult:
			if u.ToolResult != nil && u.ToolResult.ToolName == "search" {
				foundToolResult = true
			}
		}
	}
	if !foundToolCall {
		t.Fatal("expected tool_call update")
	}
	if !foundToolResult {
		t.Fatal("expected tool_result update")
	}
}

func TestAgentAskStreamRuntime_StopClosesUpdatesChannel(t *testing.T) {
	t.Parallel()

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "x"})
		return nil
	})

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 4)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}

	if err := rt.Submit("agent-1", "hello"); err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	_ = collectAgentAskUpdates(t, rt.Updates())
	rt.Stop()

	select {
	case _, ok := <-rt.Updates():
		if ok {
			t.Fatal("updates channel remained open after Stop")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("updates channel did not close after Stop")
	}
}

func TestAgentAskStreamRuntime_CancelCallsCancelEndpointAndCancelsContext(t *testing.T) {
	t.Parallel()

	var (
		mu             sync.Mutex
		cancelCalled   bool
		ctxErr         error
		capturedAgentID string
	)

	source := AgentAskStreamSourceFunc(func(ctx context.Context, onEvent func(AgentAskStreamEvent) error) error {
		// Block until context is cancelled, simulating an in-flight stream.
		<-ctx.Done()
		ctxErr = ctx.Err()
		return ctx.Err()
	})

	// Create a canceler that records when it's called.
	canceler := AgentAskCancelerFunc(func(ctx context.Context, agentID string) error {
		mu.Lock()
		cancelCalled = true
		capturedAgentID = agentID
		mu.Unlock()
		return nil
	})

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 4)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()

	rt.SetCanceler(canceler)

	if err := rt.Submit("agent-1", "hello"); err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	// Wait for the stream to be in flight.
	time.Sleep(50 * time.Millisecond)

	if !rt.IsInFlight() {
		t.Fatal("expected stream to be in flight before Cancel")
	}

	if err := rt.Cancel(); err != nil {
		t.Fatalf("Cancel error: %v", err)
	}

	// Wait for cancellation to propagate.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if !cancelCalled {
		t.Fatal("canceler was not called")
	}
	if capturedAgentID != "agent-1" {
		t.Fatalf("canceler agentID = %q, want %q", capturedAgentID, "agent-1")
	}
	mu.Unlock()

	if !errors.Is(ctxErr, context.Canceled) {
		t.Fatalf("source context error = %v, want context.Canceled", ctxErr)
	}

	// After cancel, the runtime should emit a cancelled update.
	updates := collectAgentAskUpdates(t, rt.Updates())

	var foundCancelled bool
	for _, u := range updates {
		if u.Type == AgentAskUpdateCancelled {
			foundCancelled = true
		}
	}
	if !foundCancelled {
		t.Fatalf("expected cancelled update, got: %+v", updates)
	}
}

func TestAgentAskStreamRuntime_CancelWithoutInFlightReturnsError(t *testing.T) {
	t.Parallel()

	source := AgentAskStreamSourceFunc(func(_ context.Context, _ func(AgentAskStreamEvent) error) error {
		return nil
	})

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 4)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()

	err = rt.Cancel()
	if err == nil {
		t.Fatal("Cancel with no in-flight stream error = nil, want error")
	}
	if !strings.Contains(err.Error(), "no stream in flight") {
		t.Fatalf("Cancel error = %q, want contains 'no stream in flight'", err.Error())
	}
}

func TestAgentAskStreamRuntime_RapidResubmitAfterCancelIsAccepted(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	source := AgentAskStreamSourceFunc(func(ctx context.Context, onEvent func(AgentAskStreamEvent) error) error {
		callCount.Add(1)
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "ok"})
		<-ctx.Done()
		return ctx.Err()
	})

	canceler := AgentAskCancelerFunc(func(_ context.Context, _ string) error {
		return nil
	})

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 4)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()

	rt.SetCanceler(canceler)

	// First submit.
	if err := rt.Submit("agent-1", "hello"); err != nil {
		t.Fatalf("first Submit error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Cancel.
	if err := rt.Cancel(); err != nil {
		t.Fatalf("Cancel error: %v", err)
	}

	// Drain updates from first stream.
	_ = collectAgentAskUpdates(t, rt.Updates())

	// Give the runtime a moment to finish cleaning up.
	time.Sleep(50 * time.Millisecond)

	// Rapid re-submit within 100ms should be accepted.
	if err := rt.Submit("agent-1", "hello again"); err != nil {
		t.Fatalf("rapid re-submit error: %v", err)
	}

	// Wait for the second stream to start.
	time.Sleep(50 * time.Millisecond)

	if callCount.Load() != 2 {
		t.Fatalf("source called %d times, want 2", callCount.Load())
	}
}

func collectAgentAskUpdates(t *testing.T, updates <-chan AgentAskStreamUpdate) []AgentAskStreamUpdate {
	t.Helper()

	collected := make([]AgentAskStreamUpdate, 0, 8)
	for {
		select {
		case update, ok := <-updates:
			if !ok {
				return collected
			}
			collected = append(collected, update)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for agent ask stream updates")
		}
	}
}
