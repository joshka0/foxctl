package testfixture_test

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/interfaces/tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/testfixture"
)

// ---------------------------------------------------------------------------
// Integration tests: CockpitScreen + BootManager + live daemon fixture
// ---------------------------------------------------------------------------

// TestBootManager_FetchesRealAgentsFromDaemon verifies that when the boot
// manager is configured with an AgentAdapter pointing at a live daemon with
// seeded agents, it fetches those agents and populates the CockpitScreen
// before transitioning to Ready.
func TestBootManager_FetchesRealAgentsFromDaemon(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon integration test in -short mode")
	}

	roles := []string{"researcher", "coder", "planner"}
	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{Roles: roles})

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

	if cs.Phase() != tui.CockpitPhaseReady {
		t.Fatalf("expected Ready, got %s", cs.Phase())
	}

	agents := cs.Agents()
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}

	// Verify deterministic sort: coder, planner, researcher (by role).
	expectedRoles := []string{"coder", "planner", "researcher"}
	for i, exp := range expectedRoles {
		if agents[i].Role != exp {
			t.Errorf("agent[%d].Role=%q, want %q", i, agents[i].Role, exp)
		}
	}
}

// TestBootManager_FetchesZeroAgentsFromDaemon verifies the empty-state path
// when the daemon has no agents.
func TestBootManager_FetchesZeroAgentsFromDaemon(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon integration test in -short mode")
	}

	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{})

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

	if cs.Phase() != tui.CockpitPhaseReady {
		t.Fatalf("expected Ready, got %s", cs.Phase())
	}

	agents := cs.Agents()
	if len(agents) != 0 {
		t.Fatalf("expected 0 agents, got %d", len(agents))
	}
}

// TestBootManager_RealAgentFields verifies that the fetched agents have the
// expected fields populated from the daemon.
func TestBootManager_RealAgentFields(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon integration test in -short mode")
	}

	roles := []string{"researcher"}
	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{Roles: roles})

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
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}

	a := agents[0]
	if a.ID == "" {
		t.Error("agent ID should not be empty")
	}
	if a.Role != "researcher" {
		t.Errorf("agent Role=%q, want researcher", a.Role)
	}
	// Workspace should be the default test workspace.
	if a.Workspace != "test-ws" {
		t.Errorf("agent Workspace=%q, want test-ws", a.Workspace)
	}
	// ParentID should default to em-dash for root agents.
	if a.ParentID != "—" {
		t.Errorf("agent ParentID=%q, want em-dash", a.ParentID)
	}
}
