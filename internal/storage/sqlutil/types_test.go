package sqlutil

import (
	"database/sql/driver"
	"reflect"
	"testing"
	"time"
)

func TestJSONSlice_Scan(t *testing.T) {
	tests := []struct {
		name    string
		src     interface{}
		want    JSONSlice
		wantErr bool
	}{
		{
			name: "string source",
			src:  `["tag1","tag2","tag3"]`,
			want: JSONSlice{"tag1", "tag2", "tag3"},
		},
		{
			name: "byte slice source",
			src:  []byte(`["a","b"]`),
			want: JSONSlice{"a", "b"},
		},
		{
			name: "nil source",
			src:  nil,
			want: nil,
		},
		{
			name: "empty string",
			src:  "",
			want: nil,
		},
		{
			name: "empty array",
			src:  `[]`,
			want: JSONSlice{},
		},
		{
			name:    "invalid json",
			src:     `{bad}`,
			wantErr: true,
		},
		{
			name:    "unsupported type",
			src:     123,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got JSONSlice
			err := got.Scan(tt.src)
			if (err != nil) != tt.wantErr {
				t.Errorf("JSONSlice.Scan() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("JSONSlice.Scan() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJSONSlice_Value(t *testing.T) {
	tests := []struct {
		name    string
		slice   JSONSlice
		want    driver.Value
		wantErr bool
	}{
		{
			name:  "valid slice",
			slice: JSONSlice{"a", "b", "c"},
			want:  []byte(`["a","b","c"]`),
		},
		{
			name:  "nil slice",
			slice: nil,
			want:  nil,
		},
		{
			name:  "empty slice",
			slice: JSONSlice{},
			want:  []byte(`[]`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.slice.Value()
			if (err != nil) != tt.wantErr {
				t.Errorf("JSONSlice.Value() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("JSONSlice.Value() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTimestamp_Scan(t *testing.T) {
	tests := []struct {
		name    string
		src     interface{}
		want    Timestamp
		wantErr bool
	}{
		{
			name: "string source",
			src:  "2024-01-01T12:00:00.123456789Z",
			want: Timestamp{Time: time.Date(2024, 1, 1, 12, 0, 0, 123456789, time.UTC)},
		},
		{
			name: "byte slice source",
			src:  []byte("2024-06-15T08:30:45.987654321Z"),
			want: Timestamp{Time: time.Date(2024, 6, 15, 8, 30, 45, 987654321, time.UTC)},
		},
		{
			name: "nil source",
			src:  nil,
			want: Timestamp{},
		},
		{
			name: "empty string",
			src:  "",
			want: Timestamp{},
		},
		{
			name:    "invalid timestamp",
			src:     "not-a-time",
			wantErr: true,
		},
		{
			name:    "unsupported type",
			src:     123,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Timestamp
			err := got.Scan(tt.src)
			if (err != nil) != tt.wantErr {
				t.Errorf("Timestamp.Scan() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !got.Equal(tt.want.Time) {
				t.Errorf("Timestamp.Scan() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTimestamp_Value(t *testing.T) {
	tests := []struct {
		name string
		ts   Timestamp
		want driver.Value
	}{
		{
			name: "valid timestamp",
			ts:   Timestamp{Time: time.Date(2024, 1, 1, 12, 0, 0, 123456789, time.UTC)},
			want: "2024-01-01T12:00:00.123456789Z",
		},
		{
			name: "zero time",
			ts:   Timestamp{},
			want: nil,
		},
		{
			name: "timestamp with timezone converted to UTC",
			ts:   Timestamp{Time: time.Date(2024, 6, 15, 8, 30, 45, 987654321, time.FixedZone("PST", -7*3600))},
			want: "2024-06-15T15:30:45.987654321Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.ts.Value()
			if err != nil {
				t.Errorf("Timestamp.Value() unexpected error = %v", err)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Timestamp.Value() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewTimestamp(t *testing.T) {
	t1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	ts := NewTimestamp(t1)
	if !ts.Equal(t1) {
		t.Errorf("NewTimestamp() = %v, want %v", ts, t1)
	}
}

func TestJSONSlice_RoundTrip(t *testing.T) {
	// Test that Scan and Value are inverses
	original := JSONSlice{"tag1", "tag2", "tag3"}

	// Convert to driver value
	val, err := original.Value()
	if err != nil {
		t.Fatalf("Value() failed: %v", err)
	}

	// Scan back
	var scanned JSONSlice
	if err := scanned.Scan(val); err != nil {
		t.Fatalf("Scan() failed: %v", err)
	}

	if !reflect.DeepEqual(scanned, original) {
		t.Errorf("Round trip failed: original = %v, scanned = %v", original, scanned)
	}
}

func TestTimestamp_RoundTrip(t *testing.T) {
	// Test that Scan and Value are inverses
	original := Timestamp{Time: time.Date(2024, 6, 15, 14, 30, 45, 123456789, time.UTC)}

	// Convert to driver value
	val, err := original.Value()
	if err != nil {
		t.Fatalf("Value() failed: %v", err)
	}

	// Scan back
	var scanned Timestamp
	if err := scanned.Scan(val); err != nil {
		t.Fatalf("Scan() failed: %v", err)
	}

	if !scanned.Equal(original.Time) {
		t.Errorf("Round trip failed: original = %v, scanned = %v", original, scanned)
	}
}
