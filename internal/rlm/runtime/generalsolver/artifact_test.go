package generalsolver

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseWorkArtifact(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    WorkArtifact
		wantErr bool
	}{
		{
			name: "solved artifact",
			input: `{
				"work_item_id": "n1",
				"status": "solved",
				"answer": "solution = 42",
				"confidence": 0.95,
				"checks": ["type check passed"]
			}`,
			want: WorkArtifact{
				WorkItemID: "n1",
				Status:     "solved",
				Answer:     "solution = 42",
				Confidence: 0.95,
				Checks:     []string{"type check passed"},
			},
		},
		{
			name: "partial artifact",
			input: `{
				"work_item_id": "n2",
				"status": "partial",
				"answer": "partial answer",
				"derived": {"step": 3},
				"confidence": 0.5
			}`,
			want: WorkArtifact{
				WorkItemID: "n2",
				Status:     "partial",
				Answer:     "partial answer",
				Derived:    map[string]any{"step": float64(3)},
				Confidence: 0.5,
			},
		},
		{
			name: "blocked artifact",
			input: `{
				"work_item_id": "n3",
				"status": "blocked",
				"confidence": 0.0
			}`,
			want: WorkArtifact{
				WorkItemID: "n3",
				Status:     "blocked",
				Confidence: 0.0,
			},
		},
		{
			name: "failed artifact with counterexamples",
			input: `{
				"work_item_id": "n4",
				"status": "failed",
				"counterexamples": [
					{"input": 5, "expected": 10, "got": 9}
				],
				"confidence": 0.0
			}`,
			want: WorkArtifact{
				WorkItemID: "n4",
				Status:     "failed",
				Counterexamples: []map[string]any{
					{"input": float64(5), "expected": float64(10), "got": float64(9)},
				},
				Confidence: 0.0,
			},
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "malformed JSON",
			input:   `{broken`,
			wantErr: true,
		},
		{
			name: "artifact with code",
			input: `{
				"work_item_id": "n5",
				"status": "solved",
				"answer": "42",
				"code": "def solve(): return 42",
				"confidence": 0.9
			}`,
			want: WorkArtifact{
				WorkItemID: "n5",
				Status:     "solved",
				Answer:     "42",
				Code:       "def solve(): return 42",
				Confidence: 0.9,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseWorkArtifact([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.WorkItemID != tt.want.WorkItemID {
				t.Errorf("work_item_id: got %q, want %q", got.WorkItemID, tt.want.WorkItemID)
			}
			if got.Status != tt.want.Status {
				t.Errorf("status: got %q, want %q", got.Status, tt.want.Status)
			}
			if got.Code != tt.want.Code {
				t.Errorf("code: got %q, want %q", got.Code, tt.want.Code)
			}
			if len(got.Checks) != len(tt.want.Checks) {
				t.Errorf("checks: got %d, want %d", len(got.Checks), len(tt.want.Checks))
			}
			if len(got.Counterexamples) != len(tt.want.Counterexamples) {
				t.Errorf("counterexamples: got %d, want %d", len(got.Counterexamples), len(tt.want.Counterexamples))
			}
		})
	}
}

func TestParseWorkArtifactFromText(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantID  string
		wantErr bool
	}{
		{
			name:   "pure JSON",
			input:  `{"work_item_id":"n1","status":"solved","answer":"42","confidence":0.9}`,
			wantID: "n1",
		},
		{
			name:   "JSON embedded in text",
			input:  `Here is the result:\n{"work_item_id":"n2","status":"solved","answer":"7","confidence":0.8}\nDone.`,
			wantID: "n2",
		},
		{
			name:    "no JSON in text",
			input:   "no json here",
			wantErr: true,
		},
		{
			name:    "empty text",
			input:   "",
			wantErr: true,
		},
		{
			name:   "JSON with surrounding prose",
			input:  "The solver produced:\n{\"work_item_id\":\"n3\",\"status\":\"partial\",\"confidence\":0.4}\nThat's the result.",
			wantID: "n3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseWorkArtifactFromText(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.WorkItemID != tt.wantID {
				t.Errorf("work_item_id: got %q, want %q", got.WorkItemID, tt.wantID)
			}
		})
	}
}

func TestValidateWorkArtifact(t *testing.T) {
	tests := []struct {
		name        string
		artifact    WorkArtifact
		wantErr     bool
		errContains string
	}{
		{
			name: "valid solved",
			artifact: WorkArtifact{
				WorkItemID: "n1",
				Status:     ArtifactStatusSolved,
				Answer:     "42",
				Confidence: 0.9,
			},
		},
		{
			name: "valid partial",
			artifact: WorkArtifact{
				WorkItemID: "n2",
				Status:     ArtifactStatusPartial,
				Confidence: 0.5,
			},
		},
		{
			name: "valid blocked",
			artifact: WorkArtifact{
				WorkItemID: "n3",
				Status:     ArtifactStatusBlocked,
			},
		},
		{
			name: "valid failed",
			artifact: WorkArtifact{
				WorkItemID: "n4",
				Status:     ArtifactStatusFailed,
			},
		},
		{
			name: "empty work_item_id",
			artifact: WorkArtifact{
				WorkItemID: "",
				Status:     ArtifactStatusSolved,
				Answer:     "42",
				Confidence: 0.9,
			},
			wantErr:     true,
			errContains: "work_item_id",
		},
		{
			name: "invalid status",
			artifact: WorkArtifact{
				WorkItemID: "n1",
				Status:     "unknown",
				Answer:     "42",
			},
			wantErr:     true,
			errContains: "not valid",
		},
		{
			name: "solved without answer",
			artifact: WorkArtifact{
				WorkItemID: "n1",
				Status:     ArtifactStatusSolved,
				Answer:     nil,
				Confidence: 0.9,
			},
			wantErr:     true,
			errContains: "answer is nil",
		},
		{
			name: "solved with zero confidence",
			artifact: WorkArtifact{
				WorkItemID: "n1",
				Status:     ArtifactStatusSolved,
				Answer:     "42",
				Confidence: 0.0,
			},
			wantErr:     true,
			errContains: "confidence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkArtifact(tt.artifact)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestNormalizeCounterexamples(t *testing.T) {
	artifact := &WorkArtifact{
		WorkItemID: "n1",
		Status:     "failed",
		Counterexamples: []map[string]any{
			{"input": 5},
			nil,
		},
	}
	NormalizeCounterexamples(artifact)
	for _, ce := range artifact.Counterexamples {
		if ce == nil {
			t.Error("counterexample should not be nil after normalization")
		}
	}
	if len(artifact.Counterexamples) != 2 {
		t.Errorf("expected 2 counterexamples, got %d", len(artifact.Counterexamples))
	}
}

func TestNormalizeCounterexamplesNilArtifact(t *testing.T) {
	NormalizeCounterexamples(nil)
}

func TestParseWorkArtifactRoundTrip(t *testing.T) {
	original := WorkArtifact{
		WorkItemID: "n1",
		Status:     ArtifactStatusSolved,
		Answer:     "solution = 42",
		Code:       "def solve(): return 42",
		Derived:    map[string]any{"steps": float64(3)},
		Checks:     []string{"format ok", "value ok"},
		Counterexamples: []map[string]any{
			{"input": float64(5), "expected": float64(10)},
		},
		Confidence: 0.95,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	parsed, err := ParseWorkArtifact(data)
	if err != nil {
		t.Fatalf("ParseWorkArtifact: %v", err)
	}
	if parsed.WorkItemID != original.WorkItemID {
		t.Errorf("work_item_id mismatch: %q vs %q", parsed.WorkItemID, original.WorkItemID)
	}
	if parsed.Status != original.Status {
		t.Errorf("status mismatch: %q vs %q", parsed.Status, original.Status)
	}
	if len(parsed.Checks) != len(original.Checks) {
		t.Errorf("checks count mismatch: %d vs %d", len(parsed.Checks), len(original.Checks))
	}
	if len(parsed.Counterexamples) != len(original.Counterexamples) {
		t.Errorf("counterexamples count mismatch: %d vs %d", len(parsed.Counterexamples), len(original.Counterexamples))
	}
}
