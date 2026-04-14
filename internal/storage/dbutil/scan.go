package dbutil

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/platform/timeutil"
	"github.com/joshka0/foxctl/internal/runtime/observability"
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
			observability.Emit(context.Background(), observability.NewEvent("dbutil.zero_time_detected").
				WithComponent("dbutil").
				WithData("timestamp", ts).
				WithData("message", "zero time detected from non-empty timestamp (possible database corruption)").
				Error(nil, 0))
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
// ScanJSONArrayMust parses jsonStr into a slice of strings, tolerating failures.
// It returns an empty slice if jsonStr is empty or contains only whitespace.
// If unmarshaling fails, it emits an observability event ("dbutil.json_unmarshal_failed")
// that includes the original data and captured caller location, and returns nil.
func ScanJSONArrayMust(jsonStr string) []string {
	var result []string
	if len(strings.TrimSpace(jsonStr)) == 0 {
		return result
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// Log metadata about the payload without exposing raw content
		dataHash := fmt.Sprintf("%x", sha256.Sum256([]byte(jsonStr)))
		snippetLen := 200
		if len(jsonStr) < snippetLen {
			snippetLen = len(jsonStr)
		}
		event := observability.NewEvent("dbutil.json_unmarshal_failed").
			WithComponent("dbutil").
			WithData("data_length", len(jsonStr)).
			WithData("data_hash", dataHash).
			WithData("data_snippet", jsonStr[:snippetLen])
		pcs := make([]uintptr, 8)
		count := runtime.Callers(2, pcs)
		frames := runtime.CallersFrames(pcs[:count])
		for {
			frame, more := frames.Next()
			fullPath := frame.File
			normalizedPath := filepath.ToSlash(fullPath)
			if normalizedPath != "" && !strings.Contains(normalizedPath, "/internal/storage/dbutil/") {
				file := filepath.Base(fullPath)
				event = event.
					WithData("caller", fmt.Sprintf("%s:%d", file, frame.Line)).
					WithData("caller_file", file).
					WithData("caller_path", fullPath).
					WithData("caller_line", frame.Line)
				if frame.Function != "" {
					event = event.WithData("caller_func", frame.Function)
				}
				break
			}
			if !more {
				break
			}
		}
		observability.Emit(context.Background(), event.Error(err, 0))
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
