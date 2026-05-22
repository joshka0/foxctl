package timeutil

import "time"

// Clock provides time operations for code that needs deterministic tests.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	Until(t time.Time) time.Duration
}

// RealClock implements Clock using the system clock.
type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

func (RealClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

func (RealClock) Until(t time.Time) time.Duration {
	return time.Until(t)
}
