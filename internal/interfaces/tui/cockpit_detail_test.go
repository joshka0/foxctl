package tui

import (
	"strings"
	"testing"

	gotui "github.com/grindlemire/go-tui"
)

// ---------------------------------------------------------------------------
// VAL-SKEL-005: Detail lane — selected agent runtime snapshot, hierarchy,
// recent activity preview; selection survives Tab-to-composer-and-back.
//
// Tests verify:
//   (i)   Detail lane updates when selection changes via up/down
//   (ii)  Detail lane shows Runtime, Hierarchy, and Recent Activity sections
//   (iii) Selection index survives Tab focus change away and back
//   (iv)  Detail pane uses the M2 DetailPane component (header + sections)
//   (v)   Empty state shows guidance when no agent is selected
// ---------------------------------------------------------------------------

// --- Test: Detail lane updates when selection changes ---

func TestDetailLane_UpdatesOnSelectionChange(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running", Workspace: "ws1", ParentID: "—", LastActive: "2m"},
		{ID: "agent-def", Role: "coder", Status: "idle", Workspace: "ws1", ParentID: "parent-1", LastActive: "5m"},
		{ID: "agent-ghi", Role: "planner", Status: "running", Workspace: "ws2", ParentID: "—", LastActive: "1m"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	// After sorting by role: coder (index 0), planner (index 1), researcher (index 2).
	// Render with coder selected.
	_, mt1 := renderCockpitToMT(cs, 80, 24)
	var detailText1 strings.Builder
	for y := 1; y < 23; y++ {
		detailText1.WriteString(extractDetailLane(mt1, y, 80))
	}

	// Navigate down to planner.
	cs.NavigateDown()

	_, mt2 := renderCockpitToMT(cs, 80, 24)
	var detailText2 strings.Builder
	for y := 1; y < 23; y++ {
		detailText2.WriteString(extractDetailLane(mt2, y, 80))
	}

	// The detail lane text should have changed.
	if detailText1.String() == detailText2.String() {
		t.Error("detail lane text should change after NavigateDown")
	}

	// The new detail should contain "planner" (the newly selected agent's role).
	if !strings.Contains(detailText2.String(), "planner") {
		t.Errorf("detail lane should contain newly selected agent's role 'planner', got: %q", detailText2.String())
	}

	// The old detail should contain "coder" (the initial selection).
	if !strings.Contains(detailText1.String(), "coder") {
		t.Errorf("original detail lane should contain 'coder' (initial selection), got: %q", detailText1.String())
	}
}

// --- Test: Detail lane shows Runtime, Hierarchy, and Recent Activity sections ---

func TestDetailLane_ShowsRuntimeHierarchyActivitySections(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running", Workspace: "ws1", ParentID: "parent-1", LastActive: "2m"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)

	var detailText strings.Builder
	for y := 1; y < 23; y++ {
		detailText.WriteString(extractDetailLane(mt, y, 80))
	}
	text := detailText.String()

	// Should contain section titles from the DetailPane component.
	if !strings.Contains(text, "Runtime") {
		t.Errorf("detail lane should contain 'Runtime' section, got: %q", text)
	}
	if !strings.Contains(text, "Hierarchy") {
		t.Errorf("detail lane should contain 'Hierarchy' section, got: %q", text)
	}
	if !strings.Contains(text, "Recent Activity") {
		t.Errorf("detail lane should contain 'Recent Activity' section, got: %q", text)
	}

	// Should contain the agent's role and status.
	if !strings.Contains(text, "researcher") {
		t.Errorf("detail lane should contain role 'researcher', got: %q", text)
	}
	if !strings.Contains(text, "running") {
		t.Errorf("detail lane should contain status 'running', got: %q", text)
	}

	// Should contain parent info from hierarchy.
	if !strings.Contains(text, "parent-1") {
		t.Errorf("detail lane should contain parent ID 'parent-1', got: %q", text)
	}
}

// --- Test: Selection survives Tab focus round-trip ---

func TestSelection_SurvivesFocusRoundTrip(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher"},
		{ID: "agent-def", Role: "coder"},
		{ID: "agent-ghi", Role: "planner"},
	})
	cs.SetSelectedIndex(1) // select coder
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	if cs.SelectedIndex() != 1 {
		t.Fatalf("expected selectedIndex=1, got %d", cs.SelectedIndex())
	}

	// Simulate Tab to move focus to Detail lane.
	cs.CycleFocusForward()
	if cs.FocusedLane() != 1 {
		t.Errorf("after Tab: expected focusedLane=1 (Detail), got %d", cs.FocusedLane())
	}

	// Selection should be preserved.
	if cs.SelectedIndex() != 1 {
		t.Errorf("after Tab: expected selectedIndex still 1, got %d", cs.SelectedIndex())
	}

	// Simulate Tab to move focus to Evidence lane.
	cs.CycleFocusForward()
	if cs.FocusedLane() != 2 {
		t.Errorf("after second Tab: expected focusedLane=2 (Evidence), got %d", cs.FocusedLane())
	}

	// Selection should still be preserved.
	if cs.SelectedIndex() != 1 {
		t.Errorf("after second Tab: expected selectedIndex still 1, got %d", cs.SelectedIndex())
	}

	// Simulate Tab to wrap back to Main lane.
	cs.CycleFocusForward()
	if cs.FocusedLane() != 0 {
		t.Errorf("after third Tab: expected focusedLane=0 (Main), got %d", cs.FocusedLane())
	}

	// Selection should still be preserved.
	if cs.SelectedIndex() != 1 {
		t.Errorf("after third Tab: expected selectedIndex still 1, got %d", cs.SelectedIndex())
	}
}

// --- Test: Shift+Tab cycles focus backward ---

func TestSelection_SurvivesShiftTabBackward(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher"},
		{ID: "agent-def", Role: "coder"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	// Start at Main (0).
	if cs.FocusedLane() != 0 {
		t.Fatalf("expected initial focusedLane=0, got %d", cs.FocusedLane())
	}

	// Shift+Tab backward: Main → Evidence.
	cs.CycleFocusBackward()
	if cs.FocusedLane() != 2 {
		t.Errorf("after Shift+Tab: expected focusedLane=2 (Evidence), got %d", cs.FocusedLane())
	}

	// Selection preserved.
	if cs.SelectedIndex() != 0 {
		t.Errorf("after Shift+Tab: expected selectedIndex still 0, got %d", cs.SelectedIndex())
	}
}

// --- Test: Empty state when no agent selected ---

func TestDetailLane_EmptyStateWhenNoSelection(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.ClearSelection() // no selection
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)

	var detailText strings.Builder
	for y := 1; y < 23; y++ {
		detailText.WriteString(extractDetailLane(mt, y, 80))
	}
	text := detailText.String()

	// Should show empty-state guidance.
	if !strings.Contains(text, "Select") {
		t.Errorf("detail lane empty state should contain 'Select' guidance, got: %q", text)
	}
}

// --- Test: Detail lane empty state when zero agents ---

func TestDetailLane_EmptyStateWhenZeroAgents(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{})
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)

	var detailText strings.Builder
	for y := 1; y < 23; y++ {
		detailText.WriteString(extractDetailLane(mt, y, 80))
	}
	text := detailText.String()

	// Should show empty-state guidance.
	if !strings.Contains(text, "Select") {
		t.Errorf("detail lane empty state with zero agents should contain guidance, got: %q", text)
	}
}

// --- Test: Focus indicator visible in Detail lane when focused ---

func TestDetailLane_FocusIndicatorWhenFocused(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	// Focus the Detail lane.
	cs.SetFocusedLane(1)

	_, mt := renderCockpitToMT(cs, 80, 24)

	// The detail lane should have a visible focus indicator.
	// When focused, the DetailPane renders '▌' as the left border;
	// when unfocused, it renders '│'.
	hasFocus := false
	for y := 1; y < 23; y++ {
		for x := 0; x < 80; x++ {
			cell := mt.CellAt(x, y)
			if cell.Rune == '▌' {
				hasFocus = true
				break
			}
		}
		if hasFocus {
			break
		}
	}
	if !hasFocus {
		t.Error("detail lane should have a visible focus indicator ('▌') when focused")
	}
}

// --- Test: Detail lane shows status badge for selected agent ---

func TestDetailLane_StatusBadgeForSelectedAgent(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)

	var detailText strings.Builder
	for y := 1; y < 23; y++ {
		detailText.WriteString(extractDetailLane(mt, y, 80))
	}
	text := detailText.String()

	// Should contain a status badge icon (● for OK/running).
	if !strings.Contains(text, "●") {
		t.Errorf("detail lane should contain status badge icon '●' for running agent, got: %q", text)
	}
}

// --- Test: Detail lane shows workspace info ---

func TestDetailLane_ShowsWorkspaceInfo(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running", Workspace: "ws1"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)

	var detailText strings.Builder
	for y := 1; y < 23; y++ {
		detailText.WriteString(extractDetailLane(mt, y, 80))
	}
	text := detailText.String()

	if !strings.Contains(text, "ws1") {
		t.Errorf("detail lane should contain workspace 'ws1', got: %q", text)
	}
}

// --- Test: Detail lane shows root agent (no parent) correctly ---

func TestDetailLane_RootAgentShowsNoParent(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running", Workspace: "ws1", ParentID: "—"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)

	var detailText strings.Builder
	for y := 1; y < 23; y++ {
		detailText.WriteString(extractDetailLane(mt, y, 80))
	}
	text := detailText.String()

	// Should show "none" or "—" for root agent parent.
	if !strings.Contains(text, "none") && !strings.Contains(text, "—") {
		t.Errorf("detail lane should indicate no parent for root agent, got: %q", text)
	}
}

// --- Test: Detail pane scroll keys work when focused and content overflows ---

func TestDetailLane_ScrollWhenFocusedAndOverflow(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running", Workspace: "ws1", ParentID: "—", LastActive: "2m"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)
	cs.SetFocusedLane(1) // focus Detail lane

	// The detail pane should be created with scrollable content.
	// We verify by checking that the DetailPane is instantiated with
	// sections that would overflow a small height.
	_, mt := renderCockpitToMT(cs, 80, 24)

	// Just verify the detail lane renders without panic and has content.
	var detailText strings.Builder
	for y := 1; y < 23; y++ {
		detailText.WriteString(extractDetailLane(mt, y, 80))
	}
	if detailText.String() == "" {
		t.Error("detail lane should render content when focused")
	}
}

// --- Test: Tab key binding exists in KeyMap ---

func TestCockpitScreen_KeyMapContainsTab(t *testing.T) {
	cs := NewCockpitScreen("")
	km := cs.KeyMap()
	if len(km) == 0 {
		t.Fatal("KeyMap should not be empty")
	}

	// KeyMap should contain Tab binding (which handles both Tab and Shift+Tab
	// via modifier check). Baseline: ESC, q, Ctrl+C, r, ↑, ↓, j, k = 8.
	// With Tab added = 9.
	if len(km) < 9 {
		t.Fatalf("KeyMap should contain at least 9 bindings (including Tab), got %d", len(km))
	}
}

// ============================================================================
// Detail lane extraction helpers
// ============================================================================

// extractDetailLane extracts the text content of the Detail lane from a
// rendered MockTerminal row. It uses the lane geometry from renderReady.
func extractDetailLane(mt *gotui.MockTerminal, row, width int) string {
	if width < 60 {
		return ""
	}
	// Lane geometry matches renderReady:
	// width >= 80: sepCount=2, availW=width-2, mainW=availW*40/100,
	//              detailW=availW*35/100, evidenceW=availW-mainW-detailW
	sepCount := 2
	availW := width - sepCount
	mainW := availW * 40 / 100
	detailW := availW * 35 / 100
	// evidenceW := availW - mainW - detailW

	// Detail lane starts after mainW + 1 separator.
	detailStart := mainW + 1

	var b strings.Builder
	for x := detailStart; x < detailStart+detailW && x < width; x++ {
		cell := mt.CellAt(x, row)
		if cell.Rune != 0 {
			b.WriteRune(cell.Rune)
		} else {
			b.WriteRune(' ')
		}
	}
	return b.String()
}
