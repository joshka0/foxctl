// Package dbutil provides database utility functions for common operations.
package dbutil

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/timeutil"
)

// ScanTimestamps scans multiple RFC3339Nano timestamp strings from a row.
// Returns parsed times in the same order as the input strings.
func ScanTimestamps(timestamps ...string) ([]time.Time, error) {
	result := make([]time.Time, len(timestamps))
	for i, ts := range timestamps {
		t, err := timeutil.ParseRFC3339Nano(ts)
		if err != nil {
			return nil, err
		}
		result[i] = t
	}
	return result, nil
}

// ScanTimestampsMust scans multiple RFC3339Nano timestamp strings.
// Returns zero time for any timestamps that fail to parse.
// WARNING: Validates that returned times are not zero (0001-01-01) and logs warnings
// to help detect database corruption early. Callers should handle zero times appropriately.
func ScanTimestampsMust(timestamps ...string) []time.Time {
	result := make([]time.Time, len(timestamps))
	for i, ts := range timestamps {
		result[i] = timeutil.MustParseRFC3339Nano(ts)
		// Validate timestamps at database boundary to catch corruption early
		if result[i].IsZero() && ts != "" {
			log.Printf("dbutil: WARNING - zero time detected from non-empty timestamp %q (possible database corruption)", ts)
		}
	}
	return result
}

// ScanJSONArray unmarshals a JSON string into a string slice.
// Returns an error if unmarshaling fails.
func ScanJSONArray(jsonStr string) ([]string, error) {
	var result []string
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ScanJSONArrayMust unmarshals a JSON string into a string slice.
// Returns nil if unmarshaling fails and logs a warning.
// WARNING: Use ScanJSONArray instead to properly handle errors.
func ScanJSONArrayMust(jsonStr string) []string {
	var result []string
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		log.Printf("dbutil: WARNING - failed to unmarshal JSON array: %v (data: %q)", err, jsonStr)
		return nil
	}
	return result
}

// IsNoRows checks if an error is sql.ErrNoRows.
func IsNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// TimeScanner helps scan multiple timestamp columns with consistent error handling.
type TimeScanner struct {
	Created  string
	Updated  string
	Accessed string
}

// Parse parses all timestamps and returns them in order: created, updated, accessed.
func (ts *TimeScanner) Parse() (created, updated, accessed time.Time, err error) {
	times, err := ScanTimestamps(ts.Created, ts.Updated, ts.Accessed)
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, err
	}
	return times[0], times[1], times[2], nil
}

// MustParse parses all timestamps, returning zero times for any that fail.
// WARNING: Logs warnings for zero times to help detect database corruption early.
func (ts *TimeScanner) MustParse() (created, updated, accessed time.Time) {
	times := ScanTimestampsMust(ts.Created, ts.Updated, ts.Accessed)
	return times[0], times[1], times[2]
}
