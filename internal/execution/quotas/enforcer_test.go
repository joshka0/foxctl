package quotas

import (
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/storage/quotas"
)

func TestEnforcerJobSubmission(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := quotas.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer func() { _ = store.Close() }() //nolint:errcheck

	enforcer := NewEnforcer(store)
	ns := "org/team/project"

	// Set quotas
	q := agent.Quotas{
		Namespace:         ns,
		MaxConcurrentJobs: 2,
		CPULimit:          1000,
		MemMBLimit:        2048,
	}
	if err := store.Set(ctx, ns, q); err != nil {
		t.Fatalf("failed to set quotas: %v", err)
	}

	// Test: First job should be allowed
	t.Run("FirstJobAllowed", func(t *testing.T) {
		err := enforcer.CheckJobSubmission(ctx, ns, 100, 512)
		if err != nil {
			t.Errorf("expected first job to be allowed, got error: %v", err)
		}

		// Record job start
		if err := enforcer.RecordJobStart(ctx, ns, 100, 512); err != nil {
			t.Fatalf("failed to record job start: %v", err)
		}
	})

	// Test: Second job should be allowed
	t.Run("SecondJobAllowed", func(t *testing.T) {
		err := enforcer.CheckJobSubmission(ctx, ns, 200, 512)
		if err != nil {
			t.Errorf("expected second job to be allowed, got error: %v", err)
		}

		if err := enforcer.RecordJobStart(ctx, ns, 200, 512); err != nil {
			t.Fatalf("failed to record job start: %v", err)
		}
	})

	// Test: Third job should be rejected (concurrent jobs limit)
	t.Run("ThirdJobRejected", func(t *testing.T) {
		err := enforcer.CheckJobSubmission(ctx, ns, 100, 512)
		if err == nil {
			t.Error("expected third job to be rejected due to concurrent jobs limit")
		}

		if !IsQuotaExceeded(err) {
			t.Errorf("expected QuotaExceededError, got: %v", err)
		}

		qe := err.(*QuotaExceededError)
		if qe.Resource != "concurrent_jobs" {
			t.Errorf("expected resource concurrent_jobs, got %s", qe.Resource)
		}
	})

	// Test: Complete first job, third should now be allowed
	t.Run("ThirdJobAllowedAfterCompletion", func(t *testing.T) {
		if err := enforcer.RecordJobEnd(ctx, ns, 100, 512); err != nil {
			t.Fatalf("failed to record job end: %v", err)
		}

		err := enforcer.CheckJobSubmission(ctx, ns, 100, 512)
		if err != nil {
			t.Errorf("expected third job to be allowed after completion, got error: %v", err)
		}
	})

	// Test: CPU limit exceeded
	t.Run("CPULimitExceeded", func(t *testing.T) {
		err := enforcer.CheckJobSubmission(ctx, ns, 900, 512)
		if err == nil {
			t.Error("expected job to be rejected due to CPU limit")
		}

		if !IsQuotaExceeded(err) {
			t.Errorf("expected QuotaExceededError, got: %v", err)
		}

		qe := err.(*QuotaExceededError)
		if qe.Resource != "cpu" {
			t.Errorf("expected resource cpu, got %s", qe.Resource)
		}
	})

	// Test: Memory limit exceeded
	t.Run("MemoryLimitExceeded", func(t *testing.T) {
		err := enforcer.CheckJobSubmission(ctx, ns, 100, 2000)
		if err == nil {
			t.Error("expected job to be rejected due to memory limit")
		}

		if !IsQuotaExceeded(err) {
			t.Errorf("expected QuotaExceededError, got: %v", err)
		}

		qe := err.(*QuotaExceededError)
		if qe.Resource != "memory_mb" {
			t.Errorf("expected resource memory_mb, got %s", qe.Resource)
		}
	})
}

func TestEnforcerNoQuotas(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := quotas.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer func() { _ = store.Close() }() //nolint:errcheck

	enforcer := NewEnforcer(store)
	ns := "org/unlimited"

	// Test: Job should be allowed without quotas defined
	t.Run("NoQuotasAllowed", func(t *testing.T) {
		err := enforcer.CheckJobSubmission(ctx, ns, 10000, 100000)
		if err != nil {
			t.Errorf("expected job to be allowed without quotas, got error: %v", err)
		}
	})
}

func TestEnforcerLLMCalls(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := quotas.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer func() { _ = store.Close() }() //nolint:errcheck

	enforcer := NewEnforcer(store)
	ns := "org/llm"

	// Set quotas with LLM limit
	q := agent.Quotas{
		Namespace:      ns,
		LLMCallsPerMin: 3,
	}
	if err := store.Set(ctx, ns, q); err != nil {
		t.Fatalf("failed to set quotas: %v", err)
	}

	// Test: First 3 calls should be allowed
	t.Run("AllowedCalls", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			if err := enforcer.CheckLLMCall(ctx, ns); err != nil {
				t.Errorf("call %d: expected to be allowed, got error: %v", i+1, err)
			}
			if err := enforcer.RecordLLMCall(ctx, ns); err != nil {
				t.Fatalf("call %d: failed to record: %v", i+1, err)
			}
		}
	})

	// Test: Fourth call should be rejected
	t.Run("RejectedCall", func(t *testing.T) {
		err := enforcer.CheckLLMCall(ctx, ns)
		if err == nil {
			t.Error("expected fourth call to be rejected")
		}

		if !IsQuotaExceeded(err) {
			t.Errorf("expected QuotaExceededError, got: %v", err)
		}

		qe := err.(*QuotaExceededError)
		if qe.Resource != "llm_calls_per_min" {
			t.Errorf("expected resource llm_calls_per_min, got %s", qe.Resource)
		}
	})

	// Test: After counter reset, call should be allowed
	t.Run("AllowedAfterReset", func(t *testing.T) {
		// Manually set last reset timestamp to > 60 seconds ago
		consumption, err := store.GetConsumption(ctx, ns)
		if err != nil {
			t.Fatalf("failed to get consumption: %v", err)
		}
		delta := agent.QuotaConsumption{
			Namespace:   ns,
			LastResetTS: time.Now().Unix() - 61,
		}
		// Update to set old timestamp
		if err := store.UpdateConsumption(ctx, ns, delta); err != nil {
			// Need to reset the consumption first
			resetDelta := agent.QuotaConsumption{
				Namespace:    ns,
				LLMCalls1Min: -consumption.LLMCalls1Min,
				LastResetTS:  time.Now().Unix() - 61,
			}
			if err := store.UpdateConsumption(ctx, ns, resetDelta); err != nil {
				t.Fatalf("failed to reset timestamp: %v", err)
			}
		}

		// Check should succeed and reset counter
		if err := enforcer.CheckLLMCall(ctx, ns); err != nil {
			t.Errorf("expected call to be allowed after reset, got error: %v", err)
		}

		// Verify counter was reset
		consumption, err = store.GetConsumption(ctx, ns)
		if err != nil {
			t.Fatalf("failed to get consumption: %v", err)
		}
		if consumption.LLMCalls1Min != 0 {
			t.Errorf("expected counter to be reset to 0, got %d", consumption.LLMCalls1Min)
		}
	})
}

func TestEnforcerEgress(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := quotas.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer func() { _ = store.Close() }() //nolint:errcheck

	enforcer := NewEnforcer(store)
	ns := "org/network"

	// Set quotas with egress limit
	q := agent.Quotas{
		Namespace:         ns,
		EgressBytesPerMin: 10000,
	}
	if err := store.Set(ctx, ns, q); err != nil {
		t.Fatalf("failed to set quotas: %v", err)
	}

	// Test: First egress within limit
	t.Run("AllowedEgress", func(t *testing.T) {
		if err := enforcer.CheckEgress(ctx, ns, 5000); err != nil {
			t.Errorf("expected egress to be allowed, got error: %v", err)
		}
		if err := enforcer.RecordEgress(ctx, ns, 5000); err != nil {
			t.Fatalf("failed to record egress: %v", err)
		}
	})

	// Test: Second egress within limit
	t.Run("SecondEgressAllowed", func(t *testing.T) {
		if err := enforcer.CheckEgress(ctx, ns, 4000); err != nil {
			t.Errorf("expected egress to be allowed, got error: %v", err)
		}
		if err := enforcer.RecordEgress(ctx, ns, 4000); err != nil {
			t.Fatalf("failed to record egress: %v", err)
		}
	})

	// Test: Third egress exceeds limit
	t.Run("RejectedEgress", func(t *testing.T) {
		err := enforcer.CheckEgress(ctx, ns, 2000)
		if err == nil {
			t.Error("expected egress to be rejected")
		}

		if !IsQuotaExceeded(err) {
			t.Errorf("expected QuotaExceededError, got: %v", err)
		}

		qe := err.(*QuotaExceededError)
		if qe.Resource != "egress_bytes_per_min" {
			t.Errorf("expected resource egress_bytes_per_min, got %s", qe.Resource)
		}
	})
}

func TestEnforcerZeroLimits(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := quotas.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer func() { _ = store.Close() }() //nolint:errcheck

	enforcer := NewEnforcer(store)
	ns := "org/unlimited"

	// Set quotas with zero limits (unlimited)
	q := agent.Quotas{
		Namespace:         ns,
		MaxConcurrentJobs: 0,
		CPULimit:          0,
		MemMBLimit:        0,
		LLMCallsPerMin:    0,
		EgressBytesPerMin: 0,
	}
	if err := store.Set(ctx, ns, q); err != nil {
		t.Fatalf("failed to set quotas: %v", err)
	}

	// Test: All operations should be allowed with zero limits
	t.Run("UnlimitedJobs", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			if err := enforcer.CheckJobSubmission(ctx, ns, 1000, 1000); err != nil {
				t.Errorf("expected unlimited jobs to be allowed, got error: %v", err)
			}
			if err := enforcer.RecordJobStart(ctx, ns, 1000, 1000); err != nil {
				t.Fatalf("failed to record job start: %v", err)
			}
		}
	})

	t.Run("UnlimitedLLM", func(t *testing.T) {
		for i := 0; i < 1000; i++ {
			if err := enforcer.CheckLLMCall(ctx, ns); err != nil {
				t.Errorf("expected unlimited LLM calls to be allowed, got error: %v", err)
			}
		}
	})

	t.Run("UnlimitedEgress", func(t *testing.T) {
		if err := enforcer.CheckEgress(ctx, ns, 999999999); err != nil {
			t.Errorf("expected unlimited egress to be allowed, got error: %v", err)
		}
	})
}
