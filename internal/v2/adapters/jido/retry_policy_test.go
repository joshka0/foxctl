package jido

import (
	"testing"
	"time"
)

func TestResolveRetryPolicyConfig_UsesEnvOverridesByFailureClass(t *testing.T) {
	t.Setenv(EnvJidoRetryPolicy, `{
		"classes": {
			"transport": {
				"base_delay_ms": 1500,
				"max_delay_ms": 12000,
				"max_attempts": 3,
				"suggestion": "retry transport later",
				"patterns": ["socket closed"]
			}
		}
	}`)

	policy := resolveRetryPolicyConfig(RetryPolicy{})
	class, cfg, ok := classifyRetryFailure("socket closed by peer", policy)
	if !ok {
		t.Fatal("expected transport retry classification")
	}
	if class != RetryFailureTransport {
		t.Fatalf("class=%q want %q", class, RetryFailureTransport)
	}
	if cfg.BaseDelay != 1500*time.Millisecond {
		t.Fatalf("base_delay=%s want 1500ms", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 12*time.Second {
		t.Fatalf("max_delay=%s want 12s", cfg.MaxDelay)
	}
	if cfg.MaxAttempts != 3 {
		t.Fatalf("max_attempts=%d want 3", cfg.MaxAttempts)
	}
	if cfg.Suggestion != "retry transport later" {
		t.Fatalf("suggestion=%q want retry transport later", cfg.Suggestion)
	}
}

func TestOrchestrationReconciler_RetryPlan_StopsAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events: &fakeEventAppender{},
		RetryPolicy: RetryPolicy{
			Classes: map[RetryFailureClass]RetryClassPolicy{
				RetryFailureTransport: {
					Enabled:     true,
					BaseDelay:   time.Second,
					MaxDelay:    5 * time.Second,
					MaxAttempts: 2,
					Patterns:    []string{"connection refused"},
				},
			},
		},
		Now: func() time.Time { return time.Date(2026, time.March, 6, 16, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}

	if _, ok := reconciler.retryPlan("connection refused", 1); !ok {
		t.Fatal("attempt 2 should still be retryable")
	}
	if _, ok := reconciler.retryPlan("connection refused", 2); ok {
		t.Fatal("attempt 3 should not be retryable")
	}
}
