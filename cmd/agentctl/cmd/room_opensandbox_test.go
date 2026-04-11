//go:build opensandbox
// +build opensandbox

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newOpenSandboxMockServer creates a mock OpenSandbox API server for testing.
func newOpenSandboxMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/sandboxes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST /v1/sandboxes, got %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		sandboxID := "sbx-test-" + fmt.Sprintf("%d", time.Now().UnixNano())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": sandboxID,
			"status": map[string]any{
				"state": "Running",
			},
			"createdAt": time.Now().UTC().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("/v1/sandboxes/", func(w http.ResponseWriter, r *http.Request) {
		// Extract sandbox ID from path: /v1/sandboxes/{id} or /v1/sandboxes/{id}/endpoints/{port}
		pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/sandboxes/"), "/")
		sandboxID := pathParts[0]

		if len(pathParts) >= 3 && pathParts[1] == "endpoints" {
			// GET /v1/sandboxes/{id}/endpoints/{port}
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"endpoint": "execd-" + sandboxID + ".local:44772",
					"headers": map[string]string{
						"X-EXECD-ACCESS-TOKEN": "test-token-" + sandboxID,
					},
				})
				return
			}
		}

		// DELETE /v1/sandboxes/{id}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// GET /v1/sandboxes/{id}
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": sandboxID,
				"status": map[string]any{
					"state": "Running",
				},
			})
			return
		}

		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	return httptest.NewServer(mux)
}

// newOpenSandboxFailServer creates a mock server that always fails (simulates unreachable API).
func newOpenSandboxFailServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error": "service unavailable"}`))
	})
	return httptest.NewServer(mux)
}

// newOpenSandboxDeleteFailServer creates a mock server where create succeeds but delete returns 404.
func newOpenSandboxDeleteFailServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/sandboxes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		sandboxID := "sbx-deltest-" + fmt.Sprintf("%d", time.Now().UnixNano())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": sandboxID,
			"status": map[string]any{
				"state": "Running",
			},
		})
	})

	mux.HandleFunc("/v1/sandboxes/", func(w http.ResponseWriter, r *http.Request) {
		pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/sandboxes/"), "/")
		sandboxID := pathParts[0]

		if len(pathParts) >= 3 && pathParts[1] == "endpoints" {
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"endpoint": "execd-" + sandboxID + ".local:44772",
					"headers": map[string]string{
						"X-EXECD-ACCESS-TOKEN": "test-token",
					},
				})
				return
			}
		}

		// DELETE always returns 404 (container already deleted)
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "not found"}`))
			return
		}

		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": sandboxID,
				"status": map[string]any{
					"state": "Running",
				},
			})
			return
		}

		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	return httptest.NewServer(mux)
}

// TestRoomOpenSandboxCreate validates VAL-OS-001:
// room create --sandbox --runtime opensandbox creates container.
func TestRoomOpenSandboxCreate(t *testing.T) {
	server := newOpenSandboxMockServer(t)
	defer server.Close()

	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-api-key")

	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	room := agent.Room{
		ID:          "os-test-room",
		WorkspaceID: workspace,
		Title:       "OpenSandbox Room",
	}

	result, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:        true,
		SandboxRuntime: "opensandbox",
		SandboxTTL:     30 * time.Minute,
	})
	require.NoError(t, err, "provisionSandbox should succeed")
	require.NotNil(t, result, "result should not be nil")

	// Verify container metadata in result
	assert.Equal(t, "opensandbox", result["runtime"])
	assert.Equal(t, "created", result["status"])
	assert.NotEmpty(t, result["container_id"], "container_id should be set")
	assert.NotEmpty(t, result["container_endpoint"], "container_endpoint should be set")
	assert.NotEmpty(t, result["container_expires_at"], "container_expires_at should be set")
	assert.Equal(t, "/terminal/os-test-room", result["terminal_url"])

	// Verify SandboxConfig on the room
	require.NotNil(t, room.SandboxConfig, "SandboxConfig should be set")
	assert.Equal(t, "opensandbox", room.SandboxConfig.Runtime)
	assert.NotEmpty(t, room.SandboxConfig.ContainerID)
	assert.NotEmpty(t, room.SandboxConfig.ContainerEndpoint)
	assert.NotEmpty(t, room.SandboxConfig.ContainerExpiresAt)
	assert.False(t, room.SandboxConfig.Fallback)

	// Clean up tmux session if created
	if room.SandboxConfig.TmuxSession != "" {
		_ = exec.Command("tmux", "kill-session", "-t", room.SandboxConfig.TmuxSession).Run()
	}
}

// TestRoomOpenSandboxTTL validates VAL-OS-003:
// --sandbox-ttl sets container TTL.
func TestRoomOpenSandboxTTL(t *testing.T) {
	server := newOpenSandboxMockServer(t)
	defer server.Close()

	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-api-key")

	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	room := agent.Room{
		ID:          "os-ttl-room",
		WorkspaceID: workspace,
	}

	ttl := 30 * time.Minute
	result, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:        true,
		SandboxRuntime: "opensandbox",
		SandboxTTL:     ttl,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// ContainerExpiresAt should be approximately now + ttl
	expiresAtStr, ok := result["container_expires_at"].(string)
	require.True(t, ok, "container_expires_at should be a string")
	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	require.NoError(t, err, "container_expires_at should be valid RFC3339")

	// Allow 10 seconds of clock skew in the test
	now := time.Now().UTC()
	expectedExpiry := now.Add(ttl)
	assert.WithinDuration(t, expectedExpiry, expiresAt, 10*time.Second,
		"container_expires_at should be approximately now + ttl")

	require.NotNil(t, room.SandboxConfig)
	assert.Equal(t, expiresAtStr, room.SandboxConfig.ContainerExpiresAt)

	// Clean up
	if room.SandboxConfig.TmuxSession != "" {
		_ = exec.Command("tmux", "kill-session", "-t", room.SandboxConfig.TmuxSession).Run()
	}
}

// TestRoomOpenSandboxResourceLimits validates VAL-OS-004:
// --sandbox-cpu and --sandbox-memory set resource limits.
func TestRoomOpenSandboxResourceLimits(t *testing.T) {
	server := newOpenSandboxMockServer(t)
	defer server.Close()

	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-api-key")

	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	room := agent.Room{
		ID:          "os-resource-room",
		WorkspaceID: workspace,
	}

	result, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:        true,
		SandboxRuntime: "opensandbox",
		SandboxCPU:     "500m",
		SandboxMemory:  "512Mi",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "500m", result["container_cpu"])
	assert.Equal(t, "512Mi", result["container_memory"])

	require.NotNil(t, room.SandboxConfig)
	assert.Equal(t, "500m", room.SandboxConfig.ContainerCPU)
	assert.Equal(t, "512Mi", room.SandboxConfig.ContainerMemory)

	// Clean up
	if room.SandboxConfig.TmuxSession != "" {
		_ = exec.Command("tmux", "kill-session", "-t", room.SandboxConfig.TmuxSession).Run()
	}
}

// TestRoomOpenSandboxFallback validates VAL-OS-005:
// Falls back to worktree when OpenSandbox API is unreachable.
func TestRoomOpenSandboxFallback(t *testing.T) {
	server := newOpenSandboxFailServer(t)
	defer server.Close()

	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-api-key")

	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	room := agent.Room{
		ID:          "os-fallback-room",
		WorkspaceID: workspace,
	}

	result, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:        true,
		SandboxRuntime: "opensandbox",
	})
	require.NoError(t, err, "provisionSandbox should succeed with fallback")
	require.NotNil(t, result)

	// Should have fallen back to worktree mode
	assert.Equal(t, "worktree", result["runtime"])
	assert.Equal(t, true, result["fallback"])
	assert.NotEmpty(t, result["fallback_reason"])
	assert.NotEmpty(t, result["worktree_path"])
	assert.NotEmpty(t, result["tmux_session"])

	require.NotNil(t, room.SandboxConfig)
	assert.Equal(t, "worktree", room.SandboxConfig.Runtime)
	assert.True(t, room.SandboxConfig.Fallback)
	assert.NotEmpty(t, room.SandboxConfig.WorktreePath)

	// Clean up
	if room.SandboxConfig.TmuxSession != "" {
		_ = exec.Command("tmux", "kill-session", "-t", room.SandboxConfig.TmuxSession).Run()
	}
}

// TestRoomOpenSandboxDestroy validates VAL-OS-006:
// room destroy cleans up OpenSandbox container.
func TestRoomOpenSandboxDestroy(t *testing.T) {
	server := newOpenSandboxMockServer(t)
	defer server.Close()

	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-api-key")

	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	// First create an OpenSandbox room
	room := agent.Room{
		ID:          "os-destroy-room",
		WorkspaceID: workspace,
	}
	result, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:        true,
		SandboxRuntime: "opensandbox",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, room.SandboxConfig)

	containerID := room.SandboxConfig.ContainerID
	require.NotEmpty(t, containerID)

	// Now clean up
	cleanupResult, err := cleanupSandbox(ctx, workspace, "os-destroy-room", room.SandboxConfig)
	require.NoError(t, err, "cleanupSandbox should succeed")

	assert.Equal(t, true, cleanupResult["container_deleted"], "container should be deleted")
	assert.Equal(t, true, cleanupResult["tmux_killed"], "tmux session should be killed")
	assert.Equal(t, "cleaned", cleanupResult["status"])
}

// TestRoomOpenSandboxDestroyAlreadyDeleted validates VAL-OS-007:
// room destroy handles already-deleted container gracefully.
func TestRoomOpenSandboxDestroyAlreadyDeleted(t *testing.T) {
	server := newOpenSandboxDeleteFailServer(t)
	defer server.Close()

	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-api-key")

	ctx := context.Background()
	workspace := t.TempDir()

	// Create a SandboxConfig that points to a "deleted" container
	sc := &agent.SandboxConfig{
		WorktreePath:      "/tmp/nonexistent-worktree-path-for-test",
		WorktreeBranch:    "sandbox/room-os-already-deleted",
		TmuxSession:       "agentctl-sandbox-os-already-deleted",
		TerminalURL:       "/terminal/os-already-deleted",
		Runtime:           "opensandbox",
		ContainerID:       "sbx-already-deleted-123",
		ContainerEndpoint: "execd-sbx-already-deleted-123.local:44772",
	}

	cleanupResult, err := cleanupSandbox(ctx, workspace, "os-already-deleted", sc)
	require.NoError(t, err, "cleanupSandbox should succeed for already-deleted container")

	assert.Equal(t, true, cleanupResult["container_deleted"],
		"container should be marked deleted when API returns 404")
	assert.Equal(t, true, cleanupResult["worktree_removed"],
		"worktree cleanup should succeed even for missing dir")
}

// TestRoomOpenSandboxConcurrent validates VAL-OS-008:
// Multiple concurrent OpenSandbox rooms with independent containers.
func TestRoomOpenSandboxConcurrent(t *testing.T) {
	server := newOpenSandboxMockServer(t)
	defer server.Close()

	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-api-key")

	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	rooms := []struct {
		id    string
		title string
	}{
		{"os-concurrent-1", "Concurrent Room 1"},
		{"os-concurrent-2", "Concurrent Room 2"},
	}

	var configs []*agent.SandboxConfig
	for _, r := range rooms {
		room := agent.Room{
			ID:          r.id,
			WorkspaceID: workspace,
			Title:       r.title,
		}
		result, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
			Sandbox:        true,
			SandboxRuntime: "opensandbox",
		})
		require.NoError(t, err, "provisionSandbox for %s should succeed", r.id)
		require.NotNil(t, result)
		require.NotNil(t, room.SandboxConfig)

		configs = append(configs, room.SandboxConfig)
		assert.NotEmpty(t, room.SandboxConfig.ContainerID, "container ID should be set for %s", r.id)
		assert.Equal(t, "opensandbox", room.SandboxConfig.Runtime)
	}

	// Verify independent container IDs
	require.Len(t, configs, 2)
	assert.NotEqual(t, configs[0].ContainerID, configs[1].ContainerID,
		"each room should have a unique container ID")

	// Clean up
	for _, sc := range configs {
		if sc.TmuxSession != "" {
			_ = exec.Command("tmux", "kill-session", "-t", sc.TmuxSession).Run()
		}
	}
}

// TestRoomOpenSandboxIdempotent validates that re-provisioning returns existing info.
func TestRoomOpenSandboxIdempotent(t *testing.T) {
	server := newOpenSandboxMockServer(t)
	defer server.Close()

	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-api-key")

	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	room := agent.Room{
		ID:          "os-idempotent-room",
		WorkspaceID: workspace,
	}

	// First provision
	result1, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:        true,
		SandboxRuntime: "opensandbox",
	})
	require.NoError(t, err)
	require.NotNil(t, result1)
	require.NotNil(t, room.SandboxConfig)
	containerID1 := room.SandboxConfig.ContainerID

	// Second provision (idempotent)
	result2, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:        true,
		SandboxRuntime: "opensandbox",
	})
	require.NoError(t, err)
	require.NotNil(t, result2)

	assert.Equal(t, "existing", result2["status"], "second provision should return existing")
	assert.Equal(t, containerID1, result2["container_id"],
		"should return same container ID")

	// Clean up
	if room.SandboxConfig.TmuxSession != "" {
		_ = exec.Command("tmux", "kill-session", "-t", room.SandboxConfig.TmuxSession).Run()
	}
}

// TestRoomOpenSandboxCommandIntegration validates that the full CLI command
// flow works with --sandbox --sandbox-runtime opensandbox.
func TestRoomOpenSandboxCommandIntegration(t *testing.T) {
	server := newOpenSandboxMockServer(t)
	defer server.Close()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-api-key")

	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	cmd, out := newRoomTestCommand(ctx)
	err := runRoomCreateWithProvision(cmd, workspace, "os-cli-room", "CLI Test Room", "", nil, roomCreateProvisionOptions{
		Sandbox:        true,
		SandboxRuntime: "opensandbox",
		SandboxTTL:     60 * time.Minute,
		SandboxCPU:     "500m",
		SandboxMemory:  "512Mi",
	})
	require.NoError(t, err, "runRoomCreateWithProvision should succeed")

	// Parse the output envelope
	env := decodeRoomEnvelopeAny(t, out)

	// Check envelope structure
	assert.Equal(t, "ok", env["status"])

	data, ok := env["data"].(map[string]any)
	require.True(t, ok, "envelope data should be a map")

	// Sandbox info may be nil if the command didn't have --sandbox set properly
	// The CLI integration test validates the command path works end-to-end
	// For more granular testing, see TestRoomOpenSandboxCreate which tests provisionSandbox directly

	// Verify the room was created
	roomData, ok := data["room"].(map[string]any)
	require.True(t, ok, "room should be in response")

	// Verify sandbox info is present
	sandbox, ok := data["sandbox"].(map[string]any)
	require.True(t, ok, "sandbox info should be present in response")
	assert.Equal(t, "opensandbox", sandbox["runtime"])
	assert.NotEmpty(t, sandbox["container_id"])

	// Verify sandbox config on room
	sandboxConfig, ok := roomData["sandbox_config"].(map[string]any)
	require.True(t, ok, "sandbox_config should be on room")
	assert.Equal(t, "opensandbox", sandboxConfig["runtime"])
}

// TestBuildRoomSandboxInfoWithOpenSandbox validates that buildRoomSandboxInfo
// includes container metadata for OpenSandbox rooms (VAL-OS-009).
func TestBuildRoomSandboxInfoWithOpenSandbox(t *testing.T) {
	room := agent.RoomSummary{
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:       "/tmp/worktree",
			WorktreeBranch:     "sandbox/room-test",
			TmuxSession:        "agentctl-sandbox-test",
			TerminalURL:        "/terminal/test",
			Runtime:            "opensandbox",
			ContainerID:        "sbx-abc123",
			ContainerEndpoint:  "execd-sbx-abc123.local:44772",
			ContainerExpiresAt: "2026-04-10T12:00:00Z",
			ContainerCPU:       "500m",
			ContainerMemory:    "512Mi",
		},
	}

	info := buildRoomSandboxInfo(room)
	require.NotNil(t, info)

	assert.Equal(t, "opensandbox", info["runtime"])
	assert.Equal(t, "sbx-abc123", info["container_id"])
	assert.Equal(t, "execd-sbx-abc123.local:44772", info["container_endpoint"])
	assert.Equal(t, "2026-04-10T12:00:00Z", info["container_expires_at"])
	assert.Equal(t, "500m", info["container_cpu"])
	assert.Equal(t, "512Mi", info["container_memory"])
	assert.Equal(t, "/tmp/worktree", info["worktree_path"])
	assert.Equal(t, "/terminal/test", info["terminal_url"])
}

// TestBuildRoomSandboxInfoWithFallback validates that buildRoomSandboxInfo
// includes fallback flag.
func TestBuildRoomSandboxInfoWithFallback(t *testing.T) {
	room := agent.RoomSummary{
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath: "/tmp/worktree",
			Runtime:      "worktree",
			Fallback:     true,
		},
	}

	info := buildRoomSandboxInfo(room)
	require.NotNil(t, info)
	assert.Equal(t, true, info["fallback"])
	assert.Equal(t, "worktree", info["runtime"])
}

// TestBuildRoomSandboxInfoWorktreeNoContainer validates that worktree-only rooms
// don't include container fields.
func TestBuildRoomSandboxInfoWorktreeNoContainer(t *testing.T) {
	room := agent.RoomSummary{
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:   "/tmp/worktree",
			WorktreeBranch: "sandbox/room-test",
			TmuxSession:    "agentctl-sandbox-test",
			TerminalURL:    "/terminal/test",
			Runtime:        "worktree",
		},
	}

	info := buildRoomSandboxInfo(room)
	require.NotNil(t, info)

	assert.Equal(t, "worktree", info["runtime"])
	assert.Nil(t, info["container_id"], "worktree rooms should not have container_id")
}

// TestRoomOpenSandboxDestroyCommandIntegration validates the full CLI destroy flow
// for an OpenSandbox room.
func TestRoomOpenSandboxDestroyCommandIntegration(t *testing.T) {
	server := newOpenSandboxMockServer(t)
	defer server.Close()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-api-key")

	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	// Create the room via CLI
	createCmd, _ := newRoomTestCommand(ctx)
	err := runRoomCreateWithProvision(createCmd, workspace, "os-destroy-cli", "Destroy CLI Test", "", nil, roomCreateProvisionOptions{
		Sandbox:        true,
		SandboxRuntime: "opensandbox",
	})
	require.NoError(t, err)

	// Now destroy it
	destroyCmd, destroyOut := newRoomTestCommand(ctx)
	err = runRoomDestroy(destroyCmd, workspace, "os-destroy-cli", false)
	require.NoError(t, err, "runRoomDestroy should succeed")

	// Parse destroy output
	env := decodeRoomEnvelopeAny(t, destroyOut)
	data, ok := env["data"].(map[string]any)
	require.True(t, ok)

	assert.Equal(t, "os-destroy-cli", data["room_id"])
	assert.Equal(t, "destroyed", data["status"])

	cleanup, ok := data["sandbox_cleanup"].(map[string]any)
	require.True(t, ok, "sandbox_cleanup should be present")
	assert.Equal(t, true, cleanup["container_deleted"], "container should be deleted")
}

// TestSandboxConfigIsSandboxWithContainerID validates that IsSandbox
// returns true when only ContainerID is set (no WorktreePath).
func TestSandboxConfigIsSandboxWithContainerID(t *testing.T) {
	sc := &agent.SandboxConfig{
		ContainerID: "sbx-123",
		Runtime:     "opensandbox",
	}
	assert.True(t, sc.IsSandbox(), "IsSandbox should be true with ContainerID")

	sc2 := &agent.SandboxConfig{
		WorktreePath: "/tmp/worktree",
		Runtime:      "worktree",
	}
	assert.True(t, sc2.IsSandbox(), "IsSandbox should be true with WorktreePath")

	sc3 := &agent.SandboxConfig{
		ContainerID:  "sbx-456",
		WorktreePath: "/tmp/worktree",
		Runtime:      "opensandbox",
	}
	assert.True(t, sc3.IsSandbox(), "IsSandbox should be true with both")
}

// TestOpenSandboxRoomPersistence validates that OpenSandbox sandbox config
// persists through board store round-trip.
func TestOpenSandboxRoomPersistence(t *testing.T) {
	ctx := context.Background()
	store, err := blackboard.OpenBoardStore(ctx, t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	workspace := "/tmp/test-workspace"

	room := agent.Room{
		ID:          "os-persist-room",
		WorkspaceID: workspace,
		Title:       "Persistence Test",
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:       "/tmp/worktree",
			WorktreeBranch:     "sandbox/room-os-persist-room",
			TmuxSession:        "agentctl-sandbox-os-persist-room",
			TerminalURL:        "/terminal/os-persist-room",
			Runtime:            "opensandbox",
			ContainerID:        "sbx-persist-123",
			ContainerEndpoint:  "execd-sbx-persist-123.local:44772",
			ContainerExpiresAt: "2026-04-10T12:00:00Z",
			ContainerCPU:       "500m",
			ContainerMemory:    "512Mi",
		},
	}

	// Upsert
	upserted, err := store.UpsertRoom(ctx, room)
	require.NoError(t, err)
	require.NotNil(t, upserted.SandboxConfig)
	assert.Equal(t, "sbx-persist-123", upserted.SandboxConfig.ContainerID)

	// GetRoom
	summary, err := store.GetRoom(ctx, workspace, "os-persist-room", "")
	require.NoError(t, err)
	require.NotNil(t, summary.SandboxConfig, "SandboxConfig should survive round-trip")
	assert.Equal(t, "sbx-persist-123", summary.SandboxConfig.ContainerID)
	assert.Equal(t, "execd-sbx-persist-123.local:44772", summary.SandboxConfig.ContainerEndpoint)
	assert.Equal(t, "500m", summary.SandboxConfig.ContainerCPU)
	assert.Equal(t, "512Mi", summary.SandboxConfig.ContainerMemory)
	assert.Equal(t, "opensandbox", summary.SandboxConfig.Runtime)

	// ListRooms
	rooms, err := store.ListRooms(ctx, workspace, "", 10, false)
	require.NoError(t, err)
	require.Len(t, rooms, 1)
	require.NotNil(t, rooms[0].SandboxConfig)
	assert.Equal(t, "sbx-persist-123", rooms[0].SandboxConfig.ContainerID)
}

// TestRoomOpenSandboxDestroyNonSandbox validates that destroying a non-sandbox
// room does not attempt container cleanup.
func TestRoomOpenSandboxDestroyNonSandbox(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	err := runRoomCreateWithProvision(cmd, workspace, "os-non-sandbox", "Non-Sandbox Room", "", nil, roomCreateProvisionOptions{})
	require.NoError(t, err)

	// Destroy should work without sandbox cleanup
	destroyCmd, destroyOut := newRoomTestCommand(ctx)
	err = runRoomDestroy(destroyCmd, workspace, "os-non-sandbox", false)
	require.NoError(t, err)

	env := decodeRoomEnvelopeAny(t, destroyOut)
	data, ok := env["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "destroyed", data["status"])
	// sandbox_cleanup should be nil for non-sandbox rooms
	assert.Nil(t, data["sandbox_cleanup"])
}

// TestRoomOpenSandboxFullCLIWithEnvelope verifies the complete CLI flow produces
// a valid JSON envelope with all OpenSandbox fields.
func TestRoomOpenSandboxFullCLIWithEnvelope(t *testing.T) {
	server := newOpenSandboxMockServer(t)
	defer server.Close()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-api-key")

	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	cmd, out := newRoomTestCommand(ctx)
	err := runRoomCreateWithProvision(cmd, workspace, "os-envelope-room", "Envelope Test", "", nil, roomCreateProvisionOptions{
		Sandbox:        true,
		SandboxRuntime: "opensandbox",
		SandboxTTL:     45 * time.Minute,
		SandboxCPU:     "250m",
		SandboxMemory:  "256Mi",
	})
	require.NoError(t, err)

	// Verify it's valid JSON
	var raw map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &raw))

	// Verify envelope structure
	assert.Equal(t, float64(1), raw["version"])
	assert.Equal(t, "ok", raw["status"])
	assert.Equal(t, "agentctl.room.create", raw["command"])

	data := raw["data"].(map[string]any)
	sandbox := data["sandbox"].(map[string]any)
	assert.Equal(t, "opensandbox", sandbox["runtime"])
	assert.Equal(t, "created", sandbox["status"])
	assert.Equal(t, "250m", sandbox["container_cpu"])
	assert.Equal(t, "256Mi", sandbox["container_memory"])
	assert.NotEmpty(t, sandbox["container_id"])
	assert.NotEmpty(t, sandbox["container_endpoint"])
	assert.NotEmpty(t, sandbox["container_expires_at"])

	// Clean up
	room := data["room"].(map[string]any)
	if sc, ok := room["sandbox_config"].(map[string]any); ok {
		if session, ok := sc["tmux_session"].(string); ok && session != "" {
			_ = exec.Command("tmux", "kill-session", "-t", session).Run()
		}
	}
}

// TestRoomOpenSandboxCreateDefaults validates default values when
// no explicit TTL/CPU/memory flags are provided.
func TestRoomOpenSandboxCreateDefaults(t *testing.T) {
	server := newOpenSandboxMockServer(t)
	defer server.Close()

	t.Setenv("OPEN_SANDBOX_BASE_URL", server.URL)
	t.Setenv("OPEN_SANDBOX_API_KEY", "test-api-key")

	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	room := agent.Room{
		ID:          "os-defaults-room",
		WorkspaceID: workspace,
	}

	result, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:        true,
		SandboxRuntime: "opensandbox",
		// No TTL, CPU, or memory specified — should use defaults
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Default CPU and memory
	assert.Equal(t, "1000m", result["container_cpu"], "default CPU should be 1000m")
	assert.Equal(t, "1Gi", result["container_memory"], "default memory should be 1Gi")

	require.NotNil(t, room.SandboxConfig)
	assert.Equal(t, "1000m", room.SandboxConfig.ContainerCPU)
	assert.Equal(t, "1Gi", room.SandboxConfig.ContainerMemory)

	// Default TTL should be 60 minutes
	expiresAtStr := result["container_expires_at"].(string)
	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	require.NoError(t, err)
	expectedExpiry := time.Now().UTC().Add(60 * time.Minute)
	assert.WithinDuration(t, expectedExpiry, expiresAt, 10*time.Second)

	// Clean up
	if room.SandboxConfig.TmuxSession != "" {
		_ = exec.Command("tmux", "kill-session", "-t", room.SandboxConfig.TmuxSession).Run()
	}
}

// decodeRoomEnvelope is a helper that also works with error envelopes.
func decodeRoomEnvelopeAny(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, buf.String())
	}
	return raw
}
