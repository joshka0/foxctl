package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestBuildRipgrepArgs(t *testing.T) {
	tests := []struct {
		name    string
		in      input
		wantLen int // approximate length check
	}{
		{
			name: "basic",
			in: input{
				Pattern: "foo",
			},
			wantLen: 6, // rg --json -e foo .
		},
		{
			name: "case sensitive",
			in: input{
				Pattern:         "foo",
				CaseInsensitive: false,
			},
			wantLen: 7, // rg --json -s -e foo .
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildRipgrepArgs(tt.in, ".")
			if len(args) < 2 {
				t.Errorf("buildRipgrepArgs() returned too few args")
			}
		})
	}
}

func TestParseRipgrepOutput(t *testing.T) {
	// Mock rg --json output
	match := map[string]any{
		"type": "match",
		"data": map[string]any{
			"path":        map[string]any{"text": "file.txt"},
			"lines":       map[string]any{"text": "hello world\n"},
			"line_number": 1,
			"submatches": []any{
				map[string]any{"match": map[string]any{"text": "hello"}},
			},
		},
	}

	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(match); err != nil {
		t.Fatalf("encode match: %v", err)
	}

	results, _, err := parseRipgrepOutput(buf.Bytes(), "/tmp", 10)
	if err != nil {
		t.Fatalf("parseRipgrepOutput: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].File != "file.txt" {
		t.Errorf("expected file.txt, got %s", results[0].File)
	}
}
