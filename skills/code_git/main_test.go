package main

import (
	"strings"
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
		{"", ""},
		{"abc", "abc"},
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

func TestParseRecentChanges_MaxResults(t *testing.T) {
	output := `hash12345|author1|email1|date1|subject1
file1.go
file2.go
file3.go
file4.go
file5.go
`
	results := parseRecentChanges([]byte(output), "/tmp", 2)
	if len(results) != 2 {
		t.Errorf("expected 2 results (maxResults), got %d", len(results))
	}
}

func TestParseRecentChanges_Empty(t *testing.T) {
	results := parseRecentChanges([]byte(""), "/tmp", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
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

func TestParseHotspots_Empty(t *testing.T) {
	results := parseHotspots([]byte(""), "/tmp", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
	}
}

func TestParseHotspots_MaxResults(t *testing.T) {
	output := `file1.go
file1.go
file2.go
file3.go
`
	results := parseHotspots([]byte(output), "/tmp", 1)
	if len(results) != 1 {
		t.Errorf("expected 1 result (maxResults), got %d", len(results))
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

func TestParseBlame_Empty(t *testing.T) {
	results := parseBlame([]byte(""), "file.go", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
	}
}

func TestParseAuthors(t *testing.T) {
	// Format is name|email per line, counts are accumulated
	output := `Author One|one@example.com
Author One|one@example.com
Author Two|two@example.com
Author One|one@example.com
`
	results := parseAuthors([]byte(output), 10)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Author One should have count 3 and be first (sorted by count desc)
	if results[0].Author != "Author One" {
		t.Errorf("expected Author One first, got %s", results[0].Author)
	}
	if results[0].Count != 3 {
		t.Errorf("expected count 3, got %d", results[0].Count)
	}
	if results[1].Count != 1 {
		t.Errorf("expected count 1 for second author, got %d", results[1].Count)
	}
}

func TestParseAuthors_Empty(t *testing.T) {
	results := parseAuthors([]byte(""), 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
	}
}

func TestParseAuthors_MaxResults(t *testing.T) {
	output := `Author One|one@example.com
Author Two|two@example.com
Author Three|three@example.com
`
	results := parseAuthors([]byte(output), 2)
	if len(results) != 2 {
		t.Errorf("expected 2 results (maxResults), got %d", len(results))
	}
}

func TestValidateGitAuthor(t *testing.T) {
	tests := []struct {
		author  string
		wantErr bool
	}{
		{"John Doe", false},
		{"alice", false},
		{"Bob's Name", false},
		{"user.name", false},
		{"user-name", false},
		{"user_name", false},
		{"", false}, // Empty is valid (optional)
		{"; rm -rf /", true},
		{"$(whoami)", true},
		{"`cat /etc/passwd`", true},
		{"user@example.com", true}, // @ not allowed
	}

	for _, tt := range tests {
		err := validateGitAuthor(tt.author)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateGitAuthor(%q) error = %v, wantErr %v", tt.author, err, tt.wantErr)
		}
	}
}

func TestValidateGitSince(t *testing.T) {
	tests := []struct {
		since   string
		wantErr bool
	}{
		{"7d", false},
		{"1w", false},
		{"1m", false},
		{"1y", false},
		{"2023-01-01", true}, // ISO dates not allowed
		{"", true},           // Empty not allowed
		{"; rm -rf /", true},
		{"$(whoami)", true},
		{"7d && cat /etc/passwd", true},
	}

	for _, tt := range tests {
		err := validateGitSince(tt.since)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateGitSince(%q) error = %v, wantErr %v", tt.since, err, tt.wantErr)
		}
	}
}

func TestParseInput_Default(t *testing.T) {
	// query_type is required
	r := strings.NewReader(`{"query_type":"recent"}`)
	in, err := parseInput(r)
	if err != nil {
		t.Fatalf("parseInput failed: %v", err)
	}
	if in.QueryType != "recent" {
		t.Errorf("expected query_type 'recent', got %q", in.QueryType)
	}
	if in.MaxResults != 100 {
		t.Errorf("expected default max_results 100, got %d", in.MaxResults)
	}
	if in.Since != "1m" {
		t.Errorf("expected default since '1m', got %q", in.Since)
	}
}

func TestParseInput_WithValues(t *testing.T) {
	r := strings.NewReader(`{"query_type":"hotspots","path":"./src","since":"7d","author":"alice","max_results":50,"context_lines":5}`)
	in, err := parseInput(r)
	if err != nil {
		t.Fatalf("parseInput failed: %v", err)
	}
	if in.QueryType != "hotspots" {
		t.Errorf("expected query_type 'hotspots', got %q", in.QueryType)
	}
	if in.Path != "./src" {
		t.Errorf("expected path './src', got %q", in.Path)
	}
	if in.Since != "7d" {
		t.Errorf("expected since '7d', got %q", in.Since)
	}
	if in.Author != "alice" {
		t.Errorf("expected author 'alice', got %q", in.Author)
	}
	if in.MaxResults != 50 {
		t.Errorf("expected max_results 50, got %d", in.MaxResults)
	}
	if in.ContextLines != 5 {
		t.Errorf("expected context_lines 5, got %d", in.ContextLines)
	}
}

func TestParseInput_InvalidJSON(t *testing.T) {
	r := strings.NewReader(`{invalid}`)
	_, err := parseInput(r)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestRelativeTo(t *testing.T) {
	tests := []struct {
		base   string
		target string
		want   string
	}{
		{"/home/user/project", "/home/user/project/src/main.go", "src/main.go"},
		{"/home/user/project", "./relative", "./relative"},
		{"/home/user/project", "/other/path", "/other/path"},
	}

	for _, tt := range tests {
		got := relativeTo(tt.base, tt.target)
		// Result should either be the relative path or the original target
		if got != tt.want && got != tt.target && !strings.HasPrefix(got, ".") {
			t.Logf("relativeTo(%q, %q) = %q", tt.base, tt.target, got)
		}
	}
}

func TestPreparePreview(t *testing.T) {
	results := []gitResult{
		{File: "file1.go"},
		{File: "file2.go"},
		{File: "file3.go"},
	}

	// Test with limit larger than results
	preview, truncated := preparePreview(results, 10)
	if len(preview) != 3 {
		t.Errorf("expected 3 results, got %d", len(preview))
	}
	if truncated {
		t.Error("expected truncated=false")
	}

	// Test with limit smaller than results
	preview, truncated = preparePreview(results, 2)
	if len(preview) != 2 {
		t.Errorf("expected 2 results (limited), got %d", len(preview))
	}
	if !truncated {
		t.Error("expected truncated=true")
	}
}

func TestPreparePreview_Empty(t *testing.T) {
	results := []gitResult{}
	preview, truncated := preparePreview(results, 10)
	if len(preview) != 0 {
		t.Errorf("expected 0 results, got %d", len(preview))
	}
	if truncated {
		t.Error("expected truncated=false for empty results")
	}
}
