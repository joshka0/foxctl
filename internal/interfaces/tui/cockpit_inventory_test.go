package tui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// VAL-SKEL-004: Agent inventory — live data from GET /api/agents
// VAL-SKEL-010: Empty state with CTA when zero agents
//
// Tests verify:
//   (i)   After boot → Ready, CockpitScreen holds AgentInventoryItem rows
//   (ii)  Each row exposes six fields: short ID, role, status, workspace,
//         parent link, last-activity time
//   (iii) Deterministic sort order (role ascending, then ID ascending)
//   (iv)  Zero agents shows empty state with CTA:
//         "No agents running." + "Spawn: foxctl agent spawn --role ..."
//   (v)   Existing selection preserved when agents are refreshed
// ---------------------------------------------------------------------------

// --- Test: BootManager fetches agents and transitions to Ready ---

func TestBootManager_FetchesAgentsAndTransitionsToReady(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/api/agents":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"agents":[{"id":"agent-abc","role":"researcher","state":"running","ns":"ws1"}],"total":1}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cs := NewCockpitScreen(server.URL)
	cs.UpdateSize(80, 24)

	apiClient, err := NewAPIClient(server.URL, nil)
	if err != nil {
		t.Fatalf("create api client: %v", err)
	}
	agentAdapter, err := NewAgentAdapter(apiClient)
	if err != nil {
		t.Fatalf("create agent adapter: %v", err)
	}

	bm := NewBootManager(BootConfig{
		APIURL:       server.URL,
		Screen:       cs,
		Timeout:      2 * time.Second,
		AgentAdapter: agentAdapter,
	})

	bm.Start()
	defer bm.Stop()
	bm.WaitForDone()

	if cs.Phase() != CockpitPhaseReady {
		t.Fatalf("expected Ready, got %s", cs.Phase())
	}

	agents := cs.Agents()
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].ID != "agent-abc" {
		t.Errorf("expected ID=agent-abc, got %q", agents[0].ID)
	}
	if agents[0].Role != "researcher" {
		t.Errorf("expected Role=researcher, got %q", agents[0].Role)
	}
}

// --- Test: BootManager transitions to Ready even when agent fetch fails ---

func TestBootManager_TransitionsToReadyOnAgentFetchFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/api/agents":
			// Simulate API failure.
			http.Error(w, "internal error", http.StatusInternalServerError)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cs := NewCockpitScreen(server.URL)
	cs.UpdateSize(80, 24)

	apiClient, _ := NewAPIClient(server.URL, nil)
	agentAdapter, _ := NewAgentAdapter(apiClient)

	bm := NewBootManager(BootConfig{
		APIURL:       server.URL,
		Screen:       cs,
		Timeout:      2 * time.Second,
		AgentAdapter: agentAdapter,
	})

	bm.Start()
	defer bm.Stop()
	bm.WaitForDone()

	if cs.Phase() != CockpitPhaseReady {
		t.Fatalf("expected Ready even when agent fetch fails, got %s", cs.Phase())
	}
}

// --- Test: AgentInventoryItem six fields are rendered ---

func TestCockpitScreen_InventoryRendersSixFields(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc123", Role: "researcher", Status: "running", Workspace: "ws1", ParentID: "parent-1", LastActive: "2m"},
		{ID: "agent-def456", Role: "coder", Status: "idle", Workspace: "ws1", ParentID: "", LastActive: "5m"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)

	// After sorting by role: coder comes before researcher.
	// Row 1 should contain the first agent's short ID and role (coder).
	row1 := rowTextFromMT(mt, 1, 80)
	if !containsSubstring(row1, "agent-de") {
		t.Errorf("row 1 should contain short ID 'agent-de', got: %q", row1)
	}
	if !containsSubstring(row1, "coder") {
		t.Errorf("row 1 should contain role 'coder', got: %q", row1)
	}

	// Row 2 should contain researcher (may be truncated due to width).
	row2 := rowTextFromMT(mt, 2, 80)
	if !containsSubstring(row2, "agent-ab") {
		t.Errorf("row 2 should contain short ID prefix 'agent-ab', got: %q", row2)
	}
	if !containsSubstring(row2, "researcher") {
		t.Errorf("row 2 should contain role 'researcher', got: %q", row2)
	}
}

// --- Test: Deterministic sort order (role then ID) ---

func TestCockpitScreen_AgentsSortedByRoleThenID(t *testing.T) {
	cs := NewCockpitScreen("")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "z-agent", Role: "planner"},
		{ID: "a-agent", Role: "researcher"},
		{ID: "m-agent", Role: "coder"},
		{ID: "b-agent", Role: "coder"},
	})

	agents := cs.Agents()
	if len(agents) != 4 {
		t.Fatalf("expected 4 agents, got %d", len(agents))
	}

	// Expected order: coder (b-agent, m-agent), planner (z-agent), researcher (a-agent)
	expected := []struct{ Role, ID string }{
		{"coder", "b-agent"},
		{"coder", "m-agent"},
		{"planner", "z-agent"},
		{"researcher", "a-agent"},
	}
	for i, exp := range expected {
		if agents[i].Role != exp.Role {
			t.Errorf("agent[%d].Role=%q, want %q", i, agents[i].Role, exp.Role)
		}
		if agents[i].ID != exp.ID {
			t.Errorf("agent[%d].ID=%q, want %q", i, agents[i].ID, exp.ID)
		}
	}
}

// --- Test: Empty state shows CTA ---

func TestCockpitScreen_EmptyStateShowsCTA(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{})
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)

	// Find the empty-state message in the body rows.
	foundEmpty := false
	foundCTA := false
	for y := 1; y < 23; y++ {
		row := rowTextFromMT(mt, y, 80)
		if containsSubstring(row, "No agents running") {
			foundEmpty = true
		}
		if containsSubstring(row, "foxctl agent spawn") {
			foundCTA = true
		}
	}
	if !foundEmpty {
		t.Error("empty state should contain 'No agents running'")
	}
	if !foundCTA {
		t.Error("empty state should contain CTA with 'foxctl agent spawn'")
	}
}

// --- Test: Empty state at minimum width ---

func TestCockpitScreen_EmptyStateAtMinimumWidth(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{})
	cs.UpdateSize(60, 15)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 60, 15)

	foundEmpty := false
	for y := 1; y < 14; y++ {
		row := rowTextFromMT(mt, y, 60)
		if containsSubstring(row, "No agents running") {
			foundEmpty = true
			break
		}
	}
	if !foundEmpty {
		t.Error("empty state should render at minimum width")
	}
}

// --- Test: Selection preserved across agent refresh ---

func TestCockpitScreen_SelectionPreservedOnRefresh(t *testing.T) {
	cs := NewCockpitScreen("")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-1", Role: "researcher"},
		{ID: "agent-2", Role: "coder"},
		{ID: "agent-3", Role: "planner"},
	})
	cs.SetSelectedIndex(1) // select coder

	if cs.SelectedIndex() != 1 {
		t.Fatalf("expected selectedIndex=1, got %d", cs.SelectedIndex())
	}

	// Refresh with same agents — selection should stay.
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-1", Role: "researcher"},
		{ID: "agent-2", Role: "coder"},
		{ID: "agent-3", Role: "planner"},
	})
	if cs.SelectedIndex() != 1 {
		t.Fatalf("after refresh: expected selectedIndex=1, got %d", cs.SelectedIndex())
	}
}

// --- Test: Selection clamped when agents shrink ---

func TestCockpitScreen_SelectionClampedWhenAgentsShrink(t *testing.T) {
	cs := NewCockpitScreen("")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-1", Role: "researcher"},
		{ID: "agent-2", Role: "coder"},
		{ID: "agent-3", Role: "planner"},
	})
	cs.SetSelectedIndex(2)

	// Shrink to 1 agent — selection should clamp to 0.
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-1", Role: "researcher"},
	})
	if cs.SelectedIndex() != 0 {
		t.Fatalf("expected selectedIndex=0 after shrink, got %d", cs.SelectedIndex())
	}
}

// --- Test: AgentInventoryItem parent defaults to em-dash ---

func TestAgentInventoryItem_ParentDefaultsToEmDash(t *testing.T) {
	cs := NewCockpitScreen("")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-1", Role: "researcher", ParentID: ""},
	})
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)
	row1 := rowTextFromMT(mt, 1, 80)
	// The em-dash (—) should appear as the parent indicator.
	if !strings.Contains(row1, "—") {
		t.Errorf("row 1 should contain em-dash '—' for root agent, got: %q", row1)
	}
}

// --- Test: AgentInventoryItem workspace defaults to em-dash ---

func TestAgentInventoryItem_WorkspaceDefaultsToEmDash(t *testing.T) {
	cs := NewCockpitScreen("")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-1", Role: "researcher", Workspace: ""},
	})
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)
	row1 := rowTextFromMT(mt, 1, 80)
	if !strings.Contains(row1, "—") {
		t.Errorf("row 1 should contain em-dash '—' for empty workspace, got: %q", row1)
	}
}

// --- Test: Three agents render in sorted order ---

func TestCockpitScreen_ThreeAgentsRenderSorted(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-z", Role: "planner", Status: "running"},
		{ID: "agent-a", Role: "researcher", Status: "running"},
		{ID: "agent-m", Role: "coder", Status: "idle"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)

	// After sorting: coder (agent-m), planner (agent-z), researcher (agent-a)
	row1 := rowTextFromMT(mt, 1, 80)
	if !containsSubstring(row1, "coder") {
		t.Errorf("row 1 should contain 'coder' (first in sort), got: %q", row1)
	}
	row2 := rowTextFromMT(mt, 2, 80)
	if !containsSubstring(row2, "planner") {
		t.Errorf("row 2 should contain 'planner' (second in sort), got: %q", row2)
	}
	row3 := rowTextFromMT(mt, 3, 80)
	if !containsSubstring(row3, "researcher") {
		t.Errorf("row 3 should contain 'researcher' (third in sort), got: %q", row3)
	}
}

// --- Test: Selection indicator visible on selected row ---

func TestCockpitScreen_SelectionIndicatorVisible(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-1", Role: "researcher"},
		{ID: "agent-2", Role: "coder"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)

	// The selected row should have the "▸" indicator.
	row1 := rowTextFromMT(mt, 1, 80)
	if !containsSubstring(row1, "▸") {
		t.Errorf("selected row should contain '▸' indicator, got: %q", row1)
	}
}

// --- Test: Detail lane shows selected agent info ---

func TestCockpitScreen_DetailLaneShowsSelectedAgent(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-1", Role: "researcher", Status: "running"},
		{ID: "agent-2", Role: "coder", Status: "idle"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)

	// After sorting by role, index 0 is "coder" (comes before "researcher").
	// Find a row that contains detail info for the selected agent.
	foundDetail := false
	for y := 1; y < 23; y++ {
		row := rowTextFromMT(mt, y, 80)
		if containsSubstring(row, "Role:") && containsSubstring(row, "coder") {
			foundDetail = true
			break
		}
	}
	if !foundDetail {
		t.Error("detail lane should show selected agent's role")
	}
}

// --- Test: agentRecordToInventoryItem mapping ---

func TestAgentRecordToInventoryItem(t *testing.T) {
	rec := AgentRecord{
		ID:        "abc-123",
		Role:      "researcher",
		State:     "running",
		Namespace: "ws1",
		ParentID:  "parent-1",
	}
	item := agentRecordToInventoryItem(rec)
	if item.ID != "abc-123" {
		t.Errorf("ID=%q, want abc-123", item.ID)
	}
	if item.Role != "researcher" {
		t.Errorf("Role=%q, want researcher", item.Role)
	}
	if item.Status != "running" {
		t.Errorf("Status=%q, want running", item.Status)
	}
	if item.Workspace != "ws1" {
		t.Errorf("Workspace=%q, want ws1", item.Workspace)
	}
	if item.ParentID != "parent-1" {
		t.Errorf("ParentID=%q, want parent-1", item.ParentID)
	}
	if item.LastActive != "—" {
		t.Errorf("LastActive=%q, want —", item.LastActive)
	}
}

// --- Test: agentRecordToInventoryItem with empty parent ---

func TestAgentRecordToInventoryItem_EmptyParent(t *testing.T) {
	rec := AgentRecord{
		ID:       "abc-123",
		Role:     "researcher",
		State:    "running",
		ParentID: "",
	}
	item := agentRecordToInventoryItem(rec)
	if item.ParentID != "—" {
		t.Errorf("ParentID=%q, want em-dash for empty parent", item.ParentID)
	}
}

// --- Test: sortAgents determinism ---

func TestSortAgents_Deterministic(t *testing.T) {
	input := []AgentInventoryItem{
		{ID: "z", Role: "coder"},
		{ID: "a", Role: "coder"},
		{ID: "m", Role: "planner"},
	}
	sorted := sortAgents(input)
	// Expected order: coder (a, z), planner (m)
	expected := []string{"a", "z", "m"}
	for i, exp := range expected {
		if sorted[i].ID != exp {
			t.Errorf("sorted[%d].ID=%q, want %q", i, sorted[i].ID, exp)
		}
	}
}

// --- Test: sortAgents is stable ---

func TestSortAgents_Stable(t *testing.T) {
	input := []AgentInventoryItem{
		{ID: "a", Role: "coder", Status: "idle"},
		{ID: "a", Role: "coder", Status: "running"},
	}
	sorted := sortAgents(input)
	if sorted[0].Status != "idle" || sorted[1].Status != "running" {
		t.Error("sortAgents should be stable for equal keys")
	}
}

// --- Test: buildAgentInventoryLabel truncation ---

func TestBuildAgentInventoryLabel_Truncation(t *testing.T) {
	a := AgentInventoryItem{
		ID:         "very-long-agent-id",
		Role:       "researcher",
		Status:     "running",
		Workspace:  "workspace-name",
		ParentID:   "parent-id",
		LastActive: "10 minutes ago",
	}
	label := buildAgentInventoryLabel(a, 20)
	if runeWidth(label) > 20 {
		t.Errorf("label width=%d, should be <=20", runeWidth(label))
	}
}

// --- Test: MockTerminal render with three agents and selection ---

func TestCockpitScreen_InventoryThreeAgentsMockTerminal(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-001", Role: "coder", Status: "running", Workspace: "ws1"},
		{ID: "agent-002", Role: "planner", Status: "idle", Workspace: "ws1"},
		{ID: "agent-003", Role: "researcher", Status: "running", Workspace: "ws2"},
	})
	cs.SetSelectedIndex(1)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)

	// Verify lane headers.
	assertLaneHeaders(t, mt, 80)

	// Verify all three roles appear in the body.
	bodyText := ""
	for y := 1; y < 23; y++ {
		bodyText += rowTextFromMT(mt, y, 80)
	}
	for _, role := range []string{"coder", "planner", "researcher"} {
		if !containsSubstring(bodyText, role) {
			t.Errorf("body should contain role %q", role)
		}
	}
}
