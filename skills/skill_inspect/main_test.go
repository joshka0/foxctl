package main

import (
	"testing"
)

func TestFormatType(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"string", "string"},
		{"integer", "integer"},
		{"array", "array"},
		{"", "any"},
	}

	for _, tt := range tests {
		if got := formatType(tt.in); got != tt.want {
			t.Errorf("formatType(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExtractDescription(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"Short desc", "Short desc"},
		{"Long description that is definitely longer than the truncate limit we have set in the function which is around fifty characters...", "Long description that is definitely longer than th..."},
	}

	for _, tt := range tests {
		got := extractDescription(tt.in)
		if got != tt.want {
			t.Errorf("extractDescription(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
