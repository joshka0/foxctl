// Package testfixture provides a per-test isolated daemon fixture for TUI
// integration and end-to-end tests.
//
// The fixture boots `foxctl web serve -p 0` with a temp FOXCTL_STORAGE_ROOT,
// discovers the OS-chosen port via lsof, optionally seeds N agents
// deterministically, and tears down on t.Cleanup. On t.Fatal the fixture
// still tears down. No leaked processes; no leaked temp directories.
//
// This fixture is a precondition for every M3 walking-skeleton feature.
package testfixture

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	agentdomain "github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/interfaces/tui"
	"github.com/joshka0/foxctl/internal/storage/agents"
)

// safeBuffer is a strings.Builder protected by a mutex for concurrent access
// from the daemon's stderr goroutine and the test's main goroutine.
type safeBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (sb *safeBuffer) Write(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

func (sb *safeBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.String()
}

// SeedOpts configures what the fixture seeds after the daemon boots.
type SeedOpts struct {
	// Roles lists agent roles to create. Each role creates one agent in the
	// daemon's storage. The returned DaemonFixture.AgentIDs are in the same
	// order as Roles. Empty or nil means no agents are seeded.
	Roles []string

	// WorkspaceID is the workspace ID (namespace) used when seeding agents.
	// Defaults to "test-ws" if empty.
	WorkspaceID string
}

// DaemonFixture holds the running daemon's state and provides accessors for
// tests to interact with it.
type DaemonFixture struct {
	port        int
	baseURL     string
	storageRoot string
	daemonCmd   *exec.Cmd
	daemonPID   int
	agentIDs    []string
	apiClient   *tui.APIClient

	cleanupOnce sync.Once
	cleanupFn   func()
}

// Port returns the OS-chosen port the daemon is listening on.
func (f *DaemonFixture) Port() int { return f.port }

// BaseURL returns the base URL for the daemon (e.g., "http://localhost:12345").
func (f *DaemonFixture) BaseURL() string { return f.baseURL }

// StorageRoot returns the temp FOXCTL_STORAGE_ROOT directory path.
func (f *DaemonFixture) StorageRoot() string { return f.storageRoot }

// DaemonPID returns the PID of the daemon process.
func (f *DaemonFixture) DaemonPID() int { return f.daemonPID }

// AgentIDs returns the IDs of the seeded agents, in the same order as
// SeedOpts.Roles.
func (f *DaemonFixture) AgentIDs() []string {
	if f == nil {
		return nil
	}
	result := make([]string, len(f.agentIDs))
	copy(result, f.agentIDs)
	return result
}

// APIClient returns a configured API client pointing at the daemon.
func (f *DaemonFixture) APIClient() *tui.APIClient { return f.apiClient }

// BootDaemon starts an isolated `foxctl web serve -p 0` daemon with a temp
// FOXCTL_STORAGE_ROOT, waits for it to become healthy, optionally seeds
// agents, and registers t.Cleanup for teardown.
//
// On t.Fatal, cleanup still runs (Go's testing.T.Cleanup is called even
// after t.Fatal in subtests).
//
// Callers should check t.Fatal/Fatalf before using the fixture — BootDaemon
// calls t.Fatalf on unrecoverable errors.
func BootDaemon(t testing.TB, opts SeedOpts) *DaemonFixture {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping daemon boot test in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Find the foxctl binary.
	binPath := FoxctlBinary()
	if _, err := os.Stat(binPath); err != nil {
		t.Fatalf("foxctl binary not found at %q: %v", binPath, err)
	}

	// 2. Create a temp storage root.
	storageRoot, err := os.MkdirTemp("", "foxctl-testfixture-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	// 3. Boot the daemon with -p 0. Use exec.Command (not CommandContext) so the
	//    daemon doesn't get killed when our parent context expires. We manage the
	//    daemon lifecycle ourselves via killDaemon in cleanup.
	cmd := exec.Command(binPath, "web", "serve", "-p", "0")
	cmd.Env = append(os.Environ(),
		"FOXCTL_STORAGE_ROOT="+storageRoot,
		"FOXCTL_LOG_LEVEL=warn",
		"FOXCTL_LOGGING_OUTPUT=stderr",
	)

	// Capture stderr for error diagnostics. The daemon's port is discovered
	// via lsof (not stderr parsing), but we need stderr for error reporting.
	// Use a mutex-protected buffer to avoid data races between the stderr
	// writer goroutine and our error path reads.
	stderrBuf := &safeBuffer{}
	cmd.Stderr = stderrBuf
	cmd.Stdout = nil

	if err := cmd.Start(); err != nil {
		os.RemoveAll(storageRoot)
		t.Fatalf("start daemon: %v", err)
	}

	daemonPID := cmd.Process.Pid

	// 4. Discover the port via lsof. The daemon logs to a file (not stderr),
	//    so we use lsof to find what TCP port the process is listening on.
	//    We retry for a few seconds until the daemon binds.
	port, err := discoverListeningPort(ctx, daemonPID, 10*time.Second)
	if err != nil {
		stderrContent := stderrBuf.String()
		killDaemon(cmd, daemonPID)
		os.RemoveAll(storageRoot)
		detail := err.Error()
		if stderrContent != "" {
			detail += "\nDaemon stderr:\n" + stderrContent
		}
		t.Fatalf("discover daemon port: %s", detail)
	}

	if port <= 0 || port == 8090 {
		killDaemon(cmd, daemonPID)
		os.RemoveAll(storageRoot)
		t.Fatalf("discovered invalid port %d from daemon", port)
	}

	baseURL := fmt.Sprintf("http://localhost:%d", port)

	// 5. Wait for the health endpoint to respond.
	if healthErr := waitForHealth(ctx, baseURL, 15*time.Second); healthErr != nil {
		killDaemon(cmd, daemonPID)
		os.RemoveAll(storageRoot)
		t.Fatalf("daemon health check failed: %v", healthErr)
	}

	// Small grace period for the daemon to finish initializing HTTP handlers
	// after binding the port. Without this, early API calls can get connection
	// reset errors under race detector.
	time.Sleep(100 * time.Millisecond)

	// 6. Create the API client.
	apiClient, clientErr := tui.NewAPIClient(baseURL, &http.Client{Timeout: 10 * time.Second})
	if clientErr != nil {
		killDaemon(cmd, daemonPID)
		os.RemoveAll(storageRoot)
		t.Fatalf("create API client: %v", clientErr)
	}

	fixture := &DaemonFixture{
		port:        port,
		baseURL:     baseURL,
		storageRoot: storageRoot,
		daemonCmd:   cmd,
		daemonPID:   daemonPID,
		apiClient:   apiClient,
	}

	// 7. Seed agents if requested.
	if len(opts.Roles) > 0 {
		workspaceID := opts.WorkspaceID
		if workspaceID == "" {
			workspaceID = "test-ws"
		}
		agentIDs, seedErr := seedAgents(ctx, storageRoot, opts.Roles, workspaceID)
		if seedErr != nil {
			killDaemon(cmd, daemonPID)
			os.RemoveAll(storageRoot)
			t.Fatalf("seed agents: %v", seedErr)
		}
		fixture.agentIDs = agentIDs
	}

	// 8. Register cleanup.
	fixture.cleanupFn = func() {
		killDaemon(cmd, daemonPID)
		os.RemoveAll(storageRoot)
	}
	t.Cleanup(func() {
		fixture.cleanupOnce.Do(func() {
			fixture.cleanupFn()
		})
	})

	return fixture
}

// FoxctlBinary returns the path to the foxctl binary used by the fixture.
// It searches in this order:
//  1. FOXCTL_TEST_BINARY env var (if set)
//  2. bin/foxctl relative to the repo root
//  3. The foxctl found in PATH
func FoxctlBinary() string {
	if envBin := os.Getenv("FOXCTL_TEST_BINARY"); envBin != "" {
		return envBin
	}

	// Walk up from the current file to find the repo root (identified by go.mod).
	_, thisFile, _, _ := runtime.Caller(1)
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			binPath := filepath.Join(dir, "bin", "foxctl")
			if _, err := os.Stat(binPath); err == nil {
				return binPath
			}
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Fall back to PATH.
	if p, err := exec.LookPath("foxctl"); err == nil {
		return p
	}

	return ""
}

// killDaemon forcefully terminates the daemon process tree.
func killDaemon(cmd *exec.Cmd, pid int) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	// Try graceful shutdown first via SIGINT (same as the daemon expects).
	if runtime.GOOS != "windows" {
		_ = cmd.Process.Signal(os.Interrupt)
	} else {
		_ = cmd.Process.Kill()
	}

	// Wait for the process to exit with a short timeout.
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited cleanly.
	case <-time.After(3 * time.Second):
		// Force kill.
		_ = cmd.Process.Kill()
		<-done
	}
}

// waitForHealth polls the daemon's /healthz endpoint until it responds
// with 200 or the context expires.
func waitForHealth(ctx context.Context, baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.Get(baseURL + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		// Small backoff.
		time.Sleep(50 * time.Millisecond)
	}

	return fmt.Errorf("health endpoint %s/healthz did not respond within %v", baseURL, timeout)
}

// portRegex matches lsof LISTEN lines like:
//
//	foxctl  12345 user  7u  IPv4 0x...  0t0  TCP *:65179 (LISTEN)
//	foxctl  12345 user  7u  IPv6 0x...  0t0  TCP *:65179 (LISTEN)
//	foxctl  12345 user  7u  IPv4 0x...  0t0  TCP 127.0.0.1:65179 (LISTEN)
var portRegex = regexp.MustCompile(`TCP\s+(?:\*|127\.0\.0\.1|\[::1\]):(\d+)\s+\(LISTEN\)`)

// discoverListeningPort uses lsof to find the TCP port that the given PID is
// listening on. It retries until the port is found or the timeout expires.
func discoverListeningPort(ctx context.Context, pid int, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		port, err := lsofPort(pid)
		if err == nil && port > 0 {
			return port, nil
		}

		// Check if the process is still alive.
		proc, err := os.FindProcess(pid)
		if err != nil {
			return 0, fmt.Errorf("process %d not found: %w", pid, err)
		}
		if err := proc.Signal(os.Signal(nil)); err != nil {
			return 0, fmt.Errorf("process %d exited before port was discovered", pid)
		}

		time.Sleep(100 * time.Millisecond)
	}

	return 0, fmt.Errorf("could not discover listening port for PID %d within %v", pid, timeout)
}

// lsofPort runs lsof to find the TCP listening port for a given PID.
// On macOS the -a flag does not AND selectors as expected, so we run
// lsof -p <pid> and grep for LISTEN lines manually.
func lsofPort(pid int) (int, error) {
	cmd := exec.Command("lsof", "-p", strconv.Itoa(pid), "-P", "-n")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("lsof: %w", err)
	}

	matches := portRegex.FindAllStringSubmatch(string(output), -1)
	for _, match := range matches {
		if len(match) >= 2 {
			port, err := strconv.Atoi(match[1])
			if err == nil && port > 0 {
				return port, nil
			}
		}
	}

	return 0, fmt.Errorf("no LISTEN port found in lsof output for PID %d", pid)
}

// seedAgents creates agents directly in the agent store and returns their IDs.
// This approach avoids the spawn endpoint's daemon-runtime dependency and
// creates lightweight agent records that the GET /api/agents endpoint serves.
func seedAgents(ctx context.Context, storageRoot string, roles []string, workspaceID string) ([]string, error) {
	store, err := agents.Open(ctx, storageRoot)
	if err != nil {
		return nil, fmt.Errorf("open agents store: %w", err)
	}
	defer store.Close()

	ids := make([]string, 0, len(roles))
	now := time.Now().UTC()

	for i, role := range roles {
		agentID := fmt.Sprintf("test-%s-%04d", strings.ReplaceAll(role, "_", "-"), i)

		agent := agentdomain.Agent{
			ID:        agentID,
			Namespace: workspaceID,
			Name:      "Test " + strings.Title(role), //nolint:staticcheck // SA1019: Title is fine for ASCII roles
			Slug:      role,
			Role:      role,
			Prompt:    fmt.Sprintf("Test %s agent for fixture", role),
			State:     agentdomain.StateStopped,
			ExecMode:  agentdomain.ModeReactive,
			CreatedAt: now,
			ShareBB:   "scoped",
		}

		if createErr := store.Create(ctx, agent); createErr != nil {
			// If the agent already exists (unlikely with unique IDs), continue.
			if !strings.Contains(createErr.Error(), "UNIQUE") &&
				!strings.Contains(createErr.Error(), "already exists") {
				return ids, fmt.Errorf("create agent role=%q id=%q: %w", role, agentID, createErr)
			}
		}

		ids = append(ids, agentID)
	}

	return ids, nil
}

// FreePort returns a free TCP port on localhost. This is a utility for tests
// that need to pre-allocate ports (though the daemon's -p 0 should be
// preferred).
func FreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
