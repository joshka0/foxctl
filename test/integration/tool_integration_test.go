// Package integration contains integration tests for agentctl subsystems.
package integration

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	agenttools "github.com/jkatigb/agentctl/internal/agent/tools"
	"github.com/jkatigb/agentctl/internal/agent/types"
)

// D3 Integration Tests: Tool workflow tests per PR5 spec
// These tests verify the retrieval funnel workflow:
// code.symbol_search → code.swe_grep → edit tools

// mockRecorder captures tool calls for verification.
type mockRecorder struct {
	mu    sync.Mutex
	calls []types.ToolCall
}

func (r *mockRecorder) RecordToolCall(call types.ToolCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *mockRecorder) GetCalls() []types.ToolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]types.ToolCall, len(r.calls))
	copy(result, r.calls)
	return result
}

func (r *mockRecorder) GetToolNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, len(r.calls))
	for i, c := range r.calls {
		names[i] = c.ToolName
	}
	return names
}

// callTool is a helper that gets a tool by name and executes it.
func callTool(t *testing.T, registry *agenttools.Registry, name string, params map[string]any) (any, error) {
	t.Helper()
	tool, err := registry.GetRegistry().Get(name)
	if err != nil {
		return nil, err
	}
	return tool.Execute(context.Background(), params)
}

// TestToolIntegration_RetrievalFunnelWorkflow tests the full retrieval workflow.
// This is a D3 integration test per PR5 spec.
func TestToolIntegration_RetrievalFunnelWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create workspace with test files
	workspaceDir := t.TempDir()
	setupTestWorkspace(t, workspaceDir)

	recorder := &mockRecorder{}
	cfg := agenttools.Config{
		WorkspaceRoot:    workspaceDir,
		WorkspaceID:      "test-integration",
		MaxSearchResults: 50,
	}

	registry, err := agenttools.NewRegistry(cfg, recorder)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Step 1: Use code.search to find code (ripgrep-based)
	_, err = callTool(t, registry, "code.search", map[string]any{
		"pattern": "Login",
		"path":    ".",
	})
	if err != nil {
		t.Logf("code.search failed (expected if rg not available): %v", err)
	}

	// Step 2: Use code.symbol_search (stub returns empty but validates flow)
	_, err = callTool(t, registry, "code.symbol_search", map[string]any{
		"workspace_id": "test-integration",
		"question":     "How does user login work?",
		"max_results":  10,
	})
	if err != nil {
		t.Fatalf("code.symbol_search failed: %v", err)
	}

	// Step 3: Read file for context
	_, err = callTool(t, registry, "fs.read_file", map[string]any{
		"path": "auth/login.go",
	})
	if err != nil {
		t.Fatalf("fs.read_file failed: %v", err)
	}

	// Step 4: Apply a simple edit
	_, err = callTool(t, registry, "edit.apply_patch", map[string]any{
		"path":     "auth/login.go",
		"old_text": "// Login handles user authentication.",
		"new_text": "// Login handles user authentication and session creation.",
	})
	if err != nil {
		t.Fatalf("edit.apply_patch failed: %v", err)
	}

	// Verify telemetry recorded all tool calls
	calls := recorder.GetCalls()
	if len(calls) < 4 {
		t.Errorf("expected at least 4 tool calls recorded, got %d", len(calls))
	}

	toolNames := recorder.GetToolNames()
	expectedTools := map[string]bool{
		"code.search":        false,
		"code.symbol_search": false,
		"fs.read_file":       false,
		"edit.apply_patch":   false,
	}

	for _, name := range toolNames {
		if _, exists := expectedTools[name]; exists {
			expectedTools[name] = true
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("expected tool %q to be recorded in telemetry", name)
		}
	}
}

// TestToolIntegration_StructuredDiffWorkflow tests the structured diff workflow.
func TestToolIntegration_StructuredDiffWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	workspaceDir := t.TempDir()
	setupTestWorkspace(t, workspaceDir)

	recorder := &mockRecorder{}
	cfg := agenttools.Config{
		WorkspaceRoot:    workspaceDir,
		WorkspaceID:      "test-integration",
		MaxSearchResults: 50,
	}

	registry, err := agenttools.NewRegistry(cfg, recorder)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Apply a structured diff (line 8 is the comment in test file)
	_, err = callTool(t, registry, "edit.apply_structured_diff", map[string]any{
		"path": "auth/login.go",
		"diff_json": map[string]any{
			"hunks": []any{
				map[string]any{
					"old_start": 8,
					"old_lines": 1,
					"new_start": 8,
					"new_lines": 2,
					"lines": []any{
						" // Login handles user authentication.",
						"+// It validates credentials and creates a session.",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("edit.apply_structured_diff failed: %v", err)
	}

	// Verify the file was modified
	content, err := os.ReadFile(filepath.Join(workspaceDir, "auth/login.go"))
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}

	if !contains(string(content), "validates credentials") {
		t.Error("expected file to contain 'validates credentials' after edit")
	}

	// Verify telemetry
	calls := recorder.GetCalls()
	foundDiffTool := false
	for _, c := range calls {
		if c.ToolName == "edit.apply_structured_diff" {
			foundDiffTool = true
			break
		}
	}
	if !foundDiffTool {
		t.Error("expected edit.apply_structured_diff to be recorded in telemetry")
	}
}

// TestToolIntegration_DryRunMode tests dry-run mode for structured diff.
func TestToolIntegration_DryRunMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	workspaceDir := t.TempDir()
	setupTestWorkspace(t, workspaceDir)

	cfg := agenttools.Config{
		WorkspaceRoot:    workspaceDir,
		MaxSearchResults: 50,
	}

	registry, err := agenttools.NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Read original content
	originalContent, err := os.ReadFile(filepath.Join(workspaceDir, "auth/login.go"))
	if err != nil {
		t.Fatalf("read original file: %v", err)
	}

	// Apply diff in dry-run mode (line 8 is the comment in test file)
	_, err = callTool(t, registry, "edit.apply_structured_diff", map[string]any{
		"path":    "auth/login.go",
		"dry_run": true,
		"diff_json": map[string]any{
			"hunks": []any{
				map[string]any{
					"old_start": 8,
					"old_lines": 1,
					"new_start": 8,
					"new_lines": 1,
					"lines": []any{
						"-// Login handles user authentication.",
						"+// MODIFIED: Login handles user authentication.",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("edit.apply_structured_diff (dry_run) failed: %v", err)
	}

	// Verify file was NOT modified
	afterContent, err := os.ReadFile(filepath.Join(workspaceDir, "auth/login.go"))
	if err != nil {
		t.Fatalf("read file after dry-run: %v", err)
	}

	if string(afterContent) != string(originalContent) {
		t.Error("file was modified during dry-run mode")
	}
}

// setupTestWorkspace creates test files for integration tests.
func setupTestWorkspace(t *testing.T, dir string) {
	t.Helper()

	files := map[string]string{
		"auth/login.go": `package auth

import (
	"context"
	"fmt"
)

// Login handles user authentication.
func Login(ctx context.Context, username, password string) error {
	if username == "" || password == "" {
		return fmt.Errorf("invalid credentials")
	}
	return nil
}
`,
		"auth/logout.go": `package auth

import "context"

// Logout terminates the user session.
func Logout(ctx context.Context) error {
	return nil
}
`,
		"config/config.go": `package config

// Config holds application configuration.
type Config struct {
	Port int
	Host string
}
`,
	}

	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
