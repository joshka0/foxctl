//go:build integration

// Package integration provides integration tests for the foxctl dspy-go agent.
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/agent/runtime"
	"github.com/joshka0/foxctl/internal/agent/types"
)

// TestHierarchySpawn tests the full overseer -> agent spawning flow.
// This test requires GEMINI_API_KEY or FOXCTL_LLM_API_KEY to be set.
func TestHierarchySpawn(t *testing.T) {
	apiKey := os.Getenv("FOXCTL_LLM_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		t.Skip("Skipping: GEMINI_API_KEY or FOXCTL_LLM_API_KEY not set")
	}

	tmpDir := t.TempDir()

	// Create runtime with overseer
	rt := runtime.NewRuntime(runtime.Config{
		DefaultMaxIterations: 5,
		DefaultTimeout:       2 * time.Minute,
		LLMProvider:          "gemini",
		LLMModel:             "gemini-2.5-flash",
		LLMAPIKey:            apiKey,
		WorkspaceRoot:        tmpDir,
	})

	overseer := runtime.NewOverseer(rt, runtime.OverseerConfig{
		MaxDepth:            3, // Allow overseer -> agent -> subagent
		MaxConcurrentAgents: 5,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Spawn the overseer agent
	session, err := overseer.SpawnOverseerAgent(ctx, "test-epic", "Coordinate a test hierarchy")
	if err != nil {
		t.Fatalf("Failed to spawn overseer: %v", err)
	}

	t.Logf("Overseer spawned: session=%s, actor=%s, depth=%d",
		session.ID, session.Config.ActorID, session.Config.Depth)

	// Verify overseer hierarchy config
	if session.Config.Depth != 0 {
		t.Errorf("Overseer depth = %d, want 0", session.Config.Depth)
	}
	if session.Config.MaxDepth != 3 {
		t.Errorf("Overseer MaxDepth = %d, want 3", session.Config.MaxDepth)
	}
	if session.Config.ActorID != runtime.OverseerActorID {
		t.Errorf("Overseer ActorID = %q, want %q", session.Config.ActorID, runtime.OverseerActorID)
	}

	// Test spawning a child agent via SpawnRequest
	req := types.SpawnRequest{
		EpicID:              "test-epic",
		CallerActorID:       session.Config.ActorID,
		CallerDepth:         session.Config.Depth,
		CallerMaxDepth:      session.Config.MaxDepth,
		CallerLocalMaxDepth: session.Config.LocalMaxDepth,
		RequestedSubagents: []types.SubagentRequest{
			{
				Role:             types.RoleCoder,
				Task:             "Write a hello world function",
				SuggestedActorID: "actor:agent:coder:test",
				LocalMaxDepth:    2,
			},
		},
	}

	resp, err := overseer.HandleSpawnRequest(ctx, req)
	if err != nil {
		t.Fatalf("HandleSpawnRequest failed: %v", err)
	}

	t.Logf("Spawn response: accepted=%v, spawned=%d, denied=%d",
		resp.Accepted, len(resp.SpawnedAgents), len(resp.DeniedAgents))

	if !resp.Accepted {
		t.Fatalf("Spawn request denied: %s", resp.Reason)
	}

	if len(resp.SpawnedAgents) != 1 {
		t.Fatalf("Expected 1 spawned agent, got %d", len(resp.SpawnedAgents))
	}

	childAgent := resp.SpawnedAgents[0]
	t.Logf("Child agent spawned: session=%s, actor=%s, depth=%d",
		childAgent.SessionID, childAgent.ActorID, childAgent.Depth)

	// Verify child depth
	if childAgent.Depth != 1 {
		t.Errorf("Child depth = %d, want 1", childAgent.Depth)
	}

	// Verify hierarchy
	hierarchy := overseer.GetHierarchy(session.ID)
	if hierarchy == nil {
		t.Fatal("GetHierarchy returned nil")
	}

	t.Logf("Hierarchy: root=%s (depth=%d), children=%d",
		hierarchy.ActorID, hierarchy.Depth, len(hierarchy.Children))

	if len(hierarchy.Children) != 1 {
		t.Errorf("Expected 1 child in hierarchy, got %d", len(hierarchy.Children))
	}

	// Test depth limit enforcement - try to spawn from the child at depth limit
	// Request from depth 2 with LocalMaxDepth 2 should be denied
	req2 := types.SpawnRequest{
		EpicID:              "test-epic",
		CallerActorID:       childAgent.ActorID,
		CallerDepth:         2, // Simulating depth 2
		CallerMaxDepth:      3,
		CallerLocalMaxDepth: 2, // At limit
		RequestedSubagents: []types.SubagentRequest{
			{
				Role: types.RoleCoder,
				Task: "This should be denied",
			},
		},
	}

	resp2, err := overseer.HandleSpawnRequest(ctx, req2)
	if err != nil {
		t.Fatalf("HandleSpawnRequest failed: %v", err)
	}

	if resp2.Accepted {
		t.Error("Expected spawn at depth limit to be denied")
	}
	if len(resp2.DeniedAgents) != 1 {
		t.Errorf("Expected 1 denied agent, got %d", len(resp2.DeniedAgents))
	} else {
		t.Logf("Correctly denied: %s", resp2.DeniedAgents[0].Reason)
	}

	// Kill all sessions - cleanup errors are not actionable.
	_ = rt.Kill(session.ID)           //nolint:errcheck
	_ = rt.Kill(childAgent.SessionID) //nolint:errcheck

	t.Log("Hierarchy spawn test completed successfully")
}

// TestOverseerConcurrencyLimit tests the concurrent agent limit.
func TestOverseerConcurrencyLimit(t *testing.T) {
	apiKey := os.Getenv("FOXCTL_LLM_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		t.Skip("Skipping: GEMINI_API_KEY or FOXCTL_LLM_API_KEY not set")
	}

	tmpDir := t.TempDir()

	rt := runtime.NewRuntime(runtime.Config{
		DefaultMaxIterations: 5,
		DefaultTimeout:       2 * time.Minute,
		LLMProvider:          "gemini",
		LLMModel:             "gemini-2.5-flash",
		LLMAPIKey:            apiKey,
		WorkspaceRoot:        tmpDir,
	})

	overseer := runtime.NewOverseer(rt, runtime.OverseerConfig{
		MaxDepth:            3,
		MaxConcurrentAgents: 2, // Very low limit for testing
	})

	ctx := context.Background()

	// Spawn overseer (counts as 1)
	session, err := overseer.SpawnOverseerAgent(ctx, "test-epic", "Test concurrency limits")
	if err != nil {
		t.Fatalf("Failed to spawn overseer: %v", err)
	}
	defer func() {
		// Cleanup error is not actionable.
		_ = rt.Kill(session.ID) //nolint:errcheck
	}()

	// Spawn first child (counts as 2)
	req1 := types.SpawnRequest{
		EpicID:              "test-epic",
		CallerActorID:       session.Config.ActorID,
		CallerDepth:         0,
		CallerMaxDepth:      3,
		CallerLocalMaxDepth: 3,
		RequestedSubagents: []types.SubagentRequest{
			{Role: types.RoleCoder, Task: "task 1"},
		},
	}

	resp1, err := overseer.HandleSpawnRequest(ctx, req1)
	if err != nil {
		t.Fatalf("HandleSpawnRequest: %v", err)
	}
	if !resp1.Accepted {
		t.Fatalf("First spawn should succeed: %s", resp1.Reason)
	}
	defer func() {
		// Cleanup error is not actionable.
		_ = rt.Kill(resp1.SpawnedAgents[0].SessionID) //nolint:errcheck
	}()

	// Try to spawn another (should hit limit)
	req2 := types.SpawnRequest{
		EpicID:              "test-epic",
		CallerActorID:       session.Config.ActorID,
		CallerDepth:         0,
		CallerMaxDepth:      3,
		CallerLocalMaxDepth: 3,
		RequestedSubagents: []types.SubagentRequest{
			{Role: types.RoleCoder, Task: "task 2"},
		},
	}

	resp2, err := overseer.HandleSpawnRequest(ctx, req2)
	if err != nil {
		t.Fatalf("HandleSpawnRequest: %v", err)
	}
	if resp2.Accepted {
		t.Error("Second spawn should be denied due to concurrency limit")
		if len(resp2.SpawnedAgents) > 0 {
			// Cleanup error is not actionable.
			_ = rt.Kill(resp2.SpawnedAgents[0].SessionID) //nolint:errcheck
		}
	} else {
		t.Logf("Correctly denied due to concurrency: %s", resp2.Reason)
	}
}
