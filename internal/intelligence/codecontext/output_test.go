package codecontext

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestPrepareOutputWithArtifact_ReturnsInlineWhenSmall(t *testing.T) {
	evidence := &Evidence{
		Query: "example",
		Snippets: []Snippet{
			{File: "main.go", StartLine: 1, EndLine: 5, Text: "short snippet"},
		},
		Stats: EvidenceStats{FilesProcessed: 1},
	}

	payload, artifact, err := PrepareOutputWithArtifact(evidence, 1, 32, func(e *Evidence) ([]byte, error) {
		return []byte("unexpected"), nil
	})
	if err != nil {
		t.Fatalf("PrepareOutputWithArtifact failed: %v", err)
	}
	if artifact != nil {
		t.Fatalf("expected no artifact payload, got %+v", artifact)
	}
	if payload.SnippetsInline == nil {
		t.Fatal("expected inline snippets")
	}
	if len(payload.SnippetsInline) != 1 {
		t.Fatalf("len(snippets_inline) = %d, want 1", len(payload.SnippetsInline))
	}
	if payload.SnippetsInline[0].Preview != "short snippet" {
		t.Fatalf("preview = %q, want short snippet", payload.SnippetsInline[0].Preview)
	}
}

func TestPrepareOutputWithArtifact_ReturnsArtifactWhenLarge(t *testing.T) {
	evidence := &Evidence{
		Query: "example",
		Snippets: []Snippet{
			{File: "main.go", StartLine: 1, EndLine: 1, Text: string(make([]byte, 2000))},
		},
		Stats: EvidenceStats{FilesProcessed: 1},
	}

	persisted := []byte(`{"snippet":"payload"}`)
	payload, artifact, err := PrepareOutputWithArtifact(evidence, 1, 12, func(e *Evidence) ([]byte, error) {
		return persisted, nil
	})
	if err != nil {
		t.Fatalf("PrepareOutputWithArtifact failed: %v", err)
	}
	if artifact == nil {
		t.Fatalf("expected artifact payload")
		return
	}
	if string(artifact.Data) != string(persisted) {
		t.Fatalf("artifact data = %q, want %q", artifact.Data, persisted)
	}
	if payload.SnippetsInline != nil {
		t.Fatalf("expected no inline snippets, got %#v", payload.SnippetsInline)
	}
	if payload.Artifact != nil {
		t.Fatal("expected artifact metadata to be filled by caller")
	}
	if artifact.Count != 1 {
		t.Fatalf("artifact.Count = %d, want 1", artifact.Count)
	}
	if artifact.Kind != "application/x-ndjson" {
		t.Fatalf("artifact.Kind = %q, want application/x-ndjson", artifact.Kind)
	}
}

func TestPrepareOutputWithArtifact_PropagatesRenderError(t *testing.T) {
	failErr := errors.New("render failed")
	evidence := &Evidence{Snippets: []Snippet{{Text: string(make([]byte, 2000))}}}

	_, _, err := PrepareOutputWithArtifact(evidence, 1, 12, func(e *Evidence) ([]byte, error) {
		return nil, failErr
	})
	if err == nil {
		t.Fatal("expected render error")
	}
}

func TestPrepareOutputWithArtifact_HonorsRenderDefault(t *testing.T) {
	evidence := &Evidence{
		Query: "example",
		Snippets: []Snippet{
			{File: "main.go", StartLine: 1, EndLine: 1, Text: string(make([]byte, 2000))},
		},
	}

	payload, artifact, err := PrepareOutputWithArtifact(evidence, 1, 12, nil)
	if err != nil {
		t.Fatalf("PrepareOutputWithArtifact failed: %v", err)
	}
	if artifact == nil {
		t.Fatalf("expected artifact with default render")
		return
	}
	if len(artifact.Data) == 0 {
		t.Fatal("expected non-empty artifact data")
	}
	if _, e := json.Marshal(payload); e != nil {
		t.Fatalf("payload should marshal: %v", e)
	}
}
