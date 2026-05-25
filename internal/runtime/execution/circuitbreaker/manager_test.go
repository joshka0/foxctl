package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"testing/quick"
)

func TestManagerGetOrCreate(t *testing.T) {
	config := DefaultConfig()
	manager := NewManager(config)

	// Create first breaker
	breaker1 := manager.GetOrCreate("service-1")
	if breaker1 == nil {
		t.Fatal("expected breaker to be created")
	}

	// Get same breaker again
	breaker2 := manager.GetOrCreate("service-1")
	if breaker2 != breaker1 {
		t.Error("expected to get same breaker instance")
	}

	// Create different breaker
	breaker3 := manager.GetOrCreate("service-2")
	if breaker3 == breaker1 {
		t.Error("expected different breaker for different service")
	}

	if manager.Count() != 2 {
		t.Errorf("expected 2 breakers, got %d", manager.Count())
	}
}

func TestManagerExecute(t *testing.T) {
	config := Config{
		MaxFailures:  2,
		ResetTimeout: 0,
	}
	manager := NewManager(config)

	ctx := context.Background()
	executed := false

	// Execute successful operation
	err := manager.Execute(ctx, "test-service", func(_ context.Context) error {
		executed = true
		return nil
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if !executed {
		t.Error("expected function to be executed")
	}

	// Verify breaker was created
	breaker := manager.Get("test-service")
	if breaker == nil {
		t.Fatal("expected breaker to be created")
	}

	if breaker.State() != StateClosed {
		t.Errorf("expected closed state, got %s", breaker.State())
	}
}

func TestManagerExecuteFailures(t *testing.T) {
	config := Config{
		MaxFailures:  2,
		ResetTimeout: 0,
	}
	manager := NewManager(config)

	ctx := context.Background()
	testErr := errors.New("test error")

	// Execute failures to open circuit
	for i := 0; i < 2; i++ {
		err := manager.Execute(ctx, "failing-service", func(_ context.Context) error {
			return testErr
		})
		if !errors.Is(err, testErr) {
			t.Errorf("expected test error, got %v", err)
		}
	}

	// Verify circuit is open
	breaker := manager.Get("failing-service")
	if breaker.State() != StateOpen {
		t.Errorf("expected open state, got %s", breaker.State())
	}

	// Next execution should fail with circuit open
	executed := false
	err := manager.Execute(ctx, "failing-service", func(_ context.Context) error {
		executed = true
		return nil
	})

	if !IsCircuitOpen(err) {
		t.Errorf("expected circuit open error, got %v", err)
	}

	if executed {
		t.Error("function should not execute when circuit is open")
	}
}

func TestManagerGet(t *testing.T) {
	config := DefaultConfig()
	manager := NewManager(config)

	// Get non-existent breaker
	breaker := manager.Get("nonexistent")
	if breaker != nil {
		t.Error("expected nil for non-existent breaker")
	}

	// Create breaker
	manager.GetOrCreate("test-service")

	// Get existing breaker
	breaker = manager.Get("test-service")
	if breaker == nil {
		t.Error("expected to get existing breaker")
	}
}

func TestManagerListAll(t *testing.T) {
	config := DefaultConfig()
	manager := NewManager(config)

	// Create multiple breakers
	manager.GetOrCreate("service-1")
	manager.GetOrCreate("service-2")
	manager.GetOrCreate("service-3")

	stats := manager.ListAll()

	if len(stats) != 3 {
		t.Errorf("expected 3 stats entries, got %d", len(stats))
	}

	// Verify stats contain expected names
	names := make(map[string]bool)
	for _, s := range stats {
		names[s.Name] = true
	}

	expectedNames := []string{"service-1", "service-2", "service-3"}
	for _, name := range expectedNames {
		if !names[name] {
			t.Errorf("expected %s in stats, but not found", name)
		}
	}
}

func TestManagerReset(t *testing.T) {
	config := Config{
		MaxFailures:  2,
		ResetTimeout: 0,
	}
	manager := NewManager(config)

	// Create and open a breaker
	breaker := manager.GetOrCreate("test-service")
	breaker.RecordFailure()
	breaker.RecordFailure()

	if breaker.State() != StateOpen {
		t.Fatalf("expected open state, got %s", breaker.State())
	}

	// Reset via manager
	success := manager.Reset("test-service")
	if !success {
		t.Error("expected reset to succeed")
	}

	if breaker.State() != StateClosed {
		t.Errorf("expected closed state after reset, got %s", breaker.State())
	}

	// Try to reset non-existent breaker
	success = manager.Reset("nonexistent")
	if success {
		t.Error("expected reset of non-existent breaker to fail")
	}
}

func TestManagerResetAll(t *testing.T) {
	config := Config{
		MaxFailures:  2,
		ResetTimeout: 0,
	}
	manager := NewManager(config)

	// Create and open multiple breakers
	breaker1 := manager.GetOrCreate("service-1")
	breaker1.RecordFailure()
	breaker1.RecordFailure()

	breaker2 := manager.GetOrCreate("service-2")
	breaker2.RecordFailure()
	breaker2.RecordFailure()

	if breaker1.State() != StateOpen || breaker2.State() != StateOpen {
		t.Fatal("expected both breakers to be open")
	}

	// Reset all
	manager.ResetAll()

	if breaker1.State() != StateClosed {
		t.Errorf("expected breaker1 to be closed, got %s", breaker1.State())
	}
	if breaker2.State() != StateClosed {
		t.Errorf("expected breaker2 to be closed, got %s", breaker2.State())
	}
}

func TestManagerRemove(t *testing.T) {
	config := DefaultConfig()
	manager := NewManager(config)

	// Create breaker
	manager.GetOrCreate("test-service")

	if manager.Count() != 1 {
		t.Errorf("expected 1 breaker, got %d", manager.Count())
	}

	// Remove breaker
	success := manager.Remove("test-service")
	if !success {
		t.Error("expected remove to succeed")
	}

	if manager.Count() != 0 {
		t.Errorf("expected 0 breakers after remove, got %d", manager.Count())
	}

	// Try to remove non-existent breaker
	success = manager.Remove("nonexistent")
	if success {
		t.Error("expected remove of non-existent breaker to fail")
	}
}

func TestManagerConcurrentAccess(t *testing.T) {
	config := DefaultConfig()
	manager := NewManager(config)

	done := make(chan bool)

	// Run concurrent operations
	for i := 0; i < 10; i++ {
		go func(_ int) {
			serviceName := "concurrent-service"
			for j := 0; j < 100; j++ {
				// Intentionally ignoring error for concurrency test
				err := manager.Execute(context.Background(), serviceName, func(_ context.Context) error {
					return nil
				})
				_ = err
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have one breaker
	if manager.Count() != 1 {
		t.Errorf("expected 1 breaker, got %d", manager.Count())
	}

	breaker := manager.Get("concurrent-service")
	if breaker == nil {
		t.Fatal("expected breaker to exist")
	}

	if breaker.State() != StateClosed {
		t.Errorf("expected closed state, got %s", breaker.State())
	}
}

func TestManagerRegistryPropertyOperationsStayConsistent(t *testing.T) {
	ctx := context.Background()

	property := func(rawOps []uint8) bool {
		if len(rawOps) > 100 {
			rawOps = rawOps[:100]
		}
		manager := NewManager(Config{
			MaxFailures:         2,
			ResetTimeout:        0,
			MaxHalfOpenRequests: 1,
			SuccessThreshold:    1,
		})
		model := map[string]struct{}{}
		for _, name := range generatedBreakerNames() {
			if manager.GetOrCreate(name) == nil {
				return false
			}
			model[name] = struct{}{}
		}

		for _, op := range rawOps {
			name := generatedBreakerName(op)
			switch op % 5 {
			case 0:
				if manager.GetOrCreate(name) == nil {
					return false
				}
				model[name] = struct{}{}
			case 1:
				if err := manager.Execute(ctx, name, func(context.Context) error { return nil }); err != nil {
					return false
				}
				model[name] = struct{}{}
			case 2:
				_, exists := model[name]
				if manager.Reset(name) != exists {
					return false
				}
			case 3:
				_, exists := model[name]
				if manager.Remove(name) != exists {
					return false
				}
				delete(model, name)
			case 4:
				_, exists := model[name]
				if (manager.Get(name) != nil) != exists {
					return false
				}
			}

			if manager.Count() != len(model) {
				return false
			}
			if !managerStatsMatchModel(manager.ListAll(), model) {
				return false
			}
		}

		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("manager registry property failed: %v", err)
	}
}

func generatedBreakerName(raw uint8) string {
	names := generatedBreakerNames()
	return names[int(raw)%len(names)]
}

func generatedBreakerNames() []string {
	return []string{"api", "db", "queue", "cache", "llm"}
}

func managerStatsMatchModel(stats []Stats, model map[string]struct{}) bool {
	if len(stats) != len(model) {
		return false
	}
	seen := make(map[string]struct{}, len(stats))
	for _, stat := range stats {
		if _, ok := model[stat.Name]; !ok {
			return false
		}
		if _, duplicate := seen[stat.Name]; duplicate {
			return false
		}
		seen[stat.Name] = struct{}{}
	}
	return true
}
