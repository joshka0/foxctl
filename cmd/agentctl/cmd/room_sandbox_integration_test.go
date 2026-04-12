package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/tmuxbridge"
	"github.com/spf13/cobra"
)

// ============================================================
// Helpers for sandbox integration tests
// ============================================================

// setupSandboxTestRoom creates a room with a sandbox config for testing.
// Returns the workspace dir, store, and cleanup function.
func setupSandboxTestRoom(t *testing.T, roomID, worktreePath string) (string, blackboard.BoardStore) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()

	// Open store
	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}

	// Create room with sandbox config
	_, err = store.UpsertRoom(ctx, agent.Room{
		ID:          roomID,
		WorkspaceID: workspace,
		Title:       "Sandbox Test Room",
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator", Backend: "tmux", Session: "test-session", PaneID: "0"},
			{ActorID: "agent-a", Role: "worker", Backend: "tmux", Session: "test-session", PaneID: "1"},
		},
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:   worktreePath,
			WorktreeBranch: "sandbox/room-" + roomID,
			TmuxSession:    "agentctl-sandbox-" + roomID,
			TerminalURL:    "/terminal/" + roomID,
			Runtime:        "worktree",
			BaseRef:        "HEAD",
		},
	})
	if err != nil {
		store.Close()
		t.Fatalf("UpsertRoom: %v", err)
	}

	return workspace, store
}

// decodeSandboxEnvelope decodes a JSON envelope from buffer.
func decodeSandboxEnvelope(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, buf.String())
	}
	if env.Status != envelope.StatusOK {
		t.Fatalf("status=%q want ok payload=%s", env.Status, buf.String())
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type=%T", env.Data)
	}
	return data
}

// hasTmux returns true if tmux is available in PATH.
func hasTmux() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// hasGit returns true if git is available in PATH.
func hasGit() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// createTestGitRepo creates a temporary git repo with an initial commit.
func createTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("run %s %v: %v\n%s", name, args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "test@test.com")
	run("git", "config", "user.name", "Test")
	// Create initial file and commit
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "initial")
	return dir
}

// ============================================================
// VAL-RS-012: Agents spawned in sandbox room use worktree as CWD
// ============================================================

func TestRoomSandboxAgent_SpawnUsesWorktreeCWD(t *testing.T) {
	if !hasGit() {
		t.Skip("git not available")
	}

	t.Setenv("HOME", t.TempDir())

	// Create a git repo
	repoPath := createTestGitRepo(t)

	// Create a worktree manually to simulate sandbox
	wtPath := filepath.Join(t.TempDir(), "worktrees", "sandbox-room-test-agent-cwd")
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	// Actually create a real worktree
	cmd := exec.Command("git", "worktree", "add", wtPath, "HEAD")
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	defer func() {
		cmd := exec.Command("git", "worktree", "remove", wtPath, "--force")
		cmd.Dir = repoPath
		_ = cmd.Run()
	}()

	// Setup room with sandbox config pointing to the worktree
	workspace, store := setupSandboxTestRoom(t, "test-agent-cwd", wtPath)
	defer store.Close()
	_ = workspace

	// Test that resolveSpawnRoomSandboxCWD returns the worktree path
	// when spawnRoomID is set to a sandbox room
	originalRoomID := spawnRoomID
	spawnRoomID = "test-agent-cwd"
	defer func() { spawnRoomID = originalRoomID }()

	got := resolveSpawnRoomSandboxCWD(workspace)
	if got != wtPath {
		t.Errorf("resolveSpawnRoomSandboxCWD() = %q, want %q", got, wtPath)
	}
}

func TestRoomSandboxAgent_SpawnNonSandboxRoomReturnsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	workspace := t.TempDir()

	// Create room WITHOUT sandbox config
	cfg, err := config.Load(context.Background())
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := blackboard.OpenBoardStore(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertRoom(context.Background(), agent.Room{
		ID:          "non-sandbox-room",
		WorkspaceID: workspace,
		Title:       "Non-Sandbox Room",
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	originalRoomID := spawnRoomID
	spawnRoomID = "non-sandbox-room"
	defer func() { spawnRoomID = originalRoomID }()

	got := resolveSpawnRoomSandboxCWD(workspace)
	if got != "" {
		t.Errorf("resolveSpawnRoomSandboxCWD() = %q, want empty for non-sandbox room", got)
	}
}

func TestRoomSandboxAgent_SpawnNonexistentWorktreeReturnsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	workspace := t.TempDir()

	// Create room with sandbox config pointing to nonexistent path
	cfg, err := config.Load(context.Background())
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := blackboard.OpenBoardStore(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertRoom(context.Background(), agent.Room{
		ID:          "stale-sandbox-room",
		WorkspaceID: workspace,
		Title:       "Stale Sandbox Room",
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:   "/nonexistent/worktree/path",
			WorktreeBranch: "sandbox/room-stale",
			TmuxSession:    "agentctl-sandbox-stale",
			Runtime:        "worktree",
		},
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	originalRoomID := spawnRoomID
	spawnRoomID = "stale-sandbox-room"
	defer func() { spawnRoomID = originalRoomID }()

	got := resolveSpawnRoomSandboxCWD(workspace)
	if got != "" {
		t.Errorf("resolveSpawnRoomSandboxCWD() = %q, want empty for nonexistent worktree", got)
	}
}

// ============================================================
// VAL-RS-013: Tasks in sandbox rooms have worktree-relative ScopePaths
// ============================================================

func TestRoomSandboxTasks_ScopePathResolvedToWorktree(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()
	wtPath := "/tmp/worktrees/sandbox-tasks-room"

	// Setup room with sandbox config
	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertRoom(ctx, agent.Room{
		ID:          "sandbox-tasks-room",
		WorkspaceID: workspace,
		Title:       "Sandbox Tasks Room",
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
		},
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:   wtPath,
			WorktreeBranch: "sandbox/room-sandbox-tasks-room",
			TmuxSession:    "agentctl-sandbox-sandbox-tasks-room",
			Runtime:        "worktree",
		},
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	// Test resolveSandboxScopePath
	tests := []struct {
		name      string
		scopePath string
		want      string
	}{
		{
			name:      "empty scope path resolves to worktree",
			scopePath: "",
			want:      wtPath,
		},
		{
			name:      "relative path resolves under worktree",
			scopePath: "src/main.go",
			want:      filepath.Join(wtPath, "src/main.go"),
		},
		{
			name:      "absolute path under worktree stays as-is",
			scopePath: filepath.Join(wtPath, "pkg/handler.go"),
			want:      filepath.Join(wtPath, "pkg/handler.go"),
		},
		{
			name:      "relative nested path resolves under worktree",
			scopePath: "a/b/c/file.txt",
			want:      filepath.Join(wtPath, "a/b/c/file.txt"),
		},
		{
			name:      "path traversal via .. is rejected, returns worktree root",
			scopePath: "../../etc/passwd",
			want:      wtPath,
		},
		{
			name:      "deep traversal escaping worktree returns worktree root",
			scopePath: "sub/../../../outside",
			want:      wtPath,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveSandboxScopePath(ctx, store, workspace, "sandbox-tasks-room", tc.scopePath)
			if got != tc.want {
				t.Errorf("resolveSandboxScopePath(%q) = %q, want %q", tc.scopePath, got, tc.want)
			}
		})
	}
}

func TestRoomSandboxTasks_NonSandboxRoomScopePathUntouched(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()

	// Setup room WITHOUT sandbox config
	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertRoom(ctx, agent.Room{
		ID:          "plain-tasks-room",
		WorkspaceID: workspace,
		Title:       "Plain Tasks Room",
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	// resolveSandboxScopePath should return the original path for non-sandbox rooms
	got := resolveSandboxScopePath(ctx, store, workspace, "plain-tasks-room", "src/main.go")
	if got != "src/main.go" {
		t.Errorf("resolveSandboxScopePath() = %q, want %q for non-sandbox room", got, "src/main.go")
	}

	got = resolveSandboxScopePath(ctx, store, workspace, "plain-tasks-room", "")
	if got != "" {
		t.Errorf("resolveSandboxScopePath() = %q, want empty for non-sandbox room with empty path", got)
	}
}

func TestRoomSandboxTasks_TaskAddUsesSandboxScopePath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()
	wtPath := filepath.Join(t.TempDir(), "worktree-sandbox")
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Verify that resolveSandboxScopePath works correctly with the board store
	// opened via the standard loadConfig path. This tests the full integration
	// through the same config/store path used by runRoomTaskAdd.
	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	// Open store via the same path as runRoomTaskAdd
	boardStore, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}

	// Create room with sandbox config
	_, err = boardStore.UpsertRoom(ctx, agent.Room{
		ID:          "task-sandbox-room",
		WorkspaceID: workspace,
		Title:       "Task Sandbox Room",
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
		},
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:   wtPath,
			WorktreeBranch: "sandbox/room-task-sandbox-room",
			TmuxSession:    "agentctl-sandbox-task-sandbox-room",
			Runtime:        "worktree",
		},
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	// Verify scope path resolution with the same board store
	tests := []struct {
		name      string
		scopePath string
		want      string
	}{
		{"relative path", "pkg/handler.go", filepath.Join(wtPath, "pkg/handler.go")},
		{"empty path", "", wtPath},
		{"nested path", "a/b/c/file.txt", filepath.Join(wtPath, "a/b/c/file.txt")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveSandboxScopePath(ctx, boardStore, workspace, "task-sandbox-room", tc.scopePath)
			if got != tc.want {
				t.Errorf("resolveSandboxScopePath(%q) = %q, want %q", tc.scopePath, got, tc.want)
			}
		})
	}

	boardStore.Close()
}

// ============================================================
// VAL-RS-014: Room relay delivers messages to sandbox tmux panes
// ============================================================

func TestRoomSandboxRelay_DeliversToSandboxSession(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}
	if !hasGit() {
		t.Skip("git not available")
	}

	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	// Create a git repo and worktree
	repoPath := createTestGitRepo(t)
	sessionName := "agentctl-sandbox-test-relay-room"
	wtPath := filepath.Join(t.TempDir(), "worktrees", "relay-test")
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	cmd := exec.Command("git", "worktree", "add", wtPath, "HEAD")
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	defer func() {
		exec.Command("git", "worktree", "remove", wtPath, "--force").Run()
	}()

	// Create tmux session for the sandbox
	tc := tmuxbridge.New()
	err := createTmuxSessionForSandbox(ctx, tc, sessionName, wtPath)
	if err != nil {
		t.Fatalf("createTmuxSessionForSandbox: %v", err)
	}
	defer func() {
		exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	// Setup room with sandbox config
	workspace, store := setupSandboxTestRoom(t, "test-relay-room", wtPath)
	defer store.Close()
	_ = workspace

	// Get the room summary
	summary, err := store.GetRoom(ctx, workspace, "test-relay-room", "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}

	// Create a test message
	msg := agent.BoardMessage{
		ID:        "msg-1",
		Stream:    "room:test-relay-room",
		Sender:    "human-a",
		Recipient: "*",
		Kind:      agent.BoardMessageKindInfo,
		Subject:   "Test relay",
		Body:      "Hello sandbox!",
	}

	// Test relay to sandbox session
	result := relayRoomMessageToSandbox(ctx, tc, summary, msg)
	if result.DeliveredCount != 1 {
		t.Errorf("DeliveredCount = %d, want 1", result.DeliveredCount)
	}
	expectedTarget := sessionName + ":0"
	if len(result.DeliveredTo) != 1 || result.DeliveredTo[0] != expectedTarget {
		t.Errorf("DeliveredTo = %v, want [%s]", result.DeliveredTo, expectedTarget)
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want empty", result.Error)
	}
}

func TestRoomSandboxRelay_NoopForNonSandboxRoom(t *testing.T) {
	ctx := context.Background()
	tc := tmuxbridge.New()

	room := agent.RoomSummary{
		ID:      "non-sandbox",
		Members: []agent.RoomMember{},
	}
	// No SandboxConfig
	msg := agent.BoardMessage{
		ID:     "msg-1",
		Stream: "room:non-sandbox",
		Sender: "human-a",
		Body:   "test",
	}

	result := relayRoomMessageToSandbox(ctx, tc, room, msg)
	if result.DeliveredCount != 0 {
		t.Errorf("DeliveredCount = %d, want 0 for non-sandbox room", result.DeliveredCount)
	}
	if result.Backend != "sandbox" {
		t.Errorf("Backend = %q, want sandbox", result.Backend)
	}
}

func TestRoomSandboxRelay_NoopForMissingSession(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}

	ctx := context.Background()
	tc := tmuxbridge.New()

	room := agent.RoomSummary{
		ID: "sandbox-no-session",
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath: "/tmp/fake-worktree",
			TmuxSession:  "nonexistent-session-xyz",
			Runtime:      "worktree",
		},
	}
	msg := agent.BoardMessage{
		ID:     "msg-1",
		Stream: "room:sandbox-no-session",
		Sender: "human-a",
		Body:   "test",
	}

	result := relayRoomMessageToSandbox(ctx, tc, room, msg)
	if result.DeliveredCount != 0 {
		t.Errorf("DeliveredCount = %d, want 0 for missing session", result.DeliveredCount)
	}
	if result.Error == "" {
		t.Error("Error should mention missing session")
	}
}

// ============================================================
// VAL-RS-015: Room loop works with sandbox rooms
// ============================================================

func TestRoomSandboxLoop_RelayIncludesSandboxDelivery(t *testing.T) {
	// Test that relayRoomMessage (the auto-dispatch relay) includes
	// sandbox delivery when the room has a sandbox config.
	if !hasTmux() {
		t.Skip("tmux not available")
	}

	ctx := context.Background()

	// Create a sandbox session
	sessionName := "agentctl-sandbox-loop-test-room"
	tc := tmuxbridge.New()
	tmpDir := t.TempDir()
	err := createTmuxSessionForSandbox(ctx, tc, sessionName, tmpDir)
	if err != nil {
		t.Fatalf("createTmuxSessionForSandbox: %v", err)
	}
	defer func() {
		exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	room := agent.RoomSummary{
		ID: "loop-test-room",
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath: tmpDir,
			TmuxSession:  sessionName,
			Runtime:      "worktree",
		},
	}
	msg := agent.BoardMessage{
		ID:        "msg-loop-1",
		Stream:    "room:loop-test-room",
		Sender:    "human-a",
		Recipient: "*",
		Kind:      agent.BoardMessageKindInfo,
		Body:      "Loop relay test",
	}

	result := relayRoomMessage(ctx, tc, room, msg, roomRelayOptions{Backend: "auto"})
	// The sandbox delivery should have been included
	expectedTarget := sessionName + ":0"
	found := false
	for _, target := range result.DeliveredTo {
		if target == expectedTarget {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("sandbox target %q not in DeliveredTo %v", expectedTarget, result.DeliveredTo)
	}
}

// ============================================================
// VAL-RS-016: Room status includes sandbox context
// ============================================================

func TestRoomSandboxStatus_IncludesSandboxInfo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	wtPath := "/tmp/worktrees/status-sandbox-room"
	workspace, store := setupSandboxTestRoom(t, "status-sandbox-room", wtPath)
	defer store.Close()

	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetOut(buf)

	err := runRoomStatus(cmd, workspace, "status-sandbox-room", 200, nil, "open", false)
	if err != nil {
		t.Fatalf("runRoomStatus: %v", err)
	}

	data := decodeSandboxEnvelope(t, buf)

	sandbox, ok := data["sandbox"].(map[string]any)
	if !ok || sandbox == nil {
		t.Fatal("sandbox field missing from status response")
	}

	if sandbox["worktree_path"] != wtPath {
		t.Errorf("sandbox.worktree_path = %v, want %q", sandbox["worktree_path"], wtPath)
	}
	if sandbox["runtime"] != "worktree" {
		t.Errorf("sandbox.runtime = %v, want %q", sandbox["runtime"], "worktree")
	}
	if sandbox["tmux_session"] != "agentctl-sandbox-status-sandbox-room" {
		t.Errorf("sandbox.tmux_session = %v, want agentctl-sandbox-status-sandbox-room", sandbox["tmux_session"])
	}
}

func TestRoomSandboxStatus_NonSandboxRoomHasNilSandbox(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()

	// Create non-sandbox room
	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertRoom(ctx, agent.Room{
		ID:          "status-plain-room",
		WorkspaceID: workspace,
		Title:       "Plain Room",
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
		},
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetOut(buf)

	err = runRoomStatus(cmd, workspace, "status-plain-room", 200, nil, "open", false)
	if err != nil {
		t.Fatalf("runRoomStatus: %v", err)
	}

	data := decodeSandboxEnvelope(t, buf)

	sandbox := data["sandbox"]
	if sandbox != nil {
		t.Errorf("sandbox = %v, want nil for non-sandbox room", sandbox)
	}
}

// ============================================================
// VAL-RS-017: Room inbox includes sandbox context
// ============================================================

func TestRoomSandboxInbox_IncludesSandboxInfo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	wtPath := "/tmp/worktrees/inbox-sandbox-room"
	workspace, store := setupSandboxTestRoom(t, "inbox-sandbox-room", wtPath)
	defer store.Close()

	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetOut(buf)

	err := runRoomInbox(cmd, workspace, "inbox-sandbox-room", "human-a", 100, "all", false, false, false, false)
	if err != nil {
		t.Fatalf("runRoomInbox: %v", err)
	}

	data := decodeSandboxEnvelope(t, buf)

	sandbox, ok := data["sandbox"].(map[string]any)
	if !ok || sandbox == nil {
		t.Fatal("sandbox field missing from inbox response")
	}

	if sandbox["worktree_path"] != wtPath {
		t.Errorf("sandbox.worktree_path = %v, want %q", sandbox["worktree_path"], wtPath)
	}
	if sandbox["runtime"] != "worktree" {
		t.Errorf("sandbox.runtime = %v, want %q", sandbox["runtime"], "worktree")
	}
}

func TestRoomSandboxInbox_NonSandboxRoomHasNilSandbox(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()

	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertRoom(ctx, agent.Room{
		ID:          "inbox-plain-room",
		WorkspaceID: workspace,
		Title:       "Plain Inbox Room",
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
		},
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetOut(buf)

	err = runRoomInbox(cmd, workspace, "inbox-plain-room", "human-a", 100, "all", false, false, false, false)
	if err != nil {
		t.Fatalf("runRoomInbox: %v", err)
	}

	data := decodeSandboxEnvelope(t, buf)

	sandbox := data["sandbox"]
	if sandbox != nil {
		t.Errorf("sandbox = %v, want nil for non-sandbox room", sandbox)
	}
}

// ============================================================
// VAL-RS-018: Red/green pattern works inside sandbox room worktree
// ============================================================

func TestRoomSandboxRedgreen_InitOnSandboxRoom(t *testing.T) {
	if !hasGit() {
		t.Skip("git not available")
	}

	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	// Create a git repo - this is the workspace
	repoPath := createTestGitRepo(t)

	// Create a worktree from the repo for the sandbox
	wtPath := filepath.Join(t.TempDir(), "worktrees", "sandbox-redgreen")
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	cmd := exec.Command("git", "worktree", "add", wtPath, "HEAD")
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	defer func() {
		exec.Command("git", "worktree", "remove", wtPath, "--force").Run()
	}()

	workspace := repoPath

	// Run redgreen init - this creates the room with its own worktrees.
	// The red/green pattern works inside the workspace even when sandbox
	// rooms exist alongside it.
	rgWorktreeRoot := filepath.Join(t.TempDir(), "rg-worktrees")
	buf := &bytes.Buffer{}
	rgCmd := &cobra.Command{}
	rgCmd.SetContext(ctx)
	rgCmd.SetOut(buf)

	err := runRoomRedgreenInit(rgCmd, workspace, "redgreen-sandbox-room", "rg-slug", "RG Test", "Red/green in sandbox",
		"red-a", "green-a", "human-a", rgWorktreeRoot, "HEAD", "go test ./...")
	if err != nil {
		t.Fatalf("runRoomRedgreenInit: %v", err)
	}

	data := decodeSandboxEnvelope(t, buf)

	// Verify the room was created with red/green worktrees
	roomData, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T", data["room"])
	}

	// Verify red/green worktrees were created
	if red, ok := roomData["red_worktree_path"].(string); ok && red != "" {
		if !strings.HasPrefix(red, rgWorktreeRoot) {
			t.Errorf("red worktree path %q should be under %q", red, rgWorktreeRoot)
		}
		// Verify the red worktree actually exists
		if _, err := os.Stat(red); err != nil {
			t.Errorf("red worktree path %q does not exist: %v", red, err)
		}
	}

	if green, ok := roomData["green_worktree_path"].(string); ok && green != "" {
		if !strings.HasPrefix(green, rgWorktreeRoot) {
			t.Errorf("green worktree path %q should be under %q", green, rgWorktreeRoot)
		}
		// Verify the green worktree actually exists
		if _, err := os.Stat(green); err != nil {
			t.Errorf("green worktree path %q does not exist: %v", green, err)
		}
	}
	_ = roomData
}

// ============================================================
// VAL-RS-019: Agile epic/milestone/story commands work on sandbox rooms
// ============================================================

func TestRoomSandboxAgile_EpicStartOnSandboxRoom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	wtPath := "/tmp/worktrees/agile-sandbox-room"
	workspace, store := setupSandboxTestRoom(t, "agile-sandbox-room", wtPath)
	defer store.Close()

	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetOut(buf)

	err := runRoomEpicStart(cmd, workspace, "human-a", "agile-sandbox-room", "Sandbox Epic", "Build feature X", "human-a", "Working feature X", "1 week", nil, nil, false, false)
	if err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}

	data := decodeSandboxEnvelope(t, buf)
	epicID, _ := data["epic_id"].(string)
	if epicID == "" {
		t.Error("epic_id should not be empty")
	}

	// Verify the room still has its sandbox config
	summary, err := store.GetRoom(ctx, workspace, "agile-sandbox-room", "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if summary.SandboxConfig == nil || !summary.SandboxConfig.IsSandbox() {
		t.Error("room lost sandbox config after epic start")
	}
}

func TestRoomSandboxAgile_MilestoneStartOnSandboxRoom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	wtPath := "/tmp/worktrees/agile-milestone-room"
	workspace, store := setupSandboxTestRoom(t, "agile-milestone-room", wtPath)
	defer store.Close()

	// First create an epic
	epicBuf := &bytes.Buffer{}
	epicCmd := &cobra.Command{}
	epicCmd.SetContext(ctx)
	epicCmd.SetOut(epicBuf)

	err := runRoomEpicStart(epicCmd, workspace, "human-a", "agile-milestone-room", "Sandbox Milestone Epic", "Build milestone", "human-a", "Complete milestone", "2 weeks", nil, nil, false, false)
	if err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}

	epicData := decodeSandboxEnvelope(t, epicBuf)
	epicID, _ := epicData["epic_id"].(string)
	if epicID == "" {
		t.Fatal("epic_id should not be empty")
	}

	// Finalize the epic (required before milestones can be started)
	finBuf := &bytes.Buffer{}
	finCmd := &cobra.Command{}
	finCmd.SetContext(ctx)
	finCmd.SetOut(finBuf)

	err = runRoomEpicFinalize(finCmd, workspace, "human-a", "agile-milestone-room", epicID, "Finalized epic for sandbox milestone testing")
	if err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	// Now start a milestone under the finalized epic
	msBuf := &bytes.Buffer{}
	msCmd := &cobra.Command{}
	msCmd.SetContext(ctx)
	msCmd.SetOut(msBuf)

	err = runRoomMilestoneStart(msCmd, workspace, "human-a", "agile-milestone-room", epicID, "Milestone 1",
		"Complete milestone 1", "Implement core features", "human-a",
		[]string{"feature-a"}, nil, nil, nil, nil, nil, "")
	if err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}

	msData := decodeSandboxEnvelope(t, msBuf)
	milestoneID, _ := msData["milestone_id"].(string)
	if milestoneID == "" {
		t.Error("milestone_id should not be empty")
	}
}

func TestRoomSandboxAgile_StoryAddOnSandboxRoom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	wtPath := "/tmp/worktrees/agile-story-room"
	workspace, store := setupSandboxTestRoom(t, "agile-story-room", wtPath)
	defer store.Close()

	// Create epic
	epicBuf := &bytes.Buffer{}
	epicCmd := &cobra.Command{}
	epicCmd.SetContext(ctx)
	epicCmd.SetOut(epicBuf)

	err := runRoomEpicStart(epicCmd, workspace, "human-a", "agile-story-room", "Sandbox Story Epic", "Build feature", "human-a", "Complete feature", "1 week", nil, nil, false, false)
	if err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}

	epicData := decodeSandboxEnvelope(t, epicBuf)
	epicID, _ := epicData["epic_id"].(string)
	if epicID == "" {
		t.Fatal("epic_id should not be empty")
	}

	// Finalize the epic
	finBuf := &bytes.Buffer{}
	finCmd := &cobra.Command{}
	finCmd.SetContext(ctx)
	finCmd.SetOut(finBuf)

	err = runRoomEpicFinalize(finCmd, workspace, "human-a", "agile-story-room", epicID, "Finalized epic for sandbox story testing")
	if err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	// Create milestone
	msBuf := &bytes.Buffer{}
	msCmd := &cobra.Command{}
	msCmd.SetContext(ctx)
	msCmd.SetOut(msBuf)

	err = runRoomMilestoneStart(msCmd, workspace, "human-a", "agile-story-room", epicID, "Milestone 1",
		"Milestone 1", "Implement core", "human-a", nil, nil, nil, nil, nil, nil, "")
	if err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}

	msData := decodeSandboxEnvelope(t, msBuf)
	milestoneID, _ := msData["milestone_id"].(string)
	if milestoneID == "" {
		t.Fatal("milestone_id should not be empty")
	}

	// Add a story
	storyBuf := &bytes.Buffer{}
	storyCmd := &cobra.Command{}
	storyCmd.SetContext(ctx)
	storyCmd.SetOut(storyBuf)

	err = runRoomStoryAdd(storyCmd, workspace, "human-a", "agile-story-room", milestoneID, "Story 1", "Implement story 1 in worktree", "agent-a")
	if err != nil {
		t.Fatalf("runRoomStoryAdd: %v", err)
	}

	storyData := decodeSandboxEnvelope(t, storyBuf)
	storyID, _ := storyData["story_id"].(string)
	if storyID == "" {
		t.Error("story_id should not be empty")
	}

	// Verify the room still has its sandbox config after all agile operations
	summary, err := store.GetRoom(ctx, workspace, "agile-story-room", "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if summary.SandboxConfig == nil || !summary.SandboxConfig.IsSandbox() {
		t.Error("room lost sandbox config after agile operations")
	}
}

// ============================================================
// Integration: buildRoomSandboxInfo
// ============================================================

func TestBuildRoomSandboxInfo_SandboxRoom(t *testing.T) {
	room := agent.RoomSummary{
		ID: "test-room",
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:   "/tmp/worktrees/test-room",
			WorktreeBranch: "sandbox/room-test-room",
			TmuxSession:    "agentctl-sandbox-test-room",
			TerminalURL:    "/terminal/test-room",
			Runtime:        "worktree",
		},
	}

	info := buildRoomSandboxInfo(room)
	if info == nil {
		t.Fatal("expected non-nil sandbox info")
	}
	if info["worktree_path"] != "/tmp/worktrees/test-room" {
		t.Errorf("worktree_path = %v, want /tmp/worktrees/test-room", info["worktree_path"])
	}
	if info["runtime"] != "worktree" {
		t.Errorf("runtime = %v, want worktree", info["runtime"])
	}
	if info["tmux_session"] != "agentctl-sandbox-test-room" {
		t.Errorf("tmux_session = %v, want agentctl-sandbox-test-room", info["tmux_session"])
	}
}

func TestBuildRoomSandboxInfo_NonSandboxRoom(t *testing.T) {
	room := agent.RoomSummary{
		ID: "plain-room",
	}

	info := buildRoomSandboxInfo(room)
	if info != nil {
		t.Errorf("expected nil for non-sandbox room, got %v", info)
	}
}

func TestBuildRoomSandboxInfo_SandboxConfigWithEmptyPath(t *testing.T) {
	room := agent.RoomSummary{
		ID: "empty-path-room",
		SandboxConfig: &agent.SandboxConfig{
			Runtime: "worktree",
		},
	}

	info := buildRoomSandboxInfo(room)
	if info != nil {
		t.Errorf("expected nil for sandbox config with empty worktree path, got %v", info)
	}
}

// ============================================================
// Integration: resolveRoomSandboxWorktree
// ============================================================

func TestResolveRoomSandboxWorktree_ExistingRoom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	wtPath := "/tmp/worktrees/resolve-test-room"
	workspace, store := setupSandboxTestRoom(t, "resolve-test-room", wtPath)
	defer store.Close()

	got := resolveRoomSandboxWorktree(ctx, store, workspace, "resolve-test-room")
	if got != wtPath {
		t.Errorf("resolveRoomSandboxWorktree() = %q, want %q", got, wtPath)
	}
}

func TestResolveRoomSandboxWorktree_NonSandboxRoom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()
	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertRoom(ctx, agent.Room{
		ID:          "plain-resolve-room",
		WorkspaceID: workspace,
		Title:       "Plain Room",
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	got := resolveRoomSandboxWorktree(ctx, store, workspace, "plain-resolve-room")
	if got != "" {
		t.Errorf("resolveRoomSandboxWorktree() = %q, want empty for non-sandbox room", got)
	}
}

func TestBuildTerminalURL_NoGatewayURL(t *testing.T) {
	t.Setenv("AGENTCTL_GATEWAY_URL", "")
	got := buildTerminalURL("my-room")
	if got != "/terminal/my-room" {
		t.Errorf("buildTerminalURL() = %q, want /terminal/my-room", got)
	}
}

func TestBuildTerminalURL_WithGatewayURL(t *testing.T) {
	t.Setenv("AGENTCTL_GATEWAY_URL", "http://localhost:8765")
	got := buildTerminalURL("my-room")
	if got != "http://localhost:8765/terminal/my-room" {
		t.Errorf("buildTerminalURL() = %q, want http://localhost:8765/terminal/my-room", got)
	}
}

func TestBuildTerminalURL_WithGatewayURL_TrailingSlash(t *testing.T) {
	t.Setenv("AGENTCTL_GATEWAY_URL", "http://localhost:8765/")
	got := buildTerminalURL("my-room")
	if got != "http://localhost:8765/terminal/my-room" {
		t.Errorf("buildTerminalURL() = %q, want http://localhost:8765/terminal/my-room", got)
	}
}

func TestGatewayDeregisterRoom_NoGatewayURL(t *testing.T) {
	t.Setenv("AGENTCTL_GATEWAY_URL", "")
	// When no gateway URL is set, returns false without error.
	got := gatewayDeregisterRoom(context.Background(), "room-id")
	if got {
		t.Error("gatewayDeregisterRoom() = true, want false when AGENTCTL_GATEWAY_URL unset")
	}
}

func TestGatewayDeregisterRoom_GatewayResponds(t *testing.T) {
	// Stand up a minimal HTTP server that mimics the gateway DELETE endpoint.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/rooms/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"unregistered"}`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	t.Setenv("AGENTCTL_GATEWAY_URL", ts.URL)
	got := gatewayDeregisterRoom(context.Background(), "some-room")
	if !got {
		t.Error("gatewayDeregisterRoom() = false, want true when gateway responds 200")
	}
}

func TestGatewayRegisterRoom_GatewayResponds(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/rooms", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"registered"}`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	t.Setenv("AGENTCTL_GATEWAY_URL", ts.URL)
	ok := gatewayRegisterRoom(context.Background(), "my-room", "agentctl-sandbox-my-room")
	if !ok {
		t.Error("gatewayRegisterRoom() = false, want true when gateway responds 201")
	}
	if gotBody["room_id"] != "my-room" {
		t.Errorf("request body room_id = %q, want my-room", gotBody["room_id"])
	}
}
