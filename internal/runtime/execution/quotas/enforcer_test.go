package quotas

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/quick"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/storage/quotas"
)

func TestEnforcerJobSubmission(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := quotas.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

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
	defer store.Close()

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

func TestEnforcerTreatsWrappedMissingQuotasAsUnlimited(t *testing.T) {
	ctx := context.Background()
	ns := "org/wrapped-missing"
	enforcer := NewEnforcer(&quotaStoreStub{
		getErr: fmt.Errorf("storage lookup: %w", quotas.ErrNotFound),
	})

	if err := enforcer.CheckJobSubmission(ctx, ns, maxTestInt(), maxTestInt()); err != nil {
		t.Fatalf("CheckJobSubmission() error=%v, want unlimited when quotas are missing", err)
	}
	if err := enforcer.CheckLLMCall(ctx, ns); err != nil {
		t.Fatalf("CheckLLMCall() error=%v, want unlimited when quotas are missing", err)
	}
	if err := enforcer.CheckEgress(ctx, ns, maxTestInt()); err != nil {
		t.Fatalf("CheckEgress() error=%v, want unlimited when quotas are missing", err)
	}
}

func TestIsQuotaExceededRecognizesWrappedErrors(t *testing.T) {
	err := fmt.Errorf("operation denied: %w", &QuotaExceededError{
		Namespace: "org/wrapped",
		Resource:  "cpu",
		Limit:     10,
		Current:   10,
		Requested: 1,
	})

	if !IsQuotaExceeded(err) {
		t.Fatalf("IsQuotaExceeded(%v)=false, want true for wrapped QuotaExceededError", err)
	}
}

func TestEnforcerLLMCalls(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := quotas.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

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
		expiredNS := ns + "/expired"
		if err := store.Set(ctx, expiredNS, q); err != nil {
			t.Fatalf("failed to set expired namespace quotas: %v", err)
		}
		if err := store.UpdateConsumption(ctx, expiredNS, agent.QuotaConsumption{
			Namespace:    expiredNS,
			LLMCalls1Min: 3,
			LastResetTS:  time.Now().Unix() - 61,
		}); err != nil {
			t.Fatalf("failed to seed expired window consumption: %v", err)
		}

		// Check should succeed (window expired, so treats current consumption as 0)
		if err := enforcer.CheckLLMCall(ctx, expiredNS); err != nil {
			t.Errorf("expected call to be allowed after reset, got error: %v", err)
		}

		// Record the call, which should reset the counter to 1 for the new window
		if err := enforcer.RecordLLMCall(ctx, expiredNS); err != nil {
			t.Fatalf("failed to record LLM call: %v", err)
		}

		// Verify counter was reset to 1 (first call in new window)
		consumption, err := store.GetConsumption(ctx, expiredNS)
		if err != nil {
			t.Fatalf("failed to get consumption: %v", err)
		}
		if consumption.LLMCalls1Min != 1 {
			t.Errorf("expected counter to be reset to 1, got %d", consumption.LLMCalls1Min)
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
	defer store.Close()

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

func TestEnforcerRecordRateEventStartsNewWindowWithoutStaleCounters(t *testing.T) {
	ctx := context.Background()
	ns := "org/rate-window"
	oldWindow := time.Now().Unix() - 61

	tests := []struct {
		name      string
		seed      agent.QuotaConsumption
		record    func(*Enforcer) error
		wantCalls int
		wantBytes int
	}{
		{
			name: "llm call clears stale egress bytes",
			seed: agent.QuotaConsumption{
				Namespace:       ns,
				EgressBytes1Min: 9000,
				LastResetTS:     oldWindow,
			},
			record:    func(enforcer *Enforcer) error { return enforcer.RecordLLMCall(ctx, ns) },
			wantCalls: 1,
			wantBytes: 0,
		},
		{
			name: "egress clears stale llm calls",
			seed: agent.QuotaConsumption{
				Namespace:    ns,
				LLMCalls1Min: 3,
				LastResetTS:  oldWindow,
			},
			record:    func(enforcer *Enforcer) error { return enforcer.RecordEgress(ctx, ns, 512) },
			wantCalls: 0,
			wantBytes: 512,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := quotas.Open(ctx, t.TempDir())
			if err != nil {
				t.Fatalf("failed to open store: %v", err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Fatalf("close store: %v", err)
				}
			})
			if err := store.UpdateConsumption(ctx, ns, tt.seed); err != nil {
				t.Fatalf("seed consumption: %v", err)
			}

			enforcer := NewEnforcer(store)
			if err := tt.record(enforcer); err != nil {
				t.Fatalf("record rate event: %v", err)
			}

			got, err := store.GetConsumption(ctx, ns)
			if err != nil {
				t.Fatalf("get consumption: %v", err)
			}
			if got.LLMCalls1Min != tt.wantCalls || got.EgressBytes1Min != tt.wantBytes {
				t.Fatalf("one-minute counters = calls:%d bytes:%d, want calls:%d bytes:%d",
					got.LLMCalls1Min, got.EgressBytes1Min, tt.wantCalls, tt.wantBytes)
			}
			if got.LastResetTS <= oldWindow {
				t.Fatalf("last reset timestamp was not advanced: got %d, old %d", got.LastResetTS, oldWindow)
			}
		})
	}
}

func TestEnforcerResourceChecksAllowExactLimit(t *testing.T) {
	ctx := context.Background()
	ns := "org/exact-limit"
	limit := maxTestInt()

	t.Run("cpu", func(t *testing.T) {
		enforcer := NewEnforcer(&quotaStoreStub{
			quotas: agent.Quotas{
				Namespace: ns,
				CPULimit:  limit,
			},
			consumption: agent.QuotaConsumption{
				Namespace: ns,
				CPUUsed:   limit - 10,
			},
		})
		if err := enforcer.CheckJobSubmission(ctx, ns, 10, 0); err != nil {
			t.Fatalf("exact CPU limit should be allowed: %v", err)
		}
	})

	t.Run("memory", func(t *testing.T) {
		enforcer := NewEnforcer(&quotaStoreStub{
			quotas: agent.Quotas{
				Namespace:  ns,
				MemMBLimit: limit,
			},
			consumption: agent.QuotaConsumption{
				Namespace: ns,
				MemMBUsed: limit - 10,
			},
		})
		if err := enforcer.CheckJobSubmission(ctx, ns, 0, 10); err != nil {
			t.Fatalf("exact memory limit should be allowed: %v", err)
		}
	})

	t.Run("egress", func(t *testing.T) {
		enforcer := NewEnforcer(&quotaStoreStub{
			quotas: agent.Quotas{
				Namespace:         ns,
				EgressBytesPerMin: limit,
			},
			consumption: agent.QuotaConsumption{
				Namespace:       ns,
				EgressBytes1Min: limit - 10,
				LastResetTS:     time.Now().Unix(),
			},
		})
		if err := enforcer.CheckEgress(ctx, ns, 10); err != nil {
			t.Fatalf("exact egress limit should be allowed: %v", err)
		}
	})
}

func TestEnforcerResourceChecksRejectOverflowPastLimit(t *testing.T) {
	ctx := context.Background()
	ns := "org/overflow-limit"
	limit := maxTestInt()

	tests := []struct {
		name        string
		quotas      agent.Quotas
		consumption agent.QuotaConsumption
		run         func(*Enforcer) error
		resource    string
	}{
		{
			name:   "cpu",
			quotas: agent.Quotas{Namespace: ns, CPULimit: limit},
			consumption: agent.QuotaConsumption{
				Namespace: ns,
				CPUUsed:   limit - 5,
			},
			run:      func(enforcer *Enforcer) error { return enforcer.CheckJobSubmission(ctx, ns, 6, 0) },
			resource: "cpu",
		},
		{
			name:   "memory",
			quotas: agent.Quotas{Namespace: ns, MemMBLimit: limit},
			consumption: agent.QuotaConsumption{
				Namespace: ns,
				MemMBUsed: limit - 5,
			},
			run:      func(enforcer *Enforcer) error { return enforcer.CheckJobSubmission(ctx, ns, 0, 6) },
			resource: "memory_mb",
		},
		{
			name:   "egress",
			quotas: agent.Quotas{Namespace: ns, EgressBytesPerMin: limit},
			consumption: agent.QuotaConsumption{
				Namespace:       ns,
				EgressBytes1Min: limit - 5,
				LastResetTS:     time.Now().Unix(),
			},
			run:      func(enforcer *Enforcer) error { return enforcer.CheckEgress(ctx, ns, 6) },
			resource: "egress_bytes_per_min",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enforcer := NewEnforcer(&quotaStoreStub{
				quotas:      tt.quotas,
				consumption: tt.consumption,
			})
			err := tt.run(enforcer)
			if !IsQuotaExceeded(err) {
				t.Fatalf("expected overflowed request to exceed quota, got %v", err)
			}
			var exceeded *QuotaExceededError
			if !errors.As(err, &exceeded) {
				t.Fatalf("expected QuotaExceededError, got %T", err)
			}
			if exceeded.Resource != tt.resource {
				t.Fatalf("resource=%s want %s", exceeded.Resource, tt.resource)
			}
		})
	}
}

func TestEnforcerPropertyResourceRequestsPastLimitAreRejected(t *testing.T) {
	ctx := context.Background()
	ns := "org/limit-property"
	limit := maxTestInt()

	property := func(rawHeadroom uint8) bool {
		headroom := int(rawHeadroom%32) + 1
		request := headroom + 1
		enforcer := NewEnforcer(&quotaStoreStub{
			quotas: agent.Quotas{
				Namespace:         ns,
				CPULimit:          limit,
				MemMBLimit:        limit,
				EgressBytesPerMin: limit,
			},
			consumption: agent.QuotaConsumption{
				Namespace:       ns,
				CPUUsed:         limit - headroom,
				MemMBUsed:       limit - headroom,
				EgressBytes1Min: limit - headroom,
				LastResetTS:     time.Now().Unix(),
			},
		})

		errs := []error{
			enforcer.CheckJobSubmission(ctx, ns, request, 0),
			enforcer.CheckJobSubmission(ctx, ns, 0, request),
			enforcer.CheckEgress(ctx, ns, request),
		}
		for _, err := range errs {
			if !IsQuotaExceeded(err) {
				t.Logf("headroom=%d request=%d error=%v", headroom, request, err)
				return false
			}
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("past-limit quota property failed: %v", err)
	}
}

func TestEnforcerZeroLimits(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := quotas.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

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

func TestEnforcerRejectsInvalidStoredQuotaLimits(t *testing.T) {
	ctx := context.Background()
	ns := "org/invalid-limits"

	tests := []struct {
		name   string
		quotas agent.Quotas
		run    func(*Enforcer) error
	}{
		{
			name:   "max concurrent jobs",
			quotas: agent.Quotas{Namespace: ns, MaxConcurrentJobs: -1},
			run:    func(enforcer *Enforcer) error { return enforcer.CheckJobSubmission(ctx, ns, 1, 1) },
		},
		{
			name:   "cpu limit",
			quotas: agent.Quotas{Namespace: ns, CPULimit: -1},
			run:    func(enforcer *Enforcer) error { return enforcer.CheckJobSubmission(ctx, ns, 1, 1) },
		},
		{
			name:   "memory limit",
			quotas: agent.Quotas{Namespace: ns, MemMBLimit: -1},
			run:    func(enforcer *Enforcer) error { return enforcer.CheckJobSubmission(ctx, ns, 1, 1) },
		},
		{
			name:   "llm calls",
			quotas: agent.Quotas{Namespace: ns, LLMCallsPerMin: -1},
			run:    func(enforcer *Enforcer) error { return enforcer.CheckLLMCall(ctx, ns) },
		},
		{
			name:   "egress bytes",
			quotas: agent.Quotas{Namespace: ns, EgressBytesPerMin: -1},
			run:    func(enforcer *Enforcer) error { return enforcer.CheckEgress(ctx, ns, 1) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enforcer := NewEnforcer(&quotaStoreStub{quotas: tt.quotas})

			err := tt.run(enforcer)
			if !errors.Is(err, quotas.ErrInvalidQuotaLimit) {
				t.Fatalf("expected ErrInvalidQuotaLimit, got %v", err)
			}
		})
	}
}

func TestEnforcerRejectsInvalidConsumptionFromStore(t *testing.T) {
	ctx := context.Background()
	ns := "org/invalid-consumption"

	tests := []struct {
		name        string
		consumption agent.QuotaConsumption
		run         func(*Enforcer) error
	}{
		{
			name: "job submission",
			consumption: agent.QuotaConsumption{
				Namespace:   ns,
				LastResetTS: -1,
			},
			run: func(enforcer *Enforcer) error {
				return enforcer.CheckJobSubmission(ctx, ns, 1, 1)
			},
		},
		{
			name: "llm check",
			consumption: agent.QuotaConsumption{
				Namespace:   ns,
				LastResetTS: -1,
			},
			run: func(enforcer *Enforcer) error {
				return enforcer.CheckLLMCall(ctx, ns)
			},
		},
		{
			name: "egress check",
			consumption: agent.QuotaConsumption{
				Namespace:   ns,
				LastResetTS: -1,
			},
			run: func(enforcer *Enforcer) error {
				return enforcer.CheckEgress(ctx, ns, 1)
			},
		},
		{
			name: "record llm",
			consumption: agent.QuotaConsumption{
				Namespace:   ns,
				LastResetTS: -1,
			},
			run: func(enforcer *Enforcer) error {
				return enforcer.RecordLLMCall(ctx, ns)
			},
		},
		{
			name: "record egress",
			consumption: agent.QuotaConsumption{
				Namespace:   ns,
				LastResetTS: -1,
			},
			run: func(enforcer *Enforcer) error {
				return enforcer.RecordEgress(ctx, ns, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enforcer := NewEnforcer(&quotaStoreStub{
				quotas: agent.Quotas{
					Namespace:         ns,
					MaxConcurrentJobs: 2,
					CPULimit:          10,
					MemMBLimit:        10,
					LLMCallsPerMin:    10,
					EgressBytesPerMin: 10,
				},
				consumption: tt.consumption,
			})

			err := tt.run(enforcer)
			if !errors.Is(err, quotas.ErrNegativeConsumption) {
				t.Fatalf("expected ErrNegativeConsumption, got %v", err)
			}
		})
	}
}

func TestEnforcerRejectsNegativeResourceRequests(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := quotas.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	enforcer := NewEnforcer(store)
	ns := "org/nonnegative"
	if err := store.Set(ctx, ns, agent.Quotas{
		Namespace:         ns,
		CPULimit:          1000,
		MemMBLimit:        2048,
		EgressBytesPerMin: 10000,
	}); err != nil {
		t.Fatalf("failed to set quotas: %v", err)
	}

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "check job rejects negative CPU",
			run:  func() error { return enforcer.CheckJobSubmission(ctx, ns, -1, 512) },
		},
		{
			name: "check job rejects negative memory",
			run:  func() error { return enforcer.CheckJobSubmission(ctx, ns, 100, -1) },
		},
		{
			name: "record job start rejects negative CPU",
			run:  func() error { return enforcer.RecordJobStart(ctx, ns, -1, 512) },
		},
		{
			name: "record job start rejects negative memory",
			run:  func() error { return enforcer.RecordJobStart(ctx, ns, 100, -1) },
		},
		{
			name: "record job end rejects negative CPU",
			run:  func() error { return enforcer.RecordJobEnd(ctx, ns, -1, 512) },
		},
		{
			name: "record job end rejects negative memory",
			run:  func() error { return enforcer.RecordJobEnd(ctx, ns, 100, -1) },
		},
		{
			name: "check egress rejects negative bytes",
			run:  func() error { return enforcer.CheckEgress(ctx, ns, -1) },
		},
		{
			name: "record egress rejects negative bytes",
			run:  func() error { return enforcer.RecordEgress(ctx, ns, -1) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("expected negative quota request to be rejected")
			}
		})
	}

	consumption, err := store.GetConsumption(ctx, ns)
	if err != nil {
		t.Fatalf("failed to get consumption: %v", err)
	}
	if consumption.ActiveJobs != 0 ||
		consumption.CPUUsed != 0 ||
		consumption.MemMBUsed != 0 ||
		consumption.EgressBytes1Min != 0 {
		t.Fatalf("rejected negative requests mutated consumption: %+v", consumption)
	}
}

func TestEnforcerPropertyNegativeRequestsNeverMutateConsumption(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := quotas.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	enforcer := NewEnforcer(store)
	ns := "org/nonnegative-property"
	if err := store.Set(ctx, ns, agent.Quotas{
		Namespace:         ns,
		CPULimit:          1000,
		MemMBLimit:        2048,
		EgressBytesPerMin: 10000,
	}); err != nil {
		t.Fatalf("failed to set quotas: %v", err)
	}

	cfg := &quick.Config{MaxCount: 100}
	err = quick.Check(func(raw uint8) bool {
		negative := -int(raw%64) - 1
		calls := []func() error{
			func() error { return enforcer.CheckJobSubmission(ctx, ns, negative, 512) },
			func() error { return enforcer.CheckJobSubmission(ctx, ns, 100, negative) },
			func() error { return enforcer.RecordJobStart(ctx, ns, negative, 512) },
			func() error { return enforcer.RecordJobStart(ctx, ns, 100, negative) },
			func() error { return enforcer.RecordJobEnd(ctx, ns, negative, 512) },
			func() error { return enforcer.RecordJobEnd(ctx, ns, 100, negative) },
			func() error { return enforcer.CheckEgress(ctx, ns, negative) },
			func() error { return enforcer.RecordEgress(ctx, ns, negative) },
		}
		for _, call := range calls {
			if err := call(); err == nil {
				return false
			}
		}

		consumption, err := store.GetConsumption(ctx, ns)
		return err == nil &&
			consumption.ActiveJobs == 0 &&
			consumption.CPUUsed == 0 &&
			consumption.MemMBUsed == 0 &&
			consumption.EgressBytes1Min == 0
	}, cfg)
	if err != nil {
		t.Fatalf("negative quota request property failed: %v", err)
	}
}

func TestEnforcerRecordJobEndRejectsOverReleaseWithoutPartialMutation(t *testing.T) {
	ctx := context.Background()
	ns := "org/over-release"
	tests := []struct {
		name      string
		starts    int
		startCPU  int
		startMem  int
		endCPU    int
		endMem    int
		wantAfter agent.QuotaConsumption
	}{
		{
			name:     "too many jobs",
			starts:   0,
			startCPU: 100,
			startMem: 512,
			endCPU:   100,
			endMem:   512,
			wantAfter: agent.QuotaConsumption{
				Namespace: ns,
			},
		},
		{
			name:     "too much cpu",
			starts:   1,
			startCPU: 100,
			startMem: 512,
			endCPU:   101,
			endMem:   512,
			wantAfter: agent.QuotaConsumption{
				Namespace:  ns,
				ActiveJobs: 1,
				CPUUsed:    100,
				MemMBUsed:  512,
			},
		},
		{
			name:     "too much memory",
			starts:   1,
			startCPU: 100,
			startMem: 512,
			endCPU:   100,
			endMem:   513,
			wantAfter: agent.QuotaConsumption{
				Namespace:  ns,
				ActiveJobs: 1,
				CPUUsed:    100,
				MemMBUsed:  512,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := quotas.Open(ctx, t.TempDir())
			if err != nil {
				t.Fatalf("failed to open store: %v", err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Fatalf("close store: %v", err)
				}
			})
			enforcer := NewEnforcer(store)

			for i := 0; i < tt.starts; i++ {
				if err := enforcer.RecordJobStart(ctx, ns, tt.startCPU, tt.startMem); err != nil {
					t.Fatalf("record job start: %v", err)
				}
			}

			err = enforcer.RecordJobEnd(ctx, ns, tt.endCPU, tt.endMem)
			if !errors.Is(err, quotas.ErrNegativeConsumption) {
				t.Fatalf("expected ErrNegativeConsumption, got %v", err)
			}

			got, err := store.GetConsumption(ctx, ns)
			if err != nil {
				t.Fatalf("get consumption: %v", err)
			}
			if got != tt.wantAfter {
				t.Fatalf("consumption after rejected over-release = %+v, want %+v", got, tt.wantAfter)
			}
		})
	}
}

func TestEnforcerPropertyBalancedJobStartEndNeverOverReleases(t *testing.T) {
	ctx := context.Background()

	property := func(rawJobs, rawCPU, rawMem uint8) bool {
		jobCount := int(rawJobs%5) + 1
		cpu := int(rawCPU%64) + 1
		mem := int(rawMem%128) + 1
		ns := "org/balanced-property"

		store, err := quotas.Open(ctx, t.TempDir())
		if err != nil {
			t.Logf("open store: %v", err)
			return false
		}
		defer func() { _ = store.Close() }()
		enforcer := NewEnforcer(store)

		for i := 0; i < jobCount; i++ {
			if err := enforcer.RecordJobStart(ctx, ns, cpu, mem); err != nil {
				t.Logf("start %d/%d: %v", i+1, jobCount, err)
				return false
			}
		}
		for i := 0; i < jobCount; i++ {
			if err := enforcer.RecordJobEnd(ctx, ns, cpu, mem); err != nil {
				t.Logf("end %d/%d: %v", i+1, jobCount, err)
				return false
			}
		}

		zero, err := store.GetConsumption(ctx, ns)
		if err != nil {
			t.Logf("get zero consumption: %v", err)
			return false
		}
		if zero.ActiveJobs != 0 || zero.CPUUsed != 0 || zero.MemMBUsed != 0 {
			t.Logf("balanced start/end left consumption: %+v", zero)
			return false
		}

		err = enforcer.RecordJobEnd(ctx, ns, cpu, mem)
		after, getErr := store.GetConsumption(ctx, ns)
		return errors.Is(err, quotas.ErrNegativeConsumption) &&
			getErr == nil &&
			after == zero
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("balanced job start/end property failed: %v", err)
	}
}

type quotaStoreStub struct {
	quotas      agent.Quotas
	consumption agent.QuotaConsumption
	getErr      error
}

func (s *quotaStoreStub) Close() error {
	return nil
}

func (s *quotaStoreStub) Get(_ context.Context, ns string) (agent.Quotas, error) {
	if s.getErr != nil {
		return agent.Quotas{}, s.getErr
	}
	q := s.quotas
	if q.Namespace == "" {
		q.Namespace = ns
	}
	return q, nil
}

func (s *quotaStoreStub) Set(context.Context, string, agent.Quotas) error {
	return nil
}

func (s *quotaStoreStub) Update(context.Context, string, agent.Quotas) error {
	return nil
}

func (s *quotaStoreStub) Delete(context.Context, string) error {
	return nil
}

func (s *quotaStoreStub) ListAll(context.Context) (map[string]agent.Quotas, error) {
	return map[string]agent.Quotas{s.quotas.Namespace: s.quotas}, nil
}

func (s *quotaStoreStub) GetConsumption(_ context.Context, ns string) (agent.QuotaConsumption, error) {
	consumption := s.consumption
	if consumption.Namespace == "" {
		consumption.Namespace = ns
	}
	return consumption, nil
}

func (s *quotaStoreStub) UpdateConsumption(context.Context, string, agent.QuotaConsumption) error {
	return nil
}

func maxTestInt() int {
	return int(^uint(0) >> 1)
}
