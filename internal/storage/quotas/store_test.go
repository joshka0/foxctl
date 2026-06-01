package quotas

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/quick"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
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
	defer store.Close()

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

func TestQuotasRejectNegativeLimits(t *testing.T) {
	ctx := context.Background()
	ns := "org/team/project"
	seed := agent.Quotas{
		Namespace:         ns,
		MaxConcurrentJobs: 2,
		CPULimit:          1000,
		MemMBLimit:        2048,
		LLMCallsPerMin:    30,
		EgressBytesPerMin: 4096,
	}

	tests := []struct {
		name   string
		quotas agent.Quotas
	}{
		{name: "max concurrent jobs", quotas: agent.Quotas{Namespace: ns, MaxConcurrentJobs: -1}},
		{name: "cpu limit", quotas: agent.Quotas{Namespace: ns, CPULimit: -1}},
		{name: "memory limit", quotas: agent.Quotas{Namespace: ns, MemMBLimit: -1}},
		{name: "llm calls", quotas: agent.Quotas{Namespace: ns, LLMCallsPerMin: -1}},
		{name: "egress bytes", quotas: agent.Quotas{Namespace: ns, EgressBytesPerMin: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name+" set", func(t *testing.T) {
			store := openQuotaStore(t, ctx)

			err := store.Set(ctx, ns, tt.quotas)
			if !errors.Is(err, ErrInvalidQuotaLimit) {
				t.Fatalf("expected ErrInvalidQuotaLimit, got %v", err)
			}

			_, err = store.Get(ctx, ns)
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("invalid quota was persisted: %v", err)
			}
		})

		t.Run(tt.name+" update", func(t *testing.T) {
			store := openQuotaStore(t, ctx)
			if err := store.Set(ctx, ns, seed); err != nil {
				t.Fatalf("seed quotas: %v", err)
			}

			err := store.Update(ctx, ns, tt.quotas)
			if !errors.Is(err, ErrInvalidQuotaLimit) {
				t.Fatalf("expected ErrInvalidQuotaLimit, got %v", err)
			}

			got, err := store.Get(ctx, ns)
			if err != nil {
				t.Fatalf("get quotas: %v", err)
			}
			if got != seed {
				t.Fatalf("invalid update mutated quotas: %+v, want %+v", got, seed)
			}
		})
	}
}

func TestQuotasRejectCorruptPersistedNegativeLimits(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		column string
	}{
		{name: "max concurrent jobs", column: "max_concurrent_jobs"},
		{name: "cpu limit", column: "cpu_limit"},
		{name: "memory limit", column: "memMB_limit"},
		{name: "llm calls", column: "llm_calls_per_min"},
		{name: "egress bytes", column: "egress_bytes_per_min"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openQuotaStore(t, ctx)
			sqlStore := store.(*sqlStore)
			ns := "org/corrupt/" + tt.column

			if err := store.Set(ctx, ns, agent.Quotas{
				Namespace:         ns,
				MaxConcurrentJobs: 2,
				CPULimit:          1000,
				MemMBLimit:        2048,
				LLMCallsPerMin:    30,
				EgressBytesPerMin: 4096,
			}); err != nil {
				t.Fatalf("seed quotas: %v", err)
			}

			if _, err := sqlStore.db.ExecContext(ctx, fmt.Sprintf(`
				UPDATE ns_quotas SET %s = ? WHERE ns = ?
			`, tt.column), -1, ns); err != nil {
				t.Fatalf("corrupt quota limit: %v", err)
			}

			if _, err := store.Get(ctx, ns); !errors.Is(err, ErrInvalidQuotaLimit) {
				t.Fatalf("Get() error=%v, want ErrInvalidQuotaLimit", err)
			}
			if _, err := store.ListAll(ctx); !errors.Is(err, ErrInvalidQuotaLimit) {
				t.Fatalf("ListAll() error=%v, want ErrInvalidQuotaLimit", err)
			}
		})
	}
}

func TestQuotasPropertyNonNegativeLimitsRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := openQuotaStore(t, ctx)
	caseID := 0

	property := func(maxJobs, cpu, memory, llm, egress uint8) bool {
		ns := fmt.Sprintf("org/quotas-property/%d", caseID)
		caseID++

		q := agent.Quotas{
			Namespace:         ns,
			MaxConcurrentJobs: int(maxJobs),
			CPULimit:          int(cpu),
			MemMBLimit:        int(memory),
			LLMCallsPerMin:    int(llm),
			EgressBytesPerMin: int(egress),
		}
		if err := store.Set(ctx, ns, q); err != nil {
			t.Logf("set quotas %q: %v", ns, err)
			return false
		}

		got, err := store.Get(ctx, ns)
		if err != nil {
			t.Logf("get quotas %q: %v", ns, err)
			return false
		}
		if got != q {
			t.Logf("set/get quotas = %+v, want %+v", got, q)
			return false
		}

		updated := agent.Quotas{
			Namespace:         ns,
			MaxConcurrentJobs: int(maxJobs) + 1,
			CPULimit:          int(cpu) + 1,
			MemMBLimit:        int(memory) + 1,
			LLMCallsPerMin:    int(llm) + 1,
			EgressBytesPerMin: int(egress) + 1,
		}
		if err := store.Update(ctx, ns, updated); err != nil {
			t.Logf("update quotas %q: %v", ns, err)
			return false
		}
		got, err = store.Get(ctx, ns)
		if err != nil {
			t.Logf("get updated quotas %q: %v", ns, err)
			return false
		}
		return got == updated
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
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
	defer store.Close()

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

func TestQuotasConsumptionRejectsNegativeTotals(t *testing.T) {
	ctx := context.Background()
	ns := "org/team/project"

	tests := []struct {
		name  string
		delta agent.QuotaConsumption
	}{
		{name: "active jobs", delta: agent.QuotaConsumption{ActiveJobs: -1}},
		{name: "cpu used", delta: agent.QuotaConsumption{CPUUsed: -1}},
		{name: "memory used", delta: agent.QuotaConsumption{MemMBUsed: -1}},
		{name: "llm calls", delta: agent.QuotaConsumption{LLMCalls1Min: -1}},
		{name: "egress bytes", delta: agent.QuotaConsumption{EgressBytes1Min: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openQuotaStore(t, ctx)

			err := store.UpdateConsumption(ctx, ns, tt.delta)
			if !errors.Is(err, ErrNegativeConsumption) {
				t.Fatalf("expected ErrNegativeConsumption, got %v", err)
			}

			consumption, err := store.GetConsumption(ctx, ns)
			if err != nil {
				t.Fatalf("get consumption: %v", err)
			}
			assertConsumption(t, consumption, agent.QuotaConsumption{Namespace: ns})
		})
	}
}

func TestQuotasRejectCorruptPersistedNegativeConsumption(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		column string
	}{
		{name: "active jobs", column: "active_jobs"},
		{name: "cpu used", column: "cpu_used"},
		{name: "memory used", column: "memMB_used"},
		{name: "llm calls", column: "llm_calls_1min"},
		{name: "egress bytes", column: "egress_bytes_1min"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openQuotaStore(t, ctx)
			sqlStore := store.(*sqlStore)
			ns := "org/corrupt-consumption/" + tt.column

			if err := store.UpdateConsumption(ctx, ns, agent.QuotaConsumption{
				Namespace:       ns,
				ActiveJobs:      2,
				CPUUsed:         100,
				MemMBUsed:       512,
				LLMCalls1Min:    3,
				EgressBytes1Min: 1024,
				LastResetTS:     99,
			}); err != nil {
				t.Fatalf("seed consumption: %v", err)
			}

			if _, err := sqlStore.db.ExecContext(ctx, fmt.Sprintf(`
				UPDATE ns_consumption SET %s = ? WHERE ns = ?
			`, tt.column), -1, ns); err != nil {
				t.Fatalf("corrupt consumption: %v", err)
			}

			if _, err := store.GetConsumption(ctx, ns); !errors.Is(err, ErrNegativeConsumption) {
				t.Fatalf("GetConsumption() error=%v, want ErrNegativeConsumption", err)
			}
		})
	}
}

func TestQuotasRejectNegativeLastResetTimestamp(t *testing.T) {
	ctx := context.Background()
	ns := "org/negative-reset"
	store := openQuotaStore(t, ctx)

	err := store.UpdateConsumption(ctx, ns, agent.QuotaConsumption{
		Namespace:       ns,
		ActiveJobs:      1,
		CPUUsed:         100,
		MemMBUsed:       512,
		LLMCalls1Min:    1,
		EgressBytes1Min: 1024,
		LastResetTS:     -1,
	})
	if !errors.Is(err, ErrNegativeConsumption) {
		t.Fatalf("expected ErrNegativeConsumption, got %v", err)
	}

	consumption, err := store.GetConsumption(ctx, ns)
	if err != nil {
		t.Fatalf("get consumption: %v", err)
	}
	assertConsumption(t, consumption, agent.QuotaConsumption{Namespace: ns})
}

func TestQuotasRejectCorruptPersistedNegativeLastResetTimestamp(t *testing.T) {
	ctx := context.Background()
	ns := "org/corrupt-consumption/last-reset"
	store := openQuotaStore(t, ctx)
	sqlStore := store.(*sqlStore)

	if err := store.UpdateConsumption(ctx, ns, agent.QuotaConsumption{
		Namespace:       ns,
		ActiveJobs:      2,
		CPUUsed:         100,
		MemMBUsed:       512,
		LLMCalls1Min:    3,
		EgressBytes1Min: 1024,
		LastResetTS:     99,
	}); err != nil {
		t.Fatalf("seed consumption: %v", err)
	}

	if _, err := sqlStore.db.ExecContext(ctx, `
		UPDATE ns_consumption SET last_reset_ts = ? WHERE ns = ?
	`, -1, ns); err != nil {
		t.Fatalf("corrupt last_reset_ts: %v", err)
	}

	if _, err := store.GetConsumption(ctx, ns); !errors.Is(err, ErrNegativeConsumption) {
		t.Fatalf("GetConsumption() error=%v, want ErrNegativeConsumption", err)
	}
}

func TestQuotasConsumptionRejectsOverReleaseWithoutMutation(t *testing.T) {
	ctx := context.Background()
	ns := "org/team/project"
	seed := agent.QuotaConsumption{
		Namespace:       ns,
		ActiveJobs:      2,
		CPUUsed:         100,
		MemMBUsed:       512,
		LLMCalls1Min:    3,
		EgressBytes1Min: 1024,
		LastResetTS:     99,
	}

	tests := []struct {
		name  string
		delta agent.QuotaConsumption
	}{
		{name: "active jobs", delta: agent.QuotaConsumption{ActiveJobs: -3, LastResetTS: 100}},
		{name: "cpu used", delta: agent.QuotaConsumption{CPUUsed: -101, LastResetTS: 100}},
		{name: "memory used", delta: agent.QuotaConsumption{MemMBUsed: -513, LastResetTS: 100}},
		{name: "llm calls", delta: agent.QuotaConsumption{LLMCalls1Min: -4, LastResetTS: 100}},
		{name: "egress bytes", delta: agent.QuotaConsumption{EgressBytes1Min: -1025, LastResetTS: 100}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openQuotaStore(t, ctx)
			if err := store.UpdateConsumption(ctx, ns, seed); err != nil {
				t.Fatalf("seed consumption: %v", err)
			}

			err := store.UpdateConsumption(ctx, ns, tt.delta)
			if !errors.Is(err, ErrNegativeConsumption) {
				t.Fatalf("expected ErrNegativeConsumption, got %v", err)
			}

			consumption, err := store.GetConsumption(ctx, ns)
			if err != nil {
				t.Fatalf("get consumption: %v", err)
			}
			assertConsumption(t, consumption, seed)
		})
	}
}

func TestQuotasConsumptionLastResetTimestampIsMonotonic(t *testing.T) {
	ctx := context.Background()
	ns := "org/team/project"
	store := openQuotaStore(t, ctx)

	seed := agent.QuotaConsumption{
		Namespace:    ns,
		LLMCalls1Min: 1,
		LastResetTS:  100,
	}
	if err := store.UpdateConsumption(ctx, ns, seed); err != nil {
		t.Fatalf("seed consumption: %v", err)
	}

	if err := store.UpdateConsumption(ctx, ns, agent.QuotaConsumption{
		Namespace:    ns,
		LLMCalls1Min: 1,
		LastResetTS:  90,
	}); err != nil {
		t.Fatalf("older timestamp update: %v", err)
	}
	got, err := store.GetConsumption(ctx, ns)
	if err != nil {
		t.Fatalf("get consumption: %v", err)
	}
	assertConsumption(t, got, agent.QuotaConsumption{
		Namespace:    ns,
		LLMCalls1Min: 2,
		LastResetTS:  100,
	})

	if err := store.UpdateConsumption(ctx, ns, agent.QuotaConsumption{
		Namespace:       ns,
		EgressBytes1Min: 512,
		LastResetTS:     101,
	}); err != nil {
		t.Fatalf("newer timestamp update: %v", err)
	}
	got, err = store.GetConsumption(ctx, ns)
	if err != nil {
		t.Fatalf("get updated consumption: %v", err)
	}
	assertConsumption(t, got, agent.QuotaConsumption{
		Namespace:       ns,
		LLMCalls1Min:    2,
		EgressBytes1Min: 512,
		LastResetTS:     101,
	})
}

func TestQuotasConsumptionPropertyOlderResetTimestampsNeverMoveWindowBack(t *testing.T) {
	ctx := context.Background()
	store := openQuotaStore(t, ctx)
	caseID := 0

	property := func(rawCurrent uint8, rawRollback uint8) bool {
		ns := fmt.Sprintf("org/window-property/%d", caseID)
		caseID++
		currentTS := int64(rawCurrent) + 100
		olderTS := currentTS - int64(rawRollback%99) - 1

		seed := agent.QuotaConsumption{Namespace: ns, LLMCalls1Min: 1, LastResetTS: currentTS}
		if err := store.UpdateConsumption(ctx, ns, seed); err != nil {
			t.Logf("seed %q: %v", ns, err)
			return false
		}

		if err := store.UpdateConsumption(ctx, ns, agent.QuotaConsumption{
			Namespace:       ns,
			EgressBytes1Min: 1,
			LastResetTS:     olderTS,
		}); err != nil {
			t.Logf("older timestamp update %q: %v", ns, err)
			return false
		}

		got, err := store.GetConsumption(ctx, ns)
		if err != nil {
			t.Logf("get %q: %v", ns, err)
			return false
		}
		if got.LastResetTS != currentTS {
			t.Logf("timestamp moved backwards: got %d want %d (older %d)", got.LastResetTS, currentTS, olderTS)
			return false
		}
		return got.LLMCalls1Min == 1 && got.EgressBytes1Min == 1
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
}

func TestQuotasConsumptionPropertyRejectsNegativeResetTimestamp(t *testing.T) {
	rejectsGeneratedNegativeResetTimestamp := func(raw uint16) bool {
		return !nonNegativeConsumptionDelta(agent.QuotaConsumption{
			LastResetTS: -int64(raw) - 1,
		})
	}

	if err := quick.Check(rejectsGeneratedNegativeResetTimestamp, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("negative last_reset_ts property failed: %v", err)
	}
}

func TestQuotasConsumptionPropertyRejectsNegativeCPUResult(t *testing.T) {
	ctx := context.Background()
	store := openQuotaStore(t, ctx)
	caseID := 0

	property := func(start, decrement uint8) bool {
		ns := fmt.Sprintf("org/property/%d", caseID)
		caseID++

		seed := agent.QuotaConsumption{Namespace: ns, CPUUsed: int(start), LastResetTS: 7}
		if err := store.UpdateConsumption(ctx, ns, seed); err != nil {
			t.Logf("seed %q: %v", ns, err)
			return false
		}

		err := store.UpdateConsumption(ctx, ns, agent.QuotaConsumption{CPUUsed: -int(decrement), LastResetTS: 8})
		consumption, getErr := store.GetConsumption(ctx, ns)
		if getErr != nil {
			t.Logf("get %q: %v", ns, getErr)
			return false
		}

		if int(decrement) <= int(start) {
			want := agent.QuotaConsumption{
				Namespace:   ns,
				CPUUsed:     int(start) - int(decrement),
				LastResetTS: 8,
			}
			if err != nil {
				t.Logf("valid decrement rejected for start=%d decrement=%d: %v", start, decrement, err)
				return false
			}
			return consumption == want
		}

		want := agent.QuotaConsumption{Namespace: ns, CPUUsed: int(start), LastResetTS: 7}
		if !errors.Is(err, ErrNegativeConsumption) {
			t.Logf("over-release start=%d decrement=%d returned %v", start, decrement, err)
			return false
		}
		return consumption == want
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatal(err)
	}
}

func TestQuotasZeroLimits(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

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

func openQuotaStore(t *testing.T, ctx context.Context) Store {
	t.Helper()

	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return store
}

func assertConsumption(t *testing.T, got, want agent.QuotaConsumption) {
	t.Helper()

	if got != want {
		t.Fatalf("consumption = %+v, want %+v", got, want)
	}
}
