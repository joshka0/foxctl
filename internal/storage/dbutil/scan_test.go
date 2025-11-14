package dbutil

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestScanTimestamps(t *testing.T) {
	tests := []struct {
		name       string
		timestamps []string
		wantErr    bool
		wantLen    int
	}{
		{
			name: "valid timestamps",
			timestamps: []string{
				"2024-01-15T10:30:45.123456789Z",
				"2024-01-15T10:31:45.123456789Z",
				"2024-01-15T10:32:45.123456789Z",
			},
			wantErr: false,
			wantLen: 3,
		},
		{
			name:       "empty slice",
			timestamps: []string{},
			wantErr:    false,
			wantLen:    0,
		},
		{
			name: "one invalid timestamp",
			timestamps: []string{
				"2024-01-15T10:30:45.123456789Z",
				"invalid-timestamp",
			},
			wantErr: true,
		},
		{
			name: "all invalid timestamps",
			timestamps: []string{
				"invalid-timestamp-1",
				"invalid-timestamp-2",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ScanTimestamps(tt.timestamps...)
			if (err != nil) != tt.wantErr {
				t.Errorf("ScanTimestamps() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(got) != tt.wantLen {
				t.Errorf("ScanTimestamps() len = %v, want %v", len(got), tt.wantLen)
			}
			// Verify all returned times are non-zero for valid inputs
			if !tt.wantErr {
				for i, ts := range got {
					if ts.IsZero() {
						t.Errorf("ScanTimestamps() returned zero time at index %d", i)
					}
				}
			}
		})
	}
}

func TestScanTimestampsMust(t *testing.T) {
	tests := []struct {
		name       string
		timestamps []string
		wantLen    int
		wantZeros  []bool // expected zero status for each timestamp
	}{
		{
			name: "all valid timestamps",
			timestamps: []string{
				"2024-01-15T10:30:45.123456789Z",
				"2024-01-15T10:31:45.123456789Z",
			},
			wantLen:   2,
			wantZeros: []bool{false, false},
		},
		{
			name:       "empty slice",
			timestamps: []string{},
			wantLen:    0,
			wantZeros:  []bool{},
		},
		{
			name: "mixed valid and invalid",
			timestamps: []string{
				"2024-01-15T10:30:45.123456789Z",
				"invalid-timestamp",
			},
			wantLen:   2,
			wantZeros: []bool{false, true},
		},
		{
			name: "all invalid timestamps",
			timestamps: []string{
				"invalid-timestamp-1",
				"invalid-timestamp-2",
			},
			wantLen:   2,
			wantZeros: []bool{true, true},
		},
		{
			name: "empty string",
			timestamps: []string{
				"",
			},
			wantLen:   1,
			wantZeros: []bool{true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScanTimestampsMust(tt.timestamps...)
			if len(got) != tt.wantLen {
				t.Errorf("ScanTimestampsMust() len = %v, want %v", len(got), tt.wantLen)
				return
			}
			for i, want := range tt.wantZeros {
				if got[i].IsZero() != want {
					t.Errorf("ScanTimestampsMust() index %d IsZero = %v, want %v", i, got[i].IsZero(), want)
				}
			}
		})
	}
}

func TestScanJSONArray(t *testing.T) {
	tests := []struct {
		name    string
		jsonStr string
		want    []string
		wantErr bool
	}{
		{
			name:    "valid JSON array",
			jsonStr: `["digest1","digest2","digest3"]`,
			want:    []string{"digest1", "digest2", "digest3"},
			wantErr: false,
		},
		{
			name:    "empty JSON array",
			jsonStr: `[]`,
			want:    []string{},
			wantErr: false,
		},
		{
			name:    "single element array",
			jsonStr: `["single"]`,
			want:    []string{"single"},
			wantErr: false,
		},
		{
			name:    "invalid JSON returns error",
			jsonStr: `not-json`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty string returns error",
			jsonStr: "",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "JSON object instead of array returns error",
			jsonStr: `{"key":"value"}`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ScanJSONArray(tt.jsonStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ScanJSONArray() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ScanJSONArray() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScanJSONArrayMust(t *testing.T) {
	tests := []struct {
		name    string
		jsonStr string
		want    []string
	}{
		{
			name:    "valid JSON array",
			jsonStr: `["digest1","digest2","digest3"]`,
			want:    []string{"digest1", "digest2", "digest3"},
		},
		{
			name:    "empty JSON array",
			jsonStr: `[]`,
			want:    []string{},
		},
		{
			name:    "single element array",
			jsonStr: `["single"]`,
			want:    []string{"single"},
		},
		{
			name:    "invalid JSON returns nil",
			jsonStr: `not-json`,
			want:    nil,
		},
		{
			name:    "empty string returns nil",
			jsonStr: "",
			want:    nil,
		},
		{
			name:    "JSON object instead of array returns nil",
			jsonStr: `{"key":"value"}`,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScanJSONArrayMust(tt.jsonStr)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ScanJSONArrayMust() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsNoRows(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "sql.ErrNoRows returns true",
			err:  sql.ErrNoRows,
			want: true,
		},
		{
			name: "nil error returns false",
			err:  nil,
			want: false,
		},
		{
			name: "other error returns false",
			err:  errors.New("some other error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNoRows(tt.err); got != tt.want {
				t.Errorf("IsNoRows() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTimeScanner_Parse(t *testing.T) {
	tests := []struct {
		name        string
		scanner     TimeScanner
		wantErr     bool
		checkResult bool
	}{
		{
			name: "valid timestamps",
			scanner: TimeScanner{
				Created:  "2024-01-15T10:30:45.123456789Z",
				Updated:  "2024-01-15T10:31:45.123456789Z",
				Accessed: "2024-01-15T10:32:45.123456789Z",
			},
			wantErr:     false,
			checkResult: true,
		},
		{
			name: "invalid created timestamp",
			scanner: TimeScanner{
				Created:  "invalid",
				Updated:  "2024-01-15T10:31:45.123456789Z",
				Accessed: "2024-01-15T10:32:45.123456789Z",
			},
			wantErr: true,
		},
		{
			name: "invalid updated timestamp",
			scanner: TimeScanner{
				Created:  "2024-01-15T10:30:45.123456789Z",
				Updated:  "invalid",
				Accessed: "2024-01-15T10:32:45.123456789Z",
			},
			wantErr: true,
		},
		{
			name: "invalid accessed timestamp",
			scanner: TimeScanner{
				Created:  "2024-01-15T10:30:45.123456789Z",
				Updated:  "2024-01-15T10:31:45.123456789Z",
				Accessed: "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created, updated, accessed, err := tt.scanner.Parse()
			if (err != nil) != tt.wantErr {
				t.Errorf("TimeScanner.Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.checkResult && !tt.wantErr {
				if created.IsZero() || updated.IsZero() || accessed.IsZero() {
					t.Errorf("TimeScanner.Parse() returned zero times for valid input")
				}
				// Verify timestamps are in correct order (created <= updated <= accessed)
				if created.After(updated) || updated.After(accessed) {
					t.Errorf("TimeScanner.Parse() returned timestamps out of expected order")
				}
			}
		})
	}
}

func TestTimeScanner_MustParse(t *testing.T) {
	tests := []struct {
		name      string
		scanner   TimeScanner
		wantZeros [3]bool // expected zero status for created, updated, accessed
	}{
		{
			name: "all valid timestamps",
			scanner: TimeScanner{
				Created:  "2024-01-15T10:30:45.123456789Z",
				Updated:  "2024-01-15T10:31:45.123456789Z",
				Accessed: "2024-01-15T10:32:45.123456789Z",
			},
			wantZeros: [3]bool{false, false, false},
		},
		{
			name: "invalid created timestamp",
			scanner: TimeScanner{
				Created:  "invalid",
				Updated:  "2024-01-15T10:31:45.123456789Z",
				Accessed: "2024-01-15T10:32:45.123456789Z",
			},
			wantZeros: [3]bool{true, false, false},
		},
		{
			name: "all invalid timestamps",
			scanner: TimeScanner{
				Created:  "invalid1",
				Updated:  "invalid2",
				Accessed: "invalid3",
			},
			wantZeros: [3]bool{true, true, true},
		},
		{
			name: "mixed valid and invalid",
			scanner: TimeScanner{
				Created:  "2024-01-15T10:30:45.123456789Z",
				Updated:  "invalid",
				Accessed: "2024-01-15T10:32:45.123456789Z",
			},
			wantZeros: [3]bool{false, true, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created, updated, accessed := tt.scanner.MustParse()
			times := [3]time.Time{created, updated, accessed}
			names := [3]string{"created", "updated", "accessed"}

			for i, want := range tt.wantZeros {
				if times[i].IsZero() != want {
					t.Errorf("TimeScanner.MustParse() %s IsZero = %v, want %v", names[i], times[i].IsZero(), want)
				}
			}
		})
	}
}
