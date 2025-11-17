package quotas

import (
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
)

func TestQuotasStore(t *testing.T) {
	ctx := context.Background()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	// Open store
	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer func() { _ = store.Close() }() //nolint:errcheck

	// Test Set
	t.Run("Set", func(t *testing.T) {
		quotas := agent.Quotas{
			Namespace:         "org/team/project",
			MaxConcurrentJobs: 10,
			CPULimit:          4000,
			MemMBLimit:        8192,
			LLMCallsPerMin:    100,
			EgressBytesPerMin: 1048576,
		}

		if err := store.Set(ctx, "org/team/project", quotas); err != nil {
			t.Fatalf("failed to set quotas: %v", err)
		}
	})

	// Test Get
	t.Run("Get", func(t *testing.T) {
		quotas, err := store.Get(ctx, "org/team/project")
		if err != nil {
			t.Fatalf("failed to get quotas: %v", err)
		}

		if quotas.Namespace != "org/team/project" {
			t.Errorf("expected namespace org/team/project, got %s", quotas.Namespace)
		}
		if quotas.MaxConcurrentJobs != 10 {
			t.Errorf("expected max_concurrent_jobs 10, got %d", quotas.MaxConcurrentJobs)
		}
		if quotas.CPULimit != 4000 {
			t.Errorf("expected cpu_limit 4000, got %d", quotas.CPULimit)
		}
		if quotas.MemMBLimit != 8192 {
			t.Errorf("expected memMB_limit 8192, got %d", quotas.MemMBLimit)
		}
		if quotas.LLMCallsPerMin != 100 {
			t.Errorf("expected llm_calls_per_min 100, got %d", quotas.LLMCallsPerMin)
		}
		if quotas.EgressBytesPerMin != 1048576 {
			t.Errorf("expected egress_bytes_per_min 1048576, got %d", quotas.EgressBytesPerMin)
		}
	})

	// Test Get not found
	t.Run("GetNotFound", func(t *testing.T) {
		_, err := store.Get(ctx, "nonexistent/namespace")
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	// Test Update
	t.Run("Update", func(t *testing.T) {
		quotas := agent.Quotas{
			Namespace:         "org/team/project",
			MaxConcurrentJobs: 20,
			CPULimit:          8000,
			MemMBLimit:        16384,
			LLMCallsPerMin:    200,
			EgressBytesPerMin: 2097152,
		}

		if err := store.Update(ctx, "org/team/project", quotas); err != nil {
			t.Fatalf("failed to update quotas: %v", err)
		}

		// Verify update
		updated, err := store.Get(ctx, "org/team/project")
		if err != nil {
			t.Fatalf("failed to get updated quotas: %v", err)
		}

		if updated.MaxConcurrentJobs != 20 {
			t.Errorf("expected max_concurrent_jobs 20, got %d", updated.MaxConcurrentJobs)
		}
		if updated.CPULimit != 8000 {
			t.Errorf("expected cpu_limit 8000, got %d", updated.CPULimit)
		}
	})

	// Test Update not found
	t.Run("UpdateNotFound", func(t *testing.T) {
		quotas := agent.Quotas{
			Namespace:         "missing/namespace",
			MaxConcurrentJobs: 5,
		}

		err := store.Update(ctx, "missing/namespace", quotas)
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	// Test ListAll
	t.Run("ListAll", func(t *testing.T) {
		// Add another quota
		quotas2 := agent.Quotas{
			Namespace:         "org/another",
			MaxConcurrentJobs: 5,
			CPULimit:          2000,
			MemMBLimit:        4096,
			LLMCallsPerMin:    50,
			EgressBytesPerMin: 524288,
		}

		if err := store.Set(ctx, "org/another", quotas2); err != nil {
			t.Fatalf("failed to set second quotas: %v", err)
		}

		// List all
		allQuotas, err := store.ListAll(ctx)
		if err != nil {
			t.Fatalf("failed to list all quotas: %v", err)
		}

		if len(allQuotas) != 2 {
			t.Errorf("expected 2 quota entries, got %d", len(allQuotas))
		}

		if _, ok := allQuotas["org/team/project"]; !ok {
			t.Error("expected org/team/project in list")
		}
		if _, ok := allQuotas["org/another"]; !ok {
			t.Error("expected org/another in list")
		}
	})

	// Test Delete
	t.Run("Delete", func(t *testing.T) {
		if err := store.Delete(ctx, "org/another"); err != nil {
			t.Fatalf("failed to delete quotas: %v", err)
		}

		// Verify deletion
		_, err := store.Get(ctx, "org/another")
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound after delete, got %v", err)
		}
	})

	// Test Delete not found
	t.Run("DeleteNotFound", func(t *testing.T) {
		err := store.Delete(ctx, "missing/namespace")
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestQuotasConsumption(t *testing.T) {
	ctx := context.Background()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	// Open store
	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer func() { _ = store.Close() }() //nolint:errcheck

	ns := "org/team/project"

	// Test GetConsumption (should return zero if not exists)
	t.Run("GetConsumptionEmpty", func(t *testing.T) {
		consumption, err := store.GetConsumption(ctx, ns)
		if err != nil {
			t.Fatalf("failed to get consumption: %v", err)
		}

		if consumption.Namespace != ns {
			t.Errorf("expected namespace %s, got %s", ns, consumption.Namespace)
		}
		if consumption.ActiveJobs != 0 {
			t.Errorf("expected active_jobs 0, got %d", consumption.ActiveJobs)
		}
	})

	// Test UpdateConsumption (insert)
	t.Run("UpdateConsumptionInsert", func(t *testing.T) {
		delta := agent.QuotaConsumption{
			Namespace:       ns,
			ActiveJobs:      1,
			CPUUsed:         100,
			MemMBUsed:       512,
			LLMCalls1Min:    5,
			EgressBytes1Min: 1024,
			LastResetTS:     time.Now().Unix(),
		}

		if err := store.UpdateConsumption(ctx, ns, delta); err != nil {
			t.Fatalf("failed to update consumption: %v", err)
		}

		// Verify
		consumption, err := store.GetConsumption(ctx, ns)
		if err != nil {
			t.Fatalf("failed to get consumption: %v", err)
		}

		if consumption.ActiveJobs != 1 {
			t.Errorf("expected active_jobs 1, got %d", consumption.ActiveJobs)
		}
		if consumption.CPUUsed != 100 {
			t.Errorf("expected cpu_used 100, got %d", consumption.CPUUsed)
		}
		if consumption.MemMBUsed != 512 {
			t.Errorf("expected memMB_used 512, got %d", consumption.MemMBUsed)
		}
	})

	// Test UpdateConsumption (increment)
	t.Run("UpdateConsumptionIncrement", func(t *testing.T) {
		delta := agent.QuotaConsumption{
			Namespace:       ns,
			ActiveJobs:      1,
			CPUUsed:         50,
			MemMBUsed:       256,
			LLMCalls1Min:    3,
			EgressBytes1Min: 512,
		}

		if err := store.UpdateConsumption(ctx, ns, delta); err != nil {
			t.Fatalf("failed to update consumption: %v", err)
		}

		// Verify cumulative totals
		consumption, err := store.GetConsumption(ctx, ns)
		if err != nil {
			t.Fatalf("failed to get consumption: %v", err)
		}

		if consumption.ActiveJobs != 2 {
			t.Errorf("expected active_jobs 2, got %d", consumption.ActiveJobs)
		}
		if consumption.CPUUsed != 150 {
			t.Errorf("expected cpu_used 150, got %d", consumption.CPUUsed)
		}
		if consumption.MemMBUsed != 768 {
			t.Errorf("expected memMB_used 768, got %d", consumption.MemMBUsed)
		}
		if consumption.LLMCalls1Min != 8 {
			t.Errorf("expected llm_calls_1min 8, got %d", consumption.LLMCalls1Min)
		}
	})

	// Test UpdateConsumption (decrement)
	t.Run("UpdateConsumptionDecrement", func(t *testing.T) {
		delta := agent.QuotaConsumption{
			Namespace:  ns,
			ActiveJobs: -1,
			CPUUsed:    -50,
			MemMBUsed:  -256,
		}

		if err := store.UpdateConsumption(ctx, ns, delta); err != nil {
			t.Fatalf("failed to update consumption: %v", err)
		}

		// Verify
		consumption, err := store.GetConsumption(ctx, ns)
		if err != nil {
			t.Fatalf("failed to get consumption: %v", err)
		}

		if consumption.ActiveJobs != 1 {
			t.Errorf("expected active_jobs 1, got %d", consumption.ActiveJobs)
		}
		if consumption.CPUUsed != 100 {
			t.Errorf("expected cpu_used 100, got %d", consumption.CPUUsed)
		}
		if consumption.MemMBUsed != 512 {
			t.Errorf("expected memMB_used 512, got %d", consumption.MemMBUsed)
		}
	})

	// Test reset timestamp update
	t.Run("UpdateResetTimestamp", func(t *testing.T) {
		newTS := time.Now().Unix() + 60
		delta := agent.QuotaConsumption{
			Namespace:   ns,
			LastResetTS: newTS,
		}

		if err := store.UpdateConsumption(ctx, ns, delta); err != nil {
			t.Fatalf("failed to update reset timestamp: %v", err)
		}

		// Verify timestamp was updated
		consumption, err := store.GetConsumption(ctx, ns)
		if err != nil {
			t.Fatalf("failed to get consumption: %v", err)
		}

		if consumption.LastResetTS != newTS {
			t.Errorf("expected last_reset_ts %d, got %d", newTS, consumption.LastResetTS)
		}
	})
}

func TestQuotasZeroLimits(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer func() { _ = store.Close() }() //nolint:errcheck

	// Test setting quotas with all zeros (unlimited)
	t.Run("ZeroQuotas", func(t *testing.T) {
		quotas := agent.Quotas{
			Namespace:         "org/unlimited",
			MaxConcurrentJobs: 0,
			CPULimit:          0,
			MemMBLimit:        0,
			LLMCallsPerMin:    0,
			EgressBytesPerMin: 0,
		}

		if err := store.Set(ctx, "org/unlimited", quotas); err != nil {
			t.Fatalf("failed to set zero quotas: %v", err)
		}

		retrieved, err := store.Get(ctx, "org/unlimited")
		if err != nil {
			t.Fatalf("failed to get zero quotas: %v", err)
		}

		if retrieved.MaxConcurrentJobs != 0 {
			t.Errorf("expected MaxConcurrentJobs 0, got %d", retrieved.MaxConcurrentJobs)
		}
	})
}
