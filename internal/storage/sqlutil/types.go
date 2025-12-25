package sqlutil

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// JSONSlice is a []string that marshals to/from JSON in SQL.
// It implements sql.Scanner and driver.Valuer for automatic conversion.
type JSONSlice []string

// Scan implements sql.Scanner for reading from database.
func (j *JSONSlice) Scan(src any) error {
	if src == nil {
		*j = nil
		return nil
	}

	var data []byte
	switch v := src.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("unsupported type: %T", src)
	}

	if len(data) == 0 {
		*j = nil
		return nil
	}

	return json.Unmarshal(data, j)
}

// Value implements driver.Valuer for writing to database.
func (j JSONSlice) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Timestamp wraps time.Time with RFC3339Nano SQL encoding.
// It implements sql.Scanner and driver.Valuer for automatic conversion.
type Timestamp struct {
	time.Time
}

// Scan implements sql.Scanner for reading from database.
func (t *Timestamp) Scan(src any) error {
	if src == nil {
		t.Time = time.Time{}
		return nil
	}

	var str string
	switch v := src.(type) {
	case []byte:
		str = string(v)
	case string:
		str = v
	default:
		return fmt.Errorf("unsupported type: %T", src)
	}

	if str == "" {
		t.Time = time.Time{}
		return nil
	}

	parsed, err := time.Parse(time.RFC3339Nano, str)
	if err != nil {
		return fmt.Errorf("parse timestamp: %w", err)
	}
	t.Time = parsed
	return nil
}

// Value implements driver.Valuer for writing to database.
func (t Timestamp) Value() (driver.Value, error) {
	if t.IsZero() {
		return nil, nil
	}
	return t.UTC().Format(time.RFC3339Nano), nil
}

// NewTimestamp creates a Timestamp from time.Time.
func NewTimestamp(t time.Time) Timestamp {
	return Timestamp{Time: t}
}
