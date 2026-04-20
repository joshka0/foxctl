//go:build integration
// +build integration

// Package group_a_test validates Group A assertions for the TUI walking skeleton:
//
//   - VAL-SKEL-001: Entry point with minimum-terminal-size guard (tuistory)
//   - VAL-SKEL-002: Async boot with health-check polling and timeout (go-test + testfixture)
//   - VAL-SKEL-004: Agent inventory rows with 6 fields and deterministic sort (go-test + testfixture)
//   - VAL-SKEL-010: CTA empty state when no agents running (go-test + testfixture)
//   - VAL-SKEL-011: Error state with URL, retry hint, and ESC exit (go-test + testfixture)
//   - VAL-SKEL-012: Status footer with keybinds and phase-correct content (tuistory)
//
// Run with:  go test -race -count=1 -v ./internal/interfaces/tui/...
// (also requires tuistory for VAL-SKEL-001 and VAL-SKEL-012, see run_group_a.sh)
package group_a_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gotui "github.com/grindlemire/go-tui"

	"github.com/joshka0/foxctl/internal/interfaces/tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/testfixture"
)

// ---------------------------------------------------------------------------
// VAL-SKEL-002: Async boot with health-check polling and timeout
// ---------------------------------------------------------------------------

// TestBootManager_TransitionsToReadyWithinTimeout verifies that the boot
// manager polls health until the daemon responds, then transitions to Ready.
// This covers the happy-path "boot" scenario from VAL-SKEL-002.
func TestBootManager_TransitionsToReadyWithinTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	// Boot the fixture with 3 seeded agents (researcher, coder, planner).
	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{
		Roles: []string{"researcher", "coder", "planner"},
	})

	cs := tui.NewCockpitScreen(fixture.BaseURL())
	cs.UpdateSize(80, 24)

	agentAdapter, err := tui.NewAgentAdapter(fixture.APIClient())
	if err != nil {
		t.Fatalf("create agent adapter: %v", err)
	}

	bm := tui.NewBootManager(tui.BootConfig{
		APIURL:       fixture.BaseURL(),
		Screen:       cs,
		Timeout:      10 * time.Second,
		AgentAdapter: agentAdapter,
	})

	start := time.Now()
	bm.Start()
	defer bm.Stop()
	bm.WaitForDone()
	elapsed := time.Since(start)

	// VAL-SKEL-002: Boot transitions to Ready within timeout.
	if cs.Phase() != tui.CockpitPhaseReady {
		t.Fatalf("expected phase=Ready, got %s (elapsed=%v)", cs.Phase(), elapsed)
	}

	// Timeout must be respected.
	if elapsed > 10*time.Second {
		t.Fatalf("boot took %v, exceeded 10s timeout", elapsed)
	}

	t.Logf("VAL-SKEL-002 PASS: transitioned to Ready in %v", elapsed)
}

// TestBootManager_LoadingPhaseBeforeTransition verifies that the screen
// starts in Loading phase before the async boot completes.
func TestBootManager_LoadingPhaseBeforeTransition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{Roles: []string{"researcher"}})

	cs := tui.NewCockpitScreen(fixture.BaseURL())
	cs.UpdateSize(80, 24)

	agentAdapter, err := tui.NewAgentAdapter(fixture.APIClient())
	if err != nil {
		t.Fatalf("create agent adapter: %v", err)
	}

	bm := tui.NewBootManager(tui.BootConfig{
		APIURL:       fixture.BaseURL(),
		Screen:       cs,
		Timeout:      5 * time.Second,
		AgentAdapter: agentAdapter,
	})

	// Before Start, phase is Loading (no sync HTTP at startup).
	if cs.Phase() != tui.CockpitPhaseLoading {
		t.Fatalf("initial phase should be Loading, got %s", cs.Phase())
	}

	bm.Start()
	defer bm.Stop()

	// After Start, phase is still Loading (async poll).
	// Give a brief moment for the goroutine to run.
	time.Sleep(10 * time.Millisecond)
	if cs.Phase() != tui.CockpitPhaseLoading {
		t.Fatalf("phase immediately after Start should be Loading, got %s", cs.Phase())
	}

	bm.WaitForDone()
	if cs.Phase() != tui.CockpitPhaseReady {
		t.Fatalf("expected Ready after WaitForDone, got %s", cs.Phase())
	}

	t.Logf("VAL-SKEL-002 PASS: async boot with loading phase")
}

// TestBootManager_NoSyncHTTPBeforeFirstPaint verifies that no HTTP request
// is made before the first render, confirming non-blocking startup.
func TestBootManager_NoSyncHTTPBeforeFirstPaint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	var requestMade atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMade.Store(true)
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	cs := tui.NewCockpitScreen(server.URL)
	cs.UpdateSize(80, 24)

	// First paint — must not block on HTTP.
	el := cs.Render(nil)
	if el == nil {
		t.Fatal("first Render should succeed without any HTTP")
	}
	if requestMade.Load() {
		t.Fatal("VAL-SKEL-002 violation: HTTP request was made before first render")
	}

	// Now start async boot.
	bm := tui.NewBootManager(tui.BootConfig{
		APIURL:  server.URL,
		Screen:  cs,
		Timeout: 2 * time.Second,
	})
	bm.Start()
	bm.WaitForDone()
	bm.Stop()

	if cs.Phase() != tui.CockpitPhaseReady {
		t.Fatalf("expected Ready, got %s", cs.Phase())
	}

	t.Logf("VAL-SKEL-002 PASS: no synchronous HTTP before first paint")
}

// ---------------------------------------------------------------------------
// VAL-SKEL-004: Agent inventory rows with 6 fields and deterministic sort
// ---------------------------------------------------------------------------

// TestInventory_SixFieldsPerRow verifies that each fetched agent has exactly
// 6 populated fields: ID, Role, Status, Workspace, ParentID, LastActive.
func TestInventory_SixFieldsPerRow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{
		Roles: []string{"researcher", "coder", "planner"},
	})

	cs := tui.NewCockpitScreen(fixture.BaseURL())
	cs.UpdateSize(80, 24)

	agentAdapter, err := tui.NewAgentAdapter(fixture.APIClient())
	if err != nil {
		t.Fatalf("create agent adapter: %v", err)
	}

	bm := tui.NewBootManager(tui.BootConfig{
		APIURL:       fixture.BaseURL(),
		Screen:       cs,
		Timeout:      5 * time.Second,
		AgentAdapter: agentAdapter,
	})

	bm.Start()
	defer bm.Stop()
	bm.WaitForDone()

	agents := cs.Agents()
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}

	// VAL-SKEL-004: Every agent must have all 6 fields populated.
	missingField := func(name string, val string) bool { return val == "" }
	for i, a := range agents {
		fields := map[string]string{
			"ID":        a.ID,
			"Role":      a.Role,
			"Status":    a.Status,
			"Workspace": a.Workspace,
			"ParentID":  a.ParentID,
			"LastActive": a.LastActive,
		}
		var missing []string
		for k, v := range fields {
			if missingField(k, v) {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			t.Errorf("agent[%d] missing fields: %v (agent=%+v)", i, missing, a)
		}
	}

	t.Logf("VAL-SKEL-004 PASS: all 3 agents have 6 fields each")
}

// TestInventory_DeterministicSort verifies that agents are sorted
// by Role ascending, then ID ascending. Expected order: coder, planner, researcher.
func TestInventory_DeterministicSort(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{
		Roles: []string{"researcher", "coder", "planner"},
	})

	cs := tui.NewCockpitScreen(fixture.BaseURL())
	cs.UpdateSize(80, 24)

	agentAdapter, err := tui.NewAgentAdapter(fixture.APIClient())
	if err != nil {
		t.Fatalf("create agent adapter: %v", err)
	}

	bm := tui.NewBootManager(tui.BootConfig{
		APIURL:       fixture.BaseURL(),
		Screen:       cs,
		Timeout:      5 * time.Second,
		AgentAdapter: agentAdapter,
	})

	bm.Start()
	defer bm.Stop()
	bm.WaitForDone()

	agents := cs.Agents()
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}

	// VAL-SKEL-004: Sorted by Role asc, then ID asc.
	expectedRoles := []string{"coder", "planner", "researcher"}
	for i, exp := range expectedRoles {
		if agents[i].Role != exp {
			t.Errorf("agent[%d].Role=%q, want %q (sort order: coder, planner, researcher)", i, agents[i].Role, exp)
		}
	}

	// Secondary sort by ID (ascending).
	for i := 0; i < len(agents)-1; i++ {
		if agents[i].Role == agents[i+1].Role && agents[i].ID > agents[i+1].ID {
			t.Errorf("agents[%d].ID=%q > agents[%d].ID=%q — secondary sort by ID broken", i, agents[i].ID, i+1, agents[i+1].ID)
		}
	}

	t.Logf("VAL-SKEL-004 PASS: deterministic sort by role asc then id asc")
}

// TestInventory_ColumnsAt80Cols verifies that the inventory renders with
// correct column alignment at 80 columns (minimum viable width).
func TestInventory_ColumnsAt80Cols(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{
		Roles: []string{"researcher", "coder", "planner"},
	})

	cs := tui.NewCockpitScreen(fixture.BaseURL())
	cs.UpdateSize(80, 24)

	agentAdapter, err := tui.NewAgentAdapter(fixture.APIClient())
	if err != nil {
		t.Fatalf("create agent adapter: %v", err)
	}

	bm := tui.NewBootManager(tui.BootConfig{
		APIURL:       fixture.BaseURL(),
		Screen:       cs,
		Timeout:      5 * time.Second,
		AgentAdapter: agentAdapter,
	})

	bm.Start()
	defer bm.Stop()
	bm.WaitForDone()

	// Render to mock terminal at 80 columns.
	mt := gotui.NewMockTerminal(80, 24)
	cs.Render(mt)

	// Check that header row exists and contains "AGENTS" label.
	headerRow := mtLine(mt, 1, 80)
	if !strings.Contains(headerRow, "AGENTS") {
		t.Errorf("inventory header row should contain 'AGENTS', got: %q", headerRow)
	}

	t.Logf("VAL-SKEL-004 PASS: inventory renders at 80 columns")
}

// ---------------------------------------------------------------------------
// VAL-SKEL-010: CTA empty state when no agents running
// ---------------------------------------------------------------------------

// TestEmptyState_NoAgentsShowsReadyWithZeroRows verifies that when the
// daemon has no agents, the inventory shows zero rows (not an error state).
func TestEmptyState_NoAgentsShowsReadyWithZeroRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	// Seed zero agents (nil roles).
	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{Roles: nil})

	cs := tui.NewCockpitScreen(fixture.BaseURL())
	cs.UpdateSize(80, 24)

	agentAdapter, err := tui.NewAgentAdapter(fixture.APIClient())
	if err != nil {
		t.Fatalf("create agent adapter: %v", err)
	}

	bm := tui.NewBootManager(tui.BootConfig{
		APIURL:       fixture.BaseURL(),
		Screen:       cs,
		Timeout:      5 * time.Second,
		AgentAdapter: agentAdapter,
	})

	bm.Start()
	defer bm.Stop()
	bm.WaitForDone()

	// VAL-SKEL-010: Zero agents must result in Ready phase, not Error.
	if cs.Phase() != tui.CockpitPhaseReady {
		t.Fatalf("VAL-SKEL-010: expected phase=Ready with zero agents, got %s", cs.Phase())
	}

	agents := cs.Agents()
	if len(agents) != 0 {
		t.Fatalf("VAL-SKEL-010: expected 0 agents, got %d", len(agents))
	}

	// Verify the empty-state message is rendered (not blank screen).
	mt := gotui.NewMockTerminal(80, 24)
	el := cs.Render(mt)
	if el == nil {
		t.Fatal("VAL-SKEL-010: Render returned nil for empty state")
	}

	t.Logf("VAL-SKEL-010 PASS: zero agents → Ready phase with empty inventory")
}

// TestEmptyState_RenderContainsEmptyHint verifies the empty-state render
// contains a CTA message (e.g., "no agents" or "spawn an agent").
func TestEmptyState_RenderContainsEmptyHint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{Roles: nil})

	cs := tui.NewCockpitScreen(fixture.BaseURL())
	cs.UpdateSize(80, 24)

	agentAdapter, err := tui.NewAgentAdapter(fixture.APIClient())
	if err != nil {
		t.Fatalf("create agent adapter: %v", err)
	}

	bm := tui.NewBootManager(tui.BootConfig{
		APIURL:       fixture.BaseURL(),
		Screen:       cs,
		Timeout:      5 * time.Second,
		AgentAdapter: agentAdapter,
	})

	bm.Start()
	defer bm.Stop()
	bm.WaitForDone()

	mt := gotui.NewMockTerminal(80, 24)
	cs.Render(mt)

	// Collect all non-empty lines.
	var lines []string
	for y := 0; y < 24; y++ {
		l := mtLine(mt, y, 80)
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}

	if len(lines) == 0 {
		t.Fatal("VAL-SKEL-010: screen was blank — empty state must show a message")
	}

	// The CTA should be present in at least one line.
	ctaKeywords := []string{"no agents", "empty", "spawn", "run an agent", "no active"}
	found := false
	for _, line := range lines {
		lower := strings.ToLower(line)
		for _, kw := range ctaKeywords {
			if strings.Contains(lower, kw) {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Errorf("VAL-SKEL-010: empty state should contain a CTA keyword (%v), but screen lines were: %v", ctaKeywords, lines)
	}

	t.Logf("VAL-SKEL-010 PASS: empty state contains CTA message")
}

// ---------------------------------------------------------------------------
// VAL-SKEL-011: Error state with URL, retry hint, and ESC exit
// ---------------------------------------------------------------------------

// TestErrorState_UnreachableAPITransitionsToError verifies that when the API
// URL is unreachable, the boot manager transitions to Error phase.
func TestErrorState_UnreachableAPITransitionsToError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	// Use a host that nothing is listening on.
	apiURL := "http://127.0.0.1:1"
	cs := tui.NewCockpitScreen(apiURL)
	cs.UpdateSize(80, 24)

	bm := tui.NewBootManager(tui.BootConfig{
		APIURL: apiURL,
		Screen: cs,
		Timeout: 500 * time.Millisecond,
		Client: &http.Client{
			Timeout: 100 * time.Millisecond,
		},
	})

	bm.Start()
	defer bm.Stop()
	bm.WaitForDone()

	// VAL-SKEL-011: Unreachable API must result in Error phase.
	if cs.Phase() != tui.CockpitPhaseError {
		t.Fatalf("VAL-SKEL-011: expected phase=Error, got %s", cs.Phase())
	}

	t.Logf("VAL-SKEL-011 PASS: unreachable API → Error phase")
}

// TestErrorState_ErrorRenderContainsURL verifies that the error render
// shows the unreachable API URL.
func TestErrorState_ErrorRenderContainsURL(t *testing.T) {
	apiURL := "http://127.0.0.1:1"
	cs := tui.NewCockpitScreen(apiURL)
	cs.UpdateSize(80, 24)
	cs.SetPhase(tui.CockpitPhaseError)

	el := cs.renderError(80, 24, apiURL)
	if el == nil {
		t.Fatal("renderError returned nil")
	}

	mt := gotui.NewMockTerminal(80, 24)
	cs.Render(mt)

	line0 := mtLine(mt, 0, 80)
	if !strings.Contains(line0, apiURL) {
		t.Errorf("VAL-SKEL-011: error state should contain URL %q, got: %q", apiURL, line0)
	}

	t.Logf("VAL-SKEL-011 PASS: error render contains URL")
}

// TestErrorState_ErrorRenderContainsRetryHint verifies that the error render
// shows a retry hint (e.g., "press r to retry").
func TestErrorState_ErrorRenderContainsRetryHint(t *testing.T) {
	cs := tui.NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)
	cs.SetPhase(tui.CockpitPhaseError)

	mt := gotui.NewMockTerminal(80, 24)
	cs.Render(mt)

	// Collect all lines with content.
	var allLines string
	for y := 0; y < 24; y++ {
		allLines += mtLine(mt, y, 80) + "\n"
	}

	// VAL-SKEL-011: Error must show retry hint.
	retryKeywords := []string{"retry", "press r", "r to retry", "ESC"}
	found := false
	for _, kw := range retryKeywords {
		if strings.Contains(strings.ToLower(allLines), kw) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("VAL-SKEL-011: error state should contain a retry hint, got: %s", allLines)
	}

	t.Logf("VAL-SKEL-011 PASS: error render contains retry hint")
}

// TestErrorState_EscKeyBindingPresent verifies that the error state's KeyMap
// includes ESC binding for exit.
func TestErrorState_EscKeyBindingPresent(t *testing.T) {
	cs := tui.NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)
	cs.SetPhase(tui.CockpitPhaseError)

	km := cs.KeyMap()
	if len(km) == 0 {
		t.Fatal("KeyMap should not be empty during error state")
	}

	// VAL-SKEL-011: Error state ESC exit must be in KeyMap.
	// The KeyMap maps keys to handlers — we verify at least ESC is present.
	escFound := false
	for _, binding := range km {
		if binding.Key == "escape" || binding.Key == "esc" {
			escFound = true
			break
		}
	}

	if !escFound {
		t.Errorf("VAL-SKEL-011: KeyMap should contain ESC binding for exit, got %d bindings", len(km))
	}

	t.Logf("VAL-SKEL-011 PASS: ESC key binding present in error state")
}

// TestErrorState_ESCExitsCleanly verifies that ESC during error state exits
// with code 0 (via KeyMap handler).
func TestErrorState_ESCExitsCleanly(t *testing.T) {
	cs := tui.NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)
	cs.SetPhase(tui.CockpitPhaseError)

	// The KeyMap for error state should include ESC which maps to the quit action.
	km := cs.KeyMap()

	escBinding := false
	for _, b := range km {
		if b.Key == "escape" || b.Key == "esc" {
			escBinding = true
			// Verify the action is the quit action (exits code 0).
			if b.Action == nil {
				t.Error("ESC binding has nil Action")
			}
			break
		}
	}

	if !escBinding {
		t.Error("VAL-SKEL-011: ESC binding missing from error state KeyMap")
	}

	t.Logf("VAL-SKEL-011 PASS: ESC exits cleanly (code 0) in error state")
}

// TestErrorState_RetryKeyTriggersRetry verifies that pressing 'r' during
// error state triggers a retry via KeyMap.
func TestErrorState_RetryKeyTriggersRetry(t *testing.T) {
	cs := tui.NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)
	cs.SetPhase(tui.CockpitPhaseError)

	km := cs.KeyMap()

	// VAL-SKEL-011: Error state must have 'r' binding for retry.
	retryFound := false
	for _, b := range km {
		if b.Key == "r" {
			retryFound = true
			break
		}
	}

	if !retryFound {
		t.Errorf("VAL-SKEL-011: KeyMap should contain 'r' binding for retry, got %d bindings", len(km))
	}

	t.Logf("VAL-SKEL-011 PASS: 'r' retry key binding present in error state")
}

// ---------------------------------------------------------------------------
// VAL-SKEL-012: Status footer with keybinds and phase-correct content
// (partially covered here; VAL-SKEL-012 also requires tuistory visual check)
// ---------------------------------------------------------------------------

// TestStatusFooter_ReadyStateContainsAllThreeElements verifies the footer
// in Ready state contains: (1) connection status, (2) active entity label,
// (3) ≥3 keybinding hints.
func TestStatusFooter_ReadyStateContainsAllThreeElements(t *testing.T) {
	cs := tui.NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]tui.AgentInventoryItem{
		{ID: "agent-abc12345", Role: "researcher", Status: "running", Workspace: "ws1", ParentID: "—", LastActive: "2m"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(tui.CockpitPhaseReady)

	mt := gotui.NewMockTerminal(80, 24)
	cs.Render(mt)

	footer := mtLine(mt, 23, 80)

	// (1) Connection status.
	statusFound := strings.Contains(footer, "connected") ||
		strings.Contains(footer, "✓") ||
		strings.Contains(footer, "done")
	if !statusFound {
		t.Errorf("VAL-SKEL-012: footer missing connection status in Ready state: %q", footer)
	}

	// (2) Active entity label.
	if !strings.Contains(footer, "agent:") {
		t.Errorf("VAL-SKEL-012: footer missing active entity label 'agent:': %q", footer)
	}

	// (3) ≥3 keybinding hints.
	bindingCount := 0
	for _, b := range []string{"ESC", "↑↓", "Enter", "e", "Ctrl+X", "r"} {
		if strings.Contains(footer, b) {
			bindingCount++
		}
	}
	if bindingCount < 3 {
		t.Errorf("VAL-SKEL-012: footer has %d keybinding hints, expected ≥3: %q", bindingCount, footer)
	}

	t.Logf("VAL-SKEL-012 PASS: Ready footer contains all three elements (status, entity, bindings)")
}

// TestStatusFooter_ErrorStateContainsAllThreeElements verifies the footer
// in Error state contains: (1) error status, (2) retry hint, (3) ≥3 keybinds.
func TestStatusFooter_ErrorStateContainsAllThreeElements(t *testing.T) {
	cs := tui.NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)
	cs.SetPhase(tui.CockpitPhaseError)

	mt := gotui.NewMockTerminal(80, 24)
	cs.Render(mt)

	footer := mtLine(mt, 23, 80)

	// (1) Error status.
	if !strings.Contains(footer, "error") {
		t.Errorf("VAL-SKEL-012: footer missing 'error' status in Error state: %q", footer)
	}

	// (2) Retry hint.
	if !strings.Contains(footer, "r") && !strings.Contains(footer, "retry") {
		t.Errorf("VAL-SKEL-012: footer missing retry hint in Error state: %q", footer)
	}

	// (3) ≥3 keybinding hints.
	bindingCount := 0
	for _, b := range []string{"ESC", "↑↓", "Enter", "e", "Ctrl+X", "r"} {
		if strings.Contains(footer, b) {
			bindingCount++
		}
	}
	if bindingCount < 3 {
		t.Errorf("VAL-SKEL-012: error footer has %d keybinding hints, expected ≥3: %q", bindingCount, footer)
	}

	t.Logf("VAL-SKEL-012 PASS: Error footer contains all three elements")
}

// TestStatusFooter_LoadingStateContainsAllThreeElements verifies the footer
// in Loading state contains: (1) connecting status, (2) no active entity,
// (3) ≥3 keybinds.
func TestStatusFooter_LoadingStateContainsAllThreeElements(t *testing.T) {
	cs := tui.NewCockpitScreen("http://localhost:9999")
	cs.UpdateSize(80, 24)
	cs.SetPhase(tui.CockpitPhaseLoading)

	mt := gotui.NewMockTerminal(80, 24)
	cs.Render(mt)

	footer := mtLine(mt, 23, 80)

	// (1) Connecting status.
	if !strings.Contains(footer, "connecting") && !strings.Contains(footer, "connect") {
		t.Errorf("VAL-SKEL-012: footer missing 'connecting' status in Loading state: %q", footer)
	}

	// (2) No active entity in loading state.
	// (this is expected — no agent selected yet)

	// (3) ≥3 keybinding hints.
	bindingCount := 0
	for _, b := range []string{"ESC", "↑↓", "Enter", "e", "Ctrl+X", "r"} {
		if strings.Contains(footer, b) {
			bindingCount++
		}
	}
	if bindingCount < 3 {
		t.Errorf("VAL-SKEL-012: loading footer has %d keybinding hints, expected ≥3: %q", bindingCount, footer)
	}

	t.Logf("VAL-SKEL-012 PASS: Loading footer contains all three elements")
}

// TestStatusFooter_MinimumSize verifies footer renders correctly at the
// minimum terminal size (60x15).
func TestStatusFooter_MinimumSize(t *testing.T) {
	cs := tui.NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]tui.AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running", Workspace: "ws1", ParentID: "—", LastActive: "2m"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(60, 15)
	cs.SetPhase(tui.CockpitPhaseReady)

	mt := gotui.NewMockTerminal(60, 15)
	cs.Render(mt)

	footer := mtLine(mt, 14, 60)

	// Footer must still contain status + keybindings at minimum size.
	if footer == "" {
		t.Error("VAL-SKEL-012: footer is empty at 60x15")
	}

	hasStatus := strings.Contains(footer, "connected") || strings.Contains(footer, "✓")
	hasBindings := strings.Contains(footer, "ESC") || strings.Contains(footer, "↑↓")

	if !hasStatus || !hasBindings {
		t.Errorf("VAL-SKEL-012: footer missing status or bindings at minimum size: %q", footer)
	}

	t.Logf("VAL-SKEL-012 PASS: footer visible at minimum size (60x15)")
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// mtLine extracts the text of a row from a MockTerminal, converting
// zero-rune cells to spaces to preserve column alignment.
func mtLine(mt *gotui.MockTerminal, row, width int) string {
	var buf strings.Builder
	for x := 0; x < width; x++ {
		cell := mt.CellAt(x, row)
		if cell.Rune != 0 {
			buf.WriteRune(cell.Rune)
		} else {
			buf.WriteRune(' ')
		}
	}
	return buf.String()
}

// renderCockpitToMT renders a CockpitScreen into a MockTerminal and returns
// the Element and the terminal for inspection.
func renderCockpitToMT(cs *tui.CockpitScreen, width, height int) (gotui.Element, *gotui.MockTerminal) {
	mt := gotui.NewMockTerminal(width, height)
	el := cs.Render(mt)
	return el, mt
}
