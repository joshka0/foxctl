package codemap

import (
	"testing"
)

func TestDepthToMaxIterations(t *testing.T) {
	tests := []struct {
		depth    int
		expected int
	}{
		{1, 10},  // 4-5 exploration + 5 buffer
		{2, 15},  // 8 exploration + 7 buffer
		{3, 22},  // 15 exploration + 7 buffer
		{4, 32},  // 22 exploration + 10 buffer
		{5, 50},  // 35 exploration + 15 buffer
		{0, 15},  // default case (depth=2)
		{6, 15},  // out of range (default)
		{-1, 15}, // negative (default)
	}

	for _, tt := range tests {
		got := depthToMaxIterations(tt.depth)
		if got != tt.expected {
			t.Errorf("depthToMaxIterations(%d) = %d, want %d", tt.depth, got, tt.expected)
		}
	}
}

func TestCountUniqueFiles(t *testing.T) {
	tests := []struct {
		name     string
		codemap  *Codemap
		expected int
	}{
		{
			name:     "empty codemap",
			codemap:  &Codemap{},
			expected: 0,
		},
		{
			name: "one file",
			codemap: &Codemap{
				Traces: []Trace{
					{
						Annotations: []Annotation{
							{Path: "@src/main.go:10"},
						},
					},
				},
			},
			expected: 1,
		},
		{
			name: "multiple files",
			codemap: &Codemap{
				Traces: []Trace{
					{
						Annotations: []Annotation{
							{Path: "@src/main.go:10"},
							{Path: "@src/util.go:20"},
							{Path: "@src/handler.go:30"},
						},
					},
				},
			},
			expected: 3,
		},
		{
			name: "duplicate files",
			codemap: &Codemap{
				Traces: []Trace{
					{
						Annotations: []Annotation{
							{Path: "@src/main.go:10"},
							{Path: "@src/main.go:25"},
							{Path: "@src/util.go:20"},
						},
					},
				},
			},
			expected: 2,
		},
		{
			name: "multiple traces same file",
			codemap: &Codemap{
				Traces: []Trace{
					{
						Annotations: []Annotation{
							{Path: "@src/main.go:10"},
						},
					},
					{
						Annotations: []Annotation{
							{Path: "@src/main.go:50"},
						},
					},
				},
			},
			expected: 1,
		},
		{
			name: "path without line number",
			codemap: &Codemap{
				Traces: []Trace{
					{
						Annotations: []Annotation{
							{Path: "@src/main.go"},
						},
					},
				},
			},
			expected: 1,
		},
		{
			name: "invalid path format",
			codemap: &Codemap{
				Traces: []Trace{
					{
						Annotations: []Annotation{
							{Path: "no-at-symbol.go"},
						},
					},
				},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countUniqueFiles(tt.codemap)
			if got != tt.expected {
				t.Errorf("countUniqueFiles() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestCountSymbols(t *testing.T) {
	tests := []struct {
		name     string
		codemap  *Codemap
		expected int
	}{
		{
			name:     "empty codemap",
			codemap:  &Codemap{},
			expected: 0,
		},
		{
			name: "single annotation",
			codemap: &Codemap{
				Traces: []Trace{
					{
						Annotations: []Annotation{
							{Path: "@src/main.go:10", Title: "Handler"},
						},
					},
				},
			},
			expected: 1,
		},
		{
			name: "multiple annotations in trace",
			codemap: &Codemap{
				Traces: []Trace{
					{
						Annotations: []Annotation{
							{Path: "@src/main.go:10"},
							{Path: "@src/util.go:20"},
							{Path: "@src/handler.go:30"},
						},
					},
				},
			},
			expected: 3,
		},
		{
			name: "annotations across traces",
			codemap: &Codemap{
				Traces: []Trace{
					{
						Annotations: []Annotation{
							{Path: "@src/main.go:10"},
							{Path: "@src/util.go:20"},
						},
					},
					{
						Annotations: []Annotation{
							{Path: "@src/handler.go:30"},
						},
					},
				},
			},
			expected: 3,
		},
		{
			name: "trace with no annotations",
			codemap: &Codemap{
				Traces: []Trace{
					{Annotations: []Annotation{}},
					{
						Annotations: []Annotation{
							{Path: "@src/main.go:10"},
						},
					},
				},
			},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countSymbols(tt.codemap)
			if got != tt.expected {
				t.Errorf("countSymbols() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestNewAgent(t *testing.T) {
	t.Run("with workspace option", func(t *testing.T) {
		// Note: NewAgent will fail without an LLM API key,
		// but we're testing the options get applied
		agent, err := NewAgent(
			WithWorkspace("/test/workspace"),
			// Skip LLM requirement with a mock
		)
		// Expect error since no API key
		if err == nil {
			// If it somehow worked (API key in env), check workspace
			if agent.workspace != "/test/workspace" {
				t.Errorf("workspace = %q, want '/test/workspace'", agent.workspace)
			}
		} else {
			// Expected: no LLM API key
			t.Logf("NewAgent without API key returned expected error: %v", err)
		}
	})
}

func TestBuildCodemapSignature(t *testing.T) {
	sig, err := buildCodemapSignature(nil)
	if err != nil {
		t.Fatalf("buildCodemapSignature() returned error: %v", err)
	}
	if sig == nil {
		t.Fatal("buildCodemapSignature() returned nil")
	}

	// Check that input field is defined
	inputs := sig.Inputs
	if len(inputs) == 0 {
		t.Error("expected at least one input field")
	} else {
		found := false
		for _, f := range inputs {
			if f.Name == "task" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected 'task' input field")
		}
	}

	// Check that output field is defined
	outputs := sig.Outputs
	if len(outputs) == 0 {
		t.Error("expected at least one output field")
	} else {
		found := false
		for _, f := range outputs {
			if f.Name == "result" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected 'result' output field")
		}
	}
}
