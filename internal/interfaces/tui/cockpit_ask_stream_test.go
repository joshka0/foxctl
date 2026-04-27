package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gotui "github.com/grindlemire/go-tui"
)

// ---------------------------------------------------------------------------
// CockpitScreen ask-stream integration tests
// ---------------------------------------------------------------------------

func TestCockpitScreen_AskStreamTokensRenderProgressively(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		callCount.Add(1)
		for _, tok := range []string{"Hello", " world", "!"} {
			if err := onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: tok}); err != nil {
				return err
			}
		}
		return nil
	})

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()
	cs.SetAskStreamRuntime(rt)

	// Simulate typing and submitting.
	cs.SetComposerText("hello agent")
	if err := cs.SubmitComposer(); err != nil {
		t.Fatalf("SubmitComposer error: %v", err)
	}

	// Collect updates and feed them into the cockpit state.
	updates := collectAgentAskUpdates(t, rt.Updates())
	for _, u := range updates {
		cs.ApplyAskStreamUpdate(u)
	}

	// Render and inspect Detail lane for progressive tokens.
	_, mt := renderCockpitToMT(cs, 80, 24)
	var detailText strings.Builder
	for y := 1; y < 23; y++ {
		detailText.WriteString(extractDetailLane(mt, y, 80))
	}
	text := detailText.String()

	if !strings.Contains(text, "Hello") {
		t.Errorf("detail lane should contain 'Hello', got: %q", text)
	}
	if !strings.Contains(text, "world") {
		t.Errorf("detail lane should contain 'world', got: %q", text)
	}
	if callCount.Load() != 1 {
		t.Fatalf("source called %d times, want 1", callCount.Load())
	}
}

func TestCockpitScreen_AskStreamShowsTerminalStatusOnDone(t *testing.T) {
	t.Parallel()

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "done"})
		return nil
	})

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()
	cs.SetAskStreamRuntime(rt)

	cs.SetComposerText("hello")
	if err := cs.SubmitComposer(); err != nil {
		t.Fatalf("SubmitComposer error: %v", err)
	}

	updates := collectAgentAskUpdates(t, rt.Updates())
	for _, u := range updates {
		cs.ApplyAskStreamUpdate(u)
	}

	_, mt := renderCockpitToMT(cs, 80, 24)
	var detailText strings.Builder
	for y := 1; y < 23; y++ {
		detailText.WriteString(extractDetailLane(mt, y, 80))
	}
	text := detailText.String()

	// Should show a terminal status marker (✓ or "done").
	if !strings.Contains(text, "✓") && !strings.Contains(text, "done") {
		t.Errorf("detail lane should contain terminal status marker, got: %q", text)
	}
}

func TestCockpitScreen_AskStreamDoubleSubmitRejects(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	source := AgentAskStreamSourceFunc(func(ctx context.Context, onEvent func(AgentAskStreamEvent) error) error {
		callCount.Add(1)
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "first"})
		<-ctx.Done()
		return ctx.Err()
	})

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()
	cs.SetAskStreamRuntime(rt)

	cs.SetComposerText("hello")
	if err := cs.SubmitComposer(); err != nil {
		t.Fatalf("first submit error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Second submit should be rejected with visible message.
	cs.SetComposerText("hello again")
	err = cs.SubmitComposer()
	if err == nil {
		t.Fatal("second submit error = nil, want rejection")
	}

	_, mt := renderCockpitToMT(cs, 80, 24)

	// The rejection message should be visible in the footer (status bar).
	footerText := rowTextFromMT(mt, 23, 80)
	if !strings.Contains(footerText, "in flight") {
		t.Errorf("footer should show 'in flight' rejection message, got: %q", footerText)
	}
	if callCount.Load() != 1 {
		t.Fatalf("source called %d times, want 1", callCount.Load())
	}
}

func TestCockpitScreen_AskStreamNoGoroutineLeakOnDone(t *testing.T) {
	t.Parallel()

	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "x"})
		return nil
	})

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	cs.SetAskStreamRuntime(rt)

	cs.SetComposerText("hello")
	_ = cs.SubmitComposer()

	updates := collectAgentAskUpdates(t, rt.Updates())
	for _, u := range updates {
		cs.ApplyAskStreamUpdate(u)
	}

	rt.Stop()

	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()

	delta := after - baseline
	if delta > 2 {
		t.Fatalf("potential goroutine leak: %d extra goroutines (baseline=%d, after=%d)", delta, baseline, after)
	}
}

func TestCockpitScreen_AskStreamRejectsWhenNoAgentSelected(t *testing.T) {
	t.Parallel()

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.ClearSelection()
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	err := cs.SubmitComposer()
	if err == nil {
		t.Fatal("SubmitComposer with no selection error = nil, want error")
	}
	if !strings.Contains(err.Error(), "select") {
		t.Fatalf("error = %q, want contains 'select'", err.Error())
	}
}

func TestCockpitScreen_AskStreamRejectsWhenComposerEmpty(t *testing.T) {
	t.Parallel()

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	cs.SetComposerText("   ")
	err := cs.SubmitComposer()
	if err == nil {
		t.Fatal("SubmitComposer with empty text error = nil, want error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("error = %q, want contains 'empty'", err.Error())
	}
}

func TestCockpitScreen_AskStreamCancelledStateDistinctFromError(t *testing.T) {
	t.Parallel()

	source := AgentAskStreamSourceFunc(func(ctx context.Context, onEvent func(AgentAskStreamEvent) error) error {
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "partial"})
		// Block until context is cancelled (simulating in-flight HTTP request).
		<-ctx.Done()
		return ctx.Err()
	})

	canceler := AgentAskCancelerFunc(func(_ context.Context, _ string) error {
		return nil
	})

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()
	rt.SetCanceler(canceler)
	cs.SetAskStreamRuntime(rt)

	cs.SetComposerText("hello")
	if err := cs.SubmitComposer(); err != nil {
		t.Fatalf("SubmitComposer error: %v", err)
	}

	// Wait for stream to be in flight, then cancel.
	time.Sleep(50 * time.Millisecond)
	if err := rt.Cancel(); err != nil {
		t.Fatalf("Cancel error: %v", err)
	}

	updates := collectAgentAskUpdates(t, rt.Updates())
	for _, u := range updates {
		cs.ApplyAskStreamUpdate(u)
	}

	_, mt := renderCockpitToMT(cs, 80, 24)
	var detailText strings.Builder
	for y := 1; y < 23; y++ {
		detailText.WriteString(extractDetailLane(mt, y, 80))
	}
	text := detailText.String()

	// Cancelled state should show "⊘" or "cancelled" — distinct from error "⚠".
	if !strings.Contains(text, "⊘") && !strings.Contains(text, "cancelled") {
		t.Errorf("detail lane should contain cancelled marker, got: %q", text)
	}
	// Must NOT contain the error marker.
	if strings.Contains(text, "⚠") {
		t.Errorf("detail lane should NOT contain error marker ⚠ for cancelled state, got: %q", text)
	}
}

func TestCockpitScreen_AskStreamRapidResubmitAfterCancel(t *testing.T) {
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

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()
	rt.SetCanceler(canceler)
	cs.SetAskStreamRuntime(rt)

	// First submit.
	cs.SetComposerText("hello")
	if err := cs.SubmitComposer(); err != nil {
		t.Fatalf("first submit error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Cancel via the runtime directly (simulating Ctrl+X).
	if err := rt.Cancel(); err != nil {
		t.Fatalf("Cancel error: %v", err)
	}

	// Drain updates.
	_ = collectAgentAskUpdates(t, rt.Updates())

	// Give the runtime a moment to finish cleaning up.
	time.Sleep(50 * time.Millisecond)

	// Rapid re-submit within 100ms should be accepted.
	cs.SetComposerText("hello again")
	if err := cs.SubmitComposer(); err != nil {
		t.Fatalf("rapid re-submit error: %v", err)
	}

	// Wait for the second stream to start.
	time.Sleep(50 * time.Millisecond)

	if callCount.Load() != 2 {
		t.Fatalf("source called %d times, want 2", callCount.Load())
	}
}

func TestCockpitScreen_ComposerTextRoundTrip(t *testing.T) {
	cs := NewCockpitScreen("")
	cs.SetComposerText("hello world")
	if cs.ComposerText() != "hello world" {
		t.Fatalf("ComposerText = %q, want %q", cs.ComposerText(), "hello world")
	}
	cs.ClearComposer()
	if cs.ComposerText() != "" {
		t.Fatalf("ComposerText after Clear = %q, want empty", cs.ComposerText())
	}
}

// ---------------------------------------------------------------------------
// VAL-SKEL-016: Malformed SSE — cockpit-level assertions
// ---------------------------------------------------------------------------

func TestCockpitScreen_MalformedSSEShowsIndicator(t *testing.T) {
	t.Parallel()

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "before"})
		_ = onEvent(AgentAskStreamEvent{Phase: "totally_unknown", Content: "???"})
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "after"})
		return nil
	})

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()
	cs.SetAskStreamRuntime(rt)

	cs.SetComposerText("hello")
	if err := cs.SubmitComposer(); err != nil {
		t.Fatalf("SubmitComposer error: %v", err)
	}

	updates := collectAgentAskUpdates(t, rt.Updates())
	for _, u := range updates {
		cs.ApplyAskStreamUpdate(u)
	}

	_, mt := renderCockpitToMT(cs, 80, 24)
	var detailText strings.Builder
	for y := 1; y < 23; y++ {
		detailText.WriteString(extractDetailLane(mt, y, 80))
	}
	text := detailText.String()

	// Must contain the malformed-event indicator.
	if !strings.Contains(text, "malformed") && !strings.Contains(text, "�") {
		t.Errorf("detail lane should contain malformed-event indicator, got: %q", text)
	}
	// Subsequent well-formed token should still render.
	if !strings.Contains(text, "before") || !strings.Contains(text, "after") {
		t.Errorf("detail lane should contain 'before' and 'after' tokens, got: %q", text)
	}
}

func TestCockpitScreen_MalformedSSEDoesNotFreezeUI(t *testing.T) {
	t.Parallel()

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		_ = onEvent(AgentAskStreamEvent{Phase: "nonsense", Content: "bad"})
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "ok"})
		return nil
	})

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()
	cs.SetAskStreamRuntime(rt)

	cs.SetComposerText("hello")
	if err := cs.SubmitComposer(); err != nil {
		t.Fatalf("SubmitComposer error: %v", err)
	}

	updates := collectAgentAskUpdates(t, rt.Updates())
	for _, u := range updates {
		cs.ApplyAskStreamUpdate(u)
	}

	// ESC key should still work after malformed frame — verify KeyMap doesn't panic.
	km := cs.KeyMap()
	for _, kb := range km {
		if kb.Pattern.Key == gotui.KeyEscape {
			// Simulate ESC — should not panic even after malformed events.
			kb.Handler(gotui.KeyEvent{Key: gotui.KeyEscape})
			break
		}
	}

	// Drawer should also still work.
	cs.openEvidenceDrawer()
	if !cs.EvidenceDrawerOpen() {
		t.Error("expected drawer to open after malformed events")
	}
}

func TestCockpitScreen_MalformedSSEViaFakeServerRecovery(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not implement http.Flusher")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Well-formed.
		_, _ = w.Write([]byte("event: delta\ndata: {\"phase\":\"delta\",\"content_delta\":\"first\"}\n\n"))
		flusher.Flush()

		// Unknown event type.
		_, _ = w.Write([]byte("event: weird\ndata: {\"phase\":\"weird\",\"content\":\"???\"}\n\n"))
		flusher.Flush()

		// Invalid JSON.
		_, _ = w.Write([]byte("event: delta\ndata: {broken\n\n"))
		flusher.Flush()

		// Well-formed after malformed.
		_, _ = w.Write([]byte("event: delta\ndata: {\"phase\":\"delta\",\"content_delta\":\"second\"}\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}

	cs := NewCockpitScreen(srv.URL)
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	source := NewHTTPAgentAskStreamSource(client, "agent-abc")
	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()
	cs.SetAskStreamRuntime(rt)

	cs.SetComposerText("hello")
	if err := cs.SubmitComposer(); err != nil {
		t.Fatalf("SubmitComposer error: %v", err)
	}

	updates := collectAgentAskUpdates(t, rt.Updates())
	for _, u := range updates {
		cs.ApplyAskStreamUpdate(u)
	}

	_, mt := renderCockpitToMT(cs, 80, 24)
	var detailText strings.Builder
	for y := 1; y < 23; y++ {
		detailText.WriteString(extractDetailLane(mt, y, 80))
	}
	text := detailText.String()

	// Malformed indicator visible.
	if !strings.Contains(text, "malformed") && !strings.Contains(text, "�") {
		t.Errorf("detail lane should contain malformed indicator, got: %q", text)
	}
	// Both well-formed tokens visible.
	if !strings.Contains(text, "first") || !strings.Contains(text, "second") {
		t.Errorf("detail lane should contain 'first' and 'second' tokens, got: %q", text)
	}
}
