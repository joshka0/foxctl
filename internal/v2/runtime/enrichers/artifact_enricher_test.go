package enrichers_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/adapters/libsql/turns"
	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
	"github.com/jkatigb/agentctl/internal/v2/runtime/enrichers"
)

func TestArtifactEnricher_RequiresArtifactWriter(t *testing.T) {
	t.Parallel()

	enricher := enrichers.NewArtifactEnricher(enrichers.ArtifactEnricherConfig{})
	job := enrichers.NewJob(run.TurnRecord{
		ID:        "turn-no-writer",
		SessionID: "run-no-writer",
		TurnIndex: 1,
	}, turns.ArtifactTypeAnnotation, "v1")

	err := enricher.Enrich(context.Background(), job)
	if !errors.Is(err, enrichers.ErrMissingArtifactWriter) {
		t.Fatalf("Enrich() error = %v, want ErrMissingArtifactWriter", err)
	}
}

func TestArtifactEnricher_SavesRequestedAnnotation(t *testing.T) {
	t.Parallel()

	writer := &captureArtifactWriter{}
	now := time.Date(2026, time.February, 27, 12, 0, 0, 0, time.UTC)
	enricher := enrichers.NewArtifactEnricher(enrichers.ArtifactEnricherConfig{
		ArtifactWriter:   writer,
		IncludeEmbedding: false,
		Provider:         sourceimport.ProviderCodex,
		Now:              func() time.Time { return now },
	})
	job := enrichers.NewJob(run.TurnRecord{
		ID:        "turn-annotation-1",
		SessionID: "run-annotation-1",
		TurnIndex: 3,
		Prompt:    "review context builder salience weighting",
		FinalOutput: run.MessageRef{
			ID:   "msg-final-1",
			Role: "assistant",
			Text: "Updated context weighting and added tests.",
		},
	}, turns.ArtifactTypeAnnotation, "v1")

	if err := enricher.Enrich(context.Background(), job); err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if len(writer.artifacts) != 1 {
		t.Fatalf("saved artifacts=%d want 1", len(writer.artifacts))
	}

	artifact := writer.artifacts[0]
	if artifact.TurnID != "turn-annotation-1" {
		t.Fatalf("artifact turn_id=%q want turn-annotation-1", artifact.TurnID)
	}
	if artifact.ArtifactType != turns.ArtifactTypeAnnotation {
		t.Fatalf("artifact type=%q want %q", artifact.ArtifactType, turns.ArtifactTypeAnnotation)
	}
	if artifact.ArtifactVersion != "v1" {
		t.Fatalf("artifact version=%q want v1", artifact.ArtifactVersion)
	}
	if strings.TrimSpace(artifact.Ref) == "" {
		t.Fatal("artifact ref is empty")
	}
	if strings.TrimSpace(artifact.Summary) == "" {
		t.Fatal("artifact summary is empty")
	}
	var metadata map[string]any
	if err := json.Unmarshal(artifact.MetadataJSON, &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if got := strings.TrimSpace(toString(metadata["artifact_from"])); got != "runtime" {
		t.Fatalf("metadata.artifact_from=%q want runtime", got)
	}
}

func TestArtifactEnricher_EmbeddingDisabledRejectsEmbeddingJobs(t *testing.T) {
	t.Parallel()

	writer := &captureArtifactWriter{}
	enricher := enrichers.NewArtifactEnricher(enrichers.ArtifactEnricherConfig{
		ArtifactWriter:   writer,
		IncludeEmbedding: false,
		Provider:         sourceimport.ProviderClaude,
	})
	job := enrichers.NewJob(run.TurnRecord{
		ID:        "turn-embed-disabled",
		SessionID: "run-embed-disabled",
		TurnIndex: 1,
		Prompt:    "build embedding artifact",
	}, turns.ArtifactTypeEmbedding, "v1")

	err := enricher.Enrich(context.Background(), job)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "disabled") {
		t.Fatalf("Enrich() error = %v, want embedding disabled error", err)
	}
	if len(writer.artifacts) != 0 {
		t.Fatalf("saved artifacts=%d want 0", len(writer.artifacts))
	}
}

func TestArtifactEnricher_EmbeddingEnabledSavesEmbedding(t *testing.T) {
	t.Parallel()

	writer := &captureArtifactWriter{}
	enricher := enrichers.NewArtifactEnricher(enrichers.ArtifactEnricherConfig{
		ArtifactWriter:   writer,
		IncludeEmbedding: true,
		Embedder:         sourceimport.NewHashEmbedder(24),
		Provider:         sourceimport.ProviderClaude,
	})
	job := enrichers.NewJob(run.TurnRecord{
		ID:        "turn-embed-enabled",
		SessionID: "run-embed-enabled",
		TurnIndex: 8,
		Prompt:    "synthesize embeddings for retrieval",
	}, turns.ArtifactTypeEmbedding, "v1")

	if err := enricher.Enrich(context.Background(), job); err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if len(writer.artifacts) != 1 {
		t.Fatalf("saved artifacts=%d want 1", len(writer.artifacts))
	}

	artifact := writer.artifacts[0]
	if artifact.ArtifactType != turns.ArtifactTypeEmbedding {
		t.Fatalf("artifact type=%q want %q", artifact.ArtifactType, turns.ArtifactTypeEmbedding)
	}
	if len(artifact.Embedding) != 24 {
		t.Fatalf("embedding dims=%d want 24", len(artifact.Embedding))
	}
	if strings.TrimSpace(artifact.EmbeddingModel) == "" {
		t.Fatal("embedding model is empty")
	}
}

func TestArtifactEnricher_EmbeddingWarningsReturnedWhenNotProduced(t *testing.T) {
	t.Parallel()

	writer := &captureArtifactWriter{}
	enricher := enrichers.NewArtifactEnricher(enrichers.ArtifactEnricherConfig{
		ArtifactWriter:   writer,
		IncludeEmbedding: true,
		Embedder: failingEmbedder{
			err: errors.New("embed endpoint unavailable"),
		},
		Provider: sourceimport.ProviderCodex,
	})
	job := enrichers.NewJob(run.TurnRecord{
		ID:        "turn-embed-fail",
		SessionID: "run-embed-fail",
		TurnIndex: 2,
		Prompt:    "embedding should fail",
	}, turns.ArtifactTypeEmbedding, "v1")

	err := enricher.Enrich(context.Background(), job)
	if err == nil {
		t.Fatal("Enrich() error=nil want non-nil")
	}
	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "not produced") || !strings.Contains(errText, "embed endpoint unavailable") {
		t.Fatalf("Enrich() error=%q want warning with embed failure", err.Error())
	}
	if len(writer.artifacts) != 0 {
		t.Fatalf("saved artifacts=%d want 0", len(writer.artifacts))
	}
}

func TestArtifactEnricher_WarningsReportedWhenArtifactSaveSucceeds(t *testing.T) {
	t.Parallel()

	writer := &captureArtifactWriter{}
	var warningJobType string
	var warnings []string
	enricher := enrichers.NewArtifactEnricher(enrichers.ArtifactEnricherConfig{
		ArtifactWriter:   writer,
		IncludeEmbedding: true,
		Embedder: failingEmbedder{
			err: errors.New("embed endpoint unavailable"),
		},
		Provider: sourceimport.ProviderCodex,
		OnWarnings: func(job enrichers.Job, got []string) {
			warningJobType = job.ArtifactType
			warnings = append([]string(nil), got...)
		},
	})
	job := enrichers.NewJob(run.TurnRecord{
		ID:        "turn-warn-success",
		SessionID: "run-warn-success",
		TurnIndex: 4,
		Prompt:    "save annotation even when embedding fails",
		FinalOutput: run.MessageRef{
			ID:   "msg-warn-success",
			Role: "assistant",
			Text: "annotation still available",
		},
	}, turns.ArtifactTypeAnnotation, "v1")

	if err := enricher.Enrich(context.Background(), job); err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if len(writer.artifacts) != 1 {
		t.Fatalf("saved artifacts=%d want 1", len(writer.artifacts))
	}
	if warningJobType != turns.ArtifactTypeAnnotation {
		t.Fatalf("warning job artifact_type=%q want %q", warningJobType, turns.ArtifactTypeAnnotation)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warnings callback to receive at least one warning")
	}
	allWarnings := strings.ToLower(strings.Join(warnings, " | "))
	if !strings.Contains(allWarnings, "embed endpoint unavailable") {
		t.Fatalf("warnings=%q missing embed failure detail", allWarnings)
	}
}

func TestArtifactEnricher_DefaultProviderIsAuto(t *testing.T) {
	t.Parallel()

	writer := &captureArtifactWriter{}
	enricher := enrichers.NewArtifactEnricher(enrichers.ArtifactEnricherConfig{
		ArtifactWriter: writer,
		// Provider intentionally omitted.
	})
	job := enrichers.NewJob(run.TurnRecord{
		ID:        "turn-provider-auto",
		SessionID: "run-provider-auto",
		TurnIndex: 1,
		Prompt:    "classify this turn",
	}, turns.ArtifactTypeClassification, "v1")

	if err := enricher.Enrich(context.Background(), job); err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if len(writer.artifacts) != 1 {
		t.Fatalf("saved artifacts=%d want 1", len(writer.artifacts))
	}
	artifact := writer.artifacts[0]
	var content map[string]any
	if err := json.Unmarshal(artifact.ContentJSON, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if got := strings.TrimSpace(toString(content["provider"])); got != string(sourceimport.ProviderAuto) {
		t.Fatalf("classification provider=%q want %q", got, sourceimport.ProviderAuto)
	}
}

type captureArtifactWriter struct {
	artifacts []turns.Artifact
}

func (w *captureArtifactWriter) SaveArtifact(_ context.Context, artifact turns.Artifact) error {
	w.artifacts = append(w.artifacts, artifact)
	return nil
}

type failingEmbedder struct {
	err error
}

func (e failingEmbedder) Embed(context.Context, string) (sourceimport.EmbeddingResult, error) {
	return sourceimport.EmbeddingResult{}, e.err
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}
