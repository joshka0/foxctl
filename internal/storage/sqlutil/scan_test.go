package sqlutil

import (
	"database/sql"
	"reflect"
	"testing"
	"time"
)

func TestScanJSON(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		dest    any
		want    any
		wantErr bool
	}{
		{
			name: "valid string array",
			src:  `["a","b","c"]`,
			dest: &[]string{},
			want: &[]string{"a", "b", "c"},
		},
		{
			name: "empty string treated as null",
			src:  "",
			dest: &[]string{"existing"},
			want: &[]string{"existing"}, // unchanged
		},
		{
			name:    "invalid json",
			src:     `{bad json}`,
			dest:    &[]string{},
			wantErr: true,
		},
		{
			name: "valid object",
			src:  `{"key":"value"}`,
			dest: &map[string]string{},
			want: &map[string]string{"key": "value"},
		},
		{
			name: "empty array",
			src:  `[]`,
			dest: &[]string{},
			want: &[]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ScanJSON(tt.src, tt.dest)
			if (err != nil) != tt.wantErr {
				t.Errorf("ScanJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(tt.dest, tt.want) {
				t.Errorf("ScanJSON() got = %v, want %v", tt.dest, tt.want)
			}
		})
	}
}

func TestScanTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		want    time.Time
		wantErr bool
	}{
		{
			name: "valid RFC3339Nano timestamp",
			src:  "2024-01-01T12:00:00.123456789Z",
			want: time.Date(2024, 1, 1, 12, 0, 0, 123456789, time.UTC),
		},
		{
			name: "empty string treated as null",
			src:  "",
			want: time.Time{},
		},
		{
			name:    "invalid timestamp",
			src:     "not-a-time",
			wantErr: true,
		},
		{
			name: "valid timestamp with timezone",
			src:  "2024-06-15T08:30:45.987654321-07:00",
			want: time.Date(2024, 6, 15, 15, 30, 45, 987654321, time.UTC), // converted to UTC
		},
		{
			name:    "invalid format",
			src:     "2024-01-01 12:00:00",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ScanTimestamp(tt.src)
			if (err != nil) != tt.wantErr {
				t.Errorf("ScanTimestamp() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !got.Equal(tt.want) {
				t.Errorf("ScanTimestamp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScanNullableTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		src     sql.NullString
		want    time.Time
		wantErr bool
	}{
		{
			name: "valid nullable timestamp",
			src:  sql.NullString{String: "2024-01-01T12:00:00.123456789Z", Valid: true},
			want: time.Date(2024, 1, 1, 12, 0, 0, 123456789, time.UTC),
		},
		{
			name: "null value",
			src:  sql.NullString{String: "", Valid: false},
			want: time.Time{},
		},
		{
			name:    "valid flag but invalid timestamp",
			src:     sql.NullString{String: "invalid", Valid: true},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ScanNullableTimestamp(tt.src)
			if (err != nil) != tt.wantErr {
				t.Errorf("ScanNullableTimestamp() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !got.Equal(tt.want) {
				t.Errorf("ScanNullableTimestamp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{
			name: "valid timestamp",
			t:    time.Date(2024, 1, 1, 12, 0, 0, 123456789, time.UTC),
			want: "2024-01-01T12:00:00.123456789Z",
		},
		{
			name: "zero time returns empty string",
			t:    time.Time{},
			want: "",
		},
		{
			name: "timestamp with timezone converted to UTC",
			t:    time.Date(2024, 6, 15, 8, 30, 45, 987654321, time.FixedZone("PST", -7*3600)),
			want: "2024-06-15T15:30:45.987654321Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTimestamp(tt.t)
			if got != tt.want {
				t.Errorf("FormatTimestamp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatJSON(t *testing.T) {
	tests := []struct {
		name    string
		v       any
		want    string
		wantErr bool
	}{
		{
			name: "string array",
			v:    []string{"a", "b", "c"},
			want: `["a","b","c"]`,
		},
		{
			name: "nil value returns empty string",
			v:    nil,
			want: "",
		},
		{
			name: "empty array",
			v:    []string{},
			want: `[]`,
		},
		{
			name: "map",
			v:    map[string]string{"key": "value"},
			want: `{"key":"value"}`,
		},
		{
			name:    "unmarshalable type",
			v:       make(chan int), // channels can't be marshaled
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FormatJSON(tt.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("FormatJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("FormatJSON() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatTimestamp_RoundTrip(t *testing.T) {
	// Test that formatting and parsing are inverses
	original := time.Date(2024, 6, 15, 14, 30, 45, 123456789, time.UTC)
	formatted := FormatTimestamp(original)
	parsed, err := ScanTimestamp(formatted)
	if err != nil {
		t.Fatalf("Failed to parse formatted timestamp: %v", err)
	}
	if !parsed.Equal(original) {
		t.Errorf("Round trip failed: original = %v, parsed = %v", original, parsed)
	}
}

func TestFormatJSON_RoundTrip(t *testing.T) {
	// Test that formatting and parsing are inverses
	original := []string{"tag1", "tag2", "tag3"}
	formatted, err := FormatJSON(original)
	if err != nil {
		t.Fatalf("Failed to format JSON: %v", err)
	}

	var parsed []string
	if err := ScanJSON(formatted, &parsed); err != nil {
		t.Fatalf("Failed to parse formatted JSON: %v", err)
	}

	if !reflect.DeepEqual(parsed, original) {
		t.Errorf("Round trip failed: original = %v, parsed = %v", original, parsed)
	}
}
