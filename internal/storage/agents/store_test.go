package agents

import (
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
)

func TestAgentStore(t *testing.T) {
	ctx := context.Background()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	// Open store
	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer func() { _ = store.Close() }() //nolint:errcheck

	// Test Create
	t.Run("Create", func(t *testing.T) {
		a := agent.Agent{
			ID:          "test-agent-001",
			ParentID:    "",
			Namespace:   "org/test",
			Role:        "test-role",
			Prompt:      "You are a test agent",
			SkillsAllow: []string{"test/skill"},
			Policy: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
				Network:  "none",
			},
			ShareBB:     "scoped",
			State:       agent.StateStarting,
			CreatedAt:   time.Now().UTC(),
			HeartbeatAt: time.Now().UTC(),
		}

		if err := store.Create(ctx, a); err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
	})

	// Test Get
	t.Run("Get", func(t *testing.T) {
		a, err := store.Get(ctx, "test-agent-001")
		if err != nil {
			t.Fatalf("failed to get agent: %v", err)
		}

		if a.ID != "test-agent-001" {
			t.Errorf("expected ID test-agent-001, got %s", a.ID)
		}
		if a.Role != "test-role" {
			t.Errorf("expected role test-role, got %s", a.Role)
		}
	})

	// Test GetByNamespace
	t.Run("GetByNamespace", func(t *testing.T) {
		a, err := store.GetByNamespace(ctx, "org/test")
		if err != nil {
			t.Fatalf("failed to get agent by namespace: %v", err)
		}

		if a.ID != "test-agent-001" {
			t.Errorf("expected ID test-agent-001, got %s", a.ID)
		}
	})

	// Test UpdateState
	t.Run("UpdateState", func(t *testing.T) {
		if err := store.UpdateState(ctx, "test-agent-001", agent.StateRunning); err != nil {
			t.Fatalf("failed to update state: %v", err)
		}

		a, err := store.Get(ctx, "test-agent-001")
		if err != nil {
			t.Fatalf("failed to get agent: %v", err)
		}

		if a.State != agent.StateRunning {
			t.Errorf("expected state running, got %s", a.State)
		}
	})

	// Test UpdateHeartbeat
	t.Run("UpdateHeartbeat", func(t *testing.T) {
		time.Sleep(100 * time.Millisecond)

		oldHeartbeat, err := store.Get(ctx, "test-agent-001")
		if err != nil {
			t.Fatalf("failed to get agent: %v", err)
		}

		if err := store.UpdateHeartbeat(ctx, "test-agent-001"); err != nil {
			t.Fatalf("failed to update heartbeat: %v", err)
		}

		a, err := store.Get(ctx, "test-agent-001")
		if err != nil {
			t.Fatalf("failed to get agent: %v", err)
		}

		if !a.HeartbeatAt.After(oldHeartbeat.HeartbeatAt) {
			t.Errorf("heartbeat should be updated")
		}
	})

	// Test List
	t.Run("List", func(t *testing.T) {
		agents, err := store.List(ctx, 10)
		if err != nil {
			t.Fatalf("failed to list agents: %v", err)
		}

		if len(agents) != 1 {
			t.Errorf("expected 1 agent, got %d", len(agents))
		}
	})

	// Test Delete
	t.Run("Delete", func(t *testing.T) {
		if err := store.Delete(ctx, "test-agent-001"); err != nil {
			t.Fatalf("failed to delete agent: %v", err)
		}

		_, err := store.Get(ctx, "test-agent-001")
		if err == nil {
			t.Errorf("expected error getting deleted agent")
		}
	})
}
