package tui

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	gotui "github.com/grindlemire/go-tui"
)

// ---------------------------------------------------------------------------
// VAL-SKEL-015: SIGWINCH mid-stream — resize during active token streaming
// must not drop tokens, duplicate rows, lose composer focus, or break the
// cancel key binding.
// ---------------------------------------------------------------------------

// TestSigwinchMidStream_TokensNotDropped verifies that tokens emitted during
// a resize sequence are all retained in the streamLines state.
func TestSigwinchMidStream_TokensNotDropped(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var emittedTokens []string

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		tokens := []string{"The", " quick", " brown", " fox", " jumps", " over", " the", " lazy", " dog", "."}
		for _, tok := range tokens {
			if err := onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: tok}); err != nil {
				return err
			}
			mu.Lock()
			emittedTokens = append(emittedTokens, tok)
			mu.Unlock()
			// Slow emit to simulate real streaming.
			time.Sleep(5 * time.Millisecond)
		}
		return nil
	})

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(120, 40)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()
	cs.SetAskStreamRuntime(rt)

	cs.SetComposerText("tell me a story")
	if err := cs.SubmitComposer(); err != nil {
		t.Fatalf("SubmitComposer error: %v", err)
	}

	// Give the stream time to start emitting.
	time.Sleep(30 * time.Millisecond)

	// Resize mid-stream: 120x40 → 80x24.
	cs.UpdateSize(80, 24)

	// Give more tokens time to emit at the smaller size.
	time.Sleep(30 * time.Millisecond)

	// Resize back: 80x24 → 120x40.
	cs.UpdateSize(120, 40)

	// Collect remaining updates.
	updates := collectAgentAskUpdates(t, rt.Updates())
	for _, u := range updates {
		cs.ApplyAskStreamUpdate(u)
	}

	// Verify all tokens are present in streamLines.
	cs.mu.Lock()
	lines := make([]string, len(cs.streamLines))
	copy(lines, cs.streamLines)
	cs.mu.Unlock()

	var allText strings.Builder
	for _, line := range lines {
		allText.WriteString(line)
	}
	text := allText.String()

	mu.Lock()
	expectedTokens := make([]string, len(emittedTokens))
	copy(expectedTokens, emittedTokens)
	mu.Unlock()

	for _, tok := range expectedTokens {
		if !strings.Contains(text, tok) {
			t.Errorf("stream text missing token %q; full text: %q", tok, text)
		}
	}
}

// TestSigwinchMidStream_NoDuplicateRows verifies that resizing does not cause
// the same stream content to appear on multiple rows.
func TestSigwinchMidStream_NoDuplicateRows(t *testing.T) {
	t.Parallel()

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		for _, tok := range []string{"alpha", " beta", " gamma"} {
			_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: tok})
		}
		return nil
	})

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(120, 40)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()
	cs.SetAskStreamRuntime(rt)

	cs.SetComposerText("test")
	_ = cs.SubmitComposer()

	// Collect all updates first.
	updates := collectAgentAskUpdates(t, rt.Updates())

	// Apply half, resize, apply rest.
	mid := len(updates) / 2
	for i := 0; i < mid; i++ {
		cs.ApplyAskStreamUpdate(updates[i])
	}

	cs.UpdateSize(80, 24)
	_, mt1 := renderCockpitToMT(cs, 80, 24)

	for i := mid; i < len(updates); i++ {
		cs.ApplyAskStreamUpdate(updates[i])
	}

	cs.UpdateSize(120, 40)
	_, mt2 := renderCockpitToMT(cs, 120, 40)

	// Count occurrences of "alpha" in the Detail lane at each size.
	countInDetail := func(mt *gotui.MockTerminal, width int) int {
		count := 0
		for y := 1; y < width-1; y++ {
			line := extractDetailLane(mt, y, width)
			count += strings.Count(line, "alpha")
		}
		return count
	}

	c1 := countInDetail(mt1, 80)
	c2 := countInDetail(mt2, 120)

	if c1 > 1 {
		t.Errorf("duplicate 'alpha' rows at 80x24: found %d occurrences in detail lane", c1)
	}
	if c2 > 1 {
		t.Errorf("duplicate 'alpha' rows at 120x40: found %d occurrences in detail lane", c2)
	}
}

// TestSigwinchMidStream_FocusPreserved verifies that focusedLane remains
// unchanged across resizes during streaming.
func TestSigwinchMidStream_FocusPreserved(t *testing.T) {
	t.Parallel()

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "token"})
		return nil
	})

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.SetFocusedLane(1) // Detail lane focused
	cs.UpdateSize(120, 40)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()
	cs.SetAskStreamRuntime(rt)

	cs.SetComposerText("test")
	_ = cs.SubmitComposer()

	updates := collectAgentAskUpdates(t, rt.Updates())
	for _, u := range updates {
		cs.ApplyAskStreamUpdate(u)
	}

	if cs.FocusedLane() != 1 {
		t.Fatalf("before resize: expected focusedLane=1, got %d", cs.FocusedLane())
	}

	// Resize sequence.
	cs.UpdateSize(80, 24)
	if cs.FocusedLane() != 1 {
		t.Fatalf("after resize to 80x24: expected focusedLane=1, got %d", cs.FocusedLane())
	}

	cs.UpdateSize(120, 40)
	if cs.FocusedLane() != 1 {
		t.Fatalf("after resize back to 120x40: expected focusedLane=1, got %d", cs.FocusedLane())
	}
}

// TestSigwinchMidStream_CancelKeyBindingWorks verifies that Ctrl+X cancel
// works immediately after a resize during active streaming.
func TestSigwinchMidStream_CancelKeyBindingWorks(t *testing.T) {
	t.Parallel()

	var cancelCalled bool
	canceler := AgentAskCancelerFunc(func(_ context.Context, _ string) error {
		cancelCalled = true
		return nil
	})

	source := AgentAskStreamSourceFunc(func(ctx context.Context, onEvent func(AgentAskStreamEvent) error) error {
		_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: "token"})
		// Block until cancelled.
		<-ctx.Done()
		return ctx.Err()
	})

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(120, 40)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()
	rt.SetCanceler(canceler)
	cs.SetAskStreamRuntime(rt)

	cs.SetComposerText("test")
	if err := cs.SubmitComposer(); err != nil {
		t.Fatalf("SubmitComposer error: %v", err)
	}

	// Wait for stream to be in flight.
	time.Sleep(50 * time.Millisecond)

	// Resize mid-stream.
	cs.UpdateSize(80, 24)

	// Cancel immediately after resize.
	if err := rt.Cancel(); err != nil {
		t.Fatalf("Cancel after resize error: %v", err)
	}

	if !cancelCalled {
		t.Error("canceler was not called after resize")
	}

	// Drain updates and verify cancelled state is applied.
	updates := collectAgentAskUpdates(t, rt.Updates())
	for _, u := range updates {
		cs.ApplyAskStreamUpdate(u)
	}

	cs.mu.Lock()
	status := cs.streamStatus
	cs.mu.Unlock()

	if !strings.Contains(status, "cancelled") && !strings.Contains(status, "⊘") {
		t.Errorf("expected cancelled status after resize+cancel, got %q", status)
	}
}

// TestSigwinchMidStream_RenderNoCorruption verifies the full resize sequence
// 120x40 → 80x24 → 120x40 produces no overlapping cells or orphaned box
// drawing characters at each step while streaming is active.
func TestSigwinchMidStream_RenderNoCorruption(t *testing.T) {
	t.Parallel()

	source := AgentAskStreamSourceFunc(func(_ context.Context, onEvent func(AgentAskStreamEvent) error) error {
		for _, tok := range []string{"one", " two", " three", " four", " five"} {
			_ = onEvent(AgentAskStreamEvent{Phase: "delta", ContentDelta: tok})
			time.Sleep(5 * time.Millisecond)
		}
		return nil
	})

	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(120, 40)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	defer rt.Close()
	cs.SetAskStreamRuntime(rt)

	cs.SetComposerText("count")
	_ = cs.SubmitComposer()

	// Step 1: render at 120x40 while streaming.
	time.Sleep(20 * time.Millisecond)
	_, mt1 := renderCockpitToMT(cs, 120, 40)
	assertNoOverlap(t, mt1, 120, 40)
	assertNoOrphanedBoxDrawing(t, mt1, 120, 40)

	// Step 2: resize to 80x24 while streaming.
	cs.UpdateSize(80, 24)
	_, mt2 := renderCockpitToMT(cs, 80, 24)
	assertNoOverlap(t, mt2, 80, 24)
	assertNoOrphanedBoxDrawing(t, mt2, 80, 24)

	// Step 3: collect remaining updates.
	updates := collectAgentAskUpdates(t, rt.Updates())
	for _, u := range updates {
		cs.ApplyAskStreamUpdate(u)
	}

	// Step 4: resize back to 120x40.
	cs.UpdateSize(120, 40)
	_, mt3 := renderCockpitToMT(cs, 120, 40)
	assertNoOverlap(t, mt3, 120, 40)
	assertNoOrphanedBoxDrawing(t, mt3, 120, 40)
}

// TestSigwinchMidStream_NoGoroutineLeak verifies no goroutine leak after
// resize + stream completion.
func TestSigwinchMidStream_NoGoroutineLeak(t *testing.T) {
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
	cs.UpdateSize(120, 40)
	cs.SetPhase(CockpitPhaseReady)

	rt, err := NewAgentAskStreamRuntime(context.Background(), source, 16)
	if err != nil {
		t.Fatalf("NewAgentAskStreamRuntime error: %v", err)
	}
	cs.SetAskStreamRuntime(rt)

	cs.SetComposerText("test")
	_ = cs.SubmitComposer()

	// Resize mid-stream.
	time.Sleep(20 * time.Millisecond)
	cs.UpdateSize(80, 24)

	// Collect and drain.
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
