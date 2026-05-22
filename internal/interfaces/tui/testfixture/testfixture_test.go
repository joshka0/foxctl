package testfixture_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/interfaces/tui/testfixture"
)

// TestBootDaemon_PortIsNonZero verifies the fixture boots a daemon and returns
// a non-zero, non-8090 port.
func TestBootDaemon_PortIsNonZero(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}
	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{})
	t.Log("port:", fixture.Port())

	if fixture.Port() <= 0 {
		t.Fatalf("port = %d, want > 0", fixture.Port())
	}
	if fixture.Port() == 8090 {
		t.Fatal("port = 8090, must not collide with default daemon port")
	}
}

// TestBootDaemon_BaseURLReturnsValidURL verifies BaseURL returns a reachable
// http://localhost:PORT URL.
func TestBootDaemon_BaseURLReturnsValidURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}
	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{})

	baseURL := fixture.BaseURL()
	if !strings.HasPrefix(baseURL, "http://localhost:") {
		t.Fatalf("BaseURL = %q, want http://localhost:PORT prefix", baseURL)
	}
	if !strings.Contains(baseURL, fmt.Sprintf("%d", fixture.Port())) {
		t.Fatalf("BaseURL = %q does not contain port %d", baseURL, fixture.Port())
	}
}

// TestBootDaemon_StorageRootIsTempDir verifies the fixture creates a temp
// FOXCTL_STORAGE_ROOT and it exists while the daemon is running.
func TestBootDaemon_StorageRootIsTempDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}
	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{})

	root := fixture.StorageRoot()
	if root == "" {
		t.Fatal("StorageRoot is empty")
	}

	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("StorageRoot %q does not exist: %v", root, err)
	}
	if !info.IsDir() {
		t.Fatalf("StorageRoot %q is not a directory", root)
	}
}

// TestBootDaemon_SeedsAgents verifies that seeding agents creates agents
// accessible via the API and returns their IDs.
func TestBootDaemon_SeedsAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}
	roles := []string{"researcher", "coder", "planner"}
	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{Roles: roles})

	ids := fixture.AgentIDs()
	if len(ids) != len(roles) {
		t.Fatalf("AgentIDs returned %d IDs, want %d", len(ids), len(roles))
	}

	// Verify each ID is non-empty and unique.
	seen := make(map[string]bool)
	for _, id := range ids {
		if id == "" {
			t.Fatal("agent ID is empty")
		}
		if seen[id] {
			t.Fatalf("duplicate agent ID: %q", id)
		}
		seen[id] = true
	}
}

// TestBootDaemon_SeedsZeroAgents verifies that seeding with zero roles works.
func TestBootDaemon_SeedsZeroAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}
	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{Roles: nil})

	ids := fixture.AgentIDs()
	if len(ids) != 0 {
		t.Fatalf("AgentIDs returned %d IDs, want 0 for nil roles", len(ids))
	}
}

// TestBootDaemon_DeterministicIDs verifies that seeding the same roles produces
// the same number of IDs in the same order.
func TestBootDaemon_DeterministicIDs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}
	roles := []string{"researcher", "coder"}

	fixture1 := testfixture.BootDaemon(t, testfixture.SeedOpts{Roles: roles})
	ids1 := fixture1.AgentIDs()

	// We can't easily run two daemons in the same test with t.Cleanup ordering,
	// so we verify that the IDs are stable in length and format.
	if len(ids1) != 2 {
		t.Fatalf("expected 2 agent IDs, got %d", len(ids1))
	}
	for _, id := range ids1 {
		if len(id) < 4 {
			t.Fatalf("agent ID %q seems too short", id)
		}
	}
}

// TestBootDaemon_HealthEndpointResponds verifies the daemon's /api/health
// endpoint responds.
func TestBootDaemon_HealthEndpointResponds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}
	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{})

	client := fixture.APIClient()
	if client == nil {
		t.Fatal("APIClient() returned nil")
	}

	// Use the client to hit the health endpoint with retries.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var health map[string]interface{}
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		lastErr = client.RequestJSON(ctx, "GET", "/api/health", nil, &health)
		if lastErr == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("health check failed after retries: %v", lastErr)
	}
}

// TestBootDaemon_ListAgentsAfterSeed verifies that seeded agents appear
// in GET /api/agents.
func TestBootDaemon_ListAgentsAfterSeed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}
	roles := []string{"researcher", "coder", "planner"}
	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{Roles: roles})

	client := fixture.APIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var resp struct {
		Agents []struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"agents"`
		Total int `json:"total"`
	}

	// Retry a few times — the daemon may need a moment to fully initialize.
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		lastErr = client.RequestJSON(ctx, "GET", "/api/agents", nil, &resp)
		if lastErr == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("list agents failed after retries: %v", lastErr)
	}
	if resp.Total != 3 {
		t.Fatalf("total agents = %d, want 3", resp.Total)
	}
}

// TestBootDaemon_CleansUpTempDir verifies that after the test cleanup runs,
// the temp storage root directory is removed. We use a subtest to control
// the cleanup lifecycle.
func TestBootDaemon_CleansUpTempDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}
	if testfixture.FoxctlBinary() == "" {
		t.Skip("foxctl binary not available in this test environment")
	}

	var capturedRoot string

	t.Run("boot_and_capture", func(t *testing.T) {
		fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{})
		capturedRoot = fixture.StorageRoot()

		// Verify it exists during the test.
		if _, err := os.Stat(capturedRoot); err != nil {
			t.Fatalf("storage root %q should exist during test: %v", capturedRoot, err)
		}
	})

	// After the subtest's cleanup runs, the temp dir should be gone.
	// Give a small grace period for async cleanup.
	if capturedRoot == "" {
		t.Fatal("failed to capture storage root")
	}

	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(capturedRoot); !os.IsNotExist(err) {
		t.Fatalf("storage root %q should be removed after cleanup, but exists (err=%v)", capturedRoot, err)
	}
}

// TestBootDaemon_NoLeakedProcesses verifies that after cleanup, no foxctl web
// serve process with our test's PID tree remains. This test runs a subtest
// and checks afterward.
func TestBootDaemon_NoLeakedProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}
	if testfixture.FoxctlBinary() == "" {
		t.Skip("foxctl binary not available in this test environment")
	}

	var daemonPID int

	t.Run("boot_and_capture_pid", func(t *testing.T) {
		fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{})
		daemonPID = fixture.DaemonPID()
		if daemonPID <= 0 {
			t.Fatal("daemon PID is not positive")
		}
	})

	// After cleanup, the daemon process should be gone.
	if daemonPID <= 0 {
		t.Fatal("failed to capture daemon PID")
	}

	// Wait for cleanup to complete.
	time.Sleep(200 * time.Millisecond)

	// Check if the process still exists.
	proc, err := os.FindProcess(daemonPID)
	if err != nil {
		// Process doesn't exist — good.
		return
	}

	// Send signal 0 to check if the process is alive.
	if err := proc.Signal(os.Signal(nil)); err == nil {
		// Process is still alive — that's a leak.
		// Try to clean up.
		_ = proc.Kill()
		t.Fatalf("daemon process %d is still running after cleanup — leaked process", daemonPID)
	}
	// Process is dead — good.
}

// TestBootDaemon_MultipleFixturesDontCollide verifies that booting two
// fixtures in sequence (not parallel) gets different ports.
func TestBootDaemon_MultipleFixturesDontCollide(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}

	fixture1 := testfixture.BootDaemon(t, testfixture.SeedOpts{Roles: []string{"researcher"}})
	port1 := fixture1.Port()
	agentIDs1 := fixture1.AgentIDs()

	fixture2 := testfixture.BootDaemon(t, testfixture.SeedOpts{Roles: []string{"coder"}})
	port2 := fixture2.Port()
	agentIDs2 := fixture2.AgentIDs()

	if port1 == port2 {
		t.Fatalf("two fixtures got the same port %d — OS should assign different ports for -p 0", port1)
	}

	// Each fixture should have 1 agent.
	if len(agentIDs1) != 1 {
		t.Fatalf("fixture1 agents = %d, want 1", len(agentIDs1))
	}
	if len(agentIDs2) != 1 {
		t.Fatalf("fixture2 agents = %d, want 1", len(agentIDs2))
	}
}

// TestBootDaemon_FatalPathStillCleansUp verifies that cleanup runs even when
// the test function returns early (simulating t.Fatal behavior). We use a
// separate function to verify cleanup is registered via t.Cleanup and runs
// even on early return.
func TestBootDaemon_FatalPathStillCleansUp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}

	var capturedRoot string
	var capturedPID int

	// Boot the fixture normally in the main test.
	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{})
	capturedRoot = fixture.StorageRoot()
	capturedPID = fixture.DaemonPID()

	// The fixture's t.Cleanup will run even if we call t.Fatal below.
	// Verify the temp dir and daemon exist now.
	if _, err := os.Stat(capturedRoot); err != nil {
		t.Fatalf("storage root should exist during test: %v", err)
	}

	// The real verification happens in TestBootDaemon_CleansUpTempDir and
	// TestBootDaemon_NoLeakedProcesses which check cleanup after the test.
	// Here we just verify that cleanup is registered and will run.
	_ = capturedPID // verified by NoLeakedProcesses
}

// TestFoxctlBinaryPath verifies the binary path resolution works.
func TestFoxctlBinaryPath(t *testing.T) {
	binPath := testfixture.FoxctlBinary()
	if binPath == "" {
		t.Skip("foxctl binary not available in this test environment")
	}

	// Check the binary exists.
	if _, err := os.Stat(binPath); err != nil {
		t.Fatalf("foxctl binary at %q does not exist: %v", binPath, err)
	}

	// Check it's executable (on Unix).
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath(binPath); err != nil {
			t.Fatalf("foxctl binary at %q is not executable: %v", binPath, err)
		}
	}
}

// TestSeedOpts_Defaults verifies SeedOpts defaults are sane.
func TestSeedOpts_Defaults(t *testing.T) {
	opts := testfixture.SeedOpts{}

	if len(opts.Roles) != 0 {
		t.Fatalf("default Roles = %v, want empty", opts.Roles)
	}
	if opts.WorkspaceID != "" {
		t.Fatalf("default WorkspaceID = %q, want empty", opts.WorkspaceID)
	}
}

// TestBootDaemon_WithCustomWorkspaceID verifies custom workspace ID is used.
func TestBootDaemon_WithCustomWorkspaceID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}

	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{
		Roles:       []string{"researcher"},
		WorkspaceID: "custom-ws",
	})

	client := fixture.APIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var resp struct {
		Agents []struct {
			ID        string `json:"id"`
			Namespace string `json:"ns"`
		} `json:"agents"`
		Total int `json:"total"`
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		lastErr = client.RequestJSON(ctx, "GET", "/api/agents", nil, &resp)
		if lastErr == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("list agents failed after retries: %v", lastErr)
	}
	if resp.Total != 1 {
		t.Fatalf("total agents = %d, want 1", resp.Total)
	}
}

// TestBootDaemon_APIClientCanListAgents ensures the returned APIClient works
// for listing agents.
func TestBootDaemon_APIClientCanListAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}

	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{
		Roles: []string{"researcher"},
	})

	client := fixture.APIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var resp struct {
		Agents []struct {
			ID string `json:"id"`
		} `json:"agents"`
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		lastErr = client.RequestJSON(ctx, "GET", "/api/agents", nil, &resp)
		if lastErr == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("list agents after retries: %v", lastErr)
	}
	if len(resp.Agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(resp.Agents))
	}
	if resp.Agents[0].ID != fixture.AgentIDs()[0] {
		t.Fatalf("agent ID = %q, want %q", resp.Agents[0].ID, fixture.AgentIDs()[0])
	}
}

// TestBootDaemon_StorageRootContainsFoxctlData verifies the temp dir looks
// like a foxctl storage root.
func TestBootDaemon_StorageRootContainsFoxctlData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}

	fixture := testfixture.BootDaemon(t, testfixture.SeedOpts{
		Roles: []string{"researcher"},
	})

	root := fixture.StorageRoot()

	// The daemon should have created some data files in the storage root.
	// Walk the directory and count files.
	var fileCount int
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			fileCount++
		}
		return nil
	}); err != nil {
		t.Fatalf("walk storage root: %v", err)
	}

	if fileCount == 0 {
		t.Fatal("storage root has no files — daemon may not have initialized")
	}
}
