package circuitbreaker

import (
	"context"
	"sync"
)

// Manager manages multiple circuit breakers for different operations.
type Manager struct {
	mu       sync.RWMutex
	breakers map[string]*Breaker
	config   Config
}

// NewManager creates a new circuit breaker manager with the given default config.
func NewManager(config Config) *Manager {
	return &Manager{
		breakers: make(map[string]*Breaker),
		config:   config,
	}
}

// Execute runs a function through the circuit breaker for the given operation name.
//
// Index:
// - Purpose: Execute guarded work via a named circuit breaker
// - Flow: get/create breaker → execute fn → return error
// - SideEffects: breaker state updates
// - FailureModes: breaker open, fn errors
// - Related: Manager.GetOrCreate, Breaker.Execute
// - Keywords: circuit_breaker, execute, operation_name
func (m *Manager) Execute(ctx context.Context, name string, fn func(context.Context) error) error {
	breaker := m.GetOrCreate(name)
	return breaker.Execute(ctx, fn)
}

// GetOrCreate returns an existing breaker or creates a new one.
func (m *Manager) GetOrCreate(name string) *Breaker {
	m.mu.RLock()
	breaker, exists := m.breakers[name]
	m.mu.RUnlock()

	if exists {
		return breaker
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	breaker, exists = m.breakers[name]
	if exists {
		return breaker
	}

	breaker = New(name, m.config)
	m.breakers[name] = breaker
	return breaker
}

// Get returns an existing breaker by name, or nil if not found.
func (m *Manager) Get(name string) *Breaker {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.breakers[name]
}

// ListAll returns stats for all registered circuit breakers.
func (m *Manager) ListAll() []Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make([]Stats, 0, len(m.breakers))
	for _, breaker := range m.breakers {
		stats = append(stats, breaker.Stats())
	}
	return stats
}

// Reset resets a specific circuit breaker by name.
func (m *Manager) Reset(name string) bool {
	m.mu.RLock()
	breaker, exists := m.breakers[name]
	m.mu.RUnlock()

	if !exists {
		return false
	}

	breaker.Reset()
	return true
}

// ResetAll resets all circuit breakers.
func (m *Manager) ResetAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, breaker := range m.breakers {
		breaker.Reset()
	}
}

// Remove removes a circuit breaker from the manager.
func (m *Manager) Remove(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, exists := m.breakers[name]
	if exists {
		delete(m.breakers, name)
	}
	return exists
}

// Count returns the number of registered circuit breakers.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.breakers)
}
