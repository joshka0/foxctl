// Package quotas provides quota enforcement and tracking for agent namespaces.
package quotas

import (
	"context"
	"fmt"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/storage/quotas"
)

// Enforcer checks quota limits before allowing operations.
type Enforcer struct {
	store quotas.Store
}

// NewEnforcer creates a quota enforcer with the provided store.
func NewEnforcer(store quotas.Store) *Enforcer {
	return &Enforcer{store: store}
}

// CheckJobSubmission verifies if a namespace has available quota for a new job.
// Returns an error if the quota would be exceeded.
func (e *Enforcer) CheckJobSubmission(ctx context.Context, ns string, cpuRequest, memMBRequest int) error {
	// Get namespace quotas
	q, err := e.store.Get(ctx, ns)
	if err != nil {
		if err == quotas.ErrNotFound {
			// No quotas defined means unlimited
			return nil
		}
		return fmt.Errorf("quota check: %w", err)
	}

	// Get current consumption
	consumption, err := e.store.GetConsumption(ctx, ns)
	if err != nil {
		return fmt.Errorf("quota check: get consumption: %w", err)
	}

	// Check concurrent jobs limit
	if q.MaxConcurrentJobs > 0 {
		if consumption.ActiveJobs >= q.MaxConcurrentJobs {
			return &QuotaExceededError{
				Namespace: ns,
				Resource:  "concurrent_jobs",
				Limit:     q.MaxConcurrentJobs,
				Current:   consumption.ActiveJobs,
				Requested: 1,
			}
		}
	}

	// Check CPU limit
	if q.CPULimit > 0 {
		if consumption.CPUUsed+cpuRequest > q.CPULimit {
			return &QuotaExceededError{
				Namespace: ns,
				Resource:  "cpu",
				Limit:     q.CPULimit,
				Current:   consumption.CPUUsed,
				Requested: cpuRequest,
			}
		}
	}

	// Check memory limit
	if q.MemMBLimit > 0 {
		if consumption.MemMBUsed+memMBRequest > q.MemMBLimit {
			return &QuotaExceededError{
				Namespace: ns,
				Resource:  "memory_mb",
				Limit:     q.MemMBLimit,
				Current:   consumption.MemMBUsed,
				Requested: memMBRequest,
			}
		}
	}

	return nil
}

// CheckLLMCall verifies if a namespace can make an LLM API call.
func (e *Enforcer) CheckLLMCall(ctx context.Context, ns string) error {
	q, err := e.store.Get(ctx, ns)
	if err != nil {
		if err == quotas.ErrNotFound {
			return nil
		}
		return fmt.Errorf("quota check: %w", err)
	}

	if q.LLMCallsPerMin <= 0 {
		return nil // No limit
	}

	consumption, err := e.store.GetConsumption(ctx, ns)
	if err != nil {
		return fmt.Errorf("quota check: get consumption: %w", err)
	}

	// Reset counters if a minute has passed
	now := time.Now().Unix()
	if now-consumption.LastResetTS >= 60 {
		// Reset counter to 1 for this call
		delta := agent.QuotaConsumption{
			Namespace:    ns,
			LLMCalls1Min: 1 - consumption.LLMCalls1Min,
			LastResetTS:  now,
		}
		if err := e.store.UpdateConsumption(ctx, ns, delta); err != nil {
			return fmt.Errorf("quota check: reset LLM counter: %w", err)
		}
		// Counter reset to 1, check against limit
		if 1 > q.LLMCallsPerMin {
			return &QuotaExceededError{
				Namespace: ns,
				Resource:  "llm_calls_per_min",
				Limit:     q.LLMCallsPerMin,
				Current:   1,
				Requested: 1,
			}
		}
		return nil
	}

	if consumption.LLMCalls1Min >= q.LLMCallsPerMin {
		return &QuotaExceededError{
			Namespace: ns,
			Resource:  "llm_calls_per_min",
			Limit:     q.LLMCallsPerMin,
			Current:   consumption.LLMCalls1Min,
			Requested: 1,
		}
	}

	return nil
}

// CheckEgress verifies if a namespace can send egress bytes.
func (e *Enforcer) CheckEgress(ctx context.Context, ns string, bytes int) error {
	q, err := e.store.Get(ctx, ns)
	if err != nil {
		if err == quotas.ErrNotFound {
			return nil
		}
		return fmt.Errorf("quota check: %w", err)
	}

	if q.EgressBytesPerMin <= 0 {
		return nil // No limit
	}

	consumption, err := e.store.GetConsumption(ctx, ns)
	if err != nil {
		return fmt.Errorf("quota check: get consumption: %w", err)
	}

	// Reset counters if a minute has passed
	now := time.Now().Unix()
	if now-consumption.LastResetTS >= 60 {
		// Reset the counter to include current bytes
		delta := agent.QuotaConsumption{
			Namespace:       ns,
			EgressBytes1Min: bytes - consumption.EgressBytes1Min,
			LastResetTS:     now,
		}
		if err := e.store.UpdateConsumption(ctx, ns, delta); err != nil {
			return fmt.Errorf("quota check: reset egress counter: %w", err)
		}
		// Check if current bytes exceed limit
		if bytes > q.EgressBytesPerMin {
			return &QuotaExceededError{
				Namespace: ns,
				Resource:  "egress_bytes_per_min",
				Limit:     q.EgressBytesPerMin,
				Current:   bytes,
				Requested: bytes,
			}
		}
		return nil
	}

	if consumption.EgressBytes1Min+bytes > q.EgressBytesPerMin {
		return &QuotaExceededError{
			Namespace: ns,
			Resource:  "egress_bytes_per_min",
			Limit:     q.EgressBytesPerMin,
			Current:   consumption.EgressBytes1Min,
			Requested: bytes,
		}
	}

	return nil
}

// RecordJobStart updates consumption when a job starts.
func (e *Enforcer) RecordJobStart(ctx context.Context, ns string, cpuRequest, memMBRequest int) error {
	delta := agent.QuotaConsumption{
		Namespace:  ns,
		ActiveJobs: 1,
		CPUUsed:    cpuRequest,
		MemMBUsed:  memMBRequest,
	}
	return e.store.UpdateConsumption(ctx, ns, delta)
}

// RecordJobEnd updates consumption when a job completes.
func (e *Enforcer) RecordJobEnd(ctx context.Context, ns string, cpuRequest, memMBRequest int) error {
	delta := agent.QuotaConsumption{
		Namespace:  ns,
		ActiveJobs: -1,
		CPUUsed:    -cpuRequest,
		MemMBUsed:  -memMBRequest,
	}
	return e.store.UpdateConsumption(ctx, ns, delta)
}

// RecordLLMCall increments the LLM call counter.
func (e *Enforcer) RecordLLMCall(ctx context.Context, ns string) error {
	consumption, err := e.store.GetConsumption(ctx, ns)
	if err != nil {
		return fmt.Errorf("get consumption: %w", err)
	}
	now := time.Now().Unix()

	delta := agent.QuotaConsumption{
		Namespace:    ns,
		LLMCalls1Min: 1,
	}

	// Set reset timestamp and reset counter if window expired
	if consumption.LastResetTS == 0 || now-consumption.LastResetTS >= 60 {
		delta.LastResetTS = now
		// Reset counter to 1 for this call (not increment from old value)
		delta.LLMCalls1Min = 1 - consumption.LLMCalls1Min
	}

	return e.store.UpdateConsumption(ctx, ns, delta)
}

// RecordEgress increments the egress byte counter.
func (e *Enforcer) RecordEgress(ctx context.Context, ns string, bytes int) error {
	consumption, err := e.store.GetConsumption(ctx, ns)
	if err != nil {
		return fmt.Errorf("get consumption: %w", err)
	}
	now := time.Now().Unix()

	delta := agent.QuotaConsumption{
		Namespace:       ns,
		EgressBytes1Min: bytes,
	}

	// Set reset timestamp and reset counter if window expired
	if consumption.LastResetTS == 0 || now-consumption.LastResetTS >= 60 {
		delta.LastResetTS = now
		// Reset counter to current bytes (not increment from old value)
		delta.EgressBytes1Min = bytes - consumption.EgressBytes1Min
	}

	return e.store.UpdateConsumption(ctx, ns, delta)
}

// QuotaExceededError is returned when a quota limit is exceeded.
type QuotaExceededError struct {
	Namespace string
	Resource  string
	Limit     int
	Current   int
	Requested int
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("quota exceeded for %s in namespace %s: limit=%d, current=%d, requested=%d",
		e.Resource, e.Namespace, e.Limit, e.Current, e.Requested)
}

// IsQuotaExceeded checks if an error is a QuotaExceededError.
func IsQuotaExceeded(err error) bool {
	_, ok := err.(*QuotaExceededError)
	return ok
}
