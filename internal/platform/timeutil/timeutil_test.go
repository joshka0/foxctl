package timeutil

import (
	"testing"
	"time"
)

func TestParseRFC3339Nano(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid timestamp",
			input:   "2024-01-15T10:30:45.123456789Z",
			wantErr: false,
		},
		{
			name:    "valid timestamp without nanoseconds",
			input:   "2024-01-15T10:30:45Z",
			wantErr: false,
		},
		{
			name:    "valid timestamp with timezone",
			input:   "2024-01-15T10:30:45.123456789+00:00",
			wantErr: false,
		},
		{
			name:    "invalid format",
			input:   "2024-01-15 10:30:45",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid timestamp",
			input:   "not-a-timestamp",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRFC3339Nano(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRFC3339Nano() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.IsZero() {
				t.Errorf("ParseRFC3339Nano() returned zero time for valid input")
			}
		})
	}
}

func TestMustParseRFC3339Nano(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantZero bool
	}{
		{
			name:     "valid timestamp",
			input:    "2024-01-15T10:30:45.123456789Z",
			wantZero: false,
		},
		{
			name:     "valid timestamp without nanoseconds",
			input:    "2024-01-15T10:30:45Z",
			wantZero: false,
		},
		{
			name:     "invalid format returns zero time",
			input:    "2024-01-15 10:30:45",
			wantZero: true,
		},
		{
			name:     "empty string returns zero time",
			input:    "",
			wantZero: true,
		},
		{
			name:     "invalid timestamp returns zero time",
			input:    "not-a-timestamp",
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MustParseRFC3339Nano(tt.input)
			if got.IsZero() != tt.wantZero {
				t.Errorf("MustParseRFC3339Nano() IsZero = %v, want %v", got.IsZero(), tt.wantZero)
			}
		})
	}
}

func TestFormatRFC3339Nano(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 45, 123456789, time.UTC)

	tests := []struct {
		name  string
		input time.Time
		want  string
	}{
		{
			name:  "format time with nanoseconds",
			input: testTime,
			want:  "2024-01-15T10:30:45.123456789Z",
		},
		{
			name:  "format zero time",
			input: time.Time{},
			want:  "0001-01-01T00:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatRFC3339Nano(tt.input)
			if got != tt.want {
				t.Errorf("FormatRFC3339Nano() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNowUTC(t *testing.T) {
	before := time.Now().UTC()
	got := NowUTC()
	after := time.Now().UTC()

	// Check that the returned time is in UTC
	if got.Location() != time.UTC {
		t.Errorf("NowUTC() location = %v, want UTC", got.Location())
	}

	// Check that the time is between before and after
	if got.Before(before) || got.After(after) {
		t.Errorf("NowUTC() returned time outside expected range")
	}
}

func TestFormatNowUTC(t *testing.T) {
	got := FormatNowUTC()

	// Try to parse the formatted time to verify it's valid RFC3339Nano
	_, err := time.Parse(time.RFC3339Nano, got)
	if err != nil {
		t.Errorf("FormatNowUTC() returned invalid RFC3339Nano format: %v", err)
	}

	// Verify the timestamp ends with Z (UTC indicator)
	if got[len(got)-1] != 'Z' {
		t.Errorf("FormatNowUTC() should end with 'Z' for UTC, got: %s", got)
	}
}

func TestParseAndFormatRoundTrip(t *testing.T) {
	original := "2024-01-15T10:30:45.123456789Z"

	// Parse the original timestamp
	parsed, err := ParseRFC3339Nano(original)
	if err != nil {
		t.Fatalf("ParseRFC3339Nano() failed: %v", err)
	}

	// Format it back
	formatted := FormatRFC3339Nano(parsed)

	// Compare
	if formatted != original {
		t.Errorf("Round trip failed: got %v, want %v", formatted, original)
	}
}
