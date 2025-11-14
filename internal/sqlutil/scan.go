// Package sqlutil provides reusable utilities for database operations,
// including scanning helpers and custom SQL types.
package sqlutil

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// ScanJSON unmarshals a JSON string column into a Go value.
// If src is empty, the dest is left unchanged (representing NULL).
func ScanJSON(src string, dest interface{}) error {
	if src == "" {
		return nil // Empty string = null
	}
	if err := json.Unmarshal([]byte(src), dest); err != nil {
		return fmt.Errorf("unmarshal json: %w", err)
	}
	return nil
}

// ScanTimestamp parses an RFC3339Nano timestamp string.
// If src is empty, returns zero time (representing NULL).
func ScanTimestamp(src string) (time.Time, error) {
	if src == "" {
		return time.Time{}, nil // Empty string = null
	}
	t, err := time.Parse(time.RFC3339Nano, src)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp: %w", err)
	}
	return t, nil
}

// ScanNullableTimestamp parses a timestamp that may be NULL.
// If the SQL value is NULL (src.Valid is false), returns zero time.
func ScanNullableTimestamp(src sql.NullString) (time.Time, error) {
	if !src.Valid {
		return time.Time{}, nil
	}
	return ScanTimestamp(src.String)
}

// FormatTimestamp formats a time for SQL storage using RFC3339Nano.
// If t is zero, returns empty string (representing NULL).
func FormatTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// FormatJSON marshals a value to JSON for SQL storage.
// If v is nil, returns empty string (representing NULL).
func FormatJSON(v interface{}) (string, error) {
	if v == nil {
		return "", nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal json: %w", err)
	}
	return string(data), nil
}

// ScanRow is a helper for scanning a single row with error accumulation.
// This allows chaining multiple scan operations and checking errors once.
type ScanRow struct {
	row *sql.Row
	err error
}

// Scan wraps row.Scan with error accumulation.
// If a previous scan failed, this is a no-op.
func (s *ScanRow) Scan(dest ...interface{}) *ScanRow {
	if s.err != nil {
		return s
	}
	s.err = s.row.Scan(dest...)
	return s
}

// Err returns any accumulated errors from scanning operations.
func (s *ScanRow) Err() error {
	return s.err
}

// NoRows returns true if the error is sql.ErrNoRows.
func (s *ScanRow) NoRows() bool {
	return s.err == sql.ErrNoRows
}

// NewScanRow creates a ScanRow wrapper for error-handling convenience.
func NewScanRow(row *sql.Row) *ScanRow {
	return &ScanRow{row: row}
}
