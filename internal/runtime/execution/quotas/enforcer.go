package quotas

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/storage/quotas"
)

// Enforcer checks quota limits before allowing operations.
type Enforcer struct {
	store quotas.Store
}

// NewEnforcer creates a quota enforcer with the provided store.
func NewEnforcer(store quotas.Store) *Enforcer {
	return &Enforcer{store: store}
}

func (e *Enforcer) getValidatedConsumption(ctx context.Context, ns string) (agent.QuotaConsumption, error) {
	consumption, err := e.store.GetConsumption(ctx, ns)
	if err != nil {
		return agent.QuotaConsumption{}, err
	}
	if err := quotas.ValidateConsumption(consumption); err != nil {
		return agent.QuotaConsumption{}, err
	}
	return consumption, nil
}

// CheckJobSubmission verifies if a namespace has available quota for a new job.
// Returns an error if the quota would be exceeded.
func (e *Enforcer) CheckJobSubmission(ctx context.Context, ns string, cpuRequest, memMBRequest int) error {
	if err := validateJobResourceRequest(cpuRequest, memMBRequest); err != nil {
		return err
	}

	// Get namespace quotas
	q, err := e.store.Get(ctx, ns)
	if err != nil {
		if errors.Is(err, quotas.ErrNotFound) {
			// No quotas defined means unlimited
			return nil
		}
		return fmt.Errorf("quota check: %w", err)
	}
	if err := quotas.ValidateLimits(q); err != nil {
		return fmt.Errorf("quota check: invalid quotas: %w", err)
	}

	// Get current consumption
	consumption, err := e.getValidatedConsumption(ctx, ns)
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
	if quotaLimitExceeded(consumption.CPUUsed, cpuRequest, q.CPULimit) {
		return &QuotaExceededError{
			Namespace: ns,
			Resource:  "cpu",
			Limit:     q.CPULimit,
			Current:   consumption.CPUUsed,
			Requested: cpuRequest,
		}
	}

	// Check memory limit
	if quotaLimitExceeded(consumption.MemMBUsed, memMBRequest, q.MemMBLimit) {
		return &QuotaExceededError{
			Namespace: ns,
			Resource:  "memory_mb",
			Limit:     q.MemMBLimit,
			Current:   consumption.MemMBUsed,
			Requested: memMBRequest,
		}
	}

	return nil
}

// CheckLLMCall verifies if a namespace can make an LLM API call.
func (e *Enforcer) CheckLLMCall(ctx context.Context, ns string) error {
	q, err := e.store.Get(ctx, ns)
	if err != nil {
		if errors.Is(err, quotas.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("quota check: %w", err)
	}
	if err := quotas.ValidateLimits(q); err != nil {
		return fmt.Errorf("quota check: invalid quotas: %w", err)
	}

	if q.LLMCallsPerMin <= 0 {
		return nil // No limit
	}

	consumption, err := e.getValidatedConsumption(ctx, ns)
	if err != nil {
		return fmt.Errorf("quota check: get consumption: %w", err)
	}

	// If window expired, treat current consumption as 0
	now := time.Now().Unix()
	currentCalls := consumption.LLMCalls1Min
	if now-consumption.LastResetTS >= 60 {
		currentCalls = 0
	}

	if currentCalls >= q.LLMCallsPerMin {
		return &QuotaExceededError{
			Namespace: ns,
			Resource:  "llm_calls_per_min",
			Limit:     q.LLMCallsPerMin,
			Current:   currentCalls,
			Requested: 1,
		}
	}

	return nil
}

// CheckEgress verifies if a namespace can send egress bytes.
func (e *Enforcer) CheckEgress(ctx context.Context, ns string, bytes int) error {
	if err := validateNonNegativeQuotaRequest("egress bytes", bytes); err != nil {
		return err
	}

	q, err := e.store.Get(ctx, ns)
	if err != nil {
		if errors.Is(err, quotas.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("quota check: %w", err)
	}
	if err := quotas.ValidateLimits(q); err != nil {
		return fmt.Errorf("quota check: invalid quotas: %w", err)
	}

	if q.EgressBytesPerMin <= 0 {
		return nil // No limit
	}

	consumption, err := e.getValidatedConsumption(ctx, ns)
	if err != nil {
		return fmt.Errorf("quota check: get consumption: %w", err)
	}

	// If window expired, treat current consumption as 0
	now := time.Now().Unix()
	currentBytes := consumption.EgressBytes1Min
	if now-consumption.LastResetTS >= 60 {
		currentBytes = 0
	}

	if quotaLimitExceeded(currentBytes, bytes, q.EgressBytesPerMin) {
		return &QuotaExceededError{
			Namespace: ns,
			Resource:  "egress_bytes_per_min",
			Limit:     q.EgressBytesPerMin,
			Current:   currentBytes,
			Requested: bytes,
		}
	}

	return nil
}

// RecordJobStart updates consumption when a job starts.
func (e *Enforcer) RecordJobStart(ctx context.Context, ns string, cpuRequest, memMBRequest int) error {
	if err := validateJobResourceRequest(cpuRequest, memMBRequest); err != nil {
		return err
	}

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
	if err := validateJobResourceRequest(cpuRequest, memMBRequest); err != nil {
		return err
	}

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
	consumption, err := e.getValidatedConsumption(ctx, ns)
	if err != nil {
		return fmt.Errorf("get consumption: %w", err)
	}
	now := time.Now().Unix()

	delta := agent.QuotaConsumption{
		Namespace:    ns,
		LLMCalls1Min: 1,
		LastResetTS:  now,
	}

	// If window expired or first call, reset counter before incrementing
	if consumption.LastResetTS == 0 || now-consumption.LastResetTS >= 60 {
		delta.LLMCalls1Min = 1 - consumption.LLMCalls1Min
		delta.EgressBytes1Min = -consumption.EgressBytes1Min
	}

	return e.store.UpdateConsumption(ctx, ns, delta)
}

// RecordEgress increments the egress byte counter.
func (e *Enforcer) RecordEgress(ctx context.Context, ns string, bytes int) error {
	if err := validateNonNegativeQuotaRequest("egress bytes", bytes); err != nil {
		return err
	}

	consumption, err := e.getValidatedConsumption(ctx, ns)
	if err != nil {
		return fmt.Errorf("get consumption: %w", err)
	}
	now := time.Now().Unix()

	delta := agent.QuotaConsumption{
		Namespace:       ns,
		EgressBytes1Min: bytes,
		LastResetTS:     now,
	}

	// If window expired or first call, reset counter before incrementing
	if consumption.LastResetTS == 0 || now-consumption.LastResetTS >= 60 {
		delta.EgressBytes1Min = bytes - consumption.EgressBytes1Min
		delta.LLMCalls1Min = -consumption.LLMCalls1Min
	}

	return e.store.UpdateConsumption(ctx, ns, delta)
}

func validateJobResourceRequest(cpuRequest, memMBRequest int) error {
	if err := validateNonNegativeQuotaRequest("cpu", cpuRequest); err != nil {
		return err
	}
	return validateNonNegativeQuotaRequest("memory_mb", memMBRequest)
}

func validateNonNegativeQuotaRequest(resource string, value int) error {
	if value < 0 {
		return fmt.Errorf("%s request must be non-negative", resource)
	}
	return nil
}

func quotaLimitExceeded(current, requested, limit int) bool {
	if limit <= 0 {
		return false
	}
	if current < 0 {
		return true
	}
	if current > limit {
		return true
	}
	return requested > limit-current
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
	var exceeded *QuotaExceededError
	return errors.As(err, &exceeded)
}
