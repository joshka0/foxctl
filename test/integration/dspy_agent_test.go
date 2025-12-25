//go:build integration

// Package integration contains integration tests for the dspy-go agent.
// Run with: go test -tags=integration -v ./test/integration/...
// Requires: AGENTCTL_LLM_API_KEY environment variable set
package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/runtime"
	"github.com/jkatigb/agentctl/internal/agent/types"
)

func TestDspyAgentFileWrite(t *testing.T) {
	apiKey := os.Getenv("AGENTCTL_LLM_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY") // Fallback
	}
	if apiKey == "" {
		t.Skip("AGENTCTL_LLM_API_KEY or GEMINI_API_KEY not set, skipping integration test")
	}

	// Create temp workspace
	workspace, err := os.MkdirTemp("", "dspy-agent-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(workspace)

	t.Logf("Workspace: %s", workspace)

	// Create runtime - use empty model to let runtime pick default (gemini-2.5-flash)
	rt := runtime.NewRuntime(runtime.Config{
		DefaultMaxIterations: 10,
		DefaultTimeout:       5 * time.Minute,
		LLMProvider:          "gemini",
		LLMModel:             "", // Let runtime use default (gemini-2.5-flash)
		LLMAPIKey:            apiKey,
		WorkspaceRoot:        workspace,
	})

	// Spawn agent with task to write files
	cfg := types.AgentConfig{
		Role:          types.RoleCoder,
		ActorID:       "test-agent-1",
		WorkspaceID:   workspace,
		TaskID:        "TEST-001",
		MaxIterations: 5,
		Timeout:       types.Duration(2 * time.Minute),
		LLMAPIKey:     apiKey,
	}

	ctx := context.Background()
	session, err := rt.Spawn(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to spawn agent: %v", err)
	}

	t.Logf("Spawned session: %s", session.ID)

	// Wait for session to complete (or timeout)
	timeout := time.After(3 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			rt.Kill(session.ID)
			t.Fatal("test timed out waiting for agent to complete")
		case <-ticker.C:
			sess := session.GetSession()
			t.Logf("Session status: %s, iterations: %d", sess.Status, sess.Iterations)

			if sess.Status == types.StatusOK || sess.Status == types.StatusError {
				if sess.Status == types.StatusError {
					t.Logf("Agent error: %s", sess.Error)
				}
				if sess.Summary != "" {
					t.Logf("Agent summary: %s", sess.Summary)
				}

				// Check for created files
				file1 := filepath.Join(workspace, "file1.txt")
				file2 := filepath.Join(workspace, "file2.txt")

				if _, err := os.Stat(file1); err == nil {
					content, _ := os.ReadFile(file1)
					t.Logf("file1.txt created: %s", string(content))
				} else {
					t.Logf("file1.txt not created")
				}

				if _, err := os.Stat(file2); err == nil {
					content, _ := os.ReadFile(file2)
					t.Logf("file2.txt created: %s", string(content))
				} else {
					t.Logf("file2.txt not created")
				}

				// Log tool calls
				toolCalls := session.GetToolCalls()
				t.Logf("Tool calls: %d", len(toolCalls))
				for i, tc := range toolCalls {
					t.Logf("  [%d] %s (duration: %s, error: %s)", i, tc.ToolName, tc.Duration, tc.Error)
				}

				return
			}
		}
	}
}
