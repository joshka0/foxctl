package adapters

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	rlmruntime "github.com/joshka0/foxctl/internal/rlm/runtime"
)

func TestConvertNodeResult(t *testing.T) {
	now := time.Now().UTC()
	src := rlmruntime.NodeResult{
		Status:  rlmruntime.NodeStatusCompleted,
		Summary: "Found architecture docs",
		Answer:  "The system uses events",
		Findings: []rlmruntime.Finding{
			{
				ID:      "f1",
				Summary: "Event-sourced design",
				EvidenceRefs: []rlmruntime.EvidenceRef{
					{Kind: "path", Ref: "docs/arch.md", Title: "Architecture"},
				},
			},
		},
		EvidenceRefs: []rlmruntime.EvidenceRef{
			{Kind: "path", Ref: "internal/engine.go", Title: "Engine"},
			{Kind: "symbol", Ref: "Run()", Title: "Run func"},
		},
		ArtifactRefs: []rlmruntime.ArtifactRef{
			{ID: "art1", URI: "cas://abc", MediaType: "text/plain", Summary: "Output"},
		},
		StartedAt:   now,
		CompletedAt: now.Add(5 * time.Second),
	}

	got := ConvertNodeResult("ws1", "architecture", src)

	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.Query != "architecture" {
		t.Errorf("Query = %q", got.Query)
	}
	if got.Lane != contextengine.LaneMixed {
		t.Errorf("Lane = %q", got.Lane)
	}
	// Should have findings + evidence refs as nodes
	// 1 finding + 2 evidence refs = 3 nodes
	if len(got.Nodes) != 3 {
		t.Fatalf("Nodes = %d, want 3", len(got.Nodes))
	}
	if got.Telemetry.DurationMs != 5000 {
		t.Errorf("Telemetry.DurationMs = %d", got.Telemetry.DurationMs)
	}
	if got.Metadata["summary"] != "Found architecture docs" {
		t.Errorf("Metadata[summary] = %v", got.Metadata["summary"])
	}
	if got.Metadata["answer"] != "The system uses events" {
		t.Errorf("Metadata[answer] = %v", got.Metadata["answer"])
	}
}

func TestConvertNodeResult_Empty(t *testing.T) {
	now := time.Now().UTC()
	src := rlmruntime.NodeResult{
		Status:      rlmruntime.NodeStatusCompleted,
		Summary:     "No findings",
		StartedAt:   now,
		CompletedAt: now,
	}

	got := ConvertNodeResult("ws1", "test query", src)

	if len(got.Nodes) != 0 {
		t.Errorf("Nodes = %d, want 0 for empty result", len(got.Nodes))
	}
}

func TestConvertFinding(t *testing.T) {
	src := rlmruntime.Finding{
		ID:      "f1",
		Summary: "Key finding",
		EvidenceRefs: []rlmruntime.EvidenceRef{
			{Kind: "path", Ref: "main.go"},
		},
	}

	got := ConvertFinding("ws1", 0, src)

	if got.NodeType != contextengine.EvidenceNodeTypeContext {
		t.Errorf("NodeType = %q", got.NodeType)
	}
	if got.Statement != "Key finding" {
		t.Errorf("Statement = %q", got.Statement)
	}
	if got.Ref.Type != contextengine.RefTypePath || got.Ref.Ref != "main.go" {
		t.Errorf("Ref = %v", got.Ref)
	}
}

func TestConvertFinding_NoEvidenceRefs(t *testing.T) {
	src := rlmruntime.Finding{
		Summary: "Finding without refs",
	}
	got := ConvertFinding("ws1", 5, src)
	if got.Ref.Type != contextengine.RefTypeRun {
		t.Errorf("Ref.Type = %q, want run for no evidence", got.Ref.Type)
	}
}

func TestConvertRLMEvidenceRef(t *testing.T) {
	tests := []struct {
		name string
		src  rlmruntime.EvidenceRef
		want contextengine.EvidenceRef
	}{
		{
			"path kind",
			rlmruntime.EvidenceRef{Kind: "path", Ref: "foo.go", Title: "Foo"},
			contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "foo.go", Title: "Foo"},
		},
		{
			"symbol kind",
			rlmruntime.EvidenceRef{Kind: "symbol", Ref: "Run"},
			contextengine.EvidenceRef{Type: contextengine.RefTypeSymbol, Ref: "Run"},
		},
		{
			"task kind",
			rlmruntime.EvidenceRef{Kind: "task", Ref: "t1"},
			contextengine.EvidenceRef{Type: contextengine.RefTypeTask, Ref: "t1"},
		},
		{
			"unknown kind defaults to path",
			rlmruntime.EvidenceRef{Kind: "custom", Ref: "something"},
			contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "something"},
		},
		{
			"empty kind defaults to path",
			rlmruntime.EvidenceRef{Ref: "something"},
			contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "something"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertRLMEvidenceRef(tt.src)
			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if got.Ref != tt.want.Ref {
				t.Errorf("Ref = %q, want %q", got.Ref, tt.want.Ref)
			}
			if got.Title != tt.want.Title {
				t.Errorf("Title = %q, want %q", got.Title, tt.want.Title)
			}
		})
	}
}

func TestConvertRLMArtifactRef(t *testing.T) {
	src := rlmruntime.ArtifactRef{
		ID:        "art1",
		URI:       "cas://sha256:abc",
		MediaType: "text/plain",
		Summary:   "Test output",
	}

	got := ConvertRLMArtifactRef(src)

	if got.Type != contextengine.RefTypeArtifact {
		t.Errorf("Type = %q", got.Type)
	}
	if got.Ref != "cas://sha256:abc" {
		t.Errorf("Ref = %q", got.Ref)
	}
	if got.Title != "Test output [text/plain]" {
		t.Errorf("Title = %q", got.Title)
	}
}

func TestMapRLMRefKind(t *testing.T) {
	tests := []struct {
		kind string
		want contextengine.RefType
	}{
		{"path", contextengine.RefTypePath},
		{"file", contextengine.RefTypePath},
		{"symbol", contextengine.RefTypeSymbol},
		{"function", contextengine.RefTypeSymbol},
		{"method", contextengine.RefTypeSymbol},
		{"class", contextengine.RefTypeSymbol},
		{"task", contextengine.RefTypeTask},
		{"session", contextengine.RefTypeSession},
		{"note", contextengine.RefTypeNote},
		{"document", contextengine.RefTypeNote},
		{"artifact", contextengine.RefTypeNote},
		{"commit", contextengine.RefTypeCommit},
		{"event", contextengine.RefTypeEvent},
		{"run", contextengine.RefTypeRun},
		{"tool_call", contextengine.RefTypeToolCall},
		{"unknown_kind", contextengine.RefTypePath},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got := mapRLMRefKind(tt.kind)
			if got != tt.want {
				t.Errorf("mapRLMRefKind(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestConvertNodeResult_WithError(t *testing.T) {
	now := time.Now().UTC()
	src := rlmruntime.NodeResult{
		Status:       rlmruntime.NodeStatusFailed,
		ErrorCode:    "ETIMEOUT",
		ErrorMessage: "timed out",
		StartedAt:    now,
		CompletedAt:  now.Add(30 * time.Second),
	}

	got := ConvertNodeResult("ws1", "test", src)

	if got.Metadata["error_code"] != "ETIMEOUT" {
		t.Errorf("Metadata[error_code] = %v", got.Metadata["error_code"])
	}
	if got.Metadata["error_message"] != "timed out" {
		t.Errorf("Metadata[error_message] = %v", got.Metadata["error_message"])
	}
}
