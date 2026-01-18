// Package skilltest provides test utilities for skill tests.
package skilltest

import (
	"sync"
	"time"
)

// Clock provides time operations that can be mocked in tests.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	Until(t time.Time) time.Duration
}

// RealClock implements Clock using the real system time.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time {
	return time.Now()
}

// Since returns the time elapsed since t.
func (RealClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

// Until returns the time until t.
func (RealClock) Until(t time.Time) time.Duration {
	return time.Until(t)
}

// FakeClock implements Clock with a manually controllable time.
// It is safe for concurrent use.
type FakeClock struct {
	mu  sync.RWMutex
	now time.Time
}

// NewFakeClock creates a FakeClock initialized to the given time.
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{now: t}
}

// NewFakeClockAt creates a FakeClock at a fixed point in time.
// Useful for tests that need deterministic timestamps.
func NewFakeClockAt(year int, month time.Month, day, hour, min, sec int) *FakeClock {
	t := time.Date(year, month, day, hour, min, sec, 0, time.UTC)
	return NewFakeClock(t)
}

// Now returns the fake current time.
func (c *FakeClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

// Since returns the duration since t using the fake clock.
func (c *FakeClock) Since(t time.Time) time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now.Sub(t)
}

// Until returns the duration until t using the fake clock.
func (c *FakeClock) Until(t time.Time) time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return t.Sub(c.now)
}

// Set changes the fake clock's current time.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Advance moves the fake clock forward by the given duration.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// AdvanceMinutes is a convenience method to advance by minutes.
func (c *FakeClock) AdvanceMinutes(mins int) {
	c.Advance(time.Duration(mins) * time.Minute)
}

// AdvanceHours is a convenience method to advance by hours.
func (c *FakeClock) AdvanceHours(hours int) {
	c.Advance(time.Duration(hours) * time.Hour)
}

// AdvanceDays is a convenience method to advance by days.
func (c *FakeClock) AdvanceDays(days int) {
	c.Advance(time.Duration(days) * 24 * time.Hour)
}

// FixedTime returns a time.Time suitable for deterministic tests.
// Returns 2024-01-15 10:30:00 UTC - a fixed point for consistent snapshots.
func FixedTime() time.Time {
	return time.Date(2024, time.January, 15, 10, 30, 0, 0, time.UTC)
}

// FixedClock returns a FakeClock at the standard fixed time.
func FixedClock() *FakeClock {
	return NewFakeClock(FixedTime())
}

// TimeGenerator provides a sequence of timestamps for testing.
// Each call to Next() returns a time offset from the base by an increment.
type TimeGenerator struct {
	base      time.Time
	increment time.Duration
	current   int
	mu        sync.Mutex
}

// NewTimeGenerator creates a TimeGenerator starting at base,
// incrementing by the given duration on each call to Next().
func NewTimeGenerator(base time.Time, increment time.Duration) *TimeGenerator {
	return &TimeGenerator{
		base:      base,
		increment: increment,
		current:   0,
	}
}

// Next returns the next time in the sequence.
func (g *TimeGenerator) Next() time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	t := g.base.Add(time.Duration(g.current) * g.increment)
	g.current++
	return t
}

// Reset resets the generator to the beginning of the sequence.
func (g *TimeGenerator) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.current = 0
}
