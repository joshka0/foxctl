package main

import (
	"testing"
)

func TestParseSinceArg(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"7d", "7 days ago"},
		{"1w", "1 weeks ago"},
		{"1m", "1 months ago"},
		{"1y", "1 years ago"},
		{"2023-01-01", "2023-01-01"},
	}

	for _, tt := range tests {
		got := parseSinceArg(tt.in)
		if got != tt.want {
			t.Errorf("parseSinceArg(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseRecentChanges(t *testing.T) {
	output := `hash12345|author1|email1|date1|subject1
file1.go
file2.go

hash23456|author2|email2|date2|subject2
file3.go
`
	results := parseRecentChanges([]byte(output), "/tmp", 10)
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
	if results[0].File != "file1.go" {
		t.Errorf("expected file1.go, got %s", results[0].File)
	}
	if results[0].Commit != "hash123" {
		t.Errorf("expected shortened hash, got %s", results[0].Commit)
	}
}

func TestParseHotspots(t *testing.T) {
	output := `file1.go
file1.go
file2.go
`
	results := parseHotspots([]byte(output), "/tmp", 10)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if results[0].File != "file1.go" || results[0].Count != 2 {
		t.Errorf("expected file1.go with count 2, got %v", results[0])
	}
}

func TestParseBlame(t *testing.T) {
	hash1 := "1234567890123456789012345678901234567890"
	hash2 := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	output := hash1 + ` 1 10 1
author author1
author-mail <email1>
author-time 1234567890
	code line 1
` + hash2 + ` 2 11 1
author author2
author-mail <email2>
author-time 1234567891
	code line 2
`
	results := parseBlame([]byte(output), "file.go", 10)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Check line number parsing (match[3])
	if results[0].Line != 10 {
		t.Errorf("expected line 10, got %d", results[0].Line)
	}
	if results[1].Line != 11 {
		t.Errorf("expected line 11, got %d", results[1].Line)
	}

	if results[0].LineText != "code line 1" {
		t.Errorf("expected code line 1, got %q", results[0].LineText)
	}
}
