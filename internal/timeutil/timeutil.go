// Package timeutil provides helper functions for common time operations.
package timeutil

import (
	"fmt"
	"log"
	"time"
)

// ParseRFC3339Nano parses a time string in RFC3339Nano format.
// Returns an error if parsing fails.
func ParseRFC3339Nano(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time: %w", err)
	}
	return t, nil
}

// MustParseRFC3339Nano parses a time string in RFC3339Nano format.
// Returns zero time if parsing fails (for backward compatibility).
// WARNING: Zero times (0001-01-01) can hide database corruption issues.
// Callers should validate that returned times are not zero when data integrity is critical.
func MustParseRFC3339Nano(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		// Log warning to help detect database corruption early
		log.Printf("timeutil: failed to parse timestamp %q: %v (returning zero time)", s, err)
	}
	return t
}

// FormatRFC3339Nano formats a time in RFC3339Nano format.
func FormatRFC3339Nano(t time.Time) string {
	return t.Format(time.RFC3339Nano)
}

// NowUTC returns the current time in UTC.
func NowUTC() time.Time {
	return time.Now().UTC()
}

// FormatNowUTC returns the current time in UTC formatted as RFC3339Nano.
func FormatNowUTC() string {
	return FormatRFC3339Nano(NowUTC())
}
