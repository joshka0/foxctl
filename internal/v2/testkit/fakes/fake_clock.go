package fakes

import (
	"sync"
	"time"
)

// FakeClock returns deterministic timestamps, advancing by Step() after Now().
type FakeClock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

func NewFakeClock(start time.Time, step time.Duration) *FakeClock {
	return &FakeClock{now: start.UTC(), step: step}
}

// Now returns the current time and advances by the configured step.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	current := c.now
	c.now = c.now.Add(c.step)
	return current
}

// Peek returns the current time without advancing.
func (c *FakeClock) Peek() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
